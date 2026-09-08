package core

import (
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	orbitv1 "orbit/gen/go/orbit/v1"
	"time"
)

// Android tolerates minute-scale usage updates. Session changes and stale transitions
// remain immediate; renew unchanged leases before their advertised freshness expires.
func publishAndroid(now time.Time, previous, next *orbitv1.DeviceView) bool {
	if previous == nil {
		return true
	}
	if previous.Freshness != next.Freshness || previous.GetUsage().GetFreshness() != next.GetUsage().GetFreshness() ||
		previous.GetCodex().GetFreshness() != next.GetCodex().GetFreshness() ||
		(previous.Usage == nil) != (next.Usage == nil) || !proto.Equal(androidSessions(previous.Codex), androidSessions(next.Codex)) {
		return true
	}
	elapsed := now.Sub(previous.Metadata.ProducedAt.AsTime())
	// Keep canonical expiry semantics: do not extend stale data just to reduce traffic.
	leaseDue := func(old, fresh *timestamppb.Timestamp, state orbitv1.Freshness) bool {
		return old != nil && fresh != nil && state == orbitv1.Freshness_FRESHNESS_FRESH && fresh.AsTime().After(old.AsTime()) &&
			elapsed >= old.AsTime().Sub(previous.Metadata.ProducedAt.AsTime())/2
	}
	if leaseDue(previous.GetUsage().GetFreshUntil(), next.GetUsage().GetFreshUntil(), next.GetUsage().GetFreshness()) ||
		leaseDue(previous.GetCodex().GetFreshUntil(), next.GetCodex().GetFreshUntil(), next.GetCodex().GetFreshness()) {
		return true
	}
	if elapsed < time.Minute {
		return false
	}
	return !proto.Equal(androidContent(previous), androidContent(next)) || next.RetainUntil.AsTime().After(previous.RetainUntil.AsTime()) &&
		now.Add(time.Minute).After(previous.RetainUntil.AsTime())
}

func androidSessions(value *orbitv1.CodexView) *orbitv1.CodexView {
	if value == nil {
		return nil
	}
	copy := proto.Clone(value).(*orbitv1.CodexView)
	copy.ObservedAt, copy.FreshUntil = nil, nil
	for _, session := range copy.Sessions {
		session.UpdatedAt = nil
	}
	return copy
}
func androidContent(value *orbitv1.DeviceView) *orbitv1.DeviceView {
	copy := proto.Clone(value).(*orbitv1.DeviceView)
	copy.Metadata, copy.FreshUntil, copy.RetainUntil = nil, nil, nil
	if copy.Usage != nil {
		copy.Usage.ObservedAt, copy.Usage.FreshUntil = nil, nil
	}
	copy.Codex = androidSessions(copy.Codex)
	return copy
}
