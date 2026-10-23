package mqttadmin

import "testing"

func TestAudioCredentialIsolation(t *testing.T) {
	allowed := func(kind, action, topic string) bool {
		for _, rule := range rulesFor(kind, "device1") {
			if rule.Permission == "allow" && rule.Action == action && rule.Topic == topic {
				return true
			}
		}
		return false
	}
	for _, tc := range []struct {
		kind, action, topic string
		want                bool
	}{
		{"audio-node", "publish", "orbit/v1/nodes/device1/audio/segments", true},
		{"audio-node", "subscribe", "orbit/v1/nodes/device1/audio/acks", true},
		{"audio-node", "subscribe", "orbit/v1/nodes/device1/view", true},
		{"audio-node", "publish", "orbit/v1/nodes/other/audio/segments", false},
		{"audio-node", "subscribe", "orbit/v1/audio/jobs", false},
		{"node", "publish", "orbit/v1/nodes/device1/audio/segments", false},
		{"agent", "subscribe", "orbit/v1/audio/jobs", false},
		{"worker", "subscribe", "orbit/v1/audio/jobs", true},
		{"worker", "publish", "orbit/v1/agents/device1/audio/results", true},
		{"worker", "subscribe", "orbit/v1/agents/device1/audio/acks", true},
		{"worker", "publish", "orbit/v1/audio/jobs", false},
		{"worker", "publish", "orbit/v1/agents/other/audio/results", false},
		{"worker", "subscribe", "orbit/v1/agents/device1/commands", false},
	} {
		if got := allowed(tc.kind, tc.action, tc.topic); got != tc.want {
			t.Errorf("%s %s %s: got %t, want %t", tc.kind, tc.action, tc.topic, got, tc.want)
		}
	}
	for _, kind := range []string{"audio-node", "worker"} {
		rules := rulesFor(kind, "device1")
		if last := rules[len(rules)-1]; last.Permission != "deny" || last.Action != "all" || last.Topic != "#" {
			t.Fatalf("%s has no final deny", kind)
		}
	}
}
