package core

import (
	"strings"
	"testing"
	"time"

	orbitv1 "orbit/gen/go/orbit/v1"

	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestUsageProjectionAndStaleTransition(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)
	engine := newTestEngine(t)
	if err := engine.ApplyAgentState(testAgentState(now)); err != nil {
		t.Fatalf("ApplyAgentState: %v", err)
	}
	views, err := engine.ApplyNodeState(now, testNodeState(now))
	if err != nil || len(views) != 0 {
		t.Fatalf("ApplyNodeState views=%d err=%v", len(views), err)
	}

	views, err = engine.ApplyObservation(now, testObservation(now))
	if err != nil {
		t.Fatalf("ApplyObservation: %v", err)
	}
	if len(views) != 1 {
		t.Fatalf("got %d views, want 1", len(views))
	}
	view := views[0]
	if view.Freshness != orbitv1.Freshness_FRESHNESS_FRESH || view.Primary.Text != "$12.35" || view.Secondary.Text != "1.2M TOK" || view.Footer.Text != "4.5K TPM" {
		t.Fatalf("unexpected view: %+v", view)
	}
	if strings.Contains(view.String(), "agent-a") {
		t.Fatal("DeviceView leaked upstream agent id")
	}

	views, err = engine.Refresh(now.Add(3 * time.Minute))
	if err != nil || len(views) != 1 {
		t.Fatalf("Refresh views=%d err=%v", len(views), err)
	}
	if views[0].Freshness != orbitv1.Freshness_FRESHNESS_STALE || views[0].Primary.Text != "$12.35" {
		t.Fatalf("unexpected stale view: %+v", views[0])
	}
	views, err = engine.Refresh(now.Add(4 * time.Minute))
	if err != nil || len(views) != 0 {
		t.Fatalf("duplicate Refresh views=%d err=%v", len(views), err)
	}
}

func TestObservationRequiresCurrentAgentEpochAndNumericPresence(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)
	engine := newTestEngine(t)
	if err := engine.ApplyAgentState(testAgentState(now)); err != nil {
		t.Fatal(err)
	}
	observation := testObservation(now)
	observation.AgentEpoch = "old-epoch"
	if _, err := engine.ApplyObservation(now, observation); err == nil {
		t.Fatal("accepted observation from old agent epoch")
	}
	observation = testObservation(now)
	observation.GetUsage().Tpm = nil
	if _, err := engine.ApplyObservation(now, observation); err == nil {
		t.Fatal("accepted observation with absent TPM")
	}
}

func TestObservationRejectsUnsupportedCurrency(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)
	engine := newTestEngine(t)
	if err := engine.ApplyAgentState(testAgentState(now)); err != nil {
		t.Fatal(err)
	}
	observation := testObservation(now)
	observation.GetUsage().CurrencyCode = "EUR"
	if _, err := engine.ApplyObservation(now, observation); err == nil {
		t.Fatal("accepted a currency that V1 cannot render correctly")
	}
}

func TestNodeRejectsUnsupportedProduct(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)
	engine := newTestEngine(t)
	state := testNodeState(now)
	state.ModelId = "oled-64x32"
	if _, err := engine.ApplyNodeState(now, state); err == nil {
		t.Fatal("accepted unsupported node product")
	}
}

func TestNewAgentEpochDropsOldCanonicalUsage(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)
	engine := newTestEngine(t)
	if err := engine.ApplyAgentState(testAgentState(now)); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.ApplyNodeState(now, testNodeState(now)); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.ApplyObservation(now, testObservation(now)); err != nil {
		t.Fatal(err)
	}

	newAgent := testAgentState(now.Add(time.Second))
	newAgent.AgentEpoch = "agent-epoch-b"
	if err := engine.ApplyAgentState(newAgent); err != nil {
		t.Fatal(err)
	}
	restartedNode := testNodeState(now.Add(2 * time.Second))
	restartedNode.NodeEpoch = "node-epoch-b"
	views, err := engine.ApplyNodeState(now.Add(2*time.Second), restartedNode)
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 0 {
		t.Fatalf("old Agent epoch produced %d new views", len(views))
	}
}

func newTestEngine(t *testing.T) *Engine {
	t.Helper()
	engine, err := New(Config{
		CoreID:    "core-a",
		CoreEpoch: "core-epoch-a",
		Routes:    []Route{{NodeID: "node-a", AgentID: "agent-a", Profile: usageProfile}},
		UsagePolicy: UsagePolicy{
			MaxTTL:        2 * time.Minute,
			MaxFutureSkew: 5 * time.Second,
		},
		RetainFor: time.Hour,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return engine
}

func testAgentState(now time.Time) *orbitv1.AgentState {
	return &orbitv1.AgentState{
		Metadata:     &orbitv1.Metadata{MessageId: "m-agent", ProducerId: "agent-a", Revision: 1, ProducedAt: timestamppb.New(now)},
		AgentId:      "agent-a",
		AgentEpoch:   "agent-epoch-a",
		AgentVersion: "test",
		HostLabel:    "test host",
		Sources: []*orbitv1.SourceStatus{{
			ObservationType: orbitv1.ObservationType_OBSERVATION_TYPE_USAGE,
			Enabled:         true,
		}},
	}
}

func testNodeState(now time.Time) *orbitv1.NodeState {
	return &orbitv1.NodeState{
		Metadata:        &orbitv1.Metadata{MessageId: "m-node", ProducerId: "node-a", Revision: 1, ProducedAt: timestamppb.New(now)},
		NodeId:          "node-a",
		NodeEpoch:       "node-epoch-a",
		SeriesId:        displaySeries,
		ModelId:         oledModel,
		VariantId:       ydVariant,
		FirmwareVersion: "0.1.0",
	}
}

func testObservation(now time.Time) *orbitv1.Observation {
	cost := int64(12_345_678)
	tokens := uint64(1_234_567)
	tpm := uint64(4_567)
	return &orbitv1.Observation{
		Metadata: &orbitv1.Metadata{
			MessageId:  "m-observation",
			ProducerId: "agent-a",
			Revision:   1,
			ProducedAt: timestamppb.New(now),
			ExpiresAt:  timestamppb.New(now.Add(10 * time.Minute)),
		},
		AgentEpoch: "agent-epoch-a",
		Payload: &orbitv1.Observation_Usage{Usage: &orbitv1.UsageObservation{
			WindowStart:      timestamppb.New(now.Add(-10 * time.Hour)),
			WindowEnd:        timestamppb.New(now.Add(14 * time.Hour)),
			ActualCostMicros: &cost,
			CurrencyCode:     "USD",
			TokenCount:       &tokens,
			Tpm:              &tpm,
			ObservedAt:       timestamppb.New(now),
		}},
	}
}
