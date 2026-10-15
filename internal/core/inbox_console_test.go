package core

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"orbit/internal/appapi"
	"orbit/internal/config"
	"orbit/internal/inbox"
)

func TestInboxConsoleWithoutMQTTKeepsAuthorizationBoundaries(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.CoreConfig{Core: config.CoreIdentity{ID: "test-core"}, Console: config.ConsoleConfig{Password: "test-only", SessionHours: 1}, App: config.AppConfig{DataDir: dir}}
	engine, err := New(Config{CoreID: "test-core", CoreEpoch: "test-epoch"})
	if err != nil {
		t.Fatal(err)
	}
	routes, err := OpenRouteStore(filepath.Join(dir, "routes.sqlite"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer routes.Close()
	store, err := inbox.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	personal, err := appapi.New(store, cfg.App, nil)
	if err != nil {
		t.Fatal(err)
	}
	handler := InboxConsoleHandler(engine, routes, cfg, personal.AdminHandler(), personal.Handler())
	call := func(method, path, body string, cookie *http.Cookie, token string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "https://orbit.example"+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if cookie != nil {
			r.AddCookie(cookie)
		}
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	login := call("POST", "/api/auth/login", `{"password":"test-only"}`, nil, "")
	if login.Code != 200 {
		t.Fatal(login.Code, login.Body.String())
	}
	cookie := login.Result().Cookies()[0]
	create := call("POST", "/api/app/devices", `{"label":"浏览器","platform":"web"}`, cookie, "")
	if create.Code != 201 {
		t.Fatal(create.Code, create.Body.String())
	}
	var device map[string]string
	if err := json.Unmarshal(create.Body.Bytes(), &device); err != nil {
		t.Fatal(err)
	}
	if w := call("PUT", "/api/app/devices/"+device["id"], `{"label":"书桌工作台"}`, nil, device["token"]); w.Code != 401 {
		t.Fatal("device token authorized renaming", w.Code)
	}
	if w := call("PUT", "/api/app/devices/"+device["id"], `{"label":"书桌工作台"}`, cookie, ""); w.Code != 200 {
		t.Fatal("console could not rename device", w.Code, w.Body.String())
	}
	crossOrigin := httptest.NewRequest("PUT", "https://orbit.example/api/app/devices/"+device["id"], strings.NewReader(`{"label":"wrong"}`))
	crossOrigin.Header.Set("Content-Type", "application/json")
	crossOrigin.Header.Set("Origin", "https://evil.example")
	crossOrigin.AddCookie(cookie)
	crossResponse := httptest.NewRecorder()
	handler.ServeHTTP(crossResponse, crossOrigin)
	if crossResponse.Code != 403 {
		t.Fatal("cross-origin rename accepted", crossResponse.Code)
	}
	if w := call("PUT", "/api/nodes/"+device["id"]+"/archive", `{"archived":true}`, nil, device["token"]); w.Code != 401 {
		t.Fatal("device token authorized archive", w.Code)
	}
	for _, body := range []string{`{"archived":true}`, `{"archived":false}`} {
		if w := call("PUT", "/api/nodes/"+device["id"]+"/archive", body, cookie, ""); w.Code != 200 {
			t.Fatal("console could not archive or restore App", w.Code, w.Body.String())
		}
	}
	if w := call("GET", "/api/v1/status", "", cookie, ""); w.Code != 401 {
		t.Fatal("admin cookie authorized personal API", w.Code)
	}
	if w := call("GET", "/api/v1/status", "", nil, device["token"]); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w := call("GET", "/api/app/devices", "", nil, device["token"]); w.Code != 401 {
		t.Fatal("device authorized admin API", w.Code)
	}
	settings := call("GET", "/api/app/navigation/"+device["id"], "", cookie, "")
	if settings.Code != 200 {
		t.Fatal("cannot read device settings", settings.Code, settings.Body.String())
	}
	if w := call("PUT", "/api/app/navigation/"+device["id"], settings.Body.String(), nil, device["token"]); w.Code != 401 {
		t.Fatal("device token authorized pairing edits", w.Code)
	}
	if w := call("PUT", "/api/app/navigation/"+device["id"], settings.Body.String(), cookie, ""); w.Code != 200 {
		t.Fatal("console could not save pairings", w.Code, w.Body.String())
	}
	if w := call("DELETE", "/api/app/devices/"+device["id"], "", nil, device["token"]); w.Code != 401 {
		t.Fatal("device token authorized deletion", w.Code)
	}
	if w := call("DELETE", "/api/app/devices/"+device["id"], "", cookie, ""); w.Code != 200 {
		t.Fatal("admin deletion failed", w.Code)
	}
	if w := call("GET", "/api/v1/status", "", nil, device["token"]); w.Code != 401 {
		t.Fatal("deleted device still authorized", w.Code)
	}
	if w := call("DELETE", "/api/app/devices/"+device["id"], "", cookie, ""); w.Code != 404 {
		t.Fatal("missing device deletion", w.Code)
	}
	if w := call("GET", "/inbox/", "", nil, ""); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w := call("PUT", "/api/routes", `{"revision":1,"routes":{"node-a":{"profile":"overview-app","inputs":[]}}}`, cookie, ""); w.Code != 409 {
		t.Fatal("inbox-only console accepted routes", w.Code)
	}
	if w := call("PUT", "/api/routes", `{"revision":1,"routes":{}}`, cookie, ""); w.Code != 200 {
		t.Fatal("empty routes failed", w.Code, w.Body.String())
	}
}
