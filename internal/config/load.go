package config

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.ScalarNode || node.Tag != "!!str" {
		return errors.New("duration must be a string such as 30s or 5m")
	}

	parsed, err := time.ParseDuration(node.Value)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", node.Value, err)
	}
	d.Duration = parsed
	return nil
}

func LoadAgent(path string) (*AgentConfig, error) {
	cfg := AgentConfig{
		MQTT:    MQTTConfig{TLS: MQTTTLSConfig{Enabled: true}},
		Logging: LoggingConfig{Level: "info"},
	}
	if err := decodeStrict(path, &cfg); err != nil {
		return nil, err
	}
	if err := cfg.validate(filepath.Dir(path)); err != nil {
		return nil, fmt.Errorf("validate agent config: %w", err)
	}
	return &cfg, nil
}

func LoadCore(path string) (*CoreConfig, error) {
	cfg := CoreConfig{
		MQTT:    MQTTConfig{TLS: MQTTTLSConfig{Enabled: true}},
		Logging: LoggingConfig{Level: "info"},
	}
	if err := decodeStrict(path, &cfg); err != nil {
		return nil, err
	}
	if err := cfg.validate(filepath.Dir(path)); err != nil {
		return nil, fmt.Errorf("validate core config: %w", err)
	}
	return &cfg, nil
}

func LoadWebNode(path string) (*WebNodeConfig, error) {
	cfg := WebNodeConfig{
		MQTT:    MQTTConfig{TLS: MQTTTLSConfig{Enabled: true}},
		Web:     WebConfig{Listen: "127.0.0.1:8080"},
		Logging: LoggingConfig{Level: "info"},
	}
	if err := decodeStrict(path, &cfg); err != nil {
		return nil, err
	}
	if err := cfg.validate(filepath.Dir(path)); err != nil {
		return nil, fmt.Errorf("validate web node config: %w", err)
	}
	return &cfg, nil
}

func decodeStrict(path string, dst any) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open config: %w", err)
	}
	defer file.Close()

	decoder := yaml.NewDecoder(file)
	decoder.KnownFields(true)
	if err := decoder.Decode(dst); err != nil {
		return fmt.Errorf("decode config: %w", err)
	}

	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err != nil {
			return fmt.Errorf("decode trailing document: %w", err)
		}
		return errors.New("decode config: multiple YAML documents are not allowed")
	}
	return nil
}

func (cfg *AgentConfig) validate(baseDir string) error {
	if cfg.Agent.ID != "" {
		if err := validateID("agent.id", cfg.Agent.ID); err != nil {
			return err
		}
	}
	if strings.TrimSpace(cfg.Agent.HostLabel) == "" {
		return errors.New("agent.host_label is required")
	}
	if !cfg.Sources.Sub2API.Enabled && !cfg.Sources.Codex.Enabled {
		return errors.New("at least one of sources.sub2api or sources.codex must be enabled")
	}
	if err := cfg.MQTT.validate(baseDir); err != nil {
		return fmt.Errorf("mqtt: %w", err)
	}
	if cfg.Sources.Sub2API.Enabled {
		if err := cfg.Sources.Sub2API.validate(baseDir); err != nil {
			return fmt.Errorf("sources.sub2api: %w", err)
		}
	}
	if cfg.Sources.Codex.Enabled {
		if err := cfg.Sources.Codex.validate(baseDir); err != nil {
			return fmt.Errorf("sources.codex: %w", err)
		}
	}
	return validateLogLevel(cfg.Logging.Level)
}

func (cfg *CoreConfig) validate(baseDir string) error {
	if err := validateID("core.id", cfg.Core.ID); err != nil {
		return err
	}
	if err := cfg.MQTT.validate(baseDir); err != nil {
		return fmt.Errorf("mqtt: %w", err)
	}
	if len(cfg.ProjectionRoutes) == 0 {
		return errors.New("projection_routes must contain at least one route")
	}

	for nodeID, route := range cfg.ProjectionRoutes {
		if err := validateID("projection route node_id", nodeID); err != nil {
			return err
		}
		if route.Profile != "usage-oled-128x32" && route.Profile != "overview-web" {
			return fmt.Errorf("projection route %q: unsupported profile %q", nodeID, route.Profile)
		}
		if len(route.Inputs) == 0 {
			return fmt.Errorf("projection route %q: inputs must not be empty", nodeID)
		}
		seen := make(map[string]struct{}, len(route.Inputs))
		for i, input := range route.Inputs {
			if err := validateID("agent_id", input.AgentID); err != nil {
				return fmt.Errorf("projection route %q input %d: %w", nodeID, i, err)
			}
			if input.ObservationType != "usage" && input.ObservationType != "codex" {
				return fmt.Errorf("projection route %q input %d: unsupported observation_type %q", nodeID, i, input.ObservationType)
			}
			if _, ok := seen[input.ObservationType]; ok {
				return fmt.Errorf("projection route %q: observation_type %q is duplicated", nodeID, input.ObservationType)
			}
			seen[input.ObservationType] = struct{}{}
		}
		if route.Profile == "usage-oled-128x32" && (len(route.Inputs) != 1 || route.Inputs[0].ObservationType != "usage") {
			return fmt.Errorf("projection route %q: usage-oled-128x32 requires exactly one usage input", nodeID)
		}
	}

	requiredPolicies := make(map[string]struct{})
	for _, route := range cfg.ProjectionRoutes {
		for _, input := range route.Inputs {
			requiredPolicies[input.ObservationType] = struct{}{}
		}
	}
	for name := range requiredPolicies {
		policy, ok := cfg.ObservationPolicies[name]
		if !ok {
			return fmt.Errorf("observation_policies.%s is required", name)
		}
		if policy.MaxTTL.Duration <= 0 {
			return fmt.Errorf("observation_policies.%s.max_ttl must be positive", name)
		}
		if policy.MaxFutureSkew.Duration < 0 {
			return fmt.Errorf("observation_policies.%s.max_future_skew must not be negative", name)
		}
	}
	for name := range cfg.ObservationPolicies {
		if name != "usage" && name != "codex" {
			return fmt.Errorf("observation_policies contains unsupported type %q", name)
		}
	}
	return validateLogLevel(cfg.Logging.Level)
}

func (cfg *WebNodeConfig) validate(baseDir string) error {
	if err := validateID("node.id", cfg.Node.ID); err != nil {
		return err
	}
	if err := cfg.MQTT.validate(baseDir); err != nil {
		return fmt.Errorf("mqtt: %w", err)
	}
	host, port, err := net.SplitHostPort(cfg.Web.Listen)
	if err != nil || strings.TrimSpace(host) == "" || strings.TrimSpace(port) == "" {
		return fmt.Errorf("web.listen must be a host:port address: %q", cfg.Web.Listen)
	}
	return validateLogLevel(cfg.Logging.Level)
}

func (cfg *MQTTConfig) validate(baseDir string) error {
	u, err := url.Parse(cfg.URL)
	if err != nil || u.Host == "" || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("url must be an absolute broker URL without credentials, path, query, or fragment: %q", cfg.URL)
	}
	wantScheme := "mqtt"
	if cfg.TLS.Enabled {
		wantScheme = "mqtts"
	}
	if u.Scheme != wantScheme {
		return fmt.Errorf("url scheme must be %q when tls.enabled is %t", wantScheme, cfg.TLS.Enabled)
	}

	if cfg.TLS.Enabled {
		if cfg.TLS.CAFile == "" {
			return errors.New("tls.ca_file is required when TLS is enabled")
		}
		if err := resolveReadableFile(baseDir, &cfg.TLS.CAFile); err != nil {
			return fmt.Errorf("tls.ca_file: %w", err)
		}
	} else if cfg.TLS.CAFile != "" || cfg.TLS.CertFile != "" || cfg.TLS.KeyFile != "" {
		return errors.New("tls certificate files must be empty when TLS is disabled")
	}
	if (cfg.TLS.CertFile == "") != (cfg.TLS.KeyFile == "") {
		return errors.New("tls.cert_file and tls.key_file must be configured together")
	}
	if cfg.TLS.CertFile != "" {
		if err := resolveReadableFile(baseDir, &cfg.TLS.CertFile); err != nil {
			return fmt.Errorf("tls.cert_file: %w", err)
		}
		if err := resolveReadableFile(baseDir, &cfg.TLS.KeyFile); err != nil {
			return fmt.Errorf("tls.key_file: %w", err)
		}
	}

	username, err := readSecret(baseDir, &cfg.Credentials.UsernameFile)
	if err != nil {
		return fmt.Errorf("credentials.username_file: %w", err)
	}
	password, err := readSecret(baseDir, &cfg.Credentials.PasswordFile)
	if err != nil {
		return fmt.Errorf("credentials.password_file: %w", err)
	}
	cfg.Credentials.Username = username
	cfg.Credentials.Password = password
	return nil
}

func (cfg *Sub2APIConfig) validate(baseDir string) error {
	if cfg.Timeout.Duration <= 0 {
		return errors.New("timeout must be positive")
	}
	if cfg.PollInterval.Duration <= 0 {
		return errors.New("poll_interval must be positive")
	}
	if cfg.ObservationTTL.Duration <= 0 {
		return errors.New("observation_ttl must be positive")
	}
	if cfg.ObservationTTL.Duration < cfg.PollInterval.Duration {
		return errors.New("observation_ttl must be at least poll_interval")
	}
	if cfg.Timezone == "" {
		return errors.New("timezone is required")
	}
	if _, err := time.LoadLocation(cfg.Timezone); err != nil {
		return fmt.Errorf("timezone must be a valid IANA location: %w", err)
	}
	if cfg.CurrencyCode != "USD" {
		return errors.New("currency_code must be USD in V1")
	}

	base, err := parseHTTPSURL("base_url", cfg.BaseURL)
	if err != nil {
		return err
	}
	endpoints := []struct {
		name        string
		value       *string
		defaultPath string
	}{
		{name: "login_endpoint", value: &cfg.LoginEndpoint, defaultPath: "/api/v1/auth/login"},
		{name: "refresh_endpoint", value: &cfg.RefreshEndpoint, defaultPath: "/api/v1/auth/refresh"},
		{name: "usage_endpoint", value: &cfg.UsageEndpoint, defaultPath: "/api/v1/usage/dashboard/stats"},
	}
	for _, endpoint := range endpoints {
		if *endpoint.value == "" {
			resolved := *base
			resolved.Path = strings.TrimRight(base.Path, "/") + endpoint.defaultPath
			*endpoint.value = resolved.String()
		}
		if _, err := parseHTTPSURL(endpoint.name, *endpoint.value); err != nil {
			return err
		}
	}

	email, err := readSecret(baseDir, &cfg.Credentials.EmailFile)
	if err != nil {
		return fmt.Errorf("credentials.email_file: %w", err)
	}
	password, err := readSecret(baseDir, &cfg.Credentials.PasswordFile)
	if err != nil {
		return fmt.Errorf("credentials.password_file: %w", err)
	}
	cfg.Credentials.Email = email
	cfg.Credentials.Password = password
	return nil
}

func (cfg *CodexConfig) validate(baseDir string) error {
	if cfg.PollInterval.Duration <= 0 {
		return errors.New("poll_interval must be positive")
	}
	if cfg.ObservationTTL.Duration <= 0 {
		return errors.New("observation_ttl must be positive")
	}
	if cfg.ObservationTTL.Duration < cfg.PollInterval.Duration {
		return errors.New("observation_ttl must be at least poll_interval")
	}
	if cfg.SessionLimit < 1 || cfg.SessionLimit > 20 {
		return errors.New("session_limit must be between 1 and 20")
	}
	if cfg.CodexHome != "" {
		if err := resolveReadableDir(baseDir, &cfg.CodexHome); err != nil {
			return fmt.Errorf("codex_home: %w", err)
		}
	}
	if err := validateNonEmptyStrings("ignore.cwd", cfg.Ignore.CWD); err != nil {
		return err
	}
	return validateNonEmptyStrings("ignore.source", cfg.Ignore.Source)
}

func parseHTTPSURL(name, raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("%s must be an absolute HTTPS URL without credentials, query, or fragment: %q", name, raw)
	}
	return u, nil
}

func readSecret(baseDir string, path *string) (string, error) {
	if err := resolveReadableFile(baseDir, path); err != nil {
		return "", err
	}
	contents, err := os.ReadFile(*path)
	if err != nil {
		return "", err
	}
	secret := strings.TrimRight(string(contents), "\r\n")
	if secret == "" {
		return "", errors.New("file is empty")
	}
	return secret, nil
}

func resolveReadableFile(baseDir string, path *string) error {
	if *path == "" {
		return errors.New("path is required")
	}
	if !filepath.IsAbs(*path) {
		*path = filepath.Join(baseDir, *path)
	}
	abs, err := filepath.Abs(*path)
	if err != nil {
		return err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("must reference a regular file")
	}
	file, err := os.Open(abs)
	if err != nil {
		return err
	}
	file.Close()
	*path = abs
	return nil
}

func resolveReadableDir(baseDir string, path *string) error {
	if *path == "" {
		return errors.New("path is required")
	}
	if *path == "~" || strings.HasPrefix(*path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		if *path == "~" {
			*path = home
		} else {
			*path = filepath.Join(home, strings.TrimPrefix(*path, "~/"))
		}
	}
	if !filepath.IsAbs(*path) {
		*path = filepath.Join(baseDir, *path)
	}
	abs, err := filepath.Abs(*path)
	if err != nil {
		return err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("must reference a directory")
	}
	directory, err := os.Open(abs)
	if err != nil {
		return err
	}
	directory.Close()
	*path = abs
	return nil
}

func validateNonEmptyStrings(field string, values []string) error {
	for index, value := range values {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s[%d] must not be empty", field, index)
		}
	}
	return nil
}

func validateID(name, value string) error {
	if !idPattern.MatchString(value) {
		return fmt.Errorf("%s must match %s", name, idPattern.String())
	}
	return nil
}

func validateLogLevel(level string) error {
	switch level {
	case "debug", "info", "warn", "error":
		return nil
	default:
		return fmt.Errorf("logging.level must be one of debug, info, warn, error, got %q", level)
	}
}
