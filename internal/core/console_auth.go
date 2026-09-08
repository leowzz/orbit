package core

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"io"
	"mime"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const consoleCookie = "orbit_core_session"
const consoleSessionTTL = 24 * time.Hour

type loginAttempts struct {
	count int
	until time.Time
}
type consoleAuth struct {
	mu         sync.Mutex
	password   [32]byte
	configured bool
	sessions   map[string]time.Time
	attempts   map[string]loginAttempts
	now        func() time.Time
}

func newConsoleAuth(password string) *consoleAuth {
	return &consoleAuth{password: sha256.Sum256([]byte(password)), configured: strings.TrimSpace(password) != "", sessions: make(map[string]time.Time), attempts: make(map[string]loginAttempts), now: time.Now}
}
func (a *consoleAuth) valid(r *http.Request) bool {
	cookie, err := r.Cookie(consoleCookie)
	if err != nil {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	until, ok := a.sessions[cookie.Value]
	if !ok || !until.After(a.now()) {
		delete(a.sessions, cookie.Value)
		return false
	}
	return true
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
	for token, until := range a.sessions {
		if !until.After(now) {
			delete(a.sessions, token)
		}
	}
	if len(a.sessions) >= 256 {
		http.Error(w, "too many active sessions", 429)
		return
	}
	var bytes [32]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		http.Error(w, "cannot create session", 500)
		return
	}
	token := hex.EncodeToString(bytes[:])
	until := now.Add(consoleSessionTTL)
	a.sessions[token] = until
	cookie := sessionCookie(r, token, int(consoleSessionTTL.Seconds()))
	cookie.Expires = until
	http.SetCookie(w, cookie)
	writeConsoleJSON(w, map[string]bool{"authenticated": true})
}
func (a *consoleAuth) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(consoleCookie); err == nil {
		a.mu.Lock()
		delete(a.sessions, cookie.Value)
		a.mu.Unlock()
	}
	http.SetCookie(w, sessionCookie(r, "", -1))
	writeConsoleJSON(w, map[string]bool{"authenticated": false})
}
