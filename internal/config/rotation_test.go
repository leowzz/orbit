package config

import (
	"testing"
	"time"
)

func TestRotatingOLEDValidation(t *testing.T) {
	cfg := CoreConfig{ObservationPolicies: map[string]ObservationPolicy{
		"usage": {MaxTTL: Duration{Duration: time.Minute}}, "codex": {MaxTTL: Duration{Duration: time.Minute}},
	}}
	inputs := []ProjectionInput{{AgentID: "agent-a", ObservationType: "usage"}, {AgentID: "agent-b", ObservationType: "codex"}}
	for _, tc := range []struct {
		profile        string
		seconds, count int
		valid          bool
	}{
		{"rotating-oled-128x32", 0, 2, true}, {"rotating-oled-128x32", 1, 2, true}, {"rotating-oled-128x32", 3, 2, true}, {"rotating-oled-128x32", 3600, 2, true},
		{"rotating-oled-128x32", -1, 2, false}, {"rotating-oled-128x32", 3601, 2, false}, {"rotating-oled-128x32", 3, 1, false}, {"usage-oled-128x32", 3, 1, false},
	} {
		err := cfg.ValidateRoutes(map[string]ProjectionRoute{"node-a": {Profile: tc.profile, RotationSeconds: tc.seconds, Inputs: inputs[:tc.count]}})
		if (err == nil) != tc.valid {
			t.Fatalf("profile=%s seconds=%d count=%d valid=%t err=%v", tc.profile, tc.seconds, tc.count, tc.valid, err)
		}
	}
}
