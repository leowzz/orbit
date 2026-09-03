package sub2api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestFetchUsageLogsInAndParsesExactValues(t *testing.T) {
	var loginBody map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login":
			if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
				t.Fatalf("unexpected login request: %s %q", r.Method, r.Header.Get("Content-Type"))
			}
			if err := json.NewDecoder(r.Body).Decode(&loginBody); err != nil {
				t.Fatal(err)
			}
			writeJSON(w, `{"code":0,"message":"success","data":{"access_token":"access","refresh_token":"refresh","expires_in":3600}}`)
		case "/usage":
			if r.Method != http.MethodGet {
				t.Errorf("usage method = %s", r.Method)
			}
			if got := r.Header.Get("Authorization"); got != "Bearer access" {
				t.Fatalf("Authorization = %q", got)
			}
			writeJSON(w, `{"code":0,"message":"success","data":{"today_actual_cost":1.234567,"today_tokens":12345,"tpm":67890}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	source := newTestSource(t, server.URL, time.Second)
	usage, err := source.FetchUsage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if usage != (Usage{TodayActualCostMicros: 1_234_567, TodayTokens: 12345, TPM: 67890}) {
		t.Fatalf("usage = %+v", usage)
	}
	if loginBody["email"] != "device@example.com" || loginBody["password"] != `p"ass` {
		t.Fatalf("login body = %#v", loginBody)
	}
}

func TestFetchUsage401RefreshesAndRetriesOnce(t *testing.T) {
	var mu sync.Mutex
	usageCalls := 0
	refreshCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/login":
			writeTokenEnvelope(w, "old-access", "old-refresh")
		case "/refresh":
			refreshCalls++
			if r.Method != http.MethodPost {
				t.Errorf("refresh method = %s", r.Method)
			}
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body["refresh_token"] != "old-refresh" || len(body) != 1 {
				t.Fatalf("refresh body = %#v", body)
			}
			writeTokenEnvelope(w, "new-access", "new-refresh")
		case "/usage":
			usageCalls++
			if usageCalls == 1 {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			if got := r.Header.Get("Authorization"); got != "Bearer new-access" {
				t.Fatalf("Authorization = %q", got)
			}
			writeUsageEnvelope(w)
		}
	}))
	defer server.Close()

	usage, err := newTestSource(t, server.URL, time.Second).FetchUsage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if usage.TPM != 3 || refreshCalls != 1 || usageCalls != 2 {
		t.Fatalf("usage=%+v refreshCalls=%d usageCalls=%d", usage, refreshCalls, usageCalls)
	}
}

func TestFetchUsage401FailedRefreshLogsInAndRetriesOnce(t *testing.T) {
	loginCalls := 0
	usageCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login":
			loginCalls++
			if loginCalls == 1 {
				writeTokenEnvelope(w, "old-access", "old-refresh")
			} else {
				writeTokenEnvelope(w, "login-access", "login-refresh")
			}
		case "/refresh":
			w.WriteHeader(http.StatusServiceUnavailable)
		case "/usage":
			usageCalls++
			if usageCalls == 1 {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			if got := r.Header.Get("Authorization"); got != "Bearer login-access" {
				t.Fatalf("Authorization = %q", got)
			}
			writeUsageEnvelope(w)
		}
	}))
	defer server.Close()

	_, err := newTestSource(t, server.URL, time.Second).FetchUsage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if loginCalls != 2 || usageCalls != 2 {
		t.Fatalf("loginCalls=%d usageCalls=%d", loginCalls, usageCalls)
	}
}

func TestFinal401DoesNotLoop(t *testing.T) {
	usageCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login", "/refresh":
			writeTokenEnvelope(w, "access", "refresh")
		case "/usage":
			usageCalls++
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	defer server.Close()

	_, err := newTestSource(t, server.URL, time.Second).FetchUsage(context.Background())
	assertSourceError(t, err, ErrorUnauthorized, http.StatusUnauthorized)
	if usageCalls != 2 {
		t.Fatalf("usageCalls = %d", usageCalls)
	}
}

func TestRefresh429PreservesRetryAfterAndSkipsLogin(t *testing.T) {
	loginCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login":
			loginCalls++
			writeTokenEnvelope(w, "access", "refresh")
		case "/usage":
			w.WriteHeader(http.StatusUnauthorized)
		case "/refresh":
			w.Header().Set("Retry-After", " 17 ")
			w.WriteHeader(http.StatusTooManyRequests)
		}
	}))
	defer server.Close()

	_, err := newTestSource(t, server.URL, time.Second).FetchUsage(context.Background())
	sourceErr := assertSourceError(t, err, ErrorRateLimited, http.StatusTooManyRequests)
	if sourceErr.RetryAfter != 17*time.Second {
		t.Fatalf("RetryAfter = %s", sourceErr.RetryAfter)
	}
	if loginCalls != 1 {
		t.Fatalf("loginCalls = %d", loginCalls)
	}
}

func TestRequestTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		writeTokenEnvelope(w, "access", "refresh")
	}))
	defer server.Close()

	_, err := newTestSource(t, server.URL, 5*time.Millisecond).FetchUsage(context.Background())
	assertSourceError(t, err, ErrorTimeout, 0)
}

func TestNon2xxAndOversizedResponses(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		kind   ErrorKind
	}{
		{name: "HTTP", status: http.StatusBadGateway, body: "unavailable", kind: ErrorHTTP},
		{name: "oversized", status: http.StatusOK, body: strings.Repeat("x", maxResponseSize+1), kind: ErrorOversized},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()

			_, err := newTestSource(t, server.URL, time.Second).FetchUsage(context.Background())
			assertSourceError(t, err, test.kind, test.status)
		})
	}
}

func TestRedirectIsRejected(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" {
			http.Redirect(w, r, "/redirected", http.StatusFound)
			return
		}
		writeTokenEnvelope(w, "access", "refresh")
	}))
	defer server.Close()
	_, err := newTestSource(t, server.URL, time.Second).FetchUsage(context.Background())
	assertSourceError(t, err, ErrorHTTP, http.StatusFound)
}

func TestUsageValidation(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "missing cost", body: `{"code":0,"message":"ok","data":{"today_tokens":1,"tpm":2}}`},
		{name: "negative cost", body: `{"code":0,"message":"ok","data":{"today_actual_cost":-1,"today_tokens":1,"tpm":2}}`},
		{name: "sub-micro cost", body: `{"code":0,"message":"ok","data":{"today_actual_cost":0.0000001,"today_tokens":1,"tpm":2}}`},
		{name: "string cost", body: `{"code":0,"message":"ok","data":{"today_actual_cost":"1","today_tokens":1,"tpm":2}}`},
		{name: "negative tokens", body: `{"code":0,"message":"ok","data":{"today_actual_cost":1,"today_tokens":-1,"tpm":2}}`},
		{name: "fractional tokens", body: `{"code":0,"message":"ok","data":{"today_actual_cost":1,"today_tokens":1.0,"tpm":2}}`},
		{name: "missing tpm", body: `{"code":0,"message":"ok","data":{"today_actual_cost":1,"today_tokens":1}}`},
		{name: "negative tpm", body: `{"code":0,"message":"ok","data":{"today_actual_cost":1,"today_tokens":1,"tpm":-2}}`},
		{name: "fractional tpm", body: `{"code":0,"message":"ok","data":{"today_actual_cost":1,"today_tokens":1,"tpm":2.0}}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := sourceServingUsage(t, test.body)
			_, err := source.FetchUsage(context.Background())
			assertSourceError(t, err, ErrorInvalidResponse, 0)
		})
	}
}

func TestUsageCostAcceptsInteger(t *testing.T) {
	source := sourceServingUsage(t, `{"code":0,"message":"ok","data":{"today_actual_cost":1,"today_tokens":2,"tpm":3}}`)
	usage, err := source.FetchUsage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if usage.TodayActualCostMicros != 1_000_000 {
		t.Fatalf("TodayActualCostMicros = %d", usage.TodayActualCostMicros)
	}
}

func TestEnvelopeValidation(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		kind    ErrorKind
		apiCode int64
	}{
		{name: "invalid JSON", body: `{`, kind: ErrorInvalidResponse},
		{name: "trailing JSON", body: `{"code":0,"message":"ok","data":{}} {}`, kind: ErrorInvalidResponse},
		{name: "missing code", body: `{"message":"ok","data":{}}`, kind: ErrorInvalidResponse},
		{name: "fractional code", body: `{"code":0.0,"message":"ok","data":{}}`, kind: ErrorInvalidResponse},
		{name: "missing message", body: `{"code":0,"data":{}}`, kind: ErrorInvalidResponse},
		{name: "nonobject data", body: `{"code":0,"message":"ok","data":[]}`, kind: ErrorInvalidResponse},
		{name: "API error", body: `{"code":1001,"message":"failed","data":{}}`, kind: ErrorAPI, apiCode: 1001},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := sourceServingUsage(t, test.body)
			_, err := source.FetchUsage(context.Background())
			sourceErr := assertSourceError(t, err, test.kind, 0)
			if sourceErr.APICode != test.apiCode {
				t.Fatalf("APICode = %d", sourceErr.APICode)
			}
		})
	}
}

func TestTokenEnvelopeValidation(t *testing.T) {
	tests := []string{
		`{"code":0,"message":"ok","data":{"refresh_token":"refresh","expires_in":3600}}`,
		`{"code":0,"message":"ok","data":{"access_token":"access","expires_in":3600}}`,
		`{"code":0,"message":"ok","data":{"access_token":"access","refresh_token":"refresh"}}`,
		`{"code":0,"message":"ok","data":{"access_token":"access","refresh_token":"refresh","expires_in":1.0}}`,
		`{"code":0,"message":"ok","data":{"access_token":"access","refresh_token":"refresh","expires_in":-1}}`,
	}
	for index, body := range tests {
		t.Run(fmt.Sprint(index), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { writeJSON(w, body) }))
			defer server.Close()
			_, err := newTestSource(t, server.URL, time.Second).FetchUsage(context.Background())
			assertSourceError(t, err, ErrorInvalidResponse, 0)
		})
	}
}

func TestNewRejectsInvalidConfiguration(t *testing.T) {
	_, err := New(Config{})
	assertSourceError(t, err, ErrorInvalidConfig, 0)
}

func TestNewRejectsPlaintextEndpoints(t *testing.T) {
	_, err := New(Config{
		LoginEndpoint:   "http://sub2api.example/login",
		RefreshEndpoint: "https://sub2api.example/refresh",
		UsageEndpoint:   "https://sub2api.example/usage",
		Email:           "device@example.com",
		Password:        "password",
	})
	assertSourceError(t, err, ErrorInvalidConfig, 0)
}

func newTestSource(t *testing.T, baseURL string, timeout time.Duration) *Source {
	t.Helper()
	source, err := newSource(Config{
		LoginEndpoint:   baseURL + "/login",
		RefreshEndpoint: baseURL + "/refresh",
		UsageEndpoint:   baseURL + "/usage",
		Email:           "device@example.com",
		Password:        `p"ass`,
		Timeout:         timeout,
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	return source
}

func sourceServingUsage(t *testing.T, usageBody string) *Source {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" {
			writeTokenEnvelope(w, "access", "refresh")
			return
		}
		writeJSON(w, usageBody)
	}))
	t.Cleanup(server.Close)
	return newTestSource(t, server.URL, time.Second)
}

func writeTokenEnvelope(w http.ResponseWriter, access, refresh string) {
	writeJSON(w, fmt.Sprintf(`{"code":0,"message":"success","data":{"access_token":%q,"refresh_token":%q,"expires_in":3600}}`, access, refresh))
}

func writeUsageEnvelope(w http.ResponseWriter) {
	writeJSON(w, `{"code":0,"message":"success","data":{"today_actual_cost":1.25,"today_tokens":2,"tpm":3}}`)
}

func writeJSON(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(body))
}

func assertSourceError(t *testing.T, err error, kind ErrorKind, status int) *Error {
	t.Helper()
	if err == nil {
		t.Fatal("expected error")
	}
	var sourceErr *Error
	if !errors.As(err, &sourceErr) {
		t.Fatalf("error type = %T (%v)", err, err)
	}
	if sourceErr.Kind != kind || sourceErr.StatusCode != status {
		t.Fatalf("error = %+v", sourceErr)
	}
	return sourceErr
}
