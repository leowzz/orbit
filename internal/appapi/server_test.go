package appapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/png"
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

func TestAuthenticatedTwoDeviceFlow(t *testing.T) {
	store, err := inbox.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	tokens := map[string]string{"a": strings.Repeat("a", 32), "b": strings.Repeat("b", 32), "revoked": strings.Repeat("r", 32)}
	devices := map[string]config.AppDevice{}
	for id, token := range tokens {
		d := sha256.Sum256([]byte(token))
		devices[id] = config.AppDevice{TokenSHA256: hex.EncodeToString(d[:]), Revoked: id == "revoked"}
	}
	api := New(store, config.AppConfig{Devices: devices, DataDir: t.TempDir()}, nil)
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	request := func(method, path, node string, body []byte) (int, []byte) {
		t.Helper()
		r, _ := http.NewRequest(method, server.URL+path, bytes.NewReader(body))
		if node != "" {
			r.Header.Set("Authorization", "Bearer "+tokens[node])
		}
		resp, e := http.DefaultClient.Do(r)
		if e != nil {
			t.Fatal(e)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, b
	}
	if status, _ := request("GET", "/api/v1/status", "", nil); status != 401 {
		t.Fatal(status)
	}
	if status, _ := request("GET", "/api/v1/status", "revoked", nil); status != 403 {
		t.Fatal(status)
	}
	op := inbox.Operation{ID: uuid.NewString(), ItemID: uuid.NewString(), Type: "create", Kind: "text", Body: "hello"}
	raw, _ := json.Marshal(op)
	status, first := request("POST", "/api/v1/operations", "a", raw)
	if status != 200 {
		t.Fatal(status, string(first))
	}
	_, retry := request("POST", "/api/v1/operations", "a", raw)
	if !bytes.Equal(first, retry) {
		t.Fatal("receipt changed")
	}
	status, b := request("GET", "/api/v1/sync/snapshot", "b", nil)
	var page inbox.Page
	if err = json.Unmarshal(b, &page); err != nil || status != 200 || len(page.Items) != 1 || page.Items[0].CreatedBy != "a" {
		t.Fatal(status, string(b), err)
	}
	// The handler rejects identity injection rather than trusting a JSON node_id.
	raw = []byte(strings.TrimSuffix(string(raw), "}") + `,"node_id":"b"}`)
	if status, _ = request("POST", "/api/v1/operations", "a", raw); status != 400 {
		t.Fatal(status)
	}
	// Every SSE connection immediately sends a durable watermark, even if the
	// write happened before subscription. The client must still fetch changes.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r, _ := http.NewRequestWithContext(ctx, "GET", server.URL+"/api/v1/events", nil)
	r.Header.Set("Authorization", "Bearer "+tokens["b"])
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	data := make([]byte, 256)
	n, err := resp.Body.Read(data)
	resp.Body.Close()
	if err != nil || !strings.Contains(string(data[:n]), `"cursor":"1"`) {
		t.Fatal(string(data[:n]), err)
	}
	// Unreferenced uploads are private to their owner; valid item references
	// make them available to the shared inbox.
	var buf bytes.Buffer
	_ = png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 16, 12)))
	status, b = request("POST", "/api/v1/attachments", "a", buf.Bytes())
	if status != 201 {
		t.Fatal(status, string(b))
	}
	var attachment inbox.Attachment
	_ = json.Unmarshal(b, &attachment)
	if status, _ = request("GET", "/api/v1/attachments/"+attachment.ID, "b", nil); status != 404 {
		t.Fatal(status)
	}
	op = inbox.Operation{ID: uuid.NewString(), ItemID: uuid.NewString(), Type: "create", Kind: "image", AttachmentID: attachment.ID}
	raw, _ = json.Marshal(op)
	if status, b = request("POST", "/api/v1/operations", "b", raw); status != 404 {
		t.Fatal(status, string(b))
	}
	if status, b = request("POST", "/api/v1/operations", "a", raw); status != 200 {
		t.Fatal(status, string(b))
	}
	if status, b = request("GET", "/api/v1/attachments/"+attachment.ID+"?thumbnail=1", "b", nil); status != 200 || len(b) == 0 {
		t.Fatal(status)
	}
}
