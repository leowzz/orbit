package core

import (
	"encoding/json"
	"net/http/httptest"
	"orbit/internal/config"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestArchiveDevicePersistsAcrossRestartAndDiscovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "core.sqlite")
	store, err := OpenRouteStore(path, seedRoutes())
	if err != nil {
		t.Fatal(err)
	}
	engine := newTestEngine(t)
	handler := InboxConsoleHandler(engine, store, &config.CoreConfig{Console: config.ConsoleConfig{Password: "test", SessionHours: 1}})
	login := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(`{"password":"test"}`))
	req.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(login, req)
	if login.Code != 200 {
		t.Fatal(login.Code, login.Body.String())
	}
	call := func(method, path, body string, auth bool, origin string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", origin)
		if auth {
			for _, cookie := range login.Result().Cookies() {
				r.AddCookie(cookie)
			}
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	for _, tc := range []struct {
		body   string
		auth   bool
		origin string
		code   int
	}{
		{`{"archived":true}`, false, "", 401},
		{`{"archived":true}`, true, "https://evil.example", 403},
		{`{}`, true, "", 400}, {`{"archived":null}`, true, "", 400}, {`{"archived":"true"}`, true, "", 400},
		{`{"archived":true,"label":"unexpected"}`, true, "", 400}, {`{"archived":true} {}`, true, "", 400},
	} {
		if w := call("PUT", "/api/nodes/node-a/archive", tc.body, tc.auth, tc.origin); w.Code != tc.code {
			t.Fatal(tc, w.Code, w.Body.String())
		}
	}
	for _, id := range []string{"node-a", "app-phone", "no-longer-discovered"} {
		for range 2 {
			if w := call("PUT", "/api/nodes/"+id+"/archive", `{"archived":true}`, true, ""); w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
		}
	}
	// Retained self-descriptions must not unarchive a device.
	if _, err := engine.ApplyNodeState(time.Now(), testNodeState(time.Now())); err != nil {
		t.Fatal(err)
	}
	response := call("GET", "/api/state", "", true, "")
	var state struct {
		Archived []string `json:"archived_nodes"`
	}
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &state) != nil || !reflect.DeepEqual(state.Archived, []string{"app-phone", "no-longer-discovered", "node-a"}) {
		t.Fatal(response.Code, response.Body.String())
	}
	routes, err := store.Load()
	if err != nil || routes.Revision != 1 || !reflect.DeepEqual(routes.Routes, seedRoutes()) {
		t.Fatal("archive changed routes", routes, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenRouteStore(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	ids, err := reopened.archivedNodes(t.Context())
	if err != nil || !reflect.DeepEqual(ids, state.Archived) {
		t.Fatal("archive lost on restart", ids, err)
	}
	for range 2 {
		restore := httptest.NewRequest("PUT", "/api/nodes/node-a/archive", strings.NewReader(`{"archived":false}`))
		restore.Header.Set("Content-Type", "application/json")
		restore.SetPathValue("id", "node-a")
		w := httptest.NewRecorder()
		reopened.archiveNode(w, restore)
		if w.Code != 200 {
			t.Fatal(w.Code)
		}
	}
	ids, err = reopened.archivedNodes(t.Context())
	if err != nil || !reflect.DeepEqual(ids, []string{"app-phone", "no-longer-discovered"}) {
		t.Fatal(ids, err)
	}
}

func TestArchiveValidationAndStorageFailure(t *testing.T) {
	store, err := OpenRouteStore(filepath.Join(t.TempDir(), "core.sqlite"), nil)
	if err != nil {
		t.Fatal(err)
	}
	call := func(id, media, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("PUT", "/", strings.NewReader(body))
		r.SetPathValue("id", id)
		r.Header.Set("Content-Type", media)
		w := httptest.NewRecorder()
		store.archiveNode(w, r)
		return w
	}
	if w := call("node-a", "text/plain", `{"archived":true}`); w.Code != 415 {
		t.Fatal(w.Code)
	}
	for _, id := range []string{"bad/id", "UPPERCASE", strings.Repeat("a", 65)} {
		if w := call(id, "application/json", `{"archived":true}`); w.Code != 400 {
			t.Fatal(id, w.Code)
		}
	}
	_ = store.Close()
	if w := call("node-a", "application/json", `{"archived":true}`); w.Code != 503 {
		t.Fatal(w.Code)
	}
}

func TestArchiveSchemaUpgradePreservesNames(t *testing.T) {
	path := filepath.Join(t.TempDir(), "core.sqlite")
	store, err := OpenRouteStore(path, seedRoutes())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`INSERT INTO node_labels(node_id,label) VALUES('node-a','书桌屏'); DROP TABLE node_archives`); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()
	upgraded, err := OpenRouteStore(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer upgraded.Close()
	archived, err := upgraded.archivedNodes(t.Context())
	if err != nil || len(archived) != 0 {
		t.Fatal(archived, err)
	}
	labels, err := upgraded.nodeLabels(t.Context())
	if err != nil || labels["node-a"] != "书桌屏" {
		t.Fatal("migration changed name", labels, err)
	}
	routes, err := upgraded.Load()
	if err != nil || routes.Revision != 1 || !reflect.DeepEqual(routes.Routes, seedRoutes()) {
		t.Fatal(routes, err)
	}
}
