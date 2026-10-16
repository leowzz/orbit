package appapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"google.golang.org/protobuf/proto"
	v "orbit/gen/go/orbit/v1"
	"orbit/internal/config"
	"orbit/internal/inbox"
	"orbit/internal/mqtt"
)

type navTransport struct {
	mu       sync.Mutex
	messages []mqtt.Message
	handler  mqtt.Handler
}

func (t *navTransport) Publish(_ context.Context, m mqtt.Message) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.messages = append(t.messages, m)
	return nil
}
func (t *navTransport) Subscribe(_ context.Context, _ string, h mqtt.Handler) error {
	t.handler = h
	return nil
}
func TestNavigationIsolationPersistenceAndAllowedCommands(t *testing.T) {
	dir := t.TempDir()
	store, err := inbox.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	devices := map[string]config.AppDevice{}
	for _, id := range []string{"a", "b"} {
		h := sha256.Sum256([]byte(strings.Repeat(id, 32)))
		devices[id] = config.AppDevice{TokenSHA256: hex.EncodeToString(h[:])}
	}
	api, err := New(store, config.AppConfig{DataDir: dir, Devices: devices}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	transport := &navTransport{}
	if err = api.StartNavigation(ctx, transport, "core", nil); err != nil {
		t.Fatal(err)
	}
	catalog := &v.AgentShortcuts{AgentId: "mac", HostLabel: "My Mac", Revision: 10, AtTop: true, Items: []*v.AgentShortcut{{Id: "app1", Title: "Calendar", Kind: "app"}}}
	publish := func() {
		raw, _ := proto.Marshal(catalog)
		if e := transport.handler(ctx, mqtt.Message{Topic: "orbit/v1/agents/mac/shortcuts", Payload: raw, Retain: true}); e != nil {
			t.Fatal(e)
		}
	}
	publish()
	cfg, _ := store.NavigationSettings(ctx, "a")
	cfg.AtTop = true
	cfg.Pairs = []inbox.Pairing{{ExecutorKind: "agent", ExecutorID: "mac", Enabled: true}}
	rawSettings, _ := json.Marshal(cfg)
	settingsRequest := httptest.NewRequest("PUT", "/api/app/navigation/a", bytes.NewReader(rawSettings))
	settingsResponse := httptest.NewRecorder()
	api.AdminHandler().ServeHTTP(settingsResponse, settingsRequest)
	if settingsResponse.Code != 200 {
		t.Fatal(settingsResponse.Code, settingsResponse.Body.String())
	}
	req := func(method, path, node string, body any) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest(method, path, bytes.NewReader(raw))
		if node != "" {
			r.Header.Set("Authorization", "Bearer "+strings.Repeat(node, 32))
		}
		w := httptest.NewRecorder()
		api.Handler().ServeHTTP(w, r)
		return w
	}
	if w := req("GET", "/api/v1/navigation", "", nil); w.Code != 401 {
		t.Fatal(w.Code)
	}
	var d inbox.Navigation
	w := req("GET", "/api/v1/navigation", "a", nil)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &d); err != nil || len(d.Items) != 1 || !d.AtTop {
		t.Fatal(d, err)
	}
	entry := d.Items[0].ID
	if w := req("POST", "/api/v1/navigation", "a", d); w.Code != 405 {
		t.Fatal("node can edit navigation", w.Code)
	}
	if w := req("GET", "/api/v1/navigation", "b", nil); !strings.Contains(w.Body.String(), `"items":[]`) {
		t.Fatal("configuration leaked", w.Body.String())
	}
	if w := req("POST", "/api/v1/navigation/"+entry+"/open", "b", nil); w.Code != 409 {
		t.Fatal("opened another device entry", w.Code)
	}
	if w := req("POST", "/api/v1/navigation/"+entry+"/open", "a", nil); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	transport.mu.Lock()
	var found, retained bool
	for _, m := range transport.messages {
		if m.Topic == "orbit/v1/nodes/a/navigation" {
			retained = retained || (m.Retain && len(m.Payload) > 0)
			var nav v.Navigation
			_ = proto.Unmarshal(m.Payload, &nav)
			if nav.ShowTitles || (len(nav.Items) > 0 && (nav.Revision != 1 || !nav.AtTop)) {
				t.Error("wrong snapshot", &nav)
			}
		}
		if m.Topic == "orbit/v1/agents/mac/commands" {
			found = true
			var c v.Command
			_ = proto.Unmarshal(m.Payload, &c)
			if m.Retain || c.GetOpenShortcut().GetShortcutId() != "app1" || c.GetOpenShortcut().GetCatalogRevision() != 10 || c.IntentRef.RequesterId != "a" {
				t.Error("wrong command", &c)
			}
		}
	}
	transport.mu.Unlock()
	if !found || !retained {
		t.Fatal("missing command or retained config")
	}
	catalog.Revision = 11
	catalog.Items = nil
	publish()
	catalog.Revision = 10
	catalog.Items = []*v.AgentShortcut{{Id: "app1", Title: "Old", Kind: "app"}}
	publish()
	if w := req("POST", "/api/v1/navigation/"+entry+"/open", "a", nil); w.Code != 409 {
		t.Fatal("removed shortcut executable", w.Code)
	}
	second, err := inbox.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	saved, err := second.Navigation(ctx, "a")
	if err != nil || saved.Revision != 2 || len(saved.Items) != 0 || !saved.AtTop {
		t.Fatal("not durable", saved, err)
	}
}
func TestNavigationRejectsExecutableURLsAndSpoofedCatalog(t *testing.T) {
	store, _ := inbox.Open(t.TempDir())
	defer store.Close()
	ctx := context.Background()
	api, _ := New(store, config.AppConfig{}, nil)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	tr := &navTransport{}
	_ = api.StartNavigation(ctx, tr, "core", nil)
	raw, _ := proto.Marshal(&v.AgentShortcuts{AgentId: "other", Revision: 1})
	if tr.handler(ctx, mqtt.Message{Topic: "orbit/v1/agents/mac/shortcuts", Payload: raw}) == nil {
		t.Fatal("spoof accepted")
	}
	if _, err := store.SaveNavigationIcon(ctx, []byte("<svg/>")); err == nil {
		t.Fatal("accepted unsafe image")
	}
}

func TestNodeExecutionUsesIndependentPairingAndClaimsOnce(t *testing.T) {
	dir := t.TempDir()
	store, err := inbox.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	devices := map[string]config.AppDevice{}
	for _, id := range []string{"a", "b", "c"} {
		h := sha256.Sum256([]byte(strings.Repeat(id, 32)))
		devices[id] = config.AppDevice{Label: id, TokenSHA256: hex.EncodeToString(h[:])}
	}
	api, err := New(store, config.AppConfig{DataDir: dir, Devices: devices}, nil)
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, path, node string, body any, admin bool) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest(method, path, bytes.NewReader(raw))
		r.Header.Set("Authorization", "Bearer "+strings.Repeat(node, 32))
		w := httptest.NewRecorder()
		if admin {
			api.AdminHandler().ServeHTTP(w, r)
		} else {
			api.Handler().ServeHTTP(w, r)
		}
		return w
	}
	catalog := map[string]any{"revision": 1, "items": []map[string]string{{"id": "browser", "title": "Browser", "kind": "url"}}}
	for _, id := range []string{"b", "c"} {
		if w := request("POST", "/api/v1/execution/catalog", id, catalog, false); w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	// Device credentials cannot edit a receiving device's pairings.
	if w := request("PUT", "/api/v1/navigation/a", "a", nil, false); w.Code != 404 {
		t.Fatal("public config endpoint", w.Code)
	}
	cfg, _ := store.NavigationSettings(context.Background(), "a")
	cfg.Pairs = []inbox.Pairing{{ExecutorKind: "node", ExecutorID: "b", Enabled: true}, {ExecutorKind: "node", ExecutorID: "c", Enabled: true}}
	cfg.MetricsVisible, cfg.MetricsWidth = proto.Bool(false), proto.Uint32(320)
	if w := request("PUT", "/api/app/navigation/a", "", cfg, true); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	// No projection routes or MQTT transport are configured.
	w := request("GET", "/api/v1/navigation", "a", nil, false)
	var nav inbox.Navigation
	_ = json.Unmarshal(w.Body.Bytes(), &nav)
	if nav.MetricsVisible == nil || *nav.MetricsVisible || nav.MetricsWidth == nil || *nav.MetricsWidth != 320 {
		t.Fatal("display settings did not reach the device", w.Body.String())
	}
	if len(nav.Items) != 2 || nav.Items[0].ExecutorID != "b" || nav.Items[1].ExecutorID != "c" {
		t.Fatal(nav)
	}
	open := func() {
		t.Helper()
		if w := request("POST", "/api/v1/navigation/"+nav.Items[0].ID+"/open", "a", nil, false); w.Code != 202 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	open()
	if w := request("GET", "/api/v1/execution/commands", "c", nil, false); !strings.Contains(w.Body.String(), `"commands":[]`) {
		t.Fatal("command leaked", w.Body.String())
	}
	w = request("GET", "/api/v1/execution/commands", "b", nil, false)
	var commands struct {
		Commands []nodeCommand `json:"commands"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &commands)
	if len(commands.Commands) != 1 || commands.Commands[0].ShortcutID != "browser" || commands.Commands[0].CatalogRevision != 1 {
		t.Fatal(w.Body.String())
	}
	if w = request("GET", "/api/v1/execution/commands", "b", nil, false); !strings.Contains(w.Body.String(), `"commands":[]`) {
		t.Fatal("command replayed", w.Body.String())
	}
	// Removing a pair also cancels an already queued request.
	open()
	cfg, _ = store.NavigationSettings(context.Background(), "a")
	cfg.Pairs = nil
	if w = request("PUT", "/api/app/navigation/a", "", cfg, true); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w = request("GET", "/api/v1/execution/commands", "b", nil, false); !strings.Contains(w.Body.String(), `"commands":[]`) {
		t.Fatal("unpaired request executed", w.Body.String())
	}
	if w = request("POST", "/api/v1/navigation/"+nav.Items[0].ID+"/open", "a", nil, false); w.Code != 409 {
		t.Fatal("removed pair executable", w.Code)
	}
	// The client cannot upload URLs, paths, or another endpoint identity.
	bad := map[string]any{"revision": 2, "items": []map[string]string{{"id": "browser", "title": "Browser", "kind": "url", "target": "https://example.com"}}}
	if w = request("POST", "/api/v1/execution/catalog", "b", bad, false); w.Code != 400 {
		t.Fatal("accepted targets", w.Code)
	}
	// An empty catalog removes all capabilities immediately.
	if w = request("POST", "/api/v1/execution/catalog", "b", map[string]any{"revision": 2, "items": []any{}}, false); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w = request("POST", "/api/v1/execution/catalog", "b", catalog, false); w.Code != 409 {
		t.Fatal("stale catalog restored entries", w.Code)
	}
}
