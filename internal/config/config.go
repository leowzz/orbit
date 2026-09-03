package config

import "time"

type Duration struct {
	time.Duration
}

type MQTTConfig struct {
	URL         string          `yaml:"url"`
	TLS         MQTTTLSConfig   `yaml:"tls"`
	Credentials MQTTCredentials `yaml:"credentials"`
}

type MQTTTLSConfig struct {
	Enabled  bool   `yaml:"enabled"`
	CAFile   string `yaml:"ca_file"`
	CertFile string `yaml:"cert_file"`
	KeyFile  string `yaml:"key_file"`
}

type MQTTCredentials struct {
	UsernameFile string `yaml:"username_file"`
	PasswordFile string `yaml:"password_file"`
	Username     string `yaml:"-"`
	Password     string `yaml:"-"`
}

type AgentConfig struct {
	Agent        AgentIdentity     `yaml:"agent"`
	MQTT         MQTTConfig        `yaml:"mqtt"`
	Sources      AgentSources      `yaml:"sources"`
	Capabilities AgentCapabilities `yaml:"capabilities"`
	Logging      LoggingConfig     `yaml:"logging"`
}

type AgentIdentity struct {
	ID        string `yaml:"id"`
	HostLabel string `yaml:"host_label"`
}

type AgentSources struct {
	Sub2API Sub2APIConfig `yaml:"sub2api"`
	Codex   CodexConfig   `yaml:"codex"`
}

type AgentCapabilities struct {
	OpenCodexSession CapabilityConfig `yaml:"open_codex_session"`
}

type CapabilityConfig struct {
	Enabled bool `yaml:"enabled"`
}

type Sub2APIConfig struct {
	Enabled         bool               `yaml:"enabled"`
	BaseURL         string             `yaml:"base_url"`
	LoginEndpoint   string             `yaml:"login_endpoint"`
	RefreshEndpoint string             `yaml:"refresh_endpoint"`
	UsageEndpoint   string             `yaml:"usage_endpoint"`
	Timezone        string             `yaml:"timezone"`
	CurrencyCode    string             `yaml:"currency_code"`
	Credentials     Sub2APICredentials `yaml:"credentials"`
	PollInterval    Duration           `yaml:"poll_interval"`
	Timeout         Duration           `yaml:"timeout"`
	ObservationTTL  Duration           `yaml:"observation_ttl"`
}

type Sub2APICredentials struct {
	EmailFile    string `yaml:"email_file"`
	PasswordFile string `yaml:"password_file"`
	Email        string `yaml:"-"`
	Password     string `yaml:"-"`
}

type CodexConfig struct {
	Enabled         bool               `yaml:"enabled"`
	CodexHome       string             `yaml:"codex_home"`
	PollInterval    Duration           `yaml:"poll_interval"`
	ObservationTTL  Duration           `yaml:"observation_ttl"`
	SessionLimit    int                `yaml:"session_limit"`
	IncludeArchived bool               `yaml:"include_archived"`
	Ignore          CodexIgnoreConfig  `yaml:"ignore"`
	Privacy         CodexPrivacyConfig `yaml:"privacy"`
}

type CodexIgnoreConfig struct {
	CWD    []string `yaml:"cwd"`
	Source []string `yaml:"source"`
}

type CodexPrivacyConfig struct {
	IncludeDisplayName bool `yaml:"include_display_name"`
	IncludeProjectName bool `yaml:"include_project_name"`
}

type CoreConfig struct {
	Core                CoreIdentity                 `yaml:"core"`
	MQTT                MQTTConfig                   `yaml:"mqtt"`
	ProjectionRoutes    map[string]ProjectionRoute   `yaml:"projection_routes"`
	ObservationPolicies map[string]ObservationPolicy `yaml:"observation_policies"`
	Logging             LoggingConfig                `yaml:"logging"`
}

type WebNodeConfig struct {
	Node    WebNodeIdentity `yaml:"node"`
	MQTT    MQTTConfig      `yaml:"mqtt"`
	Web     WebConfig       `yaml:"web"`
	Logging LoggingConfig   `yaml:"logging"`
}

type WebNodeIdentity struct {
	ID string `yaml:"id"`
}

type WebConfig struct {
	Listen string `yaml:"listen"`
}

type CoreIdentity struct {
	ID string `yaml:"id"`
}

type ProjectionRoute struct {
	Profile string            `yaml:"profile"`
	Inputs  []ProjectionInput `yaml:"inputs"`
}

type ProjectionInput struct {
	AgentID         string `yaml:"agent_id"`
	ObservationType string `yaml:"observation_type"`
}

type ObservationPolicy struct {
	MaxTTL        Duration `yaml:"max_ttl"`
	MaxFutureSkew Duration `yaml:"max_future_skew"`
}

type LoggingConfig struct {
	Level string `yaml:"level"`
}
