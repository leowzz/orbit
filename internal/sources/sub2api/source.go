package sub2api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultTimeout  = 10 * time.Second
	maxResponseSize = 8 * 1024
)

// Config contains the Sub2API endpoints and credentials used by an Agent.
type Config struct {
	LoginEndpoint   string
	RefreshEndpoint string
	UsageEndpoint   string
	Email           string
	Password        string
	Timeout         time.Duration
	HTTPClient      *http.Client
}

// Usage is the current Sub2API usage reading. Cost is stored in millionths of
// the API's currency unit so callers never need floating-point arithmetic.
type Usage struct {
	TodayActualCostMicros int64
	TodayTokens           uint64
	TPM                   uint64
}

type ErrorKind string

const (
	ErrorInvalidConfig   ErrorKind = "invalid_config"
	ErrorTimeout         ErrorKind = "timeout"
	ErrorTransport       ErrorKind = "transport"
	ErrorUnauthorized    ErrorKind = "unauthorized"
	ErrorRateLimited     ErrorKind = "rate_limited"
	ErrorHTTP            ErrorKind = "http"
	ErrorOversized       ErrorKind = "oversized_response"
	ErrorAPI             ErrorKind = "api"
	ErrorInvalidResponse ErrorKind = "invalid_response"
)

// Error describes a failure without exposing request credentials or tokens.
type Error struct {
	Kind       ErrorKind
	Operation  string
	StatusCode int
	RetryAfter time.Duration
	APICode    int64
	APIMessage string
	Cause      error
}

func (e *Error) Error() string {
	message := fmt.Sprintf("sub2api %s: %s", e.Operation, e.Kind)
	if e.StatusCode != 0 {
		message += fmt.Sprintf(" (HTTP %d)", e.StatusCode)
	}
	if e.Kind == ErrorAPI {
		message += fmt.Sprintf(" (API code %d: %s)", e.APICode, e.APIMessage)
	}
	if e.Cause != nil {
		message += ": " + e.Cause.Error()
	}
	return message
}

func (e *Error) Unwrap() error { return e.Cause }

type tokenPair struct {
	AccessToken  string
	RefreshToken string
}

// Source fetches Sub2API usage and keeps authentication tokens in memory only.
type Source struct {
	config Config
	client *http.Client

	mu     sync.Mutex
	tokens tokenPair
}

func New(config Config) (*Source, error) {
	return newSource(config, false)
}

func newSource(config Config, allowHTTP bool) (*Source, error) {
	for name, endpoint := range map[string]string{
		"login_endpoint":   config.LoginEndpoint,
		"refresh_endpoint": config.RefreshEndpoint,
		"usage_endpoint":   config.UsageEndpoint,
	} {
		parsed, err := url.ParseRequestURI(endpoint)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && !(allowHTTP && parsed.Scheme == "http")) {
			return nil, &Error{Kind: ErrorInvalidConfig, Operation: "configure", Cause: fmt.Errorf("%s must be an absolute HTTPS URL", name)}
		}
	}
	if config.Email == "" || config.Password == "" {
		return nil, &Error{Kind: ErrorInvalidConfig, Operation: "configure", Cause: errors.New("email and password are required")}
	}
	if config.Timeout < 0 {
		return nil, &Error{Kind: ErrorInvalidConfig, Operation: "configure", Cause: errors.New("timeout cannot be negative")}
	}
	if config.Timeout == 0 {
		config.Timeout = defaultTimeout
	}
	client := config.HTTPClient
	if client == nil {
		client = &http.Client{}
	}
	clientCopy := *client
	clientCopy.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &Source{config: config, client: &clientCopy}, nil
}

// FetchUsage returns a usage reading. A 401 response triggers at most one
// refresh/login recovery sequence and one final usage request.
func (s *Source) FetchUsage(ctx context.Context) (Usage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.tokens.AccessToken == "" {
		tokens, err := s.login(ctx)
		if err != nil {
			return Usage{}, err
		}
		s.tokens = tokens
	}

	usage, err := s.fetch(ctx, s.tokens.AccessToken)
	if err == nil || !isKind(err, ErrorUnauthorized) {
		return usage, err
	}

	tokens, recoveryErr := s.recoverSession(ctx)
	if recoveryErr != nil {
		return Usage{}, recoveryErr
	}
	s.tokens = tokens
	return s.fetch(ctx, tokens.AccessToken)
}

func (s *Source) recoverSession(ctx context.Context) (tokenPair, error) {
	if s.tokens.RefreshToken != "" {
		tokens, err := s.refresh(ctx, s.tokens.RefreshToken)
		if err == nil {
			return tokens, nil
		}
		if isKind(err, ErrorRateLimited) {
			return tokenPair{}, err
		}
	}
	return s.login(ctx)
}

func (s *Source) login(ctx context.Context) (tokenPair, error) {
	return s.authenticate(ctx, "login", s.config.LoginEndpoint, map[string]string{
		"email":    s.config.Email,
		"password": s.config.Password,
	})
}

func (s *Source) refresh(ctx context.Context, refreshToken string) (tokenPair, error) {
	return s.authenticate(ctx, "refresh", s.config.RefreshEndpoint, map[string]string{
		"refresh_token": refreshToken,
	})
}

func (s *Source) authenticate(ctx context.Context, operation, endpoint string, payload map[string]string) (tokenPair, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return tokenPair{}, &Error{Kind: ErrorInvalidResponse, Operation: operation, Cause: err}
	}
	response, err := s.request(ctx, operation, http.MethodPost, endpoint, body, "")
	if err != nil {
		return tokenPair{}, err
	}
	data, err := parseEnvelope(operation, response)
	if err != nil {
		return tokenPair{}, err
	}

	var raw struct {
		AccessToken  json.RawMessage `json:"access_token"`
		RefreshToken json.RawMessage `json:"refresh_token"`
		ExpiresIn    json.RawMessage `json:"expires_in"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return tokenPair{}, invalidResponse(operation, "data must be an object", err)
	}
	accessToken, err := requiredString(raw.AccessToken, "access_token")
	if err != nil {
		return tokenPair{}, invalidResponse(operation, err.Error(), nil)
	}
	refreshToken, err := requiredString(raw.RefreshToken, "refresh_token")
	if err != nil {
		return tokenPair{}, invalidResponse(operation, err.Error(), nil)
	}
	if _, err := nonNegativeInt64(raw.ExpiresIn, "expires_in"); err != nil {
		return tokenPair{}, invalidResponse(operation, err.Error(), nil)
	}
	return tokenPair{AccessToken: accessToken, RefreshToken: refreshToken}, nil
}

func (s *Source) fetch(ctx context.Context, accessToken string) (Usage, error) {
	response, err := s.request(ctx, "usage", http.MethodGet, s.config.UsageEndpoint, nil, accessToken)
	if err != nil {
		return Usage{}, err
	}
	data, err := parseEnvelope("usage", response)
	if err != nil {
		return Usage{}, err
	}

	var raw struct {
		TodayActualCost json.RawMessage `json:"today_actual_cost"`
		TodayTokens     json.RawMessage `json:"today_tokens"`
		TPM             json.RawMessage `json:"tpm"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return Usage{}, invalidResponse("usage", "data must be an object", err)
	}
	cost, err := costMicros(raw.TodayActualCost)
	if err != nil {
		return Usage{}, invalidResponse("usage", "today_actual_cost "+err.Error(), nil)
	}
	tokens, err := nonNegativeUint(raw.TodayTokens, "today_tokens")
	if err != nil {
		return Usage{}, invalidResponse("usage", err.Error(), nil)
	}
	tpm, err := nonNegativeUint(raw.TPM, "tpm")
	if err != nil {
		return Usage{}, invalidResponse("usage", err.Error(), nil)
	}
	return Usage{TodayActualCostMicros: cost, TodayTokens: tokens, TPM: tpm}, nil
}

func (s *Source) request(ctx context.Context, operation, method, endpoint string, body []byte, accessToken string) ([]byte, error) {
	requestCtx, cancel := context.WithTimeout(ctx, s.config.Timeout)
	defer cancel()

	request, err := http.NewRequestWithContext(requestCtx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, &Error{Kind: ErrorTransport, Operation: operation, Cause: err}
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if accessToken != "" {
		request.Header.Set("Authorization", "Bearer "+accessToken)
	}

	response, err := s.client.Do(request)
	if err != nil {
		kind := ErrorTransport
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(requestCtx.Err(), context.DeadlineExceeded) {
			kind = ErrorTimeout
		}
		return nil, &Error{Kind: kind, Operation: operation, Cause: err}
	}
	defer response.Body.Close()

	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maxResponseSize+1))
	if err != nil {
		return nil, &Error{Kind: ErrorTransport, Operation: operation, StatusCode: response.StatusCode, Cause: err}
	}
	if len(responseBody) > maxResponseSize {
		return nil, &Error{Kind: ErrorOversized, Operation: operation, StatusCode: response.StatusCode, Cause: fmt.Errorf("response exceeds %d bytes", maxResponseSize)}
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		kind := ErrorHTTP
		switch response.StatusCode {
		case http.StatusUnauthorized:
			kind = ErrorUnauthorized
		case http.StatusTooManyRequests:
			kind = ErrorRateLimited
		}
		return nil, &Error{
			Kind:       kind,
			Operation:  operation,
			StatusCode: response.StatusCode,
			RetryAfter: parseRetryAfter(response.Header.Get("Retry-After"), time.Now()),
		}
	}
	return responseBody, nil
}

type rawEnvelope struct {
	Code    json.RawMessage `json:"code"`
	Message json.RawMessage `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func parseEnvelope(operation string, body []byte) (json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var envelope rawEnvelope
	if err := decoder.Decode(&envelope); err != nil {
		return nil, invalidResponse(operation, "invalid JSON envelope", err)
	}
	if err := requireEOF(decoder); err != nil {
		return nil, invalidResponse(operation, "invalid JSON envelope", err)
	}

	code, err := signedInteger(envelope.Code, "code")
	if err != nil {
		return nil, invalidResponse(operation, err.Error(), nil)
	}
	message, err := requiredString(envelope.Message, "message")
	if err != nil {
		return nil, invalidResponse(operation, err.Error(), nil)
	}
	data := bytes.TrimSpace(envelope.Data)
	if len(data) == 0 || data[0] != '{' {
		return nil, invalidResponse(operation, "data must be an object", nil)
	}
	if code != 0 {
		return nil, &Error{Kind: ErrorAPI, Operation: operation, APICode: code, APIMessage: message}
	}
	return envelope.Data, nil
}

func requireEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func requiredString(raw json.RawMessage, field string) (string, error) {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return "", fmt.Errorf("%s is required", field)
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", fmt.Errorf("%s must be a string", field)
	}
	if value == "" {
		return "", fmt.Errorf("%s cannot be empty", field)
	}
	return value, nil
}

func signedInteger(raw json.RawMessage, field string) (int64, error) {
	if len(raw) == 0 {
		return 0, fmt.Errorf("%s is required", field)
	}
	value, err := strconv.ParseInt(string(bytes.TrimSpace(raw)), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", field)
	}
	return value, nil
}

func nonNegativeUint(raw json.RawMessage, field string) (uint64, error) {
	if len(raw) == 0 {
		return 0, fmt.Errorf("%s is required", field)
	}
	value, err := strconv.ParseUint(string(bytes.TrimSpace(raw)), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be a non-negative integer", field)
	}
	return value, nil
}

func nonNegativeInt64(raw json.RawMessage, field string) (int64, error) {
	value, err := signedInteger(raw, field)
	if err != nil || value < 0 {
		return 0, fmt.Errorf("%s must be a non-negative integer", field)
	}
	return value, nil
}

func costMicros(raw json.RawMessage) (int64, error) {
	if len(raw) == 0 {
		return 0, errors.New("is required")
	}
	value := new(big.Rat)
	if _, ok := value.SetString(string(bytes.TrimSpace(raw))); !ok {
		return 0, errors.New("must be a non-negative JSON number")
	}
	if value.Sign() < 0 {
		return 0, errors.New("must be non-negative")
	}
	value.Mul(value, big.NewRat(1_000_000, 1))
	if !value.IsInt() {
		return 0, errors.New("has precision smaller than one micro")
	}
	if !value.Num().IsInt64() {
		return 0, errors.New("is too large")
	}
	return value.Num().Int64(), nil
}

func parseRetryAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if seconds, err := strconv.ParseUint(value, 10, 63); err == nil {
		if seconds > uint64((1<<63-1)/int64(time.Second)) {
			return time.Duration(1<<63 - 1)
		}
		return time.Duration(seconds) * time.Second
	}
	if deadline, err := http.ParseTime(value); err == nil && deadline.After(now) {
		return deadline.Sub(now)
	}
	return 0
}

func invalidResponse(operation, message string, cause error) error {
	if cause != nil {
		cause = fmt.Errorf("%s: %w", message, cause)
	} else {
		cause = errors.New(message)
	}
	return &Error{Kind: ErrorInvalidResponse, Operation: operation, Cause: cause}
}

func isKind(err error, kind ErrorKind) bool {
	var sourceErr *Error
	return errors.As(err, &sourceErr) && sourceErr.Kind == kind
}
