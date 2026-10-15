package appapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http/httptest"
	"strings"
	"testing"

	"orbit/internal/config"
	"orbit/internal/inbox"
)

func TestSessionOpenUsesAuthenticatedDevice(t *testing.T) {
	store, err := inbox.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	token := strings.Repeat("a", 32)
	digest := sha256.Sum256([]byte(token))
	api, err := New(store, config.AppConfig{Devices: map[string]config.AppDevice{
		"phone": {TokenSHA256: hex.EncodeToString(digest[:])},
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	api.SetSessionOpener(func(ctx context.Context, node, session, epoch string, revision uint64) (string, error) {
		calls++
		if node != "phone" || session != "session-a" || epoch != "epoch" || revision != 42 {
			t.Fatalf("unexpected request: %s %s %s %d", node, session, epoch, revision)
		}
		return "intent-a", nil
	})
	handler := api.Handler()
	for _, tc := range []struct {
		body, token string
		status      int
	}{
		{`{"core_epoch":"epoch","view_revision":"42"}`, token, 202},
		{`{"core_epoch":"epoch","view_revision":"42"}`, "", 401},
		{`{"core_epoch":"epoch","view_revision":"42","node_id":"other"}`, token, 400},
		{`{"core_epoch":"epoch","view_revision":"0"}`, token, 400},
		{`{"view_revision":"42"}`, token, 400},
		{`{"core_epoch":"epoch","view_revision":"42"} {}`, token, 400},
	} {
		req := httptest.NewRequest("POST", "/api/v1/sessions/session-a/open", strings.NewReader(tc.body))
		req.Header.Set("Authorization", "Bearer "+tc.token)
		result := httptest.NewRecorder()
		handler.ServeHTTP(result, req)
		if result.Code != tc.status {
			t.Fatalf("status %d want %d: %s", result.Code, tc.status, result.Body)
		}
	}
	if calls != 1 {
		t.Fatalf("opener called %d times", calls)
	}
	api.SetSessionOpener(nil)
	req := httptest.NewRequest("POST", "/api/v1/sessions/session-a/open", strings.NewReader(`{"core_epoch":"epoch","view_revision":"42"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	result := httptest.NewRecorder()
	handler.ServeHTTP(result, req)
	if result.Code != 503 {
		t.Fatal(result.Code)
	}
}
