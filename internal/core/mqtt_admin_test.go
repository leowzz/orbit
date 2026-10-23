package core

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"orbit/internal/config"
)

func TestMQTTAdminUsesConsoleSessionAndOriginProtection(t *testing.T) {
	var calls atomic.Int32
	broker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(500) }))
	defer broker.Close()
	cfg := &config.CoreConfig{Console: config.ConsoleConfig{Password: "password", SessionHours: 72}, MQTTAdmin: config.MQTTAdminConfig{URL: broker.URL, APIKey: "do-not-return-key", APISecret: "do-not-return-secret"}}
	handler := ConsoleHandler(nil, newTestConsoleStore(t), cfg)
	login := httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(`{"password":"password"}`))
	login.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, login)
	if w.Code != 200 {
		t.Fatal("login failed")
	}
	cookie := w.Result().Cookies()[0]
	for _, path := range []string{"/api/mqtt/users", "/api/mqtt/users/node/rotate", "/api/mqtt/users/node/revoke", "/api/mqtt/users/node/disconnect"} {
		for _, auth := range []bool{false, true} {
			r := httptest.NewRequest("POST", path, strings.NewReader(`{"username":"new","kind":"node","participant_id":"phone"}`))
			r.Header.Set("Content-Type", "application/json")
			want := 401
			if auth {
				r.AddCookie(cookie)
				r.Header.Set("Origin", "https://another.example")
				want = 403
			}
			w = httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != want {
				t.Fatalf("%s got %d want %d", path, w.Code, want)
			}
		}
	}
	if calls.Load() != 0 {
		t.Fatal("unauthorized request reached broker")
	}
	r := httptest.NewRequest("GET", "/api/mqtt/users", nil)
	r.AddCookie(cookie)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 502 || strings.Contains(w.Body.String(), "do-not-return") {
		t.Fatal("upstream error leaked secrets")
	}
	cfg.MQTTAdmin = config.MQTTAdminConfig{URL: broker.URL}
	handler = ConsoleHandler(nil, newTestConsoleStore(t), cfg)
	login = httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(`{"password":"password"}`))
	login.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, login)
	cookie = w.Result().Cookies()[0]
	r = httptest.NewRequest("GET", "/api/mqtt/users", nil)
	r.AddCookie(cookie)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"enabled":false`) {
		t.Fatal("empty keys must disable administration only")
	}
}
