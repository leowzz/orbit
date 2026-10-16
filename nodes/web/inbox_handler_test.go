package web

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func loginWeb(t *testing.T, handler http.Handler, password string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"password": password})
	r := httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(string(body)))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	var result struct {
		Token string `json:"token"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Token == "" {
		t.Fatalf("login failed: %d", w.Code)
	}
	return result.Token
}

func TestInboxOnlyExposesInboxAndRevokesLogout(t *testing.T) {
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer upstream-secret" {
			t.Error("lost upstream credential")
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"node_id":"web","label":"My inbox","view":{"sessions":["private-session"]}}`)
	}))
	defer upstream.Close()
	cfg := AuthConfig{Password: "browser-secret", SessionTTL: time.Hour, InboxURL: upstream.URL, InboxToken: "upstream-secret"}
	handler, err := InboxHandler(cfg, "")
	if err != nil {
		t.Fatal(err)
	}
	token := loginWeb(t, handler, cfg.Password)
	call := func(method, path, origin string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://localhost"+path, nil)
		r.AddCookie(&http.Cookie{Name: authCookieName, Value: token})
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	if w := call("GET", "/", ""); w.Code != 303 || w.Header().Get("Location") != "/inbox/" {
		t.Fatal("root does not open inbox")
	}
	for _, path := range []string{"/api/state", "/api/events", "/api/app/devices", "/app.js", "/app.css"} {
		if w := call("GET", path, ""); w.Code != 404 {
			t.Errorf("removed path %s returned %d", path, w.Code)
		}
	}
	for _, path := range []string{"/api/sessions/secret/open", "/api/v1/sessions/secret/open", "/api/v1/executions"} {
		if w := call("POST", path, ""); w.Code != 404 {
			t.Errorf("host action %s returned %d", path, w.Code)
		}
	}
	if w := call("GET", "/api/v1/status", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"node_id":"web"`) || strings.Contains(w.Body.String(), "view") {
		t.Fatal("status leaked display data", w.Code, w.Body.String())
	}
	if w := call("POST", "/api/auth/logout", "https://evil.example"); w.Code != 403 {
		t.Fatal("cross-site logout allowed")
	}
	if w := call("GET", "/api/v1/status", "http://evil.example"); w.Code != 403 {
		t.Fatal("cross-site proxy allowed")
	}
	if w := call("GET", "/api/v1/status", "http://localhost"); w.Code != 200 {
		t.Fatal("same-origin rejected")
	}
	if w := call("POST", "/api/auth/logout", ""); w.Code != 204 {
		t.Fatal("logout failed")
	}
	if w := call("GET", "/api/v1/status", ""); w.Code != 401 {
		t.Fatal("captured cookie works after logout")
	}
	if calls != 2 {
		t.Fatal("unexpected upstream calls", calls)
	}
}

func TestLoginThrottleCannotBeBypassedWithForwardedHeaders(t *testing.T) {
	handler, err := InboxHandler(AuthConfig{Password: "secret", SessionTTL: time.Hour, InboxURL: "http://localhost:1", InboxToken: "device"}, "")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 7; i++ {
		r := httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(`{"password":"wrong"}`))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Forwarded-For", strings.Repeat("1", i+1)+".0.0.1")
		r.RemoteAddr = "192.0.2." + strings.Repeat("1", i+1) + ":1234"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		want := 401
		if i >= 5 {
			want = 429
		}
		if w.Code != want {
			t.Fatalf("attempt %d: got %d want %d", i+1, w.Code, want)
		}
		if i >= 5 && w.Header().Get("Retry-After") == "" {
			t.Fatal("missing retry delay")
		}
	}
}

func TestInboxHandlerFailsClosedWithoutCredentials(t *testing.T) {
	for _, cfg := range []AuthConfig{{}, {Password: "secret", SessionTTL: time.Hour, InboxURL: "https://example.com"}} {
		if _, err := InboxHandler(cfg, ""); err == nil {
			t.Fatal("incomplete credentials accepted")
		}
	}
}
