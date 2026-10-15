package appapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"google.golang.org/protobuf/proto"
	"net/http/httptest"
	orbitv1 "orbit/gen/go/orbit/v1"
	"orbit/internal/config"
	"orbit/internal/inbox"
	"testing"
)

func TestStatusProtobufUsesDeviceAuthorization(t *testing.T) {
	store, err := inbox.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	token := "01234567890123456789012345678901"
	hash := sha256.Sum256([]byte(token))
	available := true
	s, err := New(store, config.AppConfig{Devices: map[string]config.AppDevice{"phone": {TokenSHA256: hex.EncodeToString(hash[:])}}}, func(id string) json.RawMessage {
		if id != "phone" {
			t.Fatalf("unexpected identity %s", id)
		}
		if !available {
			return json.RawMessage(`null`)
		}
		return json.RawMessage(`{"nodeId":"phone","displayKind":"codex","primary":{"text":"50%"}}`)
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		token     string
		available bool
		status    int
	}{{"", true, 401}, {token, true, 200}, {token, false, 204}} {
		available = test.available
		r := httptest.NewRequest("GET", "/api/v1/status", nil)
		r.Header.Set("Authorization", "Bearer "+test.token)
		r.Header.Set("Accept", "application/x-protobuf")
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != test.status {
			t.Fatalf("%d: %s", w.Code, w.Body.String())
		}
		if w.Code == 200 {
			v := &orbitv1.DeviceView{}
			if err := proto.Unmarshal(w.Body.Bytes(), v); err != nil || v.NodeId != "phone" || v.DisplayKind != "codex" {
				t.Fatal(v, err)
			}
		}
	}
}
