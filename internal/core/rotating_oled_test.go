package core

import (
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"
	orbitv1 "orbit/gen/go/orbit/v1"
	"orbit/internal/config"
)

func rotatingEngine(t *testing.T, now time.Time, seconds int) *Engine {
	t.Helper()
	e, err := New(Config{CoreID: "core", CoreEpoch: "epoch", Routes: []Route{{
		NodeID: "node-a", Profile: rotatingOLEDProfile, RotationSeconds: seconds,
		Inputs: []RouteInput{{AgentID: "usage-agent", ObservationType: orbitv1.ObservationType_OBSERVATION_TYPE_USAGE}, {AgentID: "codex-agent", ObservationType: orbitv1.ObservationType_OBSERVATION_TYPE_CODEX}},
	}}, UsagePolicy: UsagePolicy{MaxTTL: time.Hour}, CodexPolicy: CodexPolicy{MaxTTL: time.Hour}})
	if err != nil {
		t.Fatal(err)
	}
	e.nodeProducts["node-a"] = &orbitv1.NodeState{ModelId: oledModel}
	e.usage["usage-agent"] = canonicalUsage{value: testObservation(now).GetUsage(), expiresAt: now.Add(time.Hour)}
	e.codex["codex-agent"] = canonicalCodex{value: &orbitv1.CodexObservation{WeeklyLimit: &orbitv1.CodexWeeklyLimit{
		RemainingPercent: 50, ResetsAt: timestamppb.New(now.Add(24 * time.Hour)), ObservedAt: timestamppb.New(now), FreshUntil: timestamppb.New(now.Add(3 * time.Minute)),
	}}, expiresAt: now.Add(time.Hour)}
	return e
}

func TestOLEDRotationClockAndInterval(t *testing.T) {
	now := time.Date(2026, 10, 14, 3, 0, 0, 0, time.UTC)
	for _, seconds := range []int{0, 3, 5} {
		e := rotatingEngine(t, now, seconds)
		interval := seconds
		if interval == 0 {
			interval = 3
		}
		var previous uint64
		for tick := 0; tick <= interval*2+1; tick++ {
			views, err := e.Refresh(now.Add(time.Duration(tick) * time.Second))
			if err != nil {
				t.Fatal(err)
			}
			if tick%interval != 0 {
				if len(views) != 0 {
					t.Fatalf("interval=%d tick=%d republished without a change", interval, tick)
				}
				continue
			}
			want := "$12.35"
			if tick/interval%2 == 1 {
				want = "50%"
			}
			if len(views) != 1 || views[0].Primary.Text != want || views[0].Metadata.Revision <= previous || views[0].Usage != nil || views[0].Codex != nil {
				t.Fatalf("interval=%d tick=%d views=%v", interval, tick, views)
			}
			previous = views[0].Metadata.Revision
		}
	}
}

func TestOLEDRotationMissingSourceAndFreshness(t *testing.T) {
	now := time.Now().UTC()
	e := rotatingEngine(t, now, 3)
	delete(e.codex, "codex-agent")
	views, _ := e.Refresh(now)
	if len(views) != 1 || views[0].Freshness != orbitv1.Freshness_FRESHNESS_FRESH {
		t.Fatal("missing inactive source made API page stale")
	}
	views, _ = e.Refresh(now.Add(3 * time.Second))
	if len(views) != 1 || views[0].Primary.Text != "--%" || views[0].Freshness != orbitv1.Freshness_FRESHNESS_STALE {
		t.Fatal("missing quota left previous API page visible")
	}
	views, _ = e.Refresh(now.Add(6 * time.Second))
	if len(views) != 1 || views[0].Primary.Text != "$12.35" {
		t.Fatal("rotation stalled on missing source")
	}
	delete(e.usage, "usage-agent")
	views, _ = e.projectRouteLocked(now.Add(7*time.Second), e.config.Routes[0])
	if len(views) != 1 || views[0].Primary.Text != "--" || views[0].Freshness != orbitv1.Freshness_FRESHNESS_STALE {
		t.Fatal("missing usage did not clear previous values")
	}
}

func TestOLEDRotationRouteChangesAndPersistence(t *testing.T) {
	now := time.Now().UTC()
	e := rotatingEngine(t, now, 3)
	e.Refresh(now)
	routes := append([]Route(nil), e.config.Routes...)
	views, err := e.replaceRoutes(now.Add(4*time.Second), routes, func() error { return nil })
	if err != nil || len(views) != 1 || views[0].Primary.Text != "50%" {
		t.Fatalf("unchanged route reset rotation: %v %v", views, err)
	}
	routes = append([]Route(nil), e.config.Routes...)
	routes[0].RotationSeconds = 5
	views, err = e.replaceRoutes(now.Add(4*time.Second), routes, func() error { return nil })
	if err != nil || len(views) != 1 || views[0].Primary.Text != "$12.35" {
		t.Fatalf("new interval did not restart: %v %v", views, err)
	}
	views, err = e.Refresh(now.Add(9 * time.Second))
	if err != nil || len(views) != 1 || views[0].Primary.Text != "50%" {
		t.Fatal("new interval not applied")
	}
	routes = append([]Route(nil), e.config.Routes...)
	routes[0].Profile = codexWeeklyProfile
	routes[0].RotationSeconds = 0
	routes[0].Inputs = routes[0].Inputs[1:]
	if _, err = e.replaceRoutes(now.Add(10*time.Second), routes, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if views, err = e.Refresh(now.Add(15 * time.Second)); err != nil || len(views) != 0 {
		t.Fatal("fixed page kept rotating")
	}

	path := filepath.Join(t.TempDir(), "core.sqlite")
	store, err := OpenRouteStore(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	doc := RouteDocument{Revision: 1, Routes: map[string]config.ProjectionRoute{"node-a": {Profile: rotatingOLEDProfile, RotationSeconds: 5, Inputs: []config.ProjectionInput{{AgentID: "usage-agent", ObservationType: "usage"}, {AgentID: "codex-agent", ObservationType: "codex"}}}}}
	if err = store.save(doc); err != nil {
		t.Fatal(err)
	}
	store.Close()
	store, err = OpenRouteStore(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	saved, err := store.Load()
	if err != nil || saved.Routes["node-a"].RotationSeconds != 5 || RoutesFromConfig(saved.Routes)[0].RotationSeconds != 5 {
		t.Fatal("rotation interval was not persisted")
	}
}
