package web

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"time"
)

const authCookieName = "orbit_web_auth"

type AuthConfig struct {
	Password   string
	SessionTTL time.Duration
}

type authManager struct {
	password string
	ttl      time.Duration
	now      func() time.Time
}

func newAuthManager(config AuthConfig) *authManager {
	return &authManager{
		password: config.Password,
		ttl:      config.SessionTTL,
		now:      time.Now,
	}
}

func (auth *authManager) required() bool {
	return strings.TrimSpace(auth.password) != ""
}

func (auth *authManager) issue() (string, time.Time, error) {
	if !auth.required() {
		return "", time.Time{}, errors.New("web authentication is not configured")
	}
	if auth.ttl <= 0 {
		return "", time.Time{}, errors.New("web authentication session TTL must be positive")
	}
	expiresAt := auth.now().Add(auth.ttl)
	payload := strconv.FormatInt(expiresAt.UnixNano(), 10)
	return payload + "." + auth.signature(payload), expiresAt, nil
}

func (auth *authManager) valid(token string) bool {
	if !auth.required() {
		return true
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return false
	}
	expiresAt, ok := auth.tokenExpires(token)
	if !ok || !expiresAt.After(auth.now()) {
		return false
	}
	expected := auth.signature(parts[0])
	return subtle.ConstantTimeCompare([]byte(expected), []byte(parts[1])) == 1
}

func (auth *authManager) tokenExpires(token string) (time.Time, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return time.Time{}, false
	}
	expiresUnix, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	return time.Unix(0, expiresUnix), true
}

func (auth *authManager) passwordMatches(password string) bool {
	if !auth.required() {
		return true
	}
	expected := sha256.Sum256([]byte(auth.password))
	actual := sha256.Sum256([]byte(password))
	return subtle.ConstantTimeCompare(expected[:], actual[:]) == 1
}

func (auth *authManager) signature(payload string) string {
	digest := hmac.New(sha256.New, []byte(auth.password))
	_, _ = digest.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(digest.Sum(nil))
}
