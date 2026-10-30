package appapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"orbit/internal/config"
	"orbit/internal/inbox"
	"orbit/internal/mqttadmin"
)

type fakeDeviceMQTT struct {
	users   map[string]string
	nodes   map[string]string
	fail    bool
	warning bool
	agent   bool
}

func (f *fakeDeviceMQTT) ProvisionNode(_ context.Context, username, node, password string) error {
	if f.fail {
		return errors.New("broker unavailable")
	}
	f.users[username], f.nodes[username] = password, node
	return nil
}
func (f *fakeDeviceMQTT) ProvisionAgent(_ context.Context, username, node, password string) error {
	f.agent = true
	return f.ProvisionNode(context.Background(), username, node, password)
}
func (f *fakeDeviceMQTT) Revoke(_ context.Context, username string) (mqttadmin.Result, error) {
	if f.fail {
		return mqttadmin.Result{}, errors.New("broker unavailable")
	}
	if f.warning {
		return mqttadmin.Result{Warning: "disconnect failed"}, nil
	}
	delete(f.users, username)
	return mqttadmin.Result{}, nil
}
func mqttRequest(h http.Handler, method, path, token string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, nil)
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func TestDeviceMQTTProvisionRestoreAndAuthorizationLifecycle(t *testing.T) {
	store, err := inbox.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	token := strings.Repeat("a", 64)
	digest := sha256.Sum256([]byte(token))
	cfg := config.AppConfig{Devices: map[string]config.AppDevice{
		"phone": {Platform: "android", TokenSHA256: hex.EncodeToString(digest[:])},
	}}
	broker := &fakeDeviceMQTT{users: map[string]string{}, nodes: map[string]string{}}
	open := func() *Server {
		s, err := New(store, cfg, nil)
		if err != nil {
			t.Fatal(err)
		}
		s.mqttProvisioner, s.mqttURL, s.mqttSecret = broker, "mqtts://broker.test:8883", "server-secret"
		return s
	}
	s := open()
	claim := func(token string) map[string]string {
		w := mqttRequest(s.Handler(), "POST", "/api/v1/mqtt", token)
		if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("claim status %d", w.Code)
		}
		var result struct {
			Enabled bool              `json:"enabled"`
			MQTT    map[string]string `json:"mqtt"`
		}
		if json.Unmarshal(w.Body.Bytes(), &result) != nil || !result.Enabled {
			t.Fatal("missing credentials")
		}
		if result.MQTT["nodeId"] != "phone" || len(result.MQTT["password"]) < 40 {
			t.Fatal("invalid device scope")
		}
		return result.MQTT
	}
	if w := mqttRequest(s.Handler(), "POST", "/api/v1/mqtt", ""); w.Code != 401 {
		t.Fatal("unauthenticated claim")
	}
	first := claim(token)
	if broker.nodes[first["username"]] != "phone" {
		t.Fatal("wrong node permissions")
	}
	raw, err := store.DeviceRegistry(context.Background(), nil)
	if err != nil || strings.Contains(string(raw), first["password"]) || strings.Contains(string(raw), "server-secret") {
		t.Fatal("secret persisted in device registry")
	}
	// Restored legacy installs have only a token. Core restart must return the
	// same account/password instead of making another user or rotating it.
	s = open()
	second := claim(token)
	if second["username"] != first["username"] || second["password"] != first["password"] || len(broker.users) != 1 {
		t.Fatal("provisioning is not idempotent")
	}
	listed := mqttRequest(s.AdminHandler(), "GET", "/api/app/devices", "")
	if strings.Contains(listed.Body.String(), first["password"]) || strings.Contains(listed.Body.String(), first["username"]) {
		t.Fatal("credentials leaked in list")
	}
	broker.warning = true
	if w := mqttRequest(s.AdminHandler(), "POST", "/api/app/devices/phone/rotate", ""); w.Code != 503 {
		t.Fatal("partial revocation ignored")
	}
	if w := mqttRequest(s.Handler(), "GET", "/api/v1/status", token); w.Code != 200 {
		t.Fatal("failed rotate changed token")
	}
	broker.warning = false
	w := mqttRequest(s.AdminHandler(), "POST", "/api/app/devices/phone/rotate", "")
	if w.Code != 200 || len(broker.users) != 0 {
		t.Fatal("rotation did not retire MQTT credential")
	}
	var rotated map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &rotated)
	if w := mqttRequest(s.Handler(), "POST", "/api/v1/mqtt", token); w.Code != 401 {
		t.Fatal("old token can provision")
	}
	third := claim(rotated["token"])
	if third["username"] == first["username"] || third["password"] == first["password"] {
		t.Fatal("rotated credentials reused")
	}
	if w := mqttRequest(s.AdminHandler(), "POST", "/api/app/devices/phone/revoke", ""); w.Code != 200 || len(broker.users) != 0 {
		t.Fatal("revoke left MQTT access")
	}
	if w := mqttRequest(s.Handler(), "POST", "/api/v1/mqtt", rotated["token"]); w.Code != 403 {
		t.Fatal("revoked device can provision")
	}
}

func TestDeviceMQTTFailedProvisionRetriesAndDeleteCleansReservation(t *testing.T) {
	store, _ := inbox.Open(t.TempDir())
	defer store.Close()
	token := strings.Repeat("b", 64)
	digest := sha256.Sum256([]byte(token))
	s, _ := New(store, config.AppConfig{Devices: map[string]config.AppDevice{
		"old-phone": {TokenSHA256: hex.EncodeToString(digest[:])}, // Legacy registry without platform.
	}}, nil)
	if w := mqttRequest(s.Handler(), "POST", "/api/v1/mqtt", token); w.Code != 200 || !strings.Contains(w.Body.String(), `"enabled":false`) {
		t.Fatal("disabled provisioning broke old connection")
	}
	broker := &fakeDeviceMQTT{users: map[string]string{}, nodes: map[string]string{}, fail: true}
	s.mqttProvisioner, s.mqttSecret = broker, "secret"
	if w := mqttRequest(s.Handler(), "POST", "/api/v1/mqtt", token); w.Code != 503 {
		t.Fatal("failure hidden")
	}
	reserved := s.devices["old-phone"].MQTTUsername
	if reserved == "" {
		t.Fatal("missing reservation")
	}
	broker.fail = false
	if w := mqttRequest(s.Handler(), "POST", "/api/v1/mqtt", token); w.Code != 200 || s.devices["old-phone"].MQTTUsername != reserved {
		t.Fatal("reservation not reused")
	}
	if w := mqttRequest(s.AdminHandler(), "DELETE", "/api/app/devices/old-phone", ""); w.Code != 200 || len(broker.users) != 0 {
		t.Fatal("delete left MQTT access")
	}
	if w := mqttRequest(s.Handler(), "POST", "/api/v1/mqtt", token); w.Code != 401 {
		t.Fatal("deleted device can provision")
	}
}

func TestConfigureMQTTUsesDeviceReachableListener(t *testing.T) {
	for _, test := range []struct{ node, core, want string }{
		{"mqtts://public.test", "mqtt://emqx:1883", "mqtts://public.test:8883"},
		{"", "mqtt://broker.test:1884", "mqtt://broker.test:1884"},
		{"", "mqtt://broker.test", "mqtt://broker.test:1883"},
	} {
		s := &Server{}
		cfg := &config.CoreConfig{MQTT: config.MQTTConfig{URL: test.core}, MQTTAdmin: config.MQTTAdminConfig{URL: "http://emqx:18083", NodeURL: test.node, APIKey: "key", APISecret: "secret"}}
		s.ConfigureMQTT(cfg)
		if s.mqttURL != test.want || s.mqttProvisioner == nil {
			t.Fatal("wrong MQTT listener")
		}
	}
}

func TestDeviceRegistrationClaimsOnlyItsOwnCredentials(t *testing.T) {
	store, err := inbox.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	s, err := New(store, config.AppConfig{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	broker := &fakeDeviceMQTT{users: map[string]string{}, nodes: map[string]string{}}
	s.mqttProvisioner, s.mqttURL, s.mqttSecret = broker, "mqtts://broker.test:8883", "secret"
	for _, platform := range []string{"android", "windows", "web"} {
		r := httptest.NewRequest("POST", "/api/app/devices", strings.NewReader(`{"label":"new device","platform":"`+platform+`"}`))
		w := httptest.NewRecorder()
		s.AdminHandler().ServeHTTP(w, r)
		if w.Code != 201 {
			t.Fatalf("create status %d", w.Code)
		}
		var registered map[string]string
		_ = json.Unmarshal(w.Body.Bytes(), &registered)
		r = httptest.NewRequest("POST", "/api/v1/mqtt?node_id=another-device", strings.NewReader(`{"node_id":"another-device"}`))
		r.Header.Set("Authorization", "Bearer "+registered["token"])
		w = httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if platform == "web" {
			if w.Code != 403 {
				t.Fatal("non-mobile account can provision MQTT")
			}
			continue
		}
		if w.Code != 200 {
			t.Fatalf("claim status %d", w.Code)
		}
		var result struct {
			MQTT map[string]string `json:"mqtt"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &result)
		if result.MQTT["nodeId"] != registered["id"] || broker.nodes[result.MQTT["username"]] != registered["id"] {
			t.Fatal("request escaped authenticated device scope")
		}
	}
	if len(broker.users) != 2 {
		t.Fatal("unexpected MQTT account count")
	}
	if !broker.agent {
		t.Fatal("Windows device did not use Agent provisioning")
	}
}
