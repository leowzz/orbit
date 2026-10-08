package core

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestConsoleSessionLoginExpiryLogoutAndSecureCookie(t *testing.T) {
	a := newConsoleAuth("password")
	now := time.Now()
	a.now = func() time.Time { return now }
	login := func(password string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "https://example.com/api/auth/login", strings.NewReader(`{"password":"`+password+`"}`))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		a.login(w, r)
		return w
	}
	if w := login("wrong"); w.Code != 401 || w.Header().Get("WWW-Authenticate") != "" {
		t.Fatal("wrong password must not trigger browser Basic auth")
	}
	w := login("password")
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatal("missing session cookie")
	}
	cookie := cookies[0]
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode || cookie.MaxAge != 86400 {
		t.Fatalf("unsafe cookie: %+v", cookie)
	}
	r := httptest.NewRequest("GET", "https://example.com/api/state", nil)
	r.AddCookie(cookie)
	if !a.valid(r) {
		t.Fatal("new session rejected")
	}
	newAuth := newConsoleAuth("password")
	if newAuth.valid(r) {
		t.Fatal("session survived restart")
	}
	a.logout(httptest.NewRecorder(), r)
	if a.valid(r) {
		t.Fatal("logout did not revoke token")
	}
	w = login("password")
	r = httptest.NewRequest("GET", "https://example.com/api/state", nil)
	r.AddCookie(w.Result().Cookies()[0])
	now = now.Add(consoleSessionTTL)
	if a.valid(r) {
		t.Fatal("expired session accepted")
	}
}
func TestConsoleLoginThrottleAndMalformedRequests(t *testing.T) {
	a := newConsoleAuth("password")
	now := time.Now()
	a.now = func() time.Time { return now }
	request := func(body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		a.login(w, r)
		return w
	}
	for _, body := range []string{`{"password":"password"} {}`, `{"password":"password","unknown":true}`, `{"password":"` + strings.Repeat("a", 5000) + `"}`} {
		if w := request(body); w.Code != 400 {
			t.Fatalf("malformed login accepted: %d", w.Code)
		}
	}
	for i := 0; i < 10; i++ {
		if w := request(`{"password":"wrong"}`); w.Code != 401 {
			t.Fatal(w.Code)
		}
	}
	if w := request(`{"password":"password"}`); w.Code != 429 {
		t.Fatal("login flood not limited")
	}
	now = now.Add(time.Minute)
	if w := request(`{"password":"password"}`); w.Code != 200 {
		t.Fatal("rate limit did not expire")
	}
}

func TestConsoleSessionCookieBehindHTTPSProxy(t *testing.T) {
	r := httptest.NewRequest("POST", "http://core/api/auth/login", nil)
	r.Header.Set("X-Forwarded-Proto", "https")
	if !sessionCookie(r, "token", 86400).Secure {
		t.Fatal("HTTPS proxy must set Secure cookie")
	}
}
