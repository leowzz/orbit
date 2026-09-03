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

func TestLoadAgentAcceptsCodexOnlyAndResolvesHome(t *testing.T) {
	dir := t.TempDir()
	writeFixtureFiles(t, dir, false)
	if err := os.Mkdir(filepath.Join(dir, "codex-home"), 0o700); err != nil {
		t.Fatal(err)
	}
	path := writeConfig(t, dir, "agent.yaml", validCodexOnlyYAML)

	cfg, err := LoadAgent(path)
	if err != nil {
		t.Fatalf("LoadAgent() error = %v", err)
	}
	if !filepath.IsAbs(cfg.Sources.Codex.CodexHome) {
		t.Fatalf("CodexHome = %q, want absolute path", cfg.Sources.Codex.CodexHome)
	}
	if want := filepath.Join(dir, "codex-home"); cfg.Sources.Codex.CodexHome != want {
		t.Fatalf("CodexHome = %q, want %q", cfg.Sources.Codex.CodexHome, want)
	}
	if cfg.Sources.Codex.Privacy.IncludeDisplayName || cfg.Sources.Codex.Privacy.IncludeProjectName {
		t.Fatal("Codex privacy fields must default to disabled")
	}
}

func TestLoadAgentAcceptsSub2APIAndCodexTogether(t *testing.T) {
	dir := t.TempDir()
	writeFixtureFiles(t, dir, true)
	if err := os.Mkdir(filepath.Join(dir, "codex-home"), 0o700); err != nil {
		t.Fatal(err)
	}
	codexYAML := `  codex:
    enabled: true
    codex_home: codex-home
    poll_interval: 5s
    observation_ttl: 15s
    session_limit: 3
    include_archived: false
    ignore:
      cwd: []
      source: []
    privacy:
      include_display_name: false
      include_project_name: false
`
	yaml := strings.Replace(validAgentYAML, "logging:\n", codexYAML+"logging:\n", 1)
	path := writeConfig(t, dir, "agent.yaml", yaml)

	cfg, err := LoadAgent(path)
	if err != nil {
		t.Fatalf("LoadAgent() error = %v", err)
	}
	if !cfg.Sources.Sub2API.Enabled || !cfg.Sources.Codex.Enabled {
		t.Fatalf("sources = %#v, want Sub2API and Codex enabled", cfg.Sources)
	}
}

func TestLoadAgentRejectsWhenAllSourcesAreDisabled(t *testing.T) {
	dir := t.TempDir()
	writeFixtureFiles(t, dir, true)
	yaml := strings.Replace(validAgentYAML, "  sub2api:\n    enabled: true", "  sub2api:\n    enabled: false", 1)
	path := writeConfig(t, dir, "agent.yaml", yaml)

	_, err := LoadAgent(path)
	if err == nil || !strings.Contains(err.Error(), "at least one of sources.sub2api or sources.codex") {
		t.Fatalf("LoadAgent() error = %v, want source requirement", err)
	}
}

func TestLoadAgentRejectsInvalidCodexConfiguration(t *testing.T) {
	tests := []struct {
		name    string
		replace string
		with    string
		want    string
	}{
		{name: "poll interval", replace: "poll_interval: 5s", with: "poll_interval: 0s", want: "poll_interval"},
		{name: "observation ttl", replace: "observation_ttl: 15s", with: "observation_ttl: 1s", want: "observation_ttl"},
		{name: "session limit too low", replace: "session_limit: 3", with: "session_limit: 0", want: "session_limit"},
		{name: "session limit too high", replace: "session_limit: 3", with: "session_limit: 21", want: "session_limit"},
		{name: "missing home", replace: "codex_home: codex-home", with: "codex_home: missing", want: "codex_home"},
		{name: "empty cwd ignore", replace: "cwd: []", with: "cwd: [\"\"]", want: "ignore.cwd"},
		{name: "empty source ignore", replace: "source: []", with: "source: [\"\"]", want: "ignore.source"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFixtureFiles(t, dir, false)
			if err := os.Mkdir(filepath.Join(dir, "codex-home"), 0o700); err != nil {
				t.Fatal(err)
			}
			yaml := strings.Replace(validCodexOnlyYAML, tt.replace, tt.with, 1)
			path := writeConfig(t, dir, "agent.yaml", yaml)

			_, err := LoadAgent(path)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("LoadAgent() error = %v, want containing %q", err, tt.want)
			}
		})
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

const validCodexOnlyYAML = `agent:
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
  codex:
    enabled: true
    codex_home: codex-home
    poll_interval: 5s
    observation_ttl: 15s
    session_limit: 3
    include_archived: false
    ignore:
      cwd: []
      source: []
    privacy:
      include_display_name: false
      include_project_name: false
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
