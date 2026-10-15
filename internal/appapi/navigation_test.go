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
	if err = api.StartNavigation(ctx, transport, "core", func() map[string][]string { return map[string][]string{"a": {"mac"}, "b": {"other"}} }); err != nil {
		t.Fatal(err)
	}
	catalog := &v.AgentShortcuts{AgentId: "mac", HostLabel: "My Mac", Revision: 10, Items: []*v.AgentShortcut{{Id: "app1", Title: "Calendar", Kind: "app"}}}
	publish := func() {
		raw, _ := proto.Marshal(catalog)
		if e := transport.handler(ctx, mqtt.Message{Topic: "orbit/v1/agents/mac/shortcuts", Payload: raw, Retain: true}); e != nil {
			t.Fatal(e)
		}
	}
	publish()
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
	if err := json.Unmarshal(w.Body.Bytes(), &d); err != nil || len(d.Items) != 1 {
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
			if nav.ShowTitles || (len(nav.Items) > 0 && nav.Revision != 1) {
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
	if err != nil || saved.Revision != 2 || len(saved.Items) != 0 {
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
