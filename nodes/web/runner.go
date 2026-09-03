package web

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	orbitv1 "orbit/gen/go/orbit/v1"
	"orbit/internal/mqtt"

	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const maxViewPayload = 32 * 1024

type Transport interface {
	Publish(context.Context, mqtt.Message) error
	Subscribe(context.Context, string, mqtt.Handler) error
}

type RunnerConfig struct {
	NodeID          string
	NodeEpoch       string
	FirmwareVersion string
}

type Runner struct {
	config    RunnerConfig
	transport Transport
	store     *Store
	logger    *zap.Logger
	now       func() time.Time
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
	return &Runner{config: config, transport: transport, store: store, logger: logger, now: time.Now}, nil
}

func (r *Runner) Run(ctx context.Context) error {
	viewTopic := fmt.Sprintf("orbit/v1/nodes/%s/view", r.config.NodeID)
	if err := r.transport.Subscribe(ctx, viewTopic, r.handleView); err != nil {
		return err
	}
	if err := r.publishState(ctx); err != nil {
		return err
	}
	r.logger.Debug("web node subscription ready", zap.String("topic", viewTopic))
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
	r.logger.Debug("web view accepted",
		zap.Uint64("revision", view.Metadata.Revision),
		zap.String("freshness", view.Freshness.String()),
		zap.Bool("retained", message.Retain),
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
