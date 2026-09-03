package mqtt

import (
	"testing"
	"time"
)

func TestTopicMatches(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		filter string
		topic  string
		want   bool
	}{
		{name: "exact", filter: "orbit/v1/nodes/a/view", topic: "orbit/v1/nodes/a/view", want: true},
		{name: "single wildcard", filter: "orbit/v1/agents/+/observations/usage", topic: "orbit/v1/agents/a/observations/usage", want: true},
		{name: "multi wildcard", filter: "orbit/#", topic: "orbit/v1/nodes/a/view", want: true},
		{name: "different", filter: "orbit/v1/nodes/+/state", topic: "orbit/v1/nodes/a/view", want: false},
		{name: "missing segment", filter: "orbit/v1/+", topic: "orbit/v1", want: false},
		{name: "hash only at end", filter: "orbit/#/view", topic: "orbit/a/view", want: false},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := topicMatches(test.filter, test.topic); got != test.want {
				t.Fatalf("topicMatches(%q, %q) = %v, want %v", test.filter, test.topic, got, test.want)
			}
		})
	}
}

func TestConnectionConfigRejectsTLSDowngrade(t *testing.T) {
	t.Parallel()
	_, _, err := connectionConfig(Config{
		URL:      "mqtt://broker.example:1883",
		ClientID: "agent-a",
		TLS:      TLSConfig{Enabled: true},
	})
	if err == nil {
		t.Fatal("connectionConfig accepted plaintext URL with TLS enabled")
	}
}

func TestConnectionConfigAllowsExplicitPlaintext(t *testing.T) {
	t.Parallel()
	serverURL, tlsConfig, err := connectionConfig(Config{
		URL:      "mqtt://localhost:1883",
		ClientID: "agent-a",
		TLS:      TLSConfig{Enabled: false},
	})
	if err != nil {
		t.Fatalf("connectionConfig returned error: %v", err)
	}
	if serverURL.Scheme != "mqtt" || tlsConfig != nil {
		t.Fatalf("unexpected connection config: url=%s tls=%v", serverURL, tlsConfig)
	}
}

func TestReconnectBackoff(t *testing.T) {
	t.Parallel()
	backoff := reconnectBackoff()
	if got := backoff(0); got != 0 {
		t.Fatalf("initial backoff = %v, want 0", got)
	}
	if got := backoff(1); got < time.Second || got > 2*time.Second {
		t.Fatalf("first retry backoff = %v, want 1s..2s", got)
	}
}
