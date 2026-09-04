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

func TestNTPConfigurationDefaultsAndOverrides(t *testing.T) {
	dir := t.TempDir()
	writeFixtureFiles(t, dir, true)

	agent, err := LoadAgent(writeConfig(t, dir, "agent.yaml", validAgentYAML))
	if err != nil {
		t.Fatal(err)
	}
	core, err := LoadCore(writeConfig(t, dir, "core.yaml", validCoreYAML))
	if err != nil {
		t.Fatal(err)
	}
	web, err := LoadWebNode(writeConfig(t, dir, "web.yaml", validWebNodeYAML))
	if err != nil {
		t.Fatal(err)
	}
	for name, ntp := range map[string]NTPConfig{
		"agent": agent.NTP,
		"core":  core.NTP,
		"web":   web.NTP,
	} {
		if ntp.Server != "ntp.aliyun.com" || ntp.SyncInterval.Duration != 10*time.Minute || ntp.Timeout.Duration != 2*time.Second {
			t.Errorf("%s NTP defaults = %#v", name, ntp)
		}
	}

	customYAML := strings.Replace(validAgentYAML, "mqtt:\n", "ntp:\n  server: time.example.com\n  sync_interval: 30m\n  timeout: 5s\nmqtt:\n", 1)
	custom, err := LoadAgent(writeConfig(t, dir, "custom-agent.yaml", customYAML))
	if err != nil {
		t.Fatal(err)
	}
	if custom.NTP.Server != "time.example.com" || custom.NTP.SyncInterval.Duration != 30*time.Minute || custom.NTP.Timeout.Duration != 5*time.Second {
		t.Fatalf("custom NTP config = %#v", custom.NTP)
	}

	serverOnlyYAML := strings.Replace(validAgentYAML, "mqtt:\n", "ntp:\n  server: time.example.com\nmqtt:\n", 1)
	serverOnly, err := LoadAgent(writeConfig(t, dir, "server-only-agent.yaml", serverOnlyYAML))
	if err != nil {
		t.Fatal(err)
	}
	if serverOnly.NTP.Server != "time.example.com" || serverOnly.NTP.SyncInterval.Duration != 10*time.Minute || serverOnly.NTP.Timeout.Duration != 2*time.Second {
		t.Fatalf("server-only NTP config = %#v", serverOnly.NTP)
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
	if !cfg.Capabilities.OpenCodexSession.Enabled {
		t.Fatal("Open Codex session capability was not loaded")
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

func TestLoadAgentRejectsOpenCodexCapabilityWithoutCodexSource(t *testing.T) {
	dir := t.TempDir()
	writeFixtureFiles(t, dir, true)
	yaml := strings.Replace(validAgentYAML, "logging:\n", "capabilities:\n  open_codex_session:\n    enabled: true\nlogging:\n", 1)
	path := writeConfig(t, dir, "agent.yaml", yaml)

	_, err := LoadAgent(path)
	if err == nil || !strings.Contains(err.Error(), "requires sources.codex") {
		t.Fatalf("LoadAgent() error = %v, want Codex source requirement", err)
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

func TestLoadAgentAllowsEmptyTLSCAFile(t *testing.T) {
	dir := t.TempDir()
	writeFixtureFiles(t, dir, true)
	yaml := strings.Replace(validAgentYAML, "    ca_file: ca.pem\n", "    ca_file: \"\"\n", 1)
	path := writeConfig(t, dir, "agent.yaml", yaml)

	cfg, err := LoadAgent(path)
	if err != nil {
		t.Fatalf("LoadAgent() error = %v", err)
	}
	if cfg.MQTT.TLS.CAFile != "" {
		t.Fatalf("CAFile = %q, want empty", cfg.MQTT.TLS.CAFile)
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

func TestLoadCoreAcceptsWebProjection(t *testing.T) {
	dir := t.TempDir()
	writeFixtureFiles(t, dir, false)
	path := writeConfig(t, dir, "core.yaml", validWebCoreYAML)
	cfg, err := LoadCore(path)
	if err != nil {
		t.Fatalf("LoadCore() error = %v", err)
	}
	route := cfg.ProjectionRoutes["desk-web-01"]
	if route.Profile != "overview-web" || len(route.Inputs) != 2 || cfg.ObservationPolicies["codex"].MaxTTL.Duration != time.Minute {
		t.Fatalf("unexpected web route: %#v", route)
	}
}

func TestLoadWebNode(t *testing.T) {
	dir := t.TempDir()
	writeFixtureFiles(t, dir, false)
	path := writeConfig(t, dir, "web.yaml", validWebNodeYAML)
	cfg, err := LoadWebNode(path)
	if err != nil {
		t.Fatalf("LoadWebNode() error = %v", err)
	}
	if cfg.Node.ID != "desk-web-01" || cfg.Web.Listen != "127.0.0.1:8080" || cfg.MQTT.Credentials.Username != "agent-user" {
		t.Fatalf("unexpected web config: %#v", cfg)
	}
	if cfg.Web.Auth.Password != "web-secret" || cfg.Web.Auth.SessionTTL.Duration != 12*time.Hour {
		t.Fatalf("unexpected web auth config: %#v", cfg.Web.Auth)
	}
}

func TestLoadWebNodeRejectsInvalidAuthConfiguration(t *testing.T) {
	tests := []struct {
		name    string
		replace string
		with    string
		want    string
	}{
		{name: "missing password", replace: "password: web-secret", with: "password: \"\"", want: "web.auth.password"},
		{name: "non-positive session ttl", replace: "session_ttl: 12h", with: "session_ttl: 0s", want: "web.auth.session_ttl"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFixtureFiles(t, dir, false)
			yaml := strings.Replace(validWebNodeYAML, tt.replace, tt.with, 1)
			path := writeConfig(t, dir, "web.yaml", yaml)
			if _, err := LoadWebNode(path); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("LoadWebNode() error = %v, want containing %q", err, tt.want)
			}
		})
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
capabilities:
  open_codex_session:
    enabled: true
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

const validWebCoreYAML = `core:
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
  desk-web-01:
    profile: overview-web
    inputs:
      - agent_id: agent-local
        observation_type: usage
      - agent_id: agent-local
        observation_type: codex
observation_policies:
  usage:
    max_ttl: 5m
    max_future_skew: 10s
  codex:
    max_ttl: 1m
    max_future_skew: 10s
logging:
  level: info
`

const validWebNodeYAML = `node:
  id: desk-web-01
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
web:
  listen: 127.0.0.1:8080
  auth:
    password: web-secret
    session_ttl: 12h
logging:
  level: info
`
