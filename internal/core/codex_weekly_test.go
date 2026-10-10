package core

import (
	"math"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	orbitv1 "orbit/gen/go/orbit/v1"
)

func TestCodexWeeklyOLEDProjection(t *testing.T) {
	now := time.Date(2026, 10, 10, 8, 0, 0, 0, time.UTC)
	engine := newTestEngine(t)
	engine.config.Routes = []Route{{NodeID: "node-a", Profile: codexWeeklyProfile, Inputs: []RouteInput{{AgentID: "agent-a", ObservationType: orbitv1.ObservationType_OBSERVATION_TYPE_CODEX}}}}
	engine.config.CodexPolicy = CodexPolicy{MaxTTL: time.Hour, MaxFutureSkew: 10 * time.Second}
	limit := &orbitv1.CodexWeeklyLimit{RemainingPercent: 50, ResetsAt: timestamppb.New(time.Unix(1791966111, 0)), ObservedAt: timestamppb.New(now), FreshUntil: timestamppb.New(now.Add(3 * time.Minute))}
	engine.codex["agent-a"] = canonicalCodex{value: &orbitv1.CodexObservation{WeeklyLimit: limit}, expiresAt: now.Add(time.Hour)}
	views, err := engine.projectRouteLocked(now, engine.config.Routes[0])
	if err != nil || len(views) != 1 {
		t.Fatalf("views=%v err=%v", views, err)
	}
	v := views[0]
	if v.Primary.Text != "50%" || v.Secondary.Text != "4  10/14" || v.Footer.Text != "0  16:21" || v.Codex != nil || v.Usage != nil || v.Freshness != orbitv1.Freshness_FRESHNESS_FRESH {
		t.Fatalf("view=%v", v)
	}
	before := engine.freshnessSignature(now, engine.config.Routes[0])
	after := engine.freshnessSignature(now.Add(4*time.Minute), engine.config.Routes[0])
	if before == after {
		t.Fatal("quota expiry did not change freshness signature")
	}
	views, _ = engine.projectRouteLocked(now.Add(4*time.Minute), engine.config.Routes[0])
	if views[0].Freshness != orbitv1.Freshness_FRESHNESS_STALE || views[0].Primary.Text != "50%" {
		t.Fatalf("stale view=%v", views[0])
	}
	views, _ = engine.projectRouteLocked(limit.ResetsAt.AsTime().Add(time.Second), engine.config.Routes[0])
	if views[0].Primary.Text != "--%" || views[0].Secondary.Text != "-- --/--" {
		t.Fatal("expired quota still displayed after reset")
	}
	engine.codex["agent-a"] = canonicalCodex{value: &orbitv1.CodexObservation{}, expiresAt: now.Add(time.Hour)}
	views, _ = engine.projectRouteLocked(now, engine.config.Routes[0])
	if views[0].Primary.Text != "--%" || views[0].Freshness != orbitv1.Freshness_FRESHNESS_STALE {
		t.Fatal("missing quota must be unavailable")
	}
	for _, percent := range []float64{-1, 101, math.NaN(), math.Inf(1)} {
		invalid := proto.Clone(limit).(*orbitv1.CodexWeeklyLimit)
		invalid.RemainingPercent = percent
		if validateWeeklyLimit(now, time.Second, invalid) == nil {
			t.Fatalf("accepted percent %v", percent)
		}
	}
}

func TestWeeklyRemainingAndClockRefresh(t *testing.T) {
	reset := time.Date(2026, 10, 14, 8, 21, 0, 0, time.UTC)
	for _, test := range []struct {
		remaining   time.Duration
		days, hours int64
	}{
		{4*24*time.Hour + 23*time.Hour, 4, 23},
		{24 * time.Hour, 1, 0},
		{24*time.Hour - time.Second, 0, 23},
		{time.Hour, 0, 1},
		{time.Hour - time.Second, 0, 0},
		{0, 0, 0},
		{-time.Second, 0, 0},
	} {
		days, hours := weeklyRemaining(reset.Add(-test.remaining), reset)
		if days != test.days || hours != test.hours {
			t.Fatalf("remaining %s: got %d days %d hours", test.remaining, days, hours)
		}
	}
	now := reset.Add(-24 * time.Hour)
	engine, err := New(Config{CoreID: "core-a", CoreEpoch: "epoch", Routes: []Route{{NodeID: "node-a", Profile: codexWeeklyProfile, Inputs: []RouteInput{{AgentID: "agent-a", ObservationType: orbitv1.ObservationType_OBSERVATION_TYPE_CODEX}}}}, CodexPolicy: CodexPolicy{MaxTTL: time.Hour}})
	if err != nil {
		t.Fatal(err)
	}
	engine.nodeProducts["node-a"] = &orbitv1.NodeState{NodeId: "node-a", ModelId: oledModel}
	engine.codex["agent-a"] = canonicalCodex{value: &orbitv1.CodexObservation{WeeklyLimit: &orbitv1.CodexWeeklyLimit{RemainingPercent: 50, ResetsAt: timestamppb.New(reset), ObservedAt: timestamppb.New(now), FreshUntil: timestamppb.New(now.Add(3 * time.Minute))}}, expiresAt: now.Add(time.Hour)}
	views, err := engine.Refresh(now)
	if err != nil || len(views) != 1 || views[0].Secondary.Text != "1  10/14" || views[0].Footer.Text != "0  16:21" {
		t.Fatalf("initial views=%v err=%v", views, err)
	}
	views, err = engine.Refresh(now.Add(time.Second))
	if err != nil || len(views) != 1 || views[0].Secondary.Text != "0  10/14" || views[0].Footer.Text != "23 16:21" {
		t.Fatalf("clock did not refresh countdown: views=%v err=%v", views, err)
	}
	views, err = engine.Refresh(now.Add(2 * time.Second))
	if err != nil || len(views) != 0 {
		t.Fatal("unchanged countdown republished")
	}
}
