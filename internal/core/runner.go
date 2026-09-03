package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	orbitv1 "orbit/gen/go/orbit/v1"
	"orbit/internal/mqtt"

	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
)

const (
	maxInboundPayload    = 32 * 1024
	maxDeviceViewPayload = 32 * 1024
)

type Transport interface {
	Publish(context.Context, mqtt.Message) error
	Subscribe(context.Context, string, mqtt.Handler) error
}

type Runner struct {
	engine    *Engine
	transport Transport
	logger    *zap.Logger
	now       func() time.Time
}

func NewRunner(engine *Engine, transport Transport, logger *zap.Logger) (*Runner, error) {
	if engine == nil || transport == nil {
		return nil, errors.New("core engine and transport are required")
	}
	if logger == nil {
		logger = zap.NewNop()
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
		{filter: "orbit/v1/agents/+/observations/codex", handler: r.handleCodexObservation},
		{filter: "orbit/v1/nodes/+/intents", handler: r.handleIntent},
	} {
		if err := r.transport.Subscribe(ctx, item.filter, item.handler); err != nil {
			return err
		}
	}
	r.logger.Debug("core subscriptions ready")

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case now := <-ticker.C:
			views, err := r.engine.Refresh(now.UTC())
			if err != nil {
				r.logger.Warn("refresh device views failed", zap.Error(err))
				continue
			}
			if err := r.publishViews(ctx, views); err != nil {
				r.logger.Warn("publish stale device views failed", zap.Error(err))
			}
		}
	}
}

func (r *Runner) handleIntent(ctx context.Context, message mqtt.Message) error {
	nodeID, err := topicIdentity(message.Topic, "nodes", "intents")
	if err != nil {
		return err
	}
	if message.Retain {
		return errors.New("retained intents are not accepted")
	}
	var intent orbitv1.Intent
	if err := unmarshalInbound(message.Payload, &intent); err != nil {
		return fmt.Errorf("decode intent: %w", err)
	}
	if intent.Metadata == nil || intent.Metadata.ProducerId != nodeID {
		return errors.New("intent topic identity does not match payload")
	}
	command, err := r.engine.CommandForIntent(r.now().UTC(), &intent)
	if err != nil {
		return err
	}
	payload, err := proto.Marshal(command)
	if err != nil {
		return fmt.Errorf("marshal command: %w", err)
	}
	if len(payload) > maxInboundPayload {
		return fmt.Errorf("command payload exceeds %d bytes", maxInboundPayload)
	}
	topic := fmt.Sprintf("orbit/v1/agents/%s/commands", command.TargetAgentId)
	if err := r.transport.Publish(ctx, mqtt.Message{Topic: topic, Payload: payload}); err != nil {
		return err
	}
	r.logger.Info("command published",
		zap.String("command_id", command.CommandId),
		zap.String("node_id", nodeID),
		zap.String("agent_id", command.TargetAgentId),
		zap.String("action_type", "open_codex_session"),
	)
	return nil
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
	if err := r.engine.ApplyAgentState(&state); err != nil {
		return err
	}
	r.logger.Debug("agent state accepted",
		zap.String("agent_id", state.AgentId),
		zap.Uint64("revision", state.Metadata.Revision),
		zap.Int("sources", len(state.Sources)),
		zap.Bool("retained", message.Retain),
	)
	return nil
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
	r.logger.Debug("node state accepted",
		zap.String("node_id", state.NodeId),
		zap.Uint64("revision", state.Metadata.Revision),
		zap.Int("views", len(views)),
		zap.Bool("retained", message.Retain),
	)
	return r.publishViews(ctx, views)
}

func (r *Runner) handleObservation(ctx context.Context, message mqtt.Message) error {
	return r.handleTypedObservation(ctx, message, "usage", orbitv1.ObservationType_OBSERVATION_TYPE_USAGE)
}

func (r *Runner) handleCodexObservation(ctx context.Context, message mqtt.Message) error {
	return r.handleTypedObservation(ctx, message, "codex", orbitv1.ObservationType_OBSERVATION_TYPE_CODEX)
}

func (r *Runner) handleTypedObservation(ctx context.Context, message mqtt.Message, name string, observationType orbitv1.ObservationType) error {
	agentID, err := topicIdentity(message.Topic, "agents", "observations", name)
	if err != nil {
		return err
	}
	var observation orbitv1.Observation
	if err := unmarshalInbound(message.Payload, &observation); err != nil {
		return fmt.Errorf("decode %s observation: %w", name, err)
	}
	if observation.Metadata == nil || observation.Metadata.ProducerId != agentID {
		return errors.New("observation topic identity does not match payload")
	}
	if (observationType == orbitv1.ObservationType_OBSERVATION_TYPE_USAGE && observation.GetUsage() == nil) ||
		(observationType == orbitv1.ObservationType_OBSERVATION_TYPE_CODEX && observation.GetCodex() == nil) {
		return fmt.Errorf("%s topic contains a different observation payload", name)
	}
	views, err := r.engine.ApplyObservation(r.now().UTC(), &observation)
	if err != nil {
		return err
	}
	r.logger.Debug(name+" observation accepted",
		zap.String("agent_id", agentID),
		zap.String("source_type", name),
		zap.Uint64("revision", observation.Metadata.Revision),
		zap.Int("views", len(views)),
		zap.Bool("retained", message.Retain),
	)
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
		r.logger.Debug("device view published",
			zap.String("node_id", view.NodeId),
			zap.Uint64("revision", view.GetMetadata().GetRevision()),
			zap.String("freshness", view.Freshness.String()),
			zap.String("primary", view.GetPrimary().GetText()),
			zap.String("secondary", view.GetSecondary().GetText()),
			zap.String("footer", view.GetFooter().GetText()),
			zap.Int("bytes", len(payload)),
		)
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
