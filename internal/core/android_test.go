package core

import (
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	orbitv1 "orbit/gen/go/orbit/v1"
	"testing"
	"time"
)

func TestAndroidCoalescingPreservesChangesAndFreshness(t *testing.T) {
	now := time.Now().UTC()
	fresh := orbitv1.Freshness_FRESHNESS_FRESH
	previous := &orbitv1.DeviceView{
		Metadata:  &orbitv1.Metadata{ProducedAt: timestamppb.New(now)},
		Freshness: fresh, FreshUntil: timestamppb.New(now.Add(2 * time.Minute)), RetainUntil: timestamppb.New(now.Add(time.Hour)),
		Usage: &orbitv1.UsageView{Freshness: fresh, FreshUntil: timestamppb.New(now.Add(2 * time.Minute)), ActualCostMicros: proto.Int64(100)},
		Codex: &orbitv1.CodexView{Freshness: fresh, FreshUntil: timestamppb.New(now.Add(2 * time.Minute)), Sessions: []*orbitv1.CodexSessionView{{SessionId: "one", Status: orbitv1.CodexSessionStatus_CODEX_SESSION_STATUS_RUNNING}}},
	}
	renewed := proto.Clone(previous).(*orbitv1.DeviceView)
	renewed.Codex.FreshUntil = timestamppb.New(now.Add(3 * time.Minute))
	renewed.Codex.ObservedAt = timestamppb.New(now.Add(time.Second))
	renewed.Codex.Sessions[0].UpdatedAt = timestamppb.New(now.Add(time.Second))
	if publishAndroid(now.Add(time.Second), previous, renewed) {
		t.Fatal("sent timestamp-only update")
	}
	if !publishAndroid(now.Add(time.Minute), previous, renewed) {
		t.Fatal("failed to renew freshness lease")
	}
	amount := proto.Clone(previous).(*orbitv1.DeviceView)
	amount.Usage.ActualCostMicros = proto.Int64(200)
	if publishAndroid(now.Add(time.Second), previous, amount) {
		t.Fatal("usage bypassed coalescing")
	}
	if !publishAndroid(now.Add(time.Minute), previous, amount) {
		t.Fatal("pending usage not flushed")
	}
	completed := proto.Clone(previous).(*orbitv1.DeviceView)
	completed.Codex.Sessions[0].Status = orbitv1.CodexSessionStatus_CODEX_SESSION_STATUS_COMPLETED
	if !publishAndroid(now.Add(time.Second), previous, completed) {
		t.Fatal("delayed session transition")
	}
	stale := proto.Clone(previous).(*orbitv1.DeviceView)
	stale.Codex.Freshness = orbitv1.Freshness_FRESHNESS_STALE
	if !publishAndroid(now.Add(time.Second), previous, stale) {
		t.Fatal("delayed stale transition")
	}
	if publishAndroid(now.Add(time.Minute), previous, previous) {
		t.Fatal("resent identical view")
	}
	// One stale section must not prevent renewing the other fresh section.
	previous.Usage.Freshness = orbitv1.Freshness_FRESHNESS_STALE
	previous.Freshness = orbitv1.Freshness_FRESHNESS_STALE
	renewed.Usage.Freshness = previous.Usage.Freshness
	renewed.Freshness = previous.Freshness
	if !publishAndroid(now.Add(time.Minute), previous, renewed) {
		t.Fatal("mixed freshness blocked renewal")
	}
}

func TestAndroidRefreshFlushesUsageAndReconnectBypassesWindow(t *testing.T) {
	now := time.Now().UTC()
	engine, err := New(Config{CoreID: "core-a", CoreEpoch: "epoch-a", Routes: []Route{{NodeID: "android-a", Profile: androidProfile,
		Inputs: []RouteInput{{AgentID: "agent-a", ObservationType: orbitv1.ObservationType_OBSERVATION_TYPE_USAGE}}}},
		UsagePolicy: UsagePolicy{MaxTTL: time.Hour}})
	if err != nil {
		t.Fatal(err)
	}
	state := testNodeState(now)
	state.NodeId, state.Metadata.ProducerId = "android-a", "android-a"
	state.ModelId, state.VariantId = androidModel, androidVariant
	if _, err := engine.ApplyNodeState(now, state); err != nil {
		t.Fatal(err)
	}
	if err := engine.ApplyAgentState(testAgentState(now)); err != nil {
		t.Fatal(err)
	}
	if views, err := engine.ApplyObservation(now, testObservation(now)); err != nil || len(views) != 1 {
		t.Fatalf("initial %v %v", views, err)
	}
	changed := testObservation(now.Add(time.Second))
	changed.Metadata.Revision++
	changed.GetUsage().ActualCostMicros = proto.Int64(9000000)
	if views, err := engine.ApplyObservation(now.Add(time.Second), changed); err != nil || len(views) != 0 {
		t.Fatalf("coalescing %v %v", views, err)
	}
	if views, err := engine.Refresh(now.Add(time.Minute)); err != nil || len(views) != 1 || views[0].Usage.GetActualCostMicros() != 9000000 {
		t.Fatalf("flush %v %v", views, err)
	}
	if views, err := engine.Refresh(now.Add(time.Minute + time.Second)); err != nil || len(views) != 0 {
		t.Fatalf("duplicate %v %v", views, err)
	}
	state.Metadata.Revision++
	if views, err := engine.ApplyNodeState(now.Add(time.Minute+time.Second), state); err != nil || len(views) != 1 {
		t.Fatalf("reconnect %v %v", views, err)
	}
}
