package appapi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"orbit/internal/config"
	"orbit/internal/inbox"
)

func TestAdminDevicesPersistRotateRevokeAndSync(t *testing.T) {
	dir := t.TempDir()
	store, err := inbox.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	cfg := config.AppConfig{DataDir: dir}
	s, err := New(store, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	admin := httptest.NewServer(s.AdminHandler())
	defer admin.Close()
	public := httptest.NewServer(s.Handler())
	defer public.Close()
	call := func(method, path, body string) []byte {
		t.Helper()
		req, _ := http.NewRequest(method, admin.URL+path, strings.NewReader(body))
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		if res.StatusCode >= 300 {
			t.Fatalf("%s: %d %s", path, res.StatusCode, raw)
		}
		return raw
	}
	var credential map[string]string
	json.Unmarshal(call("POST", "/api/app/devices", `{"label":"手机","platform":"android"}`), &credential)
	id, token := credential["id"], credential["token"]
	if id == "" || len(token) < 32 {
		t.Fatal("missing credentials")
	}
	req, _ := http.NewRequest("GET", public.URL+"/api/v1/events", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	stream, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Body.Close()
	reader := bufio.NewReader(stream.Body)
	if line, _ := reader.ReadString('\n'); line != "event: sync\n" {
		t.Fatal(line)
	}
	listed := call("GET", "/api/app/devices", "")
	if bytes.Contains(listed, []byte(token)) || bytes.Contains(listed, []byte("TokenSHA256")) || !bytes.Contains(listed, []byte(`"connections":1`)) {
		t.Fatalf("unsafe/wrong device list: %s", listed)
	}
	call("POST", "/api/app/devices/"+id+"/revoke", "")
	done := make(chan error, 1)
	go func() { _, e := io.Copy(io.Discard, reader); done <- e }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("revoked SSE remained open")
	}
	authenticate := func(handler http.Handler, token string) int {
		r := httptest.NewRequest("GET", "/api/v1/status", nil)
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w.Code
	}
	if authenticate(s.Handler(), token) != 403 {
		t.Fatal("revoked token accepted")
	}
	reopened, err := inbox.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	restarted, err := New(reopened, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if authenticate(restarted.Handler(), token) != 403 {
		t.Fatal("revocation lost after reopening registry")
	}
	json.Unmarshal(call("POST", "/api/app/devices/"+id+"/rotate", ""), &credential)
	newToken := credential["token"]
	if authenticate(s.Handler(), token) != 401 || authenticate(s.Handler(), newToken) != 200 {
		t.Fatal("rotation did not replace token")
	}
	// Stale deployment seed must never overwrite a console revocation/rotation.
	cfg.Devices = map[string]config.AppDevice{id: {TokenSHA256: strings.Repeat("a", 64)}}
	restarted, err = New(store, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if authenticate(restarted.Handler(), newToken) != 200 {
		t.Fatal("seed overwrote managed credential")
	}
	op := inbox.Operation{ID: uuid.NewString(), ItemID: uuid.NewString(), Type: "create", Kind: "todo", Body: "管理台待办"}
	raw, _ := json.Marshal(op)
	first := call("POST", "/api/app/operations", string(raw))
	retry := call("POST", "/api/app/operations", string(raw))
	if !bytes.Equal(first, retry) {
		t.Fatal("admin retry not idempotent")
	}
	var item inbox.Item
	json.Unmarshal(first, &item)
	items := call("GET", "/api/app/items?kind=todo", "")
	if !bytes.Contains(items, []byte(item.ID)) {
		t.Fatal("missing item")
	}
	op = inbox.Operation{ID: uuid.NewString(), ItemID: item.ID, Type: "delete", ExpectedRevision: item.Revision}
	raw, _ = json.Marshal(op)
	call("POST", "/api/app/operations", string(raw))
	if bytes.Contains(call("GET", "/api/app/items", ""), []byte(item.ID)) {
		t.Fatal("deleted item listed")
	}
	snap, err := store.Snapshot(context.Background(), "", nil, "", 100)
	if err != nil || len(snap.Items) != 1 || snap.Items[0].DeletedAt == "" {
		t.Fatal("admin deletion missing from device sync", err)
	}
	// The bearer-token listener never exposes the admin handlers.
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/api/app/devices", nil))
	if w.Code != 404 {
		t.Fatal(w.Code)
	}
}
