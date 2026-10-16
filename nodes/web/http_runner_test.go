package web

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"orbit/internal/appapi"
	"orbit/internal/config"
	"orbit/internal/inbox"
)

func TestHTTPWebSharesInboxIdentityAndSessionAuthority(t *testing.T) {
	token := strings.Repeat("a", 64)
	digest := sha256.Sum256([]byte(token))
	dataDir := t.TempDir()
	db, err := inbox.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	view := testView(time.Now())
	view.NodeId = "app-web"
	configured := true
	core, err := appapi.New(db, config.AppConfig{DataDir: dataDir, Devices: map[string]config.AppDevice{
		"app-web": {Platform: "web", TokenSHA256: hex.EncodeToString(digest[:])},
	}}, func(id string) json.RawMessage {
		if id != "app-web" {
			t.Errorf("unexpected status identity %s", id)
		}
		if !configured {
			return json.RawMessage(`null`)
		}
		raw, _ := protojson.Marshal(view)
		return raw
	})
	if err != nil {
		t.Fatal(err)
	}
	opened := 0
	core.SetSessionOpener(func(_ context.Context, node, session, epoch string, revision uint64) (string, error) {
		opened++
		if node != "app-web" || session != webTestSessionID || epoch != view.CoreEpoch || revision != view.Metadata.Revision {
			t.Errorf("wrong session authority: %s %s %s %d", node, session, epoch, revision)
		}
		return "intent-http", nil
	})
	upstream := httptest.NewServer(core.Handler())
	defer upstream.Close()
	store := NewStore()
	runner := NewHTTPRunner(upstream.URL, token, store, nil)
	if err := runner.refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	snapshot, _ := store.Snapshot()
	if snapshot.NodeID != "app-web" || snapshot.Usage == nil || snapshot.Codex == nil {
		t.Fatalf("missing unified status: %+v", snapshot)
	}
	// The existing Web browser API retains its contract, while the runner adds
	// Core's epoch and string revision to the authenticated upstream request.
	handler := HandlerWithAuth(store, runner, AuthConfig{Password: "web-password", SessionTTL: time.Hour, InboxURL: upstream.URL, InboxToken: token})
	session := loginWeb(t, handler, "web-password")
	call := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: authCookieName, Value: session})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w
	}
	if w := call("GET", "/api/v1/status", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"node_id":"app-web"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := call("GET", "/api/v1/sync/snapshot", ""); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w := call("GET", "/api/state", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"node_id":"app-web"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	body, _ := json.Marshal(map[string]uint64{"view_revision": snapshot.Revision})
	if w := call("POST", "/api/sessions/"+webTestSessionID+"/open", string(body)); w.Code != 202 || !strings.Contains(w.Body.String(), "intent-http") {
		t.Fatal(w.Code, w.Body.String())
	}
	if opened != 1 {
		t.Fatal("session request not routed", opened)
	}
	if _, err := runner.OpenCodexSession(context.Background(), webTestSessionID, snapshot.Revision+1); err == nil || opened != 1 {
		t.Fatal("accepted future revision")
	}
	configured = false
	if err := runner.refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	snapshot, _ = store.Snapshot()
	if snapshot.NodeID != "app-web" || snapshot.Usage != nil || snapshot.Codex != nil {
		t.Fatal("removed route retained old data", snapshot)
	}
	configured = true
	view.Metadata.Revision++
	if err := runner.refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if snapshot, _ = store.Snapshot(); snapshot.Usage == nil {
		t.Fatal("route did not recover")
	}
	if w := call("GET", "/api/app/devices", ""); w.Code != 404 {
		t.Fatal("gateway exposed admin API", w.Code)
	}
}

func TestHTTPWebRejectsInvalidViewsAndCredentials(t *testing.T) {
	for _, body := range []string{
		`{"node_id":"app-web","view":{"nodeId":"another-device","coreEpoch":"core","metadata":{"revision":"1"}}}`,
		`{"node_id":"app-web","view":{}}`,
		`{"node_id":"","view":null}`,
		strings.Repeat("x", maxViewPayload+1),
	} {
		t.Run(body[:min(40, len(body))], func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) }))
			defer s.Close()
			r := NewHTTPRunner(s.URL, "token", NewStore(), nil)
			if err := r.refresh(context.Background()); err == nil {
				t.Fatal("accepted invalid view")
			}
		})
	}
	for _, code := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusInternalServerError} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(code) }))
		r := NewHTTPRunner(s.URL, "secret", NewStore(), nil)
		if err := r.refresh(context.Background()); err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatal("invalid rejection", err)
		}
		s.Close()
	}
}

func TestHTTPWebDoesNotForwardTokenAcrossRedirects(t *testing.T) {
	called := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	r := NewHTTPRunner(source.URL, "secret", NewStore(), nil)
	if err := r.refresh(context.Background()); err == nil || called {
		t.Fatal("followed upstream redirect")
	}
}
