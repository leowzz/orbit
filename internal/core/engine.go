package core

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	orbitv1 "orbit/gen/go/orbit/v1"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	displaySeries = "display"
	oledModel     = "oled-128x32"
	ydVariant     = "yd-esp32-s3"
	usageProfile  = "usage-oled-128x32"
)

type Route struct {
	NodeID  string
	AgentID string
	Profile string
}

type UsagePolicy struct {
	MaxTTL        time.Duration
	MaxFutureSkew time.Duration
}

type Config struct {
	CoreID      string
	CoreEpoch   string
	Routes      []Route
	UsagePolicy UsagePolicy
	RetainFor   time.Duration
}

type participantState struct {
	epoch      string
	revision   uint64
	producedAt time.Time
}

type canonicalUsage struct {
	value      *orbitv1.UsageObservation
	expiresAt  time.Time
	revision   uint64
	agentEpoch string
}

// Engine owns the in-memory canonical state and the only DeviceView construction path.
type Engine struct {
	mu sync.Mutex

	config       Config
	agents       map[string]participantState
	nodes        map[string]participantState
	nodeProducts map[string]*orbitv1.NodeState
	usage        map[string]canonicalUsage
	viewRevision map[string]uint64
	lastFresh    map[string]orbitv1.Freshness
}

func New(config Config) (*Engine, error) {
	if config.CoreID == "" || config.CoreEpoch == "" {
		return nil, errors.New("core id and epoch are required")
	}
	if config.UsagePolicy.MaxTTL <= 0 || config.UsagePolicy.MaxFutureSkew < 0 {
		return nil, errors.New("usage max TTL must be positive and future skew cannot be negative")
	}
	if config.RetainFor <= 0 {
		config.RetainFor = 24 * time.Hour
	}
	seen := make(map[string]struct{}, len(config.Routes))
	for _, route := range config.Routes {
		if route.NodeID == "" || route.AgentID == "" || route.Profile != usageProfile {
			return nil, fmt.Errorf("invalid route for node %q", route.NodeID)
		}
		if _, duplicate := seen[route.NodeID]; duplicate {
			return nil, fmt.Errorf("duplicate route for node %q", route.NodeID)
		}
		seen[route.NodeID] = struct{}{}
	}
	return &Engine{
		config:       config,
		agents:       make(map[string]participantState),
		nodes:        make(map[string]participantState),
		nodeProducts: make(map[string]*orbitv1.NodeState),
		usage:        make(map[string]canonicalUsage),
		viewRevision: make(map[string]uint64),
		lastFresh:    make(map[string]orbitv1.Freshness),
	}, nil
}

func (e *Engine) ApplyAgentState(state *orbitv1.AgentState) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if state == nil || state.Metadata == nil || state.AgentId == "" || state.AgentEpoch == "" ||
		state.AgentVersion == "" || state.HostLabel == "" {
		return errors.New("invalid agent state")
	}
	if state.Metadata.MessageId == "" || state.Metadata.Revision == 0 {
		return errors.New("agent state metadata requires message id and revision")
	}
	if state.Metadata.ProducerId != state.AgentId {
		return errors.New("agent state producer does not match agent id")
	}
	usageEnabled := false
	for _, source := range state.Sources {
		if source.ObservationType == orbitv1.ObservationType_OBSERVATION_TYPE_USAGE && source.Enabled {
			usageEnabled = true
		}
	}
	if !usageEnabled {
		return errors.New("agent state does not declare an enabled usage source")
	}
	producedAt, err := requiredTimestamp(state.Metadata.ProducedAt, "agent produced_at")
	if err != nil {
		return err
	}
	current, exists := e.agents[state.AgentId]
	if exists {
		if current.epoch == state.AgentEpoch && state.Metadata.Revision <= current.revision {
			return errors.New("stale agent state revision")
		}
		if current.epoch != state.AgentEpoch && !producedAt.After(current.producedAt) {
			return errors.New("stale agent epoch")
		}
		if current.epoch != state.AgentEpoch {
			delete(e.usage, state.AgentId)
		}
	}
	e.agents[state.AgentId] = participantState{epoch: state.AgentEpoch, revision: state.Metadata.Revision, producedAt: producedAt}
	return nil
}

func (e *Engine) ApplyNodeState(now time.Time, state *orbitv1.NodeState) ([]*orbitv1.DeviceView, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if state == nil || state.Metadata == nil || state.NodeId == "" || state.NodeEpoch == "" || state.FirmwareVersion == "" {
		return nil, errors.New("invalid node state")
	}
	if state.Metadata.MessageId == "" || state.Metadata.Revision == 0 {
		return nil, errors.New("node state metadata requires message id and revision")
	}
	if state.Metadata.ProducerId != state.NodeId {
		return nil, errors.New("node state producer does not match node id")
	}
	if state.SeriesId != displaySeries || state.ModelId != oledModel || state.VariantId != ydVariant {
		return nil, fmt.Errorf("unsupported node product %s/%s/%s", state.SeriesId, state.ModelId, state.VariantId)
	}
	producedAt, err := requiredTimestamp(state.Metadata.ProducedAt, "node produced_at")
	if err != nil {
		return nil, err
	}
	current, exists := e.nodes[state.NodeId]
	if exists {
		if current.epoch == state.NodeEpoch && state.Metadata.Revision <= current.revision {
			return nil, errors.New("stale node state revision")
		}
		if current.epoch != state.NodeEpoch && !producedAt.After(current.producedAt) {
			return nil, errors.New("stale node epoch")
		}
	}
	e.nodes[state.NodeId] = participantState{epoch: state.NodeEpoch, revision: state.Metadata.Revision, producedAt: producedAt}
	e.nodeProducts[state.NodeId] = proto.Clone(state).(*orbitv1.NodeState)
	return e.projectNodeLocked(now, state.NodeId)
}

func (e *Engine) ApplyObservation(now time.Time, observation *orbitv1.Observation) ([]*orbitv1.DeviceView, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if observation == nil || observation.Metadata == nil || observation.AgentEpoch == "" {
		return nil, errors.New("invalid observation")
	}
	metadata := observation.Metadata
	if metadata.MessageId == "" || metadata.Revision == 0 {
		return nil, errors.New("observation metadata requires message id and revision")
	}
	agent, exists := e.agents[metadata.ProducerId]
	if !exists || agent.epoch != observation.AgentEpoch {
		return nil, errors.New("observation does not belong to current agent epoch")
	}
	usage := observation.GetUsage()
	if usage == nil {
		return nil, errors.New("unsupported observation payload")
	}
	if current, exists := e.usage[metadata.ProducerId]; exists && current.agentEpoch == observation.AgentEpoch && metadata.Revision <= current.revision {
		return nil, errors.New("stale observation revision")
	}
	expiresAt, err := e.validateUsage(now, metadata, usage)
	if err != nil {
		return nil, err
	}
	e.usage[metadata.ProducerId] = canonicalUsage{
		value:      proto.Clone(usage).(*orbitv1.UsageObservation),
		expiresAt:  expiresAt,
		revision:   metadata.Revision,
		agentEpoch: observation.AgentEpoch,
	}

	var views []*orbitv1.DeviceView
	for _, route := range e.config.Routes {
		if route.AgentID != metadata.ProducerId {
			continue
		}
		projected, err := e.projectNodeLocked(now, route.NodeID)
		if err != nil {
			return nil, err
		}
		views = append(views, projected...)
	}
	return views, nil
}

// Refresh emits a new retained view only when a current view crosses into stale state.
func (e *Engine) Refresh(now time.Time) ([]*orbitv1.DeviceView, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	var views []*orbitv1.DeviceView
	for _, route := range e.config.Routes {
		usage, exists := e.usage[route.AgentID]
		if !exists || now.Before(usage.expiresAt) || now.Equal(usage.expiresAt) {
			continue
		}
		if e.lastFresh[route.NodeID] == orbitv1.Freshness_FRESHNESS_STALE {
			continue
		}
		projected, err := e.projectNodeLocked(now, route.NodeID)
		if err != nil {
			return nil, err
		}
		views = append(views, projected...)
	}
	return views, nil
}

func (e *Engine) validateUsage(now time.Time, metadata *orbitv1.Metadata, usage *orbitv1.UsageObservation) (time.Time, error) {
	producedAt, err := requiredTimestamp(metadata.ProducedAt, "observation produced_at")
	if err != nil {
		return time.Time{}, err
	}
	if producedAt.After(now.Add(e.config.UsagePolicy.MaxFutureSkew)) {
		return time.Time{}, errors.New("observation produced_at is too far in the future")
	}
	payloadExpiry, err := requiredTimestamp(metadata.ExpiresAt, "observation expires_at")
	if err != nil {
		return time.Time{}, err
	}
	windowStart, err := requiredTimestamp(usage.WindowStart, "usage window_start")
	if err != nil {
		return time.Time{}, err
	}
	windowEnd, err := requiredTimestamp(usage.WindowEnd, "usage window_end")
	if err != nil {
		return time.Time{}, err
	}
	observedAt, err := requiredTimestamp(usage.ObservedAt, "usage observed_at")
	if err != nil {
		return time.Time{}, err
	}
	if !windowStart.Before(windowEnd) {
		return time.Time{}, errors.New("usage window_start must be before window_end")
	}
	if observedAt.After(now.Add(e.config.UsagePolicy.MaxFutureSkew)) {
		return time.Time{}, errors.New("usage observed_at is too far in the future")
	}
	if usage.ActualCostMicros == nil || usage.TokenCount == nil || usage.Tpm == nil {
		return time.Time{}, errors.New("usage numeric fields require presence")
	}
	if usage.GetActualCostMicros() < 0 {
		return time.Time{}, errors.New("usage cost cannot be negative")
	}
	if usage.CurrencyCode != "USD" {
		return time.Time{}, errors.New("V1 usage currency must be USD")
	}
	effectiveExpiry := minTime(payloadExpiry, observedAt.Add(e.config.UsagePolicy.MaxTTL), now.Add(e.config.UsagePolicy.MaxTTL))
	if !effectiveExpiry.After(now) {
		return time.Time{}, errors.New("observation is already expired")
	}
	return effectiveExpiry, nil
}

func (e *Engine) projectNodeLocked(now time.Time, nodeID string) ([]*orbitv1.DeviceView, error) {
	if _, exists := e.nodeProducts[nodeID]; !exists {
		return nil, nil
	}
	var route *Route
	for index := range e.config.Routes {
		if e.config.Routes[index].NodeID == nodeID {
			route = &e.config.Routes[index]
			break
		}
	}
	if route == nil {
		return nil, nil
	}
	usage, exists := e.usage[route.AgentID]
	if !exists {
		return nil, nil
	}
	freshness := orbitv1.Freshness_FRESHNESS_FRESH
	if now.After(usage.expiresAt) {
		freshness = orbitv1.Freshness_FRESHNESS_STALE
	}
	e.viewRevision[nodeID]++
	e.lastFresh[nodeID] = freshness
	retainUntil := usage.expiresAt.Add(e.config.RetainFor)
	view := &orbitv1.DeviceView{
		Metadata: &orbitv1.Metadata{
			MessageId:  newID(),
			ProducerId: e.config.CoreID,
			Revision:   e.viewRevision[nodeID],
			ProducedAt: timestamppb.New(now),
			ExpiresAt:  timestamppb.New(retainUntil),
		},
		NodeId:      nodeID,
		CoreEpoch:   e.config.CoreEpoch,
		Freshness:   freshness,
		FreshUntil:  timestamppb.New(usage.expiresAt),
		RetainUntil: timestamppb.New(retainUntil),
		Primary: &orbitv1.DisplaySlot{
			Text:     formatCost(usage.value.GetActualCostMicros()),
			Emphasis: orbitv1.Emphasis_EMPHASIS_STRONG,
		},
		Secondary: &orbitv1.DisplaySlot{
			Text:     formatMetric(usage.value.GetTokenCount()) + " TOK",
			Emphasis: orbitv1.Emphasis_EMPHASIS_NORMAL,
		},
		Footer: &orbitv1.DisplaySlot{
			Text:     formatMetric(usage.value.GetTpm()) + " TPM",
			Emphasis: orbitv1.Emphasis_EMPHASIS_DIM,
		},
	}
	return []*orbitv1.DeviceView{view}, nil
}

func requiredTimestamp(value *timestamppb.Timestamp, name string) (time.Time, error) {
	if value == nil {
		return time.Time{}, fmt.Errorf("%s is required", name)
	}
	if err := value.CheckValid(); err != nil {
		return time.Time{}, fmt.Errorf("invalid %s: %w", name, err)
	}
	return value.AsTime(), nil
}

func minTime(values ...time.Time) time.Time {
	minimum := values[0]
	for _, value := range values[1:] {
		if value.Before(minimum) {
			minimum = value
		}
	}
	return minimum
}

func newID() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		panic(fmt.Sprintf("read crypto random bytes: %v", err))
	}
	return hex.EncodeToString(value[:])
}

func NewEpoch() string {
	return newID()
}

func formatCost(micros int64) string {
	cents := (micros + 5_000) / 10_000
	if cents >= 10_000_000 {
		return "$99999+"
	}
	return fmt.Sprintf("$%d.%02d", cents/100, cents%100)
}

func formatMetric(value uint64) string {
	for _, unit := range []struct {
		threshold uint64
		suffix    string
	}{{1_000_000_000, "B"}, {1_000_000, "M"}, {1_000, "K"}} {
		if value < unit.threshold {
			continue
		}
		whole := value / unit.threshold
		decimal := (value % unit.threshold) * 10 / unit.threshold
		if decimal == 0 {
			return fmt.Sprintf("%d%s", whole, unit.suffix)
		}
		return fmt.Sprintf("%d.%d%s", whole, decimal, unit.suffix)
	}
	return fmt.Sprintf("%d", value)
}
