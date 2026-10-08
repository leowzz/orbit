package appapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"orbit/internal/config"
	"orbit/internal/inbox"
)

func TestBrowserSessionIsolationAndRevocation(t *testing.T) {
	store, err := inbox.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	token := strings.Repeat("b", 64)
	digest := sha256.Sum256([]byte(token))
	s, err := New(store, config.AppConfig{DataDir: t.TempDir(), Devices: map[string]config.AppDevice{"browser": {TokenSHA256: hex.EncodeToString(digest[:]), Platform: "web"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	handler := s.Handler()
	call := func(method, path, body, origin string, cookie *http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "https://orbit.example"+path, strings.NewReader(body))
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	if w := call("GET", "/inbox/", "", "", nil); w.Code != 200 || !strings.Contains(w.Body.String(), "留给下一次") {
		t.Fatal(w.Code)
	}
	if w := call("GET", "/api/v1/sync/snapshot", "", "", nil); w.Code != 401 {
		t.Fatal(w.Code)
	}
	if w := call("POST", "/api/v1/session", `{"token":"`+token+`"}`, "https://evil.example", nil); w.Code != 403 || len(w.Result().Cookies()) != 0 {
		t.Fatal(w.Code)
	}
	w := call("POST", "/api/v1/session", `{"token":"`+token+`"}`, "https://orbit.example", nil)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatal(cookies)
	}
	cookie := cookies[0]
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/api/v1" {
		t.Fatal("unsafe cookie")
	}
	if strings.Contains(w.Body.String(), token) {
		t.Fatal("token echoed")
	}
	if w := call("GET", "/api/v1/status", "", "", cookie); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w := call("POST", "/api/v1/operations", `{}`, "https://evil.example", cookie); w.Code != 403 {
		t.Fatal(w.Code)
	}
	if w := call("POST", "/api/v1/session", `{"token":"invalid"}`, "", cookie); w.Code != 401 {
		t.Fatal("existing cookie rescued bad token", w.Code)
	}
	if w := call("GET", "/api/app/devices", "", "", cookie); w.Code != 404 {
		t.Fatal("browser reached administration", w.Code)
	}
	r := httptest.NewRequest("POST", "https://orbit.example", nil).WithContext(context.Background())
	device := s.devices["browser"]
	device.Revoked = true
	s.mu.Lock()
	err = s.saveDevice(r, "browser", device)
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if w := call("GET", "/api/v1/status", "", "", cookie); w.Code != 403 {
		t.Fatal("revoked cookie accepted", w.Code)
	}
	if w := call("DELETE", "/api/v1/session", "", "https://evil.example", cookie); w.Code != 403 {
		t.Fatal(w.Code)
	}
	if w := call("DELETE", "/api/v1/session", "", "", cookie); w.Code != 204 || w.Result().Cookies()[0].MaxAge != -1 {
		t.Fatal(w.Code)
	}
}
