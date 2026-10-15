package core

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"orbit/internal/config"
)

func TestNodeNamesPersistWithoutChangingRoutesOrDiscovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "core.sqlite")
	store, err := OpenRouteStore(path, seedRoutes())
	if err != nil {
		t.Fatal(err)
	}
	engine := newTestEngine(t)
	cfg := &config.CoreConfig{Console: config.ConsoleConfig{Password: "test-password", SessionHours: 1}}
	handler := InboxConsoleHandler(engine, store, cfg)
	login := httptest.NewRecorder()
	loginRequest := httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(`{"password":"test-password"}`))
	loginRequest.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(login, loginRequest)
	if login.Code != 200 {
		t.Fatal(login.Code, login.Body.String())
	}
	cookies := login.Result().Cookies()
	call := func(method, url, body string, authorized bool, origin string) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(method, url, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Origin", origin)
		if authorized {
			for _, cookie := range cookies {
				request.AddCookie(cookie)
			}
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	if w := call("PUT", "/api/nodes/node-a/label", `{"label":"书桌屏"}`, false, ""); w.Code != 401 {
		t.Fatal(w.Code)
	}
	if w := call("PUT", "/api/nodes/node-a/label", `{"label":"书桌屏"}`, true, "https://evil.example"); w.Code != 403 {
		t.Fatal(w.Code)
	}
	for _, body := range []string{`{"label":" "}`, `{"label":"` + strings.Repeat("名", 41) + `"}`, `{"label":"屏幕","node_id":"other"}`, `{"label":"屏幕"} {}`} {
		if w := call("PUT", "/api/nodes/node-a/label", body, true, ""); w.Code != 400 {
			t.Fatal(body, w.Code)
		}
	}
	if w := call("PUT", "/api/nodes/missing/label", `{"label":"书桌屏"}`, true, ""); w.Code != 404 {
		t.Fatal(w.Code)
	}
	if w := call("PUT", "/api/nodes/node-a/label", `{"label":"  书桌屏  "}`, true, ""); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	// A retained metadata refresh must not overwrite the name set in Core.
	if _, err := engine.ApplyNodeState(time.Now(), testNodeState(time.Now())); err != nil {
		t.Fatal(err)
	}
	response := call("GET", "/api/state", "", true, "")
	var state struct {
		Labels map[string]string `json:"node_labels"`
	}
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &state) != nil || state.Labels["node-a"] != "书桌屏" {
		t.Fatal(response.Code, response.Body.String())
	}
	routes, err := store.Load()
	if err != nil || routes.Revision != 1 || len(routes.Routes) != 1 || routes.Routes["node-a"].Profile != seedRoutes()["node-a"].Profile {
		t.Fatal("rename changed routes", routes, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenRouteStore(path, seedRoutes())
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	labels, err := reopened.nodeLabels(t.Context())
	if err != nil || labels["node-a"] != "书桌屏" {
		t.Fatal("name lost after restart", labels, err)
	}
}

func TestDiscoveredNodeCanBeNamedWithoutAContentRoute(t *testing.T) {
	store, err := OpenRouteStore(filepath.Join(t.TempDir(), "core.sqlite"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	engine := newTestEngine(t)
	if _, err := engine.ApplyNodeState(time.Now(), testNodeState(time.Now())); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPut, "/api/nodes/node-a/label", strings.NewReader(`{"label":"客厅屏"}`))
	request.SetPathValue("id", "node-a")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	store.renameNode(response, request, engine)
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	routes, err := store.Load()
	if err != nil || len(routes.Routes) != 0 {
		t.Fatal("rename added a route", routes, err)
	}
}

func TestNodeNamesUpgradeExistingRouteStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "core.sqlite")
	store, err := OpenRouteStore(path, seedRoutes())
	if err != nil {
		t.Fatal(err)
	}
	// Model an existing database created before names were supported.
	if _, err := store.db.Exec(`DROP TABLE node_labels`); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	upgraded, err := OpenRouteStore(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer upgraded.Close()
	names, err := upgraded.nodeLabels(t.Context())
	if err != nil || len(names) != 0 {
		t.Fatal(names, err)
	}
	routes, err := upgraded.Load()
	if err != nil || routes.Revision != 1 || len(routes.Routes) != 1 {
		t.Fatal("migration changed routes", routes, err)
	}
}
