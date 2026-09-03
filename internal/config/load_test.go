package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadAgent(t *testing.T) {
	dir := t.TempDir()
	writeFixtureFiles(t, dir, true)
	path := writeConfig(t, dir, "agent.yaml", validAgentYAML)

	cfg, err := LoadAgent(path)
	if err != nil {
		t.Fatalf("LoadAgent() error = %v", err)
	}

	if got := cfg.Sources.Sub2API.LoginEndpoint; got != "https://sub2api.example.com/api/v1/auth/login" {
		t.Errorf("LoginEndpoint = %q", got)
	}
	if got := cfg.Sources.Sub2API.UsageEndpoint; got != "https://sub2api.example.com/api/v1/usage/dashboard/stats" {
		t.Errorf("UsageEndpoint = %q", got)
	}
	if got := cfg.Sources.Sub2API.Credentials.Email; got != "operator@example.com" {
		t.Errorf("Email = %q", got)
	}
	if got := cfg.MQTT.Credentials.Username; got != "agent-user" {
		t.Errorf("MQTT username = %q", got)
	}
	if !filepath.IsAbs(cfg.MQTT.TLS.CAFile) {
		t.Errorf("CAFile = %q, want absolute path", cfg.MQTT.TLS.CAFile)
	}
	if got := cfg.Sources.Sub2API.PollInterval.Duration; got != 30*time.Second {
		t.Errorf("PollInterval = %v", got)
	}
}

func TestLoadAgentRejectsInvalidConfiguration(t *testing.T) {
	tests := []struct {
		name    string
		replace string
		with    string
		want    string
	}{
		{name: "unknown field", replace: "  host_label: Local workstation", with: "  host_label: Local workstation\n  unexpected: true", want: "field unexpected not found"},
		{name: "non HTTPS source", replace: "base_url: https://sub2api.example.com", with: "base_url: http://sub2api.example.com", want: "absolute HTTPS URL"},
		{name: "invalid timezone", replace: "timezone: Asia/Shanghai", with: "timezone: Mars/Olympus", want: "valid IANA location"},
		{name: "lowercase currency", replace: "currency_code: USD", with: "currency_code: usd", want: "must be USD in V1"},
		{name: "unsupported currency", replace: "currency_code: USD", with: "currency_code: EUR", want: "must be USD in V1"},
		{name: "short TTL", replace: "observation_ttl: 2m", with: "observation_ttl: 10s", want: "at least poll_interval"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFixtureFiles(t, dir, true)
			path := writeConfig(t, dir, "agent.yaml", strings.Replace(validAgentYAML, tt.replace, tt.with, 1))
			_, err := LoadAgent(path)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("LoadAgent() error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestLoadAgentDefaultsTLSToEnabled(t *testing.T) {
	dir := t.TempDir()
	writeFixtureFiles(t, dir, true)
	yaml := strings.Replace(validAgentYAML, "    enabled: true\n", "", 1)
	path := writeConfig(t, dir, "agent.yaml", yaml)

	cfg, err := LoadAgent(path)
	if err != nil {
		t.Fatalf("LoadAgent() error = %v", err)
	}
	if !cfg.MQTT.TLS.Enabled {
		t.Fatal("TLS must default to enabled")
	}
}

func TestLoadCore(t *testing.T) {
	dir := t.TempDir()
	writeFixtureFiles(t, dir, false)
	path := writeConfig(t, dir, "core.yaml", validCoreYAML)

	cfg, err := LoadCore(path)
	if err != nil {
		t.Fatalf("LoadCore() error = %v", err)
	}

	route := cfg.ProjectionRoutes["desk-oled-01"]
	if route.Profile != "usage-oled-128x32" || len(route.Inputs) != 1 {
		t.Fatalf("route = %#v", route)
	}
	if got := cfg.ObservationPolicies["usage"].MaxTTL.Duration; got != 5*time.Minute {
		t.Errorf("MaxTTL = %v", got)
	}
}

func TestLoadCoreRejectsDuplicateObservationInput(t *testing.T) {
	dir := t.TempDir()
	writeFixtureFiles(t, dir, false)
	yaml := strings.Replace(validCoreYAML, "        observation_type: usage", "        observation_type: usage\n      - agent_id: other-agent\n        observation_type: usage", 1)
	path := writeConfig(t, dir, "core.yaml", yaml)

	_, err := LoadCore(path)
	if err == nil || !strings.Contains(err.Error(), "is duplicated") {
		t.Fatalf("LoadCore() error = %v, want duplicate error", err)
	}
}

func writeFixtureFiles(t *testing.T, dir string, includeSub2API bool) {
	t.Helper()
	files := map[string]string{
		"ca.pem":        "test CA",
		"client.pem":    "test certificate",
		"client.key":    "test key",
		"mqtt-username": "agent-user\n",
		"mqtt-password": "mqtt-secret\n",
	}
	if includeSub2API {
		files["sub2api-email"] = "operator@example.com\n"
		files["sub2api-password"] = "sub2api-secret\n"
	}
	for name, contents := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func writeConfig(t *testing.T, dir, name, contents string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

const validAgentYAML = `agent:
  id: agent-local
  host_label: Local workstation
mqtt:
  url: mqtts://broker.example.com:8883
  tls:
    enabled: true
    ca_file: ca.pem
    cert_file: client.pem
    key_file: client.key
  credentials:
    username_file: mqtt-username
    password_file: mqtt-password
sources:
  sub2api:
    enabled: true
    base_url: https://sub2api.example.com
    timezone: Asia/Shanghai
    currency_code: USD
    credentials:
      email_file: sub2api-email
      password_file: sub2api-password
    poll_interval: 30s
    timeout: 10s
    observation_ttl: 2m
logging:
  level: info
`

const validCoreYAML = `core:
  id: core-local
mqtt:
  url: mqtts://broker.example.com:8883
  tls:
    enabled: true
    ca_file: ca.pem
    cert_file: client.pem
    key_file: client.key
  credentials:
    username_file: mqtt-username
    password_file: mqtt-password
projection_routes:
  desk-oled-01:
    profile: usage-oled-128x32
    inputs:
      - agent_id: agent-local
        observation_type: usage
observation_policies:
  usage:
    max_ttl: 5m
    max_future_skew: 10s
logging:
  level: info
`
