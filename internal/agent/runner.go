package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	orbitv1 "orbit/gen/go/orbit/v1"
	"orbit/internal/mqtt"
	"orbit/internal/sources/sub2api"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type UsageSource interface {
	FetchUsage(context.Context) (sub2api.Usage, error)
}

type Publisher interface {
	Publish(context.Context, mqtt.Message) error
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
}

type Runner struct {
	config    Config
	source    UsageSource
	publisher Publisher
	logger    *slog.Logger
	now       func() time.Time

	observationRevision uint64
	stateRevision       uint64
	lastSuccess         time.Time
}

func New(config Config, source UsageSource, publisher Publisher, logger *slog.Logger) (*Runner, error) {
	if config.AgentID == "" || config.AgentEpoch == "" || config.AgentVersion == "" || config.HostLabel == "" {
		return nil, errors.New("agent identity, epoch, version, and host label are required")
	}
	if config.Location == nil || config.PollInterval <= 0 || config.ObservationTTL < config.PollInterval {
		return nil, errors.New("agent timezone and valid polling durations are required")
	}
	if config.CurrencyCode != "USD" {
		return nil, errors.New("agent currency code must be USD in V1")
	}
	if source == nil || publisher == nil {
		return nil, errors.New("agent source and publisher are required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Runner{config: config, source: source, publisher: publisher, logger: logger, now: time.Now}, nil
}

func (r *Runner) Run(ctx context.Context) error {
	if err := r.publishState(ctx, orbitv1.SourceHealth_SOURCE_HEALTH_UNSPECIFIED, ""); err != nil {
		return err
	}
	r.pollAndLog(ctx)
	ticker := time.NewTicker(r.config.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			r.pollAndLog(ctx)
		}
	}
}

func (r *Runner) PollOnce(ctx context.Context) error {
	usage, err := r.source.FetchUsage(ctx)
	if err != nil {
		health := orbitv1.SourceHealth_SOURCE_HEALTH_FAILED
		if !r.lastSuccess.IsZero() {
			health = orbitv1.SourceHealth_SOURCE_HEALTH_DEGRADED
		}
		publishErr := r.publishState(ctx, health, sourceErrorCode(err))
		return errors.Join(err, publishErr)
	}

	now := r.now().UTC()
	r.observationRevision++
	windowStart, windowEnd := localDayWindow(now, r.config.Location)
	cost := usage.TodayActualCostMicros
	tokens := usage.TodayTokens
	tpm := usage.TPM
	observation := &orbitv1.Observation{
		Metadata: &orbitv1.Metadata{
			MessageId:  newID(),
			ProducerId: r.config.AgentID,
			Revision:   r.observationRevision,
			ProducedAt: timestamppb.New(now),
			ExpiresAt:  timestamppb.New(now.Add(r.config.ObservationTTL)),
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
	payload, err := proto.Marshal(observation)
	if err != nil {
		return fmt.Errorf("marshal usage observation: %w", err)
	}
	if err := r.publisher.Publish(ctx, mqtt.Message{
		Topic:   fmt.Sprintf("orbit/v1/agents/%s/observations/usage", r.config.AgentID),
		Payload: payload,
	}); err != nil {
		return fmt.Errorf("publish usage observation: %w", err)
	}
	r.lastSuccess = now
	if err := r.publishState(ctx, orbitv1.SourceHealth_SOURCE_HEALTH_HEALTHY, ""); err != nil {
		return err
	}
	return nil
}

func (r *Runner) pollAndLog(ctx context.Context) {
	if err := r.PollOnce(ctx); err != nil && ctx.Err() == nil {
		r.logger.Warn("sub2api poll failed", "error", err)
	}
}

func (r *Runner) publishState(ctx context.Context, health orbitv1.SourceHealth, errorCode string) error {
	r.stateRevision++
	now := r.now().UTC()
	status := &orbitv1.SourceStatus{
		ObservationType: orbitv1.ObservationType_OBSERVATION_TYPE_USAGE,
		Enabled:         true,
		Health:          health,
		ErrorCode:       errorCode,
	}
	if !r.lastSuccess.IsZero() {
		status.LastSuccessAt = timestamppb.New(r.lastSuccess)
	}
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
		Sources:      []*orbitv1.SourceStatus{status},
	}
	payload, err := proto.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshal agent state: %w", err)
	}
	if err := r.publisher.Publish(ctx, mqtt.Message{
		Topic:   fmt.Sprintf("orbit/v1/agents/%s/state", r.config.AgentID),
		Payload: payload,
		Retain:  true,
	}); err != nil {
		return fmt.Errorf("publish agent state: %w", err)
	}
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
	return "source_error"
}
