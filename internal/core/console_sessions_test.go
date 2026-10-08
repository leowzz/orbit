package core

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"orbit/internal/config"
)

func consoleLogin(t *testing.T, auth *consoleAuth, password string) *http.Cookie {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"password": password})
	r := httptest.NewRequest("POST", "https://example.com/api/auth/login", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	auth.login(w, r)
	if w.Code != 200 || len(w.Result().Cookies()) != 1 {
		t.Fatalf("login failed: %d %s", w.Code, w.Body.String())
	}
	return w.Result().Cookies()[0]
}

func sessionRequest(cookie *http.Cookie) *http.Request {
	r := httptest.NewRequest("GET", "https://example.com/api/auth/session", nil)
	r.AddCookie(cookie)
	return r
}

func TestConsoleSessionsPersistAcrossDatabaseReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "console.sqlite")
	now := time.Now().UTC().Truncate(time.Second)
	open := func(password string, ttl time.Duration) (*RouteStore, *consoleAuth) {
		t.Helper()
		store, err := OpenRouteStore(path, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = store.Close() })
		auth, err := newConsoleAuth(password, ttl, store)
		if err != nil {
			t.Fatal(err)
		}
		auth.now = func() time.Time { return now }
		return store, auth
	}
	store, auth := open("password", 72*time.Hour)
	first := consoleLogin(t, auth, "password")
	second := consoleLogin(t, auth, "password")
	var hash string
	var expiry int64
	if err := store.db.QueryRow(`SELECT token_hash,expires_at FROM console_sessions WHERE token_hash=?`, consoleTokenHash(first.Value)).Scan(&hash, &expiry); err != nil {
		t.Fatal(err)
	}
	if hash == first.Value || len(hash) != 64 || expiry != now.Add(72*time.Hour).UnixNano() {
		t.Fatal("unsafe token persistence or wrong expiry")
	}
	_ = store.Close()
	// A new configured lifetime applies only to new logins, not existing cookies.
	store, auth = open("password", time.Hour)
	now = now.Add(2 * time.Hour)
	if !consoleValid(t, auth, sessionRequest(first)) || !consoleValid(t, auth, sessionRequest(second)) {
		t.Fatal("restart lost sessions or shortened existing lifetime")
	}
	out := httptest.NewRecorder()
	auth.logout(out, sessionRequest(first))
	if out.Code != 200 || out.Result().Cookies()[0].MaxAge != -1 {
		t.Fatal("logout failed")
	}
	_ = store.Close()
	store, auth = open("password", 96*time.Hour)
	if consoleValid(t, auth, sessionRequest(first)) || !consoleValid(t, auth, sessionRequest(second)) {
		t.Fatal("logout did not survive restart or revoked another browser")
	}
	// Altering the cookie cannot grant access.
	tampered := *second
	if strings.HasPrefix(tampered.Value, "0") {
		tampered.Value = "1" + tampered.Value[1:]
	} else {
		tampered.Value = "0" + tampered.Value[1:]
	}
	if consoleValid(t, auth, sessionRequest(&tampered)) {
		t.Fatal("tampered cookie accepted")
	}
	now = now.Add(70 * time.Hour)
	if consoleValid(t, auth, sessionRequest(second)) {
		t.Fatal("restart extended the original expiry")
	}
	_ = store.Close()
}

func TestConsolePasswordChangesPermanentlyRevokeSessions(t *testing.T) {
	store := newTestConsoleStore(t)
	old, err := newConsoleAuth("old-password", time.Hour, store)
	if err != nil {
		t.Fatal(err)
	}
	oldCookie := consoleLogin(t, old, "old-password")
	changed, err := newConsoleAuth("new-password", time.Hour, store)
	if err != nil {
		t.Fatal(err)
	}
	if consoleValid(t, changed, sessionRequest(oldCookie)) || consoleValid(t, old, sessionRequest(oldCookie)) {
		t.Fatal("password change did not revoke existing sessions")
	}
	newCookie := consoleLogin(t, changed, "new-password")
	if !consoleValid(t, changed, sessionRequest(newCookie)) {
		t.Fatal("new credential could not log in")
	}
	reverted, err := newConsoleAuth("old-password", time.Hour, store)
	if err != nil {
		t.Fatal(err)
	}
	if consoleValid(t, reverted, sessionRequest(oldCookie)) || consoleValid(t, reverted, sessionRequest(newCookie)) {
		t.Fatal("restoring an old password revived a revoked session")
	}
}

func TestConsoleStorageFailureDoesNotIssueOrForgetCookies(t *testing.T) {
	store := newTestConsoleStore(t)
	auth, err := newConsoleAuth("password", time.Hour, store)
	if err != nil {
		t.Fatal(err)
	}
	cookie := consoleLogin(t, auth, "password")
	// Simulate a failed durable write while reads still work.
	if _, err := store.db.Exec(`PRAGMA query_only=ON`); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	auth.logout(w, sessionRequest(cookie))
	if w.Code != 503 || len(w.Result().Cookies()) != 0 || !consoleValid(t, auth, sessionRequest(cookie)) {
		t.Fatal("failed logout pretended to revoke the session")
	}
	r := httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(`{"password":"password"}`))
	r.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	auth.login(w, r)
	if w.Code != 503 || len(w.Result().Cookies()) != 0 {
		t.Fatal("failed session persistence issued a cookie")
	}
	_, _ = store.db.Exec(`PRAGMA query_only=OFF`)
	handler := ConsoleHandler(nil, store, &config.CoreConfig{Console: config.ConsoleConfig{Password: "password", SessionHours: 72}})
	_ = store.Close()
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, sessionRequest(cookie))
	if w.Code != 503 || len(w.Result().Cookies()) != 0 {
		t.Fatal("storage read failure must not look like an expired login", w.Code)
	}
}

func TestConsoleSessionLimitAndExpiredRowCleanup(t *testing.T) {
	auth := newTestConsoleAuth(t, "password", time.Hour)
	now := time.Now()
	auth.now = func() time.Time { return now }
	_, err := auth.store.db.Exec(`WITH RECURSIVE n(i) AS (VALUES(1) UNION ALL SELECT i+1 FROM n WHERE i<256)
 INSERT INTO console_sessions SELECT printf('%064d',i),? FROM n`, now.Add(time.Hour).UnixNano())
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(`{"password":"password"}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	auth.login(w, r)
	if w.Code != 429 || len(w.Result().Cookies()) != 0 {
		t.Fatal("durable session limit not enforced")
	}
	now = now.Add(time.Hour)
	consoleLogin(t, auth, "password")
	var count int
	if err := auth.store.db.QueryRow(`SELECT count(*) FROM console_sessions`).Scan(&count); err != nil || count != 1 {
		t.Fatal("expired session cleanup failed", count, err)
	}
}
