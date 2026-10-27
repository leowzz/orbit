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

func TestDestinationIndependentDisplayModes(t *testing.T) {
	cfg := CoreConfig{ObservationPolicies: map[string]ObservationPolicy{
		"usage": {MaxTTL: Duration{Duration: time.Minute}}, "codex": {MaxTTL: Duration{Duration: time.Minute}},
	}}
	inputs := []ProjectionInput{{AgentID: "a", ObservationType: "usage"}, {AgentID: "b", ObservationType: "codex"}}
	for _, profile := range []string{"usage-oled-128x32", "metrics-xiaozhi-320x240", "overview-web", "overview-android", "overview-app"} {
		for _, tc := range []struct {
			mode           string
			count, seconds int
			valid          bool
		}{
			{"sessions", 2, 0, true}, {"sessions", 1, 0, false}, {"usage", 2, 0, true}, {"codex", 2, 0, true}, {"rotating", 2, 1, true},
			{"codex", 1, 0, false}, {"rotating", 1, 3, false}, {"usage", 2, 3, false}, {"other", 2, 0, false},
		} {
			err := cfg.ValidateRoutes(map[string]ProjectionRoute{"phone": {Profile: profile, DisplayMode: tc.mode, RotationSeconds: tc.seconds, Inputs: inputs[:tc.count]}})
			valid := tc.valid && !(profile == "metrics-xiaozhi-320x240" && tc.mode == "sessions")
			if (err == nil) != valid {
				t.Fatalf("%s %+v: %v", profile, tc, err)
			}
		}
	}
}

func TestSessionSwitchRequiresCodexAndDetailDestination(t *testing.T) {
	cfg := CoreConfig{ObservationPolicies: map[string]ObservationPolicy{"usage": {MaxTTL: Duration{Duration: time.Minute}}, "codex": {MaxTTL: Duration{Duration: time.Minute}}}}
	enabled := true
	route := ProjectionRoute{Profile: "overview-web", DisplayMode: "usage", SessionsEnabled: &enabled, Inputs: []ProjectionInput{{AgentID: "a", ObservationType: "usage"}}}
	if cfg.ValidateRoutes(map[string]ProjectionRoute{"node": route}) == nil {
		t.Fatal("accepted missing Codex source")
	}
	route.Inputs = append(route.Inputs, ProjectionInput{AgentID: "a", ObservationType: "codex"})
	if err := cfg.ValidateRoutes(map[string]ProjectionRoute{"node": route}); err != nil {
		t.Fatal(err)
	}
	route.Profile = "usage-oled-128x32"
	if cfg.ValidateRoutes(map[string]ProjectionRoute{"node": route}) == nil {
		t.Fatal("OLED cannot display session details")
	}
	enabled = false
	if err := cfg.ValidateRoutes(map[string]ProjectionRoute{"node": route}); err != nil {
		t.Fatal(err)
	}
}

func TestNFCDPaddingValidation(t *testing.T) {
	cfg := CoreConfig{ObservationPolicies: map[string]ObservationPolicy{"usage": {MaxTTL: Duration{Duration: time.Minute}}}}
	for _, n := range []uint32{0, 16, 64, 65} {
		route := ProjectionRoute{Profile: "overview-app", NFCDMetricsLeftPadding: &n, Inputs: []ProjectionInput{{AgentID: "agent", ObservationType: "usage"}}}
		if err := cfg.ValidateRoutes(map[string]ProjectionRoute{"node": route}); (err == nil) != (n <= 64) {
			t.Fatalf("padding %d: %v", n, err)
		}
		route.Profile = "overview-web"
		if err := cfg.ValidateRoutes(map[string]ProjectionRoute{"node": route}); err == nil {
			t.Fatal("accepted padding for non-App route")
		}
	}
}
