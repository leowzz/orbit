package codex

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"
)

func TestParseWeeklyLimit(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
		want float64
		fail bool
	}{
		{"weekly primary", `{"rateLimits":{"primary":{"usedPercent":50,"windowDurationMins":10080,"resetsAt":1791966111}}}`, 50, false},
		{"weekly secondary", `{"rateLimits":{"primary":{"usedPercent":99,"windowDurationMins":300},"secondary":{"usedPercent":25.5,"windowDurationMins":10080,"resetsAt":1791966111}}}`, 74.5, false},
		{"prefer codex bucket", `{"rateLimits":{"primary":{"usedPercent":1,"windowDurationMins":10080,"resetsAt":1791966111}},"rateLimitsByLimitId":{"codex":{"primary":{"usedPercent":60,"windowDurationMins":10080,"resetsAt":1791966111}},"other":{"primary":{"usedPercent":99,"windowDurationMins":10080,"resetsAt":1791966111}}}}`, 40, false},
		{"over limit", `{"rateLimits":{"primary":{"usedPercent":101,"windowDurationMins":10080,"resetsAt":1791966111}}}`, 0, false},
		{"missing percent", `{"rateLimits":{"primary":{"windowDurationMins":10080,"resetsAt":1791966111}}}`, 0, true},
		{"no weekly window", `{"rateLimits":{"primary":{"usedPercent":50,"windowDurationMins":300,"resetsAt":1791966111}}}`, 0, true},
		{"missing codex bucket", `{"rateLimitsByLimitId":{"other":{"primary":{"usedPercent":50,"windowDurationMins":10080,"resetsAt":1791966111}}}}`, 0, true},
		{"negative percent", `{"rateLimits":{"primary":{"usedPercent":-1,"windowDurationMins":10080,"resetsAt":1791966111}}}`, 0, true},
		{"missing reset", `{"rateLimits":{"primary":{"usedPercent":50,"windowDurationMins":10080}}}`, 0, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			limit, err := parseWeeklyLimit(json.RawMessage(test.body))
			if test.fail {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if limit.RemainingPercent != test.want || limit.ResetsAt.Unix() != 1791966111 {
				t.Fatalf("limit = %+v", limit)
			}
		})
	}
}

func TestLiveWeeklyLimit(t *testing.T) {
	if os.Getenv("ORBIT_CODEX_RATE_LIMITS_LIVE_TEST") != "1" {
		t.Skip("opt-in live account read")
	}
	home, err := resolveHome("")
	if err != nil {
		t.Fatal(err)
	}
	limit, err := readWeeklyLimit(context.Background(), "codex", home)
	if err != nil {
		t.Fatal(err)
	}
	if !limit.ResetsAt.After(time.Now()) {
		t.Fatal("reset is in the past")
	}
	t.Logf("weekly remaining %.0f%%, reset %s", limit.RemainingPercent, limit.ResetsAt.In(time.FixedZone("Asia/Shanghai", 8*60*60)).Format("01/02 15:04"))
}

func TestWeeklyLimitFailurePreservesOriginalFreshness(t *testing.T) {
	now := time.Date(2026, 10, 10, 8, 0, 0, 0, time.UTC)
	previous := &WeeklyLimit{RemainingPercent: 50, ResetsAt: now.Add(24 * time.Hour), ObservedAt: now.Add(-4 * time.Minute), FreshUntil: now.Add(-time.Minute)}
	source := &Source{rateLimitsEnabled: true, codexBinary: "/nonexistent/orbit-test-codex", lastWeeklyLimit: previous}
	limit := source.weeklyLimit(context.Background(), now)
	if limit == previous || limit == nil || !limit.FreshUntil.Equal(previous.FreshUntil) || !limit.ObservedAt.Equal(previous.ObservedAt) {
		t.Fatal("failed read renewed cached quota or returned mutable cache")
	}
	if !source.nextRateLimitsRead.Equal(now.Add(time.Minute)) {
		t.Fatal("failed read did not back off")
	}
	limit.RemainingPercent = 0
	if source.lastWeeklyLimit.RemainingPercent != 50 {
		t.Fatal("caller mutated cached quota")
	}
	source.rateLimitsEnabled = false
	if source.weeklyLimit(context.Background(), now) != nil {
		t.Fatal("disabled quota read returned data")
	}
}
