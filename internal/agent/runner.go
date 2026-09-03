package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	orbitv1 "orbit/gen/go/orbit/v1"
	"orbit/internal/mqtt"
	"orbit/internal/sources/codex"
	"orbit/internal/sources/sub2api"

	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type UsageSource interface {
	FetchUsage(context.Context) (sub2api.Usage, error)
}

type CodexSource interface {
	Fetch(context.Context) (codex.Snapshot, error)
}

type Transport interface {
	Publish(context.Context, mqtt.Message) error
	Subscribe(context.Context, string, mqtt.Handler) error
}

type Sources struct {
	Usage UsageSource
	Codex CodexSource
}

type CodexSessionOpener interface {
	Open(context.Context, string) error
}

type Capabilities struct {
	OpenCodexSession CodexSessionOpener
}

type Config struct {
	AgentID        string
	AgentEpoch     string
	AgentVersion   string
	HostLabel      string
	CurrencyCode   string
	Location       *time.Location
	PollInterval   time.Duration
	ObservationTTL time.Duration

	CodexPollInterval   time.Duration
	CodexObservationTTL time.Duration
	CodexDisplayName    bool
	CodexProjectName    bool
}

type sourceState struct {
	enabled     bool
	health      orbitv1.SourceHealth
	errorCode   string
	lastSuccess time.Time
}

type Runner struct {
	config       Config
	sources      Sources
	capabilities Capabilities
	transport    Transport
	logger       *zap.Logger
	now          func() time.Time

	mu        sync.Mutex
	commandMu sync.Mutex

	usageRevision  uint64
	codexRevision  uint64
	stateRevision  uint64
	sourceStates   map[orbitv1.ObservationType]sourceState
	commandResults map[string]cachedCommandResult
	commandOrder   []string
	resultRevision uint64
}

const (
	codexSessionIDMaxBytes   = 128
	codexDisplayNameMaxBytes = 192
	codexProjectNameMaxBytes = 96
	codexModelMaxBytes       = 64
)

func New(config Config, sources Sources, capabilities Capabilities, transport Transport, logger *zap.Logger) (*Runner, error) {
	return newRunner(config, sources, capabilities, transport, logger)
}

func newRunner(config Config, sources Sources, capabilities Capabilities, transport Transport, logger *zap.Logger) (*Runner, error) {
	if config.AgentID == "" || config.AgentEpoch == "" || config.AgentVersion == "" || config.HostLabel == "" {
		return nil, errors.New("agent identity, epoch, version, and host label are required")
	}
	if sources.Usage == nil && sources.Codex == nil {
		return nil, errors.New("at least one agent source is required")
	}
	if sources.Usage != nil {
		if config.Location == nil || config.PollInterval <= 0 || config.ObservationTTL < config.PollInterval {
			return nil, errors.New("usage timezone and valid polling durations are required")
		}
		if config.CurrencyCode != "USD" {
			return nil, errors.New("agent currency code must be USD in V1")
		}
	}
	if sources.Codex != nil && (config.CodexPollInterval <= 0 || config.CodexObservationTTL < config.CodexPollInterval) {
		return nil, errors.New("codex valid polling durations are required")
	}
	if capabilities.OpenCodexSession != nil && sources.Codex == nil {
		return nil, errors.New("open Codex session capability requires the Codex source")
	}
	if transport == nil {
		return nil, errors.New("agent transport is required")
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	states := make(map[orbitv1.ObservationType]sourceState, 2)
	if sources.Usage != nil {
		states[orbitv1.ObservationType_OBSERVATION_TYPE_USAGE] = sourceState{
			enabled: true,
			health:  orbitv1.SourceHealth_SOURCE_HEALTH_UNSPECIFIED,
		}
	}
	if sources.Codex != nil {
		states[orbitv1.ObservationType_OBSERVATION_TYPE_CODEX] = sourceState{
			enabled: true,
			health:  orbitv1.SourceHealth_SOURCE_HEALTH_UNSPECIFIED,
		}
	}
	return &Runner{
		config:         config,
		sources:        sources,
		capabilities:   capabilities,
		transport:      transport,
		logger:         logger,
		now:            time.Now,
		sourceStates:   states,
		commandResults: make(map[string]cachedCommandResult),
	}, nil
}

func (r *Runner) Run(ctx context.Context) error {
	if r.capabilities.OpenCodexSession != nil {
		topic := fmt.Sprintf("orbit/v1/agents/%s/commands", r.config.AgentID)
		if err := r.transport.Subscribe(ctx, topic, r.handleCommand); err != nil {
			return err
		}
		r.logger.Debug("agent command subscription ready", zap.String("topic", topic))
	}
	if err := r.publishState(ctx); err != nil {
		return err
	}

	var workers sync.WaitGroup
	if r.sources.Usage != nil {
		workers.Add(1)
		go func() {
			defer workers.Done()
			r.runUsage(ctx)
		}()
	}
	if r.sources.Codex != nil {
		workers.Add(1)
		go func() {
			defer workers.Done()
			r.runCodex(ctx)
		}()
	}
	<-ctx.Done()
	workers.Wait()
	return nil
}

func (r *Runner) PollOnce(ctx context.Context) error {
	if r.sources.Usage == nil {
		return errors.New("usage source is not configured")
	}
	usage, err := r.sources.Usage.FetchUsage(ctx)
	if err != nil {
		return r.handleSourceFailure(ctx, orbitv1.ObservationType_OBSERVATION_TYPE_USAGE, err)
	}

	now := r.now().UTC()
	expiresAt := now.Add(r.config.ObservationTTL)
	revision := r.nextRevision(orbitv1.ObservationType_OBSERVATION_TYPE_USAGE)
	windowStart, windowEnd := localDayWindow(now, r.config.Location)
	cost := usage.TodayActualCostMicros
	tokens := usage.TodayTokens
	tpm := usage.TPM
	observation := &orbitv1.Observation{
		Metadata: &orbitv1.Metadata{
			MessageId:  newID(),
			ProducerId: r.config.AgentID,
			Revision:   revision,
			ProducedAt: timestamppb.New(now),
			ExpiresAt:  timestamppb.New(expiresAt),
		},
		AgentEpoch: r.config.AgentEpoch,
		Payload: &orbitv1.Observation_Usage{Usage: &orbitv1.UsageObservation{
			WindowStart:      timestamppb.New(windowStart),
			WindowEnd:        timestamppb.New(windowEnd),
			ActualCostMicros: &cost,
			CurrencyCode:     r.config.CurrencyCode,
			TokenCount:       &tokens,
			Tpm:              &tpm,
			ObservedAt:       timestamppb.New(now),
		}},
	}
	if err := r.publishObservation(ctx, "usage", observation); err != nil {
		return r.handleSourceFailure(ctx, orbitv1.ObservationType_OBSERVATION_TYPE_USAGE, err)
	}
	return r.markSourceHealthy(ctx, orbitv1.ObservationType_OBSERVATION_TYPE_USAGE, now)
}

func (r *Runner) PollCodexOnce(ctx context.Context) error {
	if r.sources.Codex == nil {
		return errors.New("codex source is not configured")
	}
	snapshot, err := r.sources.Codex.Fetch(ctx)
	if err != nil {
		return r.handleSourceFailure(ctx, orbitv1.ObservationType_OBSERVATION_TYPE_CODEX, err)
	}

	now := r.now().UTC()
	revision := r.nextRevision(orbitv1.ObservationType_OBSERVATION_TYPE_CODEX)
	payload := &orbitv1.CodexObservation{
		TotalCount:   uint32(snapshot.TotalCount),
		RunningCount: uint32(snapshot.RunningCount),
		ObservedAt:   timestamppb.New(now),
	}
	for _, session := range snapshot.Sessions {
		item := &orbitv1.CodexSession{
			SessionId:    boundedUTF8(session.ID, codexSessionIDMaxBytes),
			Model:        boundedUTF8(session.Model, codexModelMaxBytes),
			Status:       codexSessionStatus(session.Status),
			ProcessAlive: session.ProcessAlive,
		}
		if r.config.CodexDisplayName {
			item.DisplayName = boundedUTF8(session.DisplayName, codexDisplayNameMaxBytes)
		}
		if r.config.CodexProjectName {
			item.ProjectName = boundedUTF8(session.ProjectName, codexProjectNameMaxBytes)
		}
		if !session.UpdatedAt.IsZero() {
			item.UpdatedAt = timestamppb.New(session.UpdatedAt)
		}
		payload.Sessions = append(payload.Sessions, item)
	}
	observation := &orbitv1.Observation{
		Metadata: &orbitv1.Metadata{
			MessageId:  newID(),
			ProducerId: r.config.AgentID,
			Revision:   revision,
			ProducedAt: timestamppb.New(now),
			ExpiresAt:  timestamppb.New(now.Add(r.config.CodexObservationTTL)),
		},
		AgentEpoch: r.config.AgentEpoch,
		Payload:    &orbitv1.Observation_Codex{Codex: payload},
	}
	if err := r.publishObservation(ctx, "codex", observation); err != nil {
		return r.handleSourceFailure(ctx, orbitv1.ObservationType_OBSERVATION_TYPE_CODEX, err)
	}
	return r.markSourceHealthy(ctx, orbitv1.ObservationType_OBSERVATION_TYPE_CODEX, now)
}

func (r *Runner) runUsage(ctx context.Context) {
	if err := r.PollOnce(ctx); err != nil && ctx.Err() == nil {
		r.logger.Warn("usage poll failed", zap.String("source_type", "usage"), zap.String("error_code", sourceErrorCode(err)))
	}
	ticker := time.NewTicker(r.config.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := r.PollOnce(ctx); err != nil && ctx.Err() == nil {
				r.logger.Warn("usage poll failed", zap.String("source_type", "usage"), zap.String("error_code", sourceErrorCode(err)))
			}
		}
	}
}

func (r *Runner) runCodex(ctx context.Context) {
	if err := r.PollCodexOnce(ctx); err != nil && ctx.Err() == nil {
		r.logger.Warn("codex poll failed", zap.String("source_type", "codex"), zap.String("error_code", sourceErrorCode(err)))
	}
	ticker := time.NewTicker(r.config.CodexPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := r.PollCodexOnce(ctx); err != nil && ctx.Err() == nil {
				r.logger.Warn("codex poll failed", zap.String("source_type", "codex"), zap.String("error_code", sourceErrorCode(err)))
			}
		}
	}
}

func (r *Runner) publishObservation(ctx context.Context, name string, observation *orbitv1.Observation) error {
	payload, err := proto.Marshal(observation)
	if err != nil {
		return fmt.Errorf("marshal %s observation: %w", name, err)
	}
	topic := fmt.Sprintf("orbit/v1/agents/%s/observations/%s", r.config.AgentID, name)
	if err := r.transport.Publish(ctx, mqtt.Message{Topic: topic, Payload: payload}); err != nil {
		return fmt.Errorf("publish %s observation: %w", name, err)
	}
	fields := []zap.Field{
		zap.String("source_type", name),
		zap.Uint64("revision", observation.GetMetadata().GetRevision()),
		zap.Int("bytes", len(payload)),
	}
	if name == "codex" {
		codexPayload := observation.GetCodex()
		fields = append(fields,
			zap.Uint32("total_count", codexPayload.GetTotalCount()),
			zap.Uint32("running_count", codexPayload.GetRunningCount()),
			zap.Int("session_count", len(codexPayload.GetSessions())),
		)
	}
	r.logger.Debug("observation published", fields...)
	return nil
}

func (r *Runner) nextRevision(source orbitv1.ObservationType) uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch source {
	case orbitv1.ObservationType_OBSERVATION_TYPE_USAGE:
		r.usageRevision++
		return r.usageRevision
	case orbitv1.ObservationType_OBSERVATION_TYPE_CODEX:
		r.codexRevision++
		return r.codexRevision
	default:
		return 0
	}
}

func (r *Runner) markSourceHealthy(ctx context.Context, source orbitv1.ObservationType, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.sourceStates[source]
	state.health = orbitv1.SourceHealth_SOURCE_HEALTH_HEALTHY
	state.errorCode = ""
	state.lastSuccess = now
	r.sourceStates[source] = state
	return r.publishStateLocked(ctx)
}

func (r *Runner) handleSourceFailure(ctx context.Context, source orbitv1.ObservationType, err error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.sourceStates[source]
	if state.lastSuccess.IsZero() {
		state.health = orbitv1.SourceHealth_SOURCE_HEALTH_FAILED
	} else {
		state.health = orbitv1.SourceHealth_SOURCE_HEALTH_DEGRADED
	}
	state.errorCode = sourceErrorCode(err)
	r.sourceStates[source] = state
	publishErr := r.publishStateLocked(ctx)
	return errors.Join(err, publishErr)
}

func (r *Runner) publishState(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.publishStateLocked(ctx)
}

func (r *Runner) publishStateLocked(ctx context.Context) error {
	r.stateRevision++
	now := r.now().UTC()
	state := &orbitv1.AgentState{
		Metadata: &orbitv1.Metadata{
			MessageId:  newID(),
			ProducerId: r.config.AgentID,
			Revision:   r.stateRevision,
			ProducedAt: timestamppb.New(now),
		},
		AgentId:      r.config.AgentID,
		AgentEpoch:   r.config.AgentEpoch,
		AgentVersion: r.config.AgentVersion,
		HostLabel:    r.config.HostLabel,
	}
	for _, source := range []orbitv1.ObservationType{
		orbitv1.ObservationType_OBSERVATION_TYPE_USAGE,
		orbitv1.ObservationType_OBSERVATION_TYPE_CODEX,
	} {
		status, ok := r.sourceStates[source]
		if !ok || !status.enabled {
			continue
		}
		item := &orbitv1.SourceStatus{
			ObservationType: source,
			Enabled:         true,
			Health:          status.health,
			ErrorCode:       status.errorCode,
		}
		if !status.lastSuccess.IsZero() {
			item.LastSuccessAt = timestamppb.New(status.lastSuccess)
		}
		state.Sources = append(state.Sources, item)
	}
	payload, err := proto.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshal agent state: %w", err)
	}
	topic := fmt.Sprintf("orbit/v1/agents/%s/state", r.config.AgentID)
	if err := r.transport.Publish(ctx, mqtt.Message{Topic: topic, Payload: payload, Retain: true}); err != nil {
		return fmt.Errorf("publish agent state: %w", err)
	}
	r.logger.Debug("agent state published",
		zap.Uint64("revision", r.stateRevision),
		zap.Int("sources", len(state.Sources)),
		zap.Int("bytes", len(payload)),
	)
	return nil
}

func localDayWindow(now time.Time, location *time.Location) (time.Time, time.Time) {
	local := now.In(location)
	start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location)
	return start.UTC(), start.AddDate(0, 0, 1).UTC()
}

func sourceErrorCode(err error) string {
	var sourceErr *sub2api.Error
	if errors.As(err, &sourceErr) {
		return string(sourceErr.Kind)
	}
	var codexErr *codex.Error
	if errors.As(err, &codexErr) {
		return string(codexErr.Kind)
	}
	return "source_error"
}

func codexSessionStatus(value string) orbitv1.CodexSessionStatus {
	switch strings.ReplaceAll(strings.ToLower(strings.TrimSpace(value)), "_", "") {
	case "running", "inprogress":
		return orbitv1.CodexSessionStatus_CODEX_SESSION_STATUS_RUNNING
	case "completed":
		return orbitv1.CodexSessionStatus_CODEX_SESSION_STATUS_COMPLETED
	case "failed", "error":
		return orbitv1.CodexSessionStatus_CODEX_SESSION_STATUS_FAILED
	case "interrupted":
		return orbitv1.CodexSessionStatus_CODEX_SESSION_STATUS_INTERRUPTED
	case "cancelled", "canceled":
		return orbitv1.CodexSessionStatus_CODEX_SESSION_STATUS_CANCELLED
	default:
		return orbitv1.CodexSessionStatus_CODEX_SESSION_STATUS_UNKNOWN
	}
}

func boundedUTF8(value string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	value = strings.ToValidUTF8(value, "")
	if len(value) <= maxBytes {
		return value
	}
	value = value[:maxBytes]
	for len(value) > 0 && !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}
