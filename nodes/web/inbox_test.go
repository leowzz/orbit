package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestInboxGatewayKeepsCredentialOnServer(t *testing.T) {
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer device-secret" || r.Header.Get("Cookie") != "" || r.URL.Path != "/api/v1/status" {
			t.Error("incorrect proxy credentials or path")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"node_id":"web"}`))
	}))
	defer upstream.Close()
	cfg := AuthConfig{Password: "browser-password", SessionTTL: time.Hour, InboxURL: upstream.URL, InboxToken: "device-secret"}
	handler := HandlerWithAuth(NewStore(), nil, cfg)
	auth := newAuthManager(cfg)
	token, _, err := auth.issue()
	if err != nil {
		t.Fatal(err)
	}
	call := func(path, origin string, loggedIn bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "http://localhost"+path, nil)
		if loggedIn {
			r.AddCookie(&http.Cookie{Name: authCookieName, Value: token})
		}
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	if w := call("/api/v1/status", "", false); w.Code != 401 {
		t.Fatal(w.Code)
	}
	if w := call("/api/v1/status", "https://evil.example", true); w.Code != 403 {
		t.Fatal(w.Code)
	}
	if w := call("/api/v1/status", "", true); w.Code != 200 || strings.Contains(w.Body.String(), "device-secret") {
		t.Fatal(w.Code)
	}
	if w := call("/api/app/devices", "", true); w.Code != 404 {
		t.Fatal(w.Code)
	}
	if calls != 1 {
		t.Fatal("unexpected upstream calls", calls)
	}
}
