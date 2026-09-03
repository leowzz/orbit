package core

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	orbitv1 "orbit/gen/go/orbit/v1"
	"orbit/internal/mqtt"

	"google.golang.org/protobuf/proto"
)

const (
	maxInboundPayload    = 8 * 1024
	maxDeviceViewPayload = 1024
)

type Transport interface {
	Publish(context.Context, mqtt.Message) error
	Subscribe(context.Context, string, mqtt.Handler) error
}

type Runner struct {
	engine    *Engine
	transport Transport
	logger    *slog.Logger
	now       func() time.Time
}

func NewRunner(engine *Engine, transport Transport, logger *slog.Logger) (*Runner, error) {
	if engine == nil || transport == nil {
		return nil, errors.New("core engine and transport are required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Runner{engine: engine, transport: transport, logger: logger, now: time.Now}, nil
}

func (r *Runner) Run(ctx context.Context) error {
	for _, item := range []struct {
		filter  string
		handler mqtt.Handler
	}{
		{filter: "orbit/v1/agents/+/state", handler: r.handleAgentState},
		{filter: "orbit/v1/nodes/+/state", handler: r.handleNodeState},
		{filter: "orbit/v1/agents/+/observations/usage", handler: r.handleObservation},
	} {
		if err := r.transport.Subscribe(ctx, item.filter, item.handler); err != nil {
			return err
		}
	}

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case now := <-ticker.C:
			views, err := r.engine.Refresh(now.UTC())
			if err != nil {
				r.logger.Warn("refresh device views failed", "error", err)
				continue
			}
			if err := r.publishViews(ctx, views); err != nil {
				r.logger.Warn("publish stale device views failed", "error", err)
			}
		}
	}
}

func (r *Runner) handleAgentState(_ context.Context, message mqtt.Message) error {
	agentID, err := topicIdentity(message.Topic, "agents", "state")
	if err != nil {
		return err
	}
	var state orbitv1.AgentState
	if err := unmarshalInbound(message.Payload, &state); err != nil {
		return fmt.Errorf("decode agent state: %w", err)
	}
	if state.AgentId != agentID {
		return errors.New("agent state topic identity does not match payload")
	}
	return r.engine.ApplyAgentState(&state)
}

func (r *Runner) handleNodeState(ctx context.Context, message mqtt.Message) error {
	nodeID, err := topicIdentity(message.Topic, "nodes", "state")
	if err != nil {
		return err
	}
	var state orbitv1.NodeState
	if err := unmarshalInbound(message.Payload, &state); err != nil {
		return fmt.Errorf("decode node state: %w", err)
	}
	if state.NodeId != nodeID {
		return errors.New("node state topic identity does not match payload")
	}
	views, err := r.engine.ApplyNodeState(r.now().UTC(), &state)
	if err != nil {
		return err
	}
	return r.publishViews(ctx, views)
}

func (r *Runner) handleObservation(ctx context.Context, message mqtt.Message) error {
	agentID, err := topicIdentity(message.Topic, "agents", "observations", "usage")
	if err != nil {
		return err
	}
	var observation orbitv1.Observation
	if err := unmarshalInbound(message.Payload, &observation); err != nil {
		return fmt.Errorf("decode usage observation: %w", err)
	}
	if observation.Metadata == nil || observation.Metadata.ProducerId != agentID {
		return errors.New("observation topic identity does not match payload")
	}
	views, err := r.engine.ApplyObservation(r.now().UTC(), &observation)
	if err != nil {
		return err
	}
	return r.publishViews(ctx, views)
}

func (r *Runner) publishViews(ctx context.Context, views []*orbitv1.DeviceView) error {
	for _, view := range views {
		payload, err := proto.Marshal(view)
		if err != nil {
			return fmt.Errorf("marshal view for node %q: %w", view.NodeId, err)
		}
		if len(payload) > maxDeviceViewPayload {
			return fmt.Errorf("view for node %q exceeds %d bytes", view.NodeId, maxDeviceViewPayload)
		}
		if err := r.transport.Publish(ctx, mqtt.Message{
			Topic:   fmt.Sprintf("orbit/v1/nodes/%s/view", view.NodeId),
			Payload: payload,
			Retain:  true,
		}); err != nil {
			return err
		}
	}
	return nil
}

func unmarshalInbound(payload []byte, message proto.Message) error {
	if len(payload) == 0 || len(payload) > maxInboundPayload {
		return fmt.Errorf("payload size %d is outside 1..%d", len(payload), maxInboundPayload)
	}
	return proto.Unmarshal(payload, message)
}

func topicIdentity(topic, participant string, suffix ...string) (string, error) {
	parts := strings.Split(topic, "/")
	wantLength := 4 + len(suffix)
	if len(parts) != wantLength || parts[0] != "orbit" || parts[1] != "v1" || parts[2] != participant {
		return "", fmt.Errorf("unexpected topic %q", topic)
	}
	for index, value := range suffix {
		if parts[4+index] != value {
			return "", fmt.Errorf("unexpected topic %q", topic)
		}
	}
	if parts[3] == "" {
		return "", fmt.Errorf("topic %q has empty identity", topic)
	}
	return parts[3], nil
}
