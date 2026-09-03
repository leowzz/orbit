package v1_test

import (
	"encoding/hex"
	"testing"
	"time"

	orbitv1 "orbit/gen/go/orbit/v1"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestUsageObservationPreservesRequiredZeroValues(t *testing.T) {
	zeroCost := int64(0)
	zeroTokens := uint64(0)
	zeroTPM := uint64(0)
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	original := &orbitv1.UsageObservation{
		WindowStart:      timestamppb.New(now.Add(-time.Hour)),
		WindowEnd:        timestamppb.New(now),
		ActualCostMicros: &zeroCost,
		CurrencyCode:     "USD",
		TokenCount:       &zeroTokens,
		Tpm:              &zeroTPM,
		ObservedAt:       timestamppb.New(now),
	}

	encoded, err := proto.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var decoded orbitv1.UsageObservation
	if err := proto.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}

	if decoded.ActualCostMicros == nil || decoded.TokenCount == nil || decoded.Tpm == nil {
		t.Fatalf("zero-valued required fields lost presence: %#v", &decoded)
	}
}

func TestObservationUsesTypedUsagePayload(t *testing.T) {
	observation := &orbitv1.Observation{
		Metadata:   &orbitv1.Metadata{MessageId: "msg-1", ProducerId: "agent-local", Revision: 1},
		AgentEpoch: "epoch-1",
		Payload:    &orbitv1.Observation_Usage{Usage: &orbitv1.UsageObservation{}},
	}

	if observation.GetUsage() == nil {
		t.Fatal("typed usage payload is missing")
	}
	if got := observation.ProtoReflect().Descriptor().Oneofs().ByName("payload"); got == nil {
		t.Fatal("Observation.payload must remain a oneof")
	}
}

func TestDeviceViewHasFixedSlotsAndFreshness(t *testing.T) {
	view := &orbitv1.DeviceView{
		Freshness: orbitv1.Freshness_FRESHNESS_FRESH,
		Primary:   &orbitv1.DisplaySlot{Text: "$12.34", Emphasis: orbitv1.Emphasis_EMPHASIS_STRONG},
		Secondary: &orbitv1.DisplaySlot{Text: "1.2M tokens"},
		Footer:    &orbitv1.DisplaySlot{Text: "TPM 42"},
	}

	if view.GetFreshness() != orbitv1.Freshness_FRESHNESS_FRESH {
		t.Fatalf("freshness = %v", view.GetFreshness())
	}
	if view.GetPrimary().GetText() == "" || view.GetSecondary().GetText() == "" || view.GetFooter().GetText() == "" {
		t.Fatalf("fixed display slots missing: %#v", view)
	}
}

func TestDeviceViewMatchesFirmwareGoldenBytes(t *testing.T) {
	view := &orbitv1.DeviceView{
		Metadata:    &orbitv1.Metadata{Revision: 7},
		NodeId:      "node-a",
		CoreEpoch:   "core-epoch-a",
		Freshness:   orbitv1.Freshness_FRESHNESS_FRESH,
		FreshUntil:  &timestamppb.Timestamp{Seconds: 2000},
		RetainUntil: &timestamppb.Timestamp{Seconds: 3000},
		Primary:     &orbitv1.DisplaySlot{Text: "$12.35"},
		Secondary:   &orbitv1.DisplaySlot{Text: "1.2M TOK"},
		Footer:      &orbitv1.DisplaySlot{Text: "4.5K TPM"},
	}
	encoded, err := (proto.MarshalOptions{Deterministic: true}).Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	const firmwareGolden = "0a02180712066e6f64652d611a0c636f72652d65706f63682d6120012a0308d00f320308b8173a080a062431322e3335420a0a08312e324d20544f4b4a0a0a08342e354b2054504d"
	if hex.EncodeToString(encoded) != firmwareGolden {
		t.Fatalf("DeviceView bytes changed: %x", encoded)
	}
}
