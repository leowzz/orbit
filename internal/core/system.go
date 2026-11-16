package core

import (
	"errors"
	"time"

	orbitv1 "orbit/gen/go/orbit/v1"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	defaultSystemMaxTTL     = 5 * time.Minute
	defaultSystemFutureSkew = 10 * time.Second
)

type SystemPolicy struct {
	MaxTTL        time.Duration
	MaxFutureSkew time.Duration
}

type canonicalSystem struct {
	value      *orbitv1.SystemObservation
	expiresAt  time.Time
	revision   uint64
	agentEpoch string
}

func effectiveSystemPolicy(policy SystemPolicy) (SystemPolicy, error) {
	if policy.MaxTTL < 0 || policy.MaxFutureSkew < 0 {
		return SystemPolicy{}, errors.New("system max TTL and future skew must not be negative")
	}
	if policy.MaxTTL == 0 && policy.MaxFutureSkew == 0 {
		return SystemPolicy{
			MaxTTL:        defaultSystemMaxTTL,
			MaxFutureSkew: defaultSystemFutureSkew,
		}, nil
	}
	if policy.MaxTTL == 0 {
		policy.MaxTTL = defaultSystemMaxTTL
	}
	return policy, nil
}

func (e *Engine) validateSystem(now time.Time, metadata *orbitv1.Metadata, value *orbitv1.SystemObservation) (time.Time, error) {
	if value == nil {
		return time.Time{}, errors.New("system payload is required")
	}
	producedAt, err := requiredTimestamp(metadata.ProducedAt, "observation produced_at")
	if err != nil {
		return time.Time{}, err
	}
	if producedAt.After(now.Add(e.config.SystemPolicy.MaxFutureSkew)) {
		return time.Time{}, errors.New("observation produced_at is too far in the future")
	}
	payloadExpiry, err := requiredTimestamp(metadata.ExpiresAt, "observation expires_at")
	if err != nil {
		return time.Time{}, err
	}
	observedAt, err := requiredTimestamp(value.ObservedAt, "system observed_at")
	if err != nil {
		return time.Time{}, err
	}
	if observedAt.After(now.Add(e.config.SystemPolicy.MaxFutureSkew)) {
		return time.Time{}, errors.New("system observed_at is too far in the future")
	}
	if value.CpuUsagePermille == nil || value.MemoryUsedBytes == nil || value.MemoryTotalBytes == nil {
		return time.Time{}, errors.New("system cpu and memory fields require presence")
	}
	if value.GetCpuUsagePermille() > 1000 {
		return time.Time{}, errors.New("system cpu usage exceeds 100%")
	}
	if value.GetMemoryUsedBytes() > value.GetMemoryTotalBytes() {
		return time.Time{}, errors.New("system memory used exceeds total")
	}
	if value.GetSwapUsedBytes() > value.GetSwapTotalBytes() {
		return time.Time{}, errors.New("system swap used exceeds total")
	}
	effectiveExpiry := minTime(payloadExpiry, observedAt.Add(e.config.SystemPolicy.MaxTTL), now.Add(e.config.SystemPolicy.MaxTTL))
	if !effectiveExpiry.After(now) {
		return time.Time{}, errors.New("observation is already expired")
	}
	return effectiveExpiry, nil
}

func (e *Engine) systemForRoute(route Route) (canonicalSystem, bool) {
	for _, input := range route.Inputs {
		if input.ObservationType == orbitv1.ObservationType_OBSERVATION_TYPE_SYSTEM {
			value, ok := e.system[input.AgentID]
			return value, ok
		}
	}
	return canonicalSystem{}, false
}

func buildSystemView(now time.Time, sys canonicalSystem) *orbitv1.SystemView {
	if sys.value == nil {
		return &orbitv1.SystemView{
			Freshness:  orbitv1.Freshness_FRESHNESS_STALE,
			FreshUntil: timestamppb.New(now),
		}
	}
	view := &orbitv1.SystemView{
		Freshness:  freshnessAt(now, sys.expiresAt),
		FreshUntil: timestamppb.New(sys.expiresAt),
		ObservedAt: sys.value.ObservedAt,
	}
	if sys.value.CpuUsagePermille != nil {
		view.CpuUsagePermille = proto.Uint32(sys.value.GetCpuUsagePermille())
	}
	if sys.value.CpuCores != nil {
		view.CpuCores = proto.Uint32(sys.value.GetCpuCores())
	}
	if sys.value.MemoryUsedBytes != nil {
		view.MemoryUsedBytes = proto.Uint64(sys.value.GetMemoryUsedBytes())
	}
	if sys.value.MemoryTotalBytes != nil {
		view.MemoryTotalBytes = proto.Uint64(sys.value.GetMemoryTotalBytes())
	}
	if sys.value.SwapUsedBytes != nil {
		view.SwapUsedBytes = proto.Uint64(sys.value.GetSwapUsedBytes())
	}
	if sys.value.SwapTotalBytes != nil {
		view.SwapTotalBytes = proto.Uint64(sys.value.GetSwapTotalBytes())
	}
	if sys.value.Load_1MMilli != nil {
		view.Load_1MMilli = proto.Uint32(sys.value.GetLoad_1MMilli())
	}
	if sys.value.UptimeSeconds != nil {
		view.UptimeSeconds = proto.Uint64(sys.value.GetUptimeSeconds())
	}
	return view
}

func consoleSystemSummary(sys canonicalSystem, now time.Time) map[string]any {
	if sys.value == nil {
		return nil
	}
	summary := map[string]any{
		"fresh":              sys.expiresAt.After(now),
		"cpu_usage_permille": sys.value.GetCpuUsagePermille(),
		"cpu_cores":          sys.value.GetCpuCores(),
		"memory_used_bytes":  sys.value.GetMemoryUsedBytes(),
		"memory_total_bytes": sys.value.GetMemoryTotalBytes(),
		"swap_used_bytes":    sys.value.GetSwapUsedBytes(),
		"swap_total_bytes":   sys.value.GetSwapTotalBytes(),
		"uptime_seconds":     sys.value.GetUptimeSeconds(),
		"fresh_until":        sys.expiresAt,
	}
	if sys.value.Load_1MMilli != nil {
		summary["load_1m_milli"] = sys.value.GetLoad_1MMilli()
	}
	if sys.value.ObservedAt != nil {
		summary["observed_at"] = sys.value.ObservedAt.AsTime()
	}
	return summary
}
