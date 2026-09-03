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
	webModel      = "web"
	webVariant    = "browser"
	webProfile    = "overview-web"
)

type RouteInput struct {
	AgentID         string
	ObservationType orbitv1.ObservationType
}

type Route struct {
	NodeID  string
	AgentID string
	Profile string
	Inputs  []RouteInput
}

type UsagePolicy struct {
	MaxTTL        time.Duration
	MaxFutureSkew time.Duration
}

type CodexPolicy struct {
	MaxTTL        time.Duration
	MaxFutureSkew time.Duration
}

type Config struct {
	CoreID      string
	CoreEpoch   string
	Routes      []Route
	UsagePolicy UsagePolicy
	CodexPolicy CodexPolicy
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

type canonicalCodex struct {
	value      *orbitv1.CodexObservation
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
	codex        map[string]canonicalCodex
	viewRevision map[string]uint64
	lastFresh    map[string]string
}

func New(config Config) (*Engine, error) {
	if config.CoreID == "" || config.CoreEpoch == "" {
		return nil, errors.New("core id and epoch are required")
	}
	if config.RetainFor <= 0 {
		config.RetainFor = 24 * time.Hour
	}
	seen := make(map[string]struct{}, len(config.Routes))
	for index := range config.Routes {
		route := &config.Routes[index]
		if len(route.Inputs) == 0 && route.AgentID != "" {
			route.Inputs = []RouteInput{{AgentID: route.AgentID, ObservationType: orbitv1.ObservationType_OBSERVATION_TYPE_USAGE}}
		}
		if route.NodeID == "" || (route.Profile != usageProfile && route.Profile != webProfile) || len(route.Inputs) == 0 {
			return nil, fmt.Errorf("invalid route for node %q", route.NodeID)
		}
		inputTypes := make(map[orbitv1.ObservationType]struct{}, len(route.Inputs))
		for _, input := range route.Inputs {
			if input.AgentID == "" || (input.ObservationType != orbitv1.ObservationType_OBSERVATION_TYPE_USAGE && input.ObservationType != orbitv1.ObservationType_OBSERVATION_TYPE_CODEX) {
				return nil, fmt.Errorf("invalid route input for node %q", route.NodeID)
			}
			if _, duplicate := inputTypes[input.ObservationType]; duplicate {
				return nil, fmt.Errorf("duplicate route input %s for node %q", input.ObservationType, route.NodeID)
			}
			inputTypes[input.ObservationType] = struct{}{}
		}
		if route.Profile == usageProfile {
			if len(route.Inputs) != 1 || route.Inputs[0].ObservationType != orbitv1.ObservationType_OBSERVATION_TYPE_USAGE {
				return nil, fmt.Errorf("invalid usage route for node %q", route.NodeID)
			}
		}
		if _, duplicate := seen[route.NodeID]; duplicate {
			return nil, fmt.Errorf("duplicate route for node %q", route.NodeID)
		}
		seen[route.NodeID] = struct{}{}
	}
	if routeUses(config.Routes, orbitv1.ObservationType_OBSERVATION_TYPE_USAGE) &&
		(config.UsagePolicy.MaxTTL <= 0 || config.UsagePolicy.MaxFutureSkew < 0) {
		return nil, errors.New("usage max TTL must be positive and future skew cannot be negative")
	}
	if routeUses(config.Routes, orbitv1.ObservationType_OBSERVATION_TYPE_CODEX) &&
		(config.CodexPolicy.MaxTTL <= 0 || config.CodexPolicy.MaxFutureSkew < 0) {
		return nil, errors.New("codex max TTL must be positive and future skew cannot be negative")
	}
	return &Engine{
		config:       config,
		agents:       make(map[string]participantState),
		nodes:        make(map[string]participantState),
		nodeProducts: make(map[string]*orbitv1.NodeState),
		usage:        make(map[string]canonicalUsage),
		codex:        make(map[string]canonicalCodex),
		viewRevision: make(map[string]uint64),
		lastFresh:    make(map[string]string),
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
	sourceEnabled := false
	for _, source := range state.Sources {
		if (source.ObservationType == orbitv1.ObservationType_OBSERVATION_TYPE_USAGE ||
			source.ObservationType == orbitv1.ObservationType_OBSERVATION_TYPE_CODEX) && source.Enabled {
			sourceEnabled = true
		}
	}
	if !sourceEnabled {
		return errors.New("agent state does not declare an enabled source")
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
			delete(e.codex, state.AgentId)
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
	if state.SeriesId != displaySeries ||
		!((state.ModelId == oledModel && state.VariantId == ydVariant) || (state.ModelId == webModel && state.VariantId == webVariant)) {
		return nil, fmt.Errorf("unsupported node product %s/%s/%s", state.SeriesId, state.ModelId, state.VariantId)
	}
	route := e.routeForNode(state.NodeId)
	if route != nil && ((route.Profile == usageProfile && state.ModelId != oledModel) || (route.Profile == webProfile && state.ModelId != webModel)) {
		return nil, fmt.Errorf("node product %s does not match projection profile %s", state.ModelId, route.Profile)
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
	observationType := orbitv1.ObservationType_OBSERVATION_TYPE_UNSPECIFIED
	switch payload := observation.Payload.(type) {
	case *orbitv1.Observation_Usage:
		observationType = orbitv1.ObservationType_OBSERVATION_TYPE_USAGE
		if current, exists := e.usage[metadata.ProducerId]; exists && current.agentEpoch == observation.AgentEpoch && metadata.Revision <= current.revision {
			return nil, errors.New("stale observation revision")
		}
		expiresAt, err := e.validateUsage(now, metadata, payload.Usage)
		if err != nil {
			return nil, err
		}
		e.usage[metadata.ProducerId] = canonicalUsage{
			value:      proto.Clone(payload.Usage).(*orbitv1.UsageObservation),
			expiresAt:  expiresAt,
			revision:   metadata.Revision,
			agentEpoch: observation.AgentEpoch,
		}
	case *orbitv1.Observation_Codex:
		observationType = orbitv1.ObservationType_OBSERVATION_TYPE_CODEX
		if current, exists := e.codex[metadata.ProducerId]; exists && current.agentEpoch == observation.AgentEpoch && metadata.Revision <= current.revision {
			return nil, errors.New("stale observation revision")
		}
		expiresAt, err := e.validateCodex(now, metadata, payload.Codex)
		if err != nil {
			return nil, err
		}
		e.codex[metadata.ProducerId] = canonicalCodex{
			value:      proto.Clone(payload.Codex).(*orbitv1.CodexObservation),
			expiresAt:  expiresAt,
			revision:   metadata.Revision,
			agentEpoch: observation.AgentEpoch,
		}
	default:
		return nil, errors.New("unsupported observation payload")
	}

	var views []*orbitv1.DeviceView
	for _, route := range e.config.Routes {
		if !routeHasInput(route, metadata.ProducerId, observationType) {
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
		signature := e.freshnessSignature(now, route)
		if signature == e.lastFresh[route.NodeID] {
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
	if usage == nil {
		return time.Time{}, errors.New("usage payload is required")
	}
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

func (e *Engine) validateCodex(now time.Time, metadata *orbitv1.Metadata, value *orbitv1.CodexObservation) (time.Time, error) {
	if value == nil {
		return time.Time{}, errors.New("codex payload is required")
	}
	producedAt, err := requiredTimestamp(metadata.ProducedAt, "observation produced_at")
	if err != nil {
		return time.Time{}, err
	}
	if producedAt.After(now.Add(e.config.CodexPolicy.MaxFutureSkew)) {
		return time.Time{}, errors.New("observation produced_at is too far in the future")
	}
	payloadExpiry, err := requiredTimestamp(metadata.ExpiresAt, "observation expires_at")
	if err != nil {
		return time.Time{}, err
	}
	observedAt, err := requiredTimestamp(value.ObservedAt, "codex observed_at")
	if err != nil {
		return time.Time{}, err
	}
	if observedAt.After(now.Add(e.config.CodexPolicy.MaxFutureSkew)) {
		return time.Time{}, errors.New("codex observed_at is too far in the future")
	}
	if value.RunningCount > value.TotalCount || uint32(len(value.Sessions)) > value.TotalCount || len(value.Sessions) > 20 {
		return time.Time{}, errors.New("codex counts or session limit are invalid")
	}
	for _, session := range value.Sessions {
		if session == nil || session.SessionId == "" || len(session.SessionId) > 128 {
			return time.Time{}, errors.New("codex session id is invalid")
		}
		if len(session.DisplayName) > 256 || len(session.ProjectName) > 128 || len(session.Model) > 128 {
			return time.Time{}, errors.New("codex session text exceeds its limit")
		}
		if session.Status == orbitv1.CodexSessionStatus_CODEX_SESSION_STATUS_UNSPECIFIED {
			return time.Time{}, errors.New("codex session status is required")
		}
		if _, err := requiredTimestamp(session.UpdatedAt, "codex session updated_at"); err != nil {
			return time.Time{}, err
		}
	}
	effectiveExpiry := minTime(payloadExpiry, observedAt.Add(e.config.CodexPolicy.MaxTTL), now.Add(e.config.CodexPolicy.MaxTTL))
	if !effectiveExpiry.After(now) {
		return time.Time{}, errors.New("observation is already expired")
	}
	return effectiveExpiry, nil
}

func (e *Engine) projectNodeLocked(now time.Time, nodeID string) ([]*orbitv1.DeviceView, error) {
	if _, exists := e.nodeProducts[nodeID]; !exists {
		return nil, nil
	}
	route := e.routeForNode(nodeID)
	if route == nil {
		return nil, nil
	}
	usage, hasUsage := e.usageForRoute(*route)
	codex, hasCodex := e.codexForRoute(*route)
	if !hasUsage && !hasCodex {
		return nil, nil
	}
	freshness, freshUntil, retainUntil := e.routeFreshness(now, *route, usage, hasUsage, codex, hasCodex)
	e.viewRevision[nodeID]++
	e.lastFresh[nodeID] = e.freshnessSignature(now, *route)
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
		FreshUntil:  timestamppb.New(freshUntil),
		RetainUntil: timestamppb.New(retainUntil),
	}
	if hasUsage {
		cost := usage.value.GetActualCostMicros()
		tokens := usage.value.GetTokenCount()
		tpm := usage.value.GetTpm()
		if route.Profile == webProfile {
			view.Usage = &orbitv1.UsageView{
				Freshness:        freshnessAt(now, usage.expiresAt),
				FreshUntil:       timestamppb.New(usage.expiresAt),
				ActualCostMicros: &cost,
				CurrencyCode:     usage.value.CurrencyCode,
				TokenCount:       &tokens,
				Tpm:              &tpm,
				ObservedAt:       usage.value.ObservedAt,
			}
		}
		view.Primary = &orbitv1.DisplaySlot{Text: formatCost(cost), Emphasis: orbitv1.Emphasis_EMPHASIS_STRONG}
		view.Secondary = &orbitv1.DisplaySlot{Text: formatMetric(tokens), Emphasis: orbitv1.Emphasis_EMPHASIS_NORMAL}
		view.Footer = &orbitv1.DisplaySlot{Text: formatMetric(tpm), Emphasis: orbitv1.Emphasis_EMPHASIS_DIM}
	}
	if hasCodex {
		view.Codex = &orbitv1.CodexView{
			Freshness:    freshnessAt(now, codex.expiresAt),
			FreshUntil:   timestamppb.New(codex.expiresAt),
			TotalCount:   codex.value.TotalCount,
			RunningCount: codex.value.RunningCount,
			ObservedAt:   codex.value.ObservedAt,
		}
		for _, session := range codex.value.Sessions {
			view.Codex.Sessions = append(view.Codex.Sessions, &orbitv1.CodexSessionView{
				SessionId:    session.SessionId,
				DisplayName:  session.DisplayName,
				ProjectName:  session.ProjectName,
				Model:        session.Model,
				Status:       session.Status,
				UpdatedAt:    session.UpdatedAt,
				ProcessAlive: session.ProcessAlive,
			})
		}
	}
	return []*orbitv1.DeviceView{view}, nil
}

func (e *Engine) routeForNode(nodeID string) *Route {
	for index := range e.config.Routes {
		if e.config.Routes[index].NodeID == nodeID {
			return &e.config.Routes[index]
		}
	}
	return nil
}

func (e *Engine) usageForRoute(route Route) (canonicalUsage, bool) {
	for _, input := range route.Inputs {
		if input.ObservationType == orbitv1.ObservationType_OBSERVATION_TYPE_USAGE {
			value, ok := e.usage[input.AgentID]
			return value, ok
		}
	}
	return canonicalUsage{}, false
}

func (e *Engine) codexForRoute(route Route) (canonicalCodex, bool) {
	for _, input := range route.Inputs {
		if input.ObservationType == orbitv1.ObservationType_OBSERVATION_TYPE_CODEX {
			value, ok := e.codex[input.AgentID]
			return value, ok
		}
	}
	return canonicalCodex{}, false
}

func (e *Engine) routeFreshness(now time.Time, route Route, usage canonicalUsage, hasUsage bool, codex canonicalCodex, hasCodex bool) (orbitv1.Freshness, time.Time, time.Time) {
	freshness := orbitv1.Freshness_FRESHNESS_FRESH
	var expiries []time.Time
	for _, input := range route.Inputs {
		switch input.ObservationType {
		case orbitv1.ObservationType_OBSERVATION_TYPE_USAGE:
			if !hasUsage {
				freshness = orbitv1.Freshness_FRESHNESS_STALE
				continue
			}
			expiries = append(expiries, usage.expiresAt)
			if freshnessAt(now, usage.expiresAt) == orbitv1.Freshness_FRESHNESS_STALE {
				freshness = orbitv1.Freshness_FRESHNESS_STALE
			}
		case orbitv1.ObservationType_OBSERVATION_TYPE_CODEX:
			if !hasCodex {
				freshness = orbitv1.Freshness_FRESHNESS_STALE
				continue
			}
			expiries = append(expiries, codex.expiresAt)
			if freshnessAt(now, codex.expiresAt) == orbitv1.Freshness_FRESHNESS_STALE {
				freshness = orbitv1.Freshness_FRESHNESS_STALE
			}
		}
	}
	if len(expiries) == 0 {
		return orbitv1.Freshness_FRESHNESS_STALE, now, now.Add(e.config.RetainFor)
	}
	freshUntil := minTime(expiries...)
	retainUntil := expiries[0]
	for _, expiry := range expiries[1:] {
		if expiry.After(retainUntil) {
			retainUntil = expiry
		}
	}
	return freshness, freshUntil, retainUntil.Add(e.config.RetainFor)
}

func (e *Engine) freshnessSignature(now time.Time, route Route) string {
	parts := make([]string, 0, len(route.Inputs))
	for _, input := range route.Inputs {
		switch input.ObservationType {
		case orbitv1.ObservationType_OBSERVATION_TYPE_USAGE:
			value, ok := e.usage[input.AgentID]
			parts = append(parts, freshnessPart(ok, now, value.expiresAt))
		case orbitv1.ObservationType_OBSERVATION_TYPE_CODEX:
			value, ok := e.codex[input.AgentID]
			parts = append(parts, freshnessPart(ok, now, value.expiresAt))
		}
	}
	return fmt.Sprint(parts)
}

func freshnessPart(exists bool, now, expiresAt time.Time) string {
	if !exists {
		return "missing"
	}
	return freshnessAt(now, expiresAt).String()
}

func freshnessAt(now, expiresAt time.Time) orbitv1.Freshness {
	if now.After(expiresAt) {
		return orbitv1.Freshness_FRESHNESS_STALE
	}
	return orbitv1.Freshness_FRESHNESS_FRESH
}

func routeUses(routes []Route, observationType orbitv1.ObservationType) bool {
	for _, route := range routes {
		for _, input := range route.Inputs {
			if input.ObservationType == observationType {
				return true
			}
		}
	}
	return false
}

func routeHasInput(route Route, agentID string, observationType orbitv1.ObservationType) bool {
	for _, input := range route.Inputs {
		if input.AgentID == agentID && input.ObservationType == observationType {
			return true
		}
	}
	return false
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
		return fmt.Sprintf("%d%s", whole, unit.suffix)
	}
	return fmt.Sprintf("%d", value)
}
