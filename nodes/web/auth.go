package web

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"strings"
	"sync"
	"time"
)

const authCookieName = "orbit_web_auth"
const maxWebSessions = 1024

type AuthConfig struct {
	InboxURL   string
	InboxToken string
	Password   string
	SessionTTL time.Duration
}

type authManager struct {
	password      string
	ttl           time.Duration
	now           func() time.Time
	mu            sync.Mutex
	sessions      map[[32]byte]*webSession
	loginWindow   time.Time
	loginAttempts int
}

type webSession struct {
	expires time.Time
	ctx     context.Context
	cancel  context.CancelFunc
}

func newAuthManager(config AuthConfig) *authManager {
	return &authManager{
		password: config.Password,
		ttl:      config.SessionTTL,
		now:      time.Now,
		sessions: make(map[[32]byte]*webSession),
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
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", time.Time{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	expiresAt := auth.now().Add(auth.ttl)
	auth.mu.Lock()
	defer auth.mu.Unlock()
	for digest, session := range auth.sessions {
		if !session.expires.After(auth.now()) {
			session.cancel()
			delete(auth.sessions, digest)
		}
	}
	if len(auth.sessions) >= maxWebSessions {
		var oldest [32]byte
		var expiry time.Time
		for digest, session := range auth.sessions {
			if expiry.IsZero() || session.expires.Before(expiry) {
				oldest, expiry = digest, session.expires
			}
		}
		auth.sessions[oldest].cancel()
		delete(auth.sessions, oldest)
	}
	ctx, cancel := context.WithCancel(context.Background())
	auth.sessions[sha256.Sum256([]byte(token))] = &webSession{expires: expiresAt, ctx: ctx, cancel: cancel}
	return token, expiresAt, nil
}

func (auth *authManager) valid(token string) bool {
	if !auth.required() {
		return true
	}
	return auth.session(token) != nil
}

func (auth *authManager) tokenExpires(token string) (time.Time, bool) {
	if session := auth.session(token); session != nil {
		return session.expires, true
	}
	return time.Time{}, false
}

func (auth *authManager) passwordMatches(password string) bool {
	if !auth.required() {
		return true
	}
	expected := sha256.Sum256([]byte(auth.password))
	actual := sha256.Sum256([]byte(password))
	return subtle.ConstantTimeCompare(expected[:], actual[:]) == 1
}

func (auth *authManager) session(token string) *webSession {
	if len(token) != 43 {
		return nil
	}
	auth.mu.Lock()
	defer auth.mu.Unlock()
	digest := sha256.Sum256([]byte(token))
	session := auth.sessions[digest]
	if session != nil && !session.expires.After(auth.now()) {
		session.cancel()
		delete(auth.sessions, digest)
		return nil
	}
	return session
}

func (auth *authManager) revoke(token string) {
	auth.mu.Lock()
	defer auth.mu.Unlock()
	digest := sha256.Sum256([]byte(token))
	if session := auth.sessions[digest]; session != nil {
		session.cancel()
		delete(auth.sessions, digest)
	}
}

// A shared password has one budget, independent of IP and proxy headers.
// Existing authenticated sessions remain usable while login is throttled.
func (auth *authManager) loginRetry() time.Duration {
	auth.mu.Lock()
	defer auth.mu.Unlock()
	now := auth.now()
	if auth.loginWindow.IsZero() || !now.Before(auth.loginWindow.Add(time.Minute)) {
		auth.loginWindow, auth.loginAttempts = now, 0
	}
	if auth.loginAttempts >= 5 {
		return auth.loginWindow.Add(time.Minute).Sub(now)
	}
	auth.loginAttempts++
	return 0
}
