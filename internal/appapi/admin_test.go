package appapi

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
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

func TestDeleteSeededDevicesPreservesMessagesAndSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	store, err := inbox.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	token := strings.Repeat("d", 32)
	digest := sha256.Sum256([]byte(token))
	cfg := config.AppConfig{DataDir: dir, Devices: map[string]config.AppDevice{
		"seed":    {TokenSHA256: hex.EncodeToString(digest[:])},
		"retired": {TokenSHA256: strings.Repeat("a", 64), Revoked: true},
	}}
	s, err := New(store, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	item, err := store.Apply(context.Background(), "seed", inbox.Operation{ID: uuid.NewString(), ItemID: uuid.NewString(), Type: "create", Kind: "text", Body: "keep this message"})
	if err != nil {
		t.Fatal(err)
	}
	public := httptest.NewServer(s.Handler())
	defer public.Close()
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
	for _, id := range []string{"seed", "retired"} {
		w := httptest.NewRecorder()
		s.AdminHandler().ServeHTTP(w, httptest.NewRequest("DELETE", "/api/app/devices/"+id, nil))
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	done := make(chan struct{})
	go func() { _, _ = io.Copy(io.Discard, reader); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("deleted device SSE remained open")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
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
	for _, api := range []*Server{s, restarted} {
		listed := httptest.NewRecorder()
		api.AdminHandler().ServeHTTP(listed, httptest.NewRequest("GET", "/api/app/devices", nil))
		if !bytes.Contains(listed.Body.Bytes(), []byte(`"devices":[]`)) {
			t.Fatal("deleted device returned", listed.Body.String())
		}
		for _, cookie := range []bool{false, true} {
			r := httptest.NewRequest("GET", "/api/v1/status", nil)
			if cookie {
				r.AddCookie(&http.Cookie{Name: browserCookie, Value: token})
			} else {
				r.Header.Set("Authorization", "Bearer "+token)
			}
			w := httptest.NewRecorder()
			api.Handler().ServeHTTP(w, r)
			if w.Code != 401 {
				t.Fatal("deleted credential accepted", w.Code)
			}
		}
	}
	page, err := reopened.Snapshot(context.Background(), "", nil, "", 100)
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != item.ID || page.Items[0].DeletedAt != "" {
		t.Fatal("device deletion changed shared messages", err)
	}
}

func TestDeleteDeviceStorageFailureKeepsAuthorization(t *testing.T) {
	store, err := inbox.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(store, config.AppConfig{Devices: map[string]config.AppDevice{"seed": {Label: "keep"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = store.Close()
	w := httptest.NewRecorder()
	s.AdminHandler().ServeHTTP(w, httptest.NewRequest("DELETE", "/api/app/devices/seed", nil))
	if w.Code != 503 || s.devices["seed"].Label != "keep" {
		t.Fatal("failed persistence removed the device", w.Code)
	}
}

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
	json.Unmarshal(call("POST", "/api/app/operations", string(raw)), &item)
	if bytes.Contains(call("GET", "/api/app/items", ""), []byte(item.ID)) {
		t.Fatal("deleted item listed")
	}
	snap, err := store.Snapshot(context.Background(), "", nil, "", 100)
	if err != nil || len(snap.Items) != 1 || snap.Items[0].DeletedAt == "" {
		t.Fatal("admin deletion missing from device sync", err)
	}
	if !bytes.Contains(call("GET", "/api/app/items?deleted=true&kind=todo&q=管理台", ""), []byte(item.ID)) {
		t.Fatal("deleted item missing from console")
	}
	op = inbox.Operation{ID: uuid.NewString(), ItemID: item.ID, Type: "restore", ExpectedRevision: item.Revision}
	raw, _ = json.Marshal(op)
	restored := call("POST", "/api/app/operations", string(raw))
	if !bytes.Equal(restored, call("POST", "/api/app/operations", string(raw))) {
		t.Fatal("restore retry not idempotent")
	}
	if bytes.Contains(call("GET", "/api/app/items?deleted=true", ""), []byte(item.ID)) || !bytes.Contains(call("GET", "/api/app/items", ""), []byte(item.ID)) {
		t.Fatal("restored item in wrong list")
	}
	changes, err := store.Changes(context.Background(), snap.Generation, snap.Cursor, 100)
	if err != nil || len(changes.Changes) != 1 || changes.Changes[0].Item.DeletedAt != "" {
		t.Fatal("restore missing from device sync", err)
	}
	// The bearer-token listener never exposes the admin handlers.
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/api/app/devices", nil))
	if w.Code != 404 {
		t.Fatal(w.Code)
	}
}
