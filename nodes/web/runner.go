package web

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	orbitv1 "orbit/gen/go/orbit/v1"
	"orbit/internal/mqtt"

	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	maxViewPayload = 32 * 1024
	intentTTL      = 20 * time.Second
)

type Transport interface {
	Publish(context.Context, mqtt.Message) error
	Subscribe(context.Context, string, mqtt.Handler) error
}

type RunnerConfig struct {
	NodeID          string
	NodeEpoch       string
	FirmwareVersion string
	Now             func() time.Time
}

type Runner struct {
	config    RunnerConfig
	transport Transport
	store     *Store
	logger    *zap.Logger
	now       func() time.Time

	intentMu       sync.Mutex
	intentRevision uint64
}

func NewRunner(config RunnerConfig, transport Transport, store *Store, logger *zap.Logger) (*Runner, error) {
	if config.NodeID == "" || config.NodeEpoch == "" || config.FirmwareVersion == "" {
		return nil, errors.New("web node id, epoch, and version are required")
	}
	if transport == nil || store == nil {
		return nil, errors.New("web node transport and store are required")
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	return &Runner{config: config, transport: transport, store: store, logger: logger, now: now}, nil
}

func (r *Runner) OpenCodexSession(ctx context.Context, sessionID string, viewRevision uint64) (string, error) {
	snapshot, err := r.store.Snapshot()
	if err != nil {
		return "", err
	}
	if snapshot == nil || snapshot.Codex == nil || snapshot.Codex.Freshness != "fresh" {
		return "", errors.New("fresh Codex state is unavailable")
	}
	if viewRevision == 0 || viewRevision > snapshot.Revision {
		return "", errors.New("view revision is invalid")
	}
	found := false
	for _, session := range snapshot.Codex.Sessions {
		if session.ID == sessionID {
			found = true
			break
		}
	}
	if !found {
		return "", errors.New("Codex session is not present in the current view")
	}

	r.intentMu.Lock()
	r.intentRevision++
	revision := r.intentRevision
	r.intentMu.Unlock()
	now := r.now().UTC()
	intentID := newID()
	intent := &orbitv1.Intent{
		Metadata: &orbitv1.Metadata{
			MessageId:  newID(),
			ProducerId: r.config.NodeID,
			Revision:   revision,
			ProducedAt: timestamppb.New(now),
			ExpiresAt:  timestamppb.New(now.Add(intentTTL)),
		},
		IntentId:     intentID,
		NodeEpoch:    r.config.NodeEpoch,
		ViewRevision: viewRevision,
		Action: &orbitv1.Intent_OpenCodexSession{
			OpenCodexSession: &orbitv1.OpenCodexSessionIntent{SessionId: sessionID},
		},
	}
	payload, err := proto.Marshal(intent)
	if err != nil {
		return "", fmt.Errorf("marshal open Codex session intent: %w", err)
	}
	topic := fmt.Sprintf("orbit/v1/nodes/%s/intents", r.config.NodeID)
	if err := r.transport.Publish(ctx, mqtt.Message{Topic: topic, Payload: payload}); err != nil {
		return "", fmt.Errorf("publish open Codex session intent: %w", err)
	}
	r.logger.Info("open Codex session intent published",
		zap.String("intent_id", intentID),
		zap.Uint64("view_revision", viewRevision),
	)
	return intentID, nil
}

func (r *Runner) Run(ctx context.Context) error {
	viewTopic := fmt.Sprintf("orbit/v1/nodes/%s/view", r.config.NodeID)
	if err := r.transport.Subscribe(ctx, viewTopic, r.handleView); err != nil {
		return err
	}
	if err := r.publishState(ctx); err != nil {
		return err
	}
	r.logger.Info("web node subscription ready",
		zap.String("node_id", r.config.NodeID),
		zap.String("topic", viewTopic),
		zap.Int("qos", 1),
	)
	<-ctx.Done()
	return nil
}

func (r *Runner) handleView(_ context.Context, message mqtt.Message) error {
	if len(message.Payload) == 0 || len(message.Payload) > maxViewPayload {
		return fmt.Errorf("view payload size %d is outside 1..%d", len(message.Payload), maxViewPayload)
	}
	var view orbitv1.DeviceView
	if err := proto.Unmarshal(message.Payload, &view); err != nil {
		return fmt.Errorf("decode device view: %w", err)
	}
	if view.NodeId != r.config.NodeID || view.Metadata == nil || view.Metadata.Revision == 0 || view.CoreEpoch == "" {
		return errors.New("device view identity or metadata is invalid")
	}
	if err := r.store.Update(&view, r.now().UTC()); err != nil {
		return err
	}
	r.logger.Info("web view accepted",
		zap.String("node_id", r.config.NodeID),
		zap.String("topic", message.Topic),
		zap.Uint64("revision", view.Metadata.Revision),
		zap.String("freshness", view.Freshness.String()),
		zap.Bool("retained", message.Retain),
		zap.Int("bytes", len(message.Payload)),
	)
	return nil
}

func (r *Runner) publishState(ctx context.Context) error {
	now := r.now().UTC()
	state := &orbitv1.NodeState{
		Metadata: &orbitv1.Metadata{
			MessageId:  newID(),
			ProducerId: r.config.NodeID,
			Revision:   1,
			ProducedAt: timestamppb.New(now),
		},
		NodeId:          r.config.NodeID,
		NodeEpoch:       r.config.NodeEpoch,
		SeriesId:        "display",
		ModelId:         "web",
		VariantId:       "browser",
		FirmwareVersion: r.config.FirmwareVersion,
	}
	payload, err := proto.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshal web node state: %w", err)
	}
	topic := fmt.Sprintf("orbit/v1/nodes/%s/state", r.config.NodeID)
	if err := r.transport.Publish(ctx, mqtt.Message{Topic: topic, Payload: payload, Retain: true}); err != nil {
		return err
	}
	r.logger.Info("web node state published",
		zap.String("node_id", r.config.NodeID),
		zap.String("node_epoch", r.config.NodeEpoch),
		zap.String("firmware_version", r.config.FirmwareVersion),
		zap.String("topic", topic),
		zap.Uint64("revision", state.Metadata.Revision),
		zap.Int("qos", 1),
		zap.Bool("retained", true),
		zap.Int("bytes", len(payload)),
	)
	return nil
}

func NewEpoch() string { return newID() }

func newID() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		panic(fmt.Sprintf("read crypto random bytes: %v", err))
	}
	return hex.EncodeToString(value[:])
}
