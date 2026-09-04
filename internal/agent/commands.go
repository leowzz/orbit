package agent

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	orbitv1 "orbit/gen/go/orbit/v1"
	codexcap "orbit/internal/capabilities/codex"
	"orbit/internal/mqtt"

	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	maxCommandPayload = 32 * 1024
	maxCommandTTL     = 30 * time.Second
	maxCommandResults = 256
)

type cachedCommandResult struct {
	fingerprint [sha256.Size]byte
	result      *orbitv1.CommandResult
}

func (r *Runner) handleCommand(ctx context.Context, message mqtt.Message) error {
	if len(message.Payload) == 0 || len(message.Payload) > maxCommandPayload {
		return fmt.Errorf("command payload size %d is outside 1..%d", len(message.Payload), maxCommandPayload)
	}
	if message.Retain {
		return errors.New("retained commands are not accepted")
	}
	expectedTopic := fmt.Sprintf("orbit/v1/agents/%s/commands", r.config.AgentID)
	if message.Topic != expectedTopic {
		return fmt.Errorf("unexpected command topic %q", message.Topic)
	}

	var command orbitv1.Command
	if err := proto.Unmarshal(message.Payload, &command); err != nil {
		return fmt.Errorf("decode command: %w", err)
	}
	normalized, err := (proto.MarshalOptions{Deterministic: true}).Marshal(&command)
	if err != nil {
		return fmt.Errorf("normalize command: %w", err)
	}
	fingerprint := sha256.Sum256(normalized)

	r.commandMu.Lock()
	result, duplicate := r.cachedResultLocked(&command, fingerprint)
	var executionStartedAt time.Time
	var executionDuration time.Duration
	if result == nil {
		status, errorCode := r.validateCommand(r.now().UTC(), &command)
		if status == orbitv1.CommandStatus_COMMAND_STATUS_UNSPECIFIED {
			executionStartedAt = r.now()
			executionContext, cancel := context.WithTimeout(ctx, 5*time.Second)
			err = r.capabilities.OpenCodexSession.Open(executionContext, command.GetOpenCodexSession().GetSessionId())
			cancel()
			executionDuration = r.now().Sub(executionStartedAt)
			if err == nil {
				status = orbitv1.CommandStatus_COMMAND_STATUS_SUCCEEDED
			} else {
				status = orbitv1.CommandStatus_COMMAND_STATUS_FAILED
				errorCode = "open_failed"
			}
		}
		result = r.newCommandResultLocked(&command, status, errorCode)
		if command.CommandId != "" {
			r.cacheResultLocked(command.CommandId, fingerprint, result)
		}
	}
	r.commandMu.Unlock()

	if err := r.publishCommandResult(ctx, result); err != nil {
		return err
	}
	fields := []zap.Field{
		zap.String("command_id", command.CommandId),
		zap.String("action_type", "open_codex_session"),
		zap.String("status", result.Status.String()),
		zap.String("error_code", result.ErrorCode),
		zap.Bool("duplicate", duplicate),
	}
	if !executionStartedAt.IsZero() {
		if intentProducedAt := command.GetIntentProducedAt(); intentProducedAt != nil && intentProducedAt.CheckValid() == nil {
			fields = append(fields, zap.Int64("node_to_execution_ms", executionStartedAt.Sub(intentProducedAt.AsTime()).Milliseconds()))
		}
		fields = append(fields,
			zap.Int64("core_to_execution_ms", executionStartedAt.Sub(command.Metadata.ProducedAt.AsTime()).Milliseconds()),
			zap.Int64("execution_ms", executionDuration.Milliseconds()),
		)
	}
	r.logger.Info("command completed", fields...)
	return nil
}

func (r *Runner) validateCommand(now time.Time, command *orbitv1.Command) (orbitv1.CommandStatus, string) {
	if command.Metadata == nil || command.Metadata.MessageId == "" || command.Metadata.ProducerId == "" || command.Metadata.Revision == 0 ||
		command.CommandId == "" || len(command.CommandId) > 128 || command.TargetAgentId != r.config.AgentID ||
		command.IntentRef == nil || command.IntentRef.IntentId == "" || command.IntentRef.RequesterId == "" ||
		command.IntentRef.RequesterKind != orbitv1.RequesterKind_REQUESTER_KIND_NODE {
		return orbitv1.CommandStatus_COMMAND_STATUS_REJECTED, "invalid_command"
	}
	producedAt, err := validCommandTime(command.Metadata.ProducedAt)
	if err != nil {
		return orbitv1.CommandStatus_COMMAND_STATUS_REJECTED, "invalid_command_time"
	}
	expiresAt, err := validCommandTime(command.Metadata.ExpiresAt)
	if err != nil || !expiresAt.After(producedAt) || expiresAt.Sub(producedAt) > maxCommandTTL || producedAt.After(now.Add(10*time.Second)) {
		return orbitv1.CommandStatus_COMMAND_STATUS_REJECTED, "invalid_command_time"
	}
	if !expiresAt.After(now) {
		return orbitv1.CommandStatus_COMMAND_STATUS_EXPIRED, "command_expired"
	}
	action := command.GetOpenCodexSession()
	if action == nil || r.capabilities.OpenCodexSession == nil {
		return orbitv1.CommandStatus_COMMAND_STATUS_REJECTED, "unsupported_action"
	}
	if _, err := codexcap.ThreadURL(action.SessionId); err != nil {
		return orbitv1.CommandStatus_COMMAND_STATUS_REJECTED, "invalid_session_id"
	}
	return orbitv1.CommandStatus_COMMAND_STATUS_UNSPECIFIED, ""
}

func validCommandTime(value *timestamppb.Timestamp) (time.Time, error) {
	if value == nil {
		return time.Time{}, errors.New("timestamp is required")
	}
	if err := value.CheckValid(); err != nil {
		return time.Time{}, err
	}
	return value.AsTime(), nil
}

func (r *Runner) cachedResultLocked(command *orbitv1.Command, fingerprint [sha256.Size]byte) (*orbitv1.CommandResult, bool) {
	cached, exists := r.commandResults[command.CommandId]
	if !exists {
		return nil, false
	}
	if cached.fingerprint != fingerprint {
		return r.newCommandResultLocked(command, orbitv1.CommandStatus_COMMAND_STATUS_REJECTED, "command_id_conflict"), true
	}
	return proto.Clone(cached.result).(*orbitv1.CommandResult), true
}

func (r *Runner) cacheResultLocked(commandID string, fingerprint [sha256.Size]byte, result *orbitv1.CommandResult) {
	if len(r.commandOrder) == maxCommandResults {
		delete(r.commandResults, r.commandOrder[0])
		r.commandOrder = r.commandOrder[1:]
	}
	r.commandOrder = append(r.commandOrder, commandID)
	r.commandResults[commandID] = cachedCommandResult{
		fingerprint: fingerprint,
		result:      proto.Clone(result).(*orbitv1.CommandResult),
	}
}

func (r *Runner) newCommandResultLocked(command *orbitv1.Command, status orbitv1.CommandStatus, errorCode string) *orbitv1.CommandResult {
	r.resultRevision++
	now := r.now().UTC()
	result := &orbitv1.CommandResult{
		Metadata: &orbitv1.Metadata{
			MessageId:  newID(),
			ProducerId: r.config.AgentID,
			Revision:   r.resultRevision,
			ProducedAt: timestamppb.New(now),
		},
		CommandId: command.CommandId,
		Status:    status,
		ErrorCode: errorCode,
	}
	if command.IntentRef != nil {
		result.IntentRef = proto.Clone(command.IntentRef).(*orbitv1.IntentRef)
	}
	if status == orbitv1.CommandStatus_COMMAND_STATUS_SUCCEEDED {
		result.SafeMessage = "Codex session opened"
	} else {
		result.SafeMessage = "Codex session was not opened"
	}
	return result
}

func (r *Runner) publishCommandResult(ctx context.Context, result *orbitv1.CommandResult) error {
	payload, err := proto.Marshal(result)
	if err != nil {
		return fmt.Errorf("marshal command result: %w", err)
	}
	topic := fmt.Sprintf("orbit/v1/agents/%s/results", r.config.AgentID)
	if err := r.transport.Publish(ctx, mqtt.Message{Topic: topic, Payload: payload}); err != nil {
		return fmt.Errorf("publish command result: %w", err)
	}
	return nil
}
