package core

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const consoleCookie = "orbit_core_session"

type loginAttempts struct {
	count int
	until time.Time
}
type consoleAuth struct {
	mu         sync.Mutex
	password   [32]byte
	configured bool
	sessionTTL time.Duration
	store      *RouteStore
	verifier   []byte
	attempts   map[string]loginAttempts
	now        func() time.Time
}

func newConsoleAuth(password string, sessionTTL time.Duration, store *RouteStore) (*consoleAuth, error) {
	if store == nil {
		return nil, errors.New("console session store is required")
	}
	verifier, err := store.consoleCredential(password)
	if err != nil {
		return nil, err
	}
	return &consoleAuth{password: sha256.Sum256([]byte(password)), configured: strings.TrimSpace(password) != "", sessionTTL: sessionTTL, store: store, verifier: verifier, attempts: make(map[string]loginAttempts), now: time.Now}, nil
}
func (a *consoleAuth) valid(r *http.Request) (bool, error) {
	cookie, err := r.Cookie(consoleCookie)
	if err != nil || !a.configured || len(cookie.Value) != 64 {
		return false, nil
	}
	return a.store.validConsoleSession(r.Context(), cookie.Value, a.verifier, a.now())
}
func sessionCookie(r *http.Request, value string, age int) *http.Cookie {
	return &http.Cookie{Name: consoleCookie, Value: value, Path: "/", MaxAge: age, HttpOnly: true, Secure: r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https", SameSite: http.SameSiteStrictMode}
}
func (a *consoleAuth) login(w http.ResponseWriter, r *http.Request) {
	if !a.configured {
		http.Error(w, "console authentication is not configured", 503)
		return
	}
	media, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if media != "application/json" {
		http.Error(w, "expected application/json", 415)
		return
	}
	var input struct {
		Password string `json:"password"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		http.Error(w, "invalid login request", 400)
		return
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.now()
	for key, attempt := range a.attempts {
		if !attempt.until.After(now) {
			delete(a.attempts, key)
		}
	}
	attempt := a.attempts[host]
	if attempt.count >= 10 {
		w.Header().Set("Retry-After", "60")
		http.Error(w, "too many login attempts", 429)
		return
	}
	supplied := sha256.Sum256([]byte(input.Password))
	if subtle.ConstantTimeCompare(supplied[:], a.password[:]) != 1 {
		if len(a.attempts) >= 1024 && attempt.count == 0 {
			http.Error(w, "too many login attempts", 429)
			return
		}
		if attempt.count == 0 {
			attempt.until = now.Add(time.Minute)
		}
		attempt.count++
		a.attempts[host] = attempt
		http.Error(w, "invalid password", 401)
		return
	}
	delete(a.attempts, host)
	var bytes [32]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		http.Error(w, "cannot create session", 500)
		return
	}
	token := hex.EncodeToString(bytes[:])
	until := now.Add(a.sessionTTL)
	if err := a.store.createConsoleSession(r.Context(), token, a.verifier, now, until); err != nil {
		if errors.Is(err, errSessionLimit) {
			http.Error(w, "too many active sessions", 429)
		} else {
			http.Error(w, "cannot save session", http.StatusServiceUnavailable)
		}
		return
	}
	cookie := sessionCookie(r, token, int(a.sessionTTL.Seconds()))
	cookie.Expires = until
	http.SetCookie(w, cookie)
	writeConsoleJSON(w, map[string]bool{"authenticated": true})
}
func (a *consoleAuth) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(consoleCookie); err == nil {
		if err := a.store.deleteConsoleSession(r.Context(), cookie.Value); err != nil {
			http.Error(w, "cannot revoke session", http.StatusServiceUnavailable)
			return
		}
	}
	http.SetCookie(w, sessionCookie(r, "", -1))
	writeConsoleJSON(w, map[string]bool{"authenticated": false})
}
