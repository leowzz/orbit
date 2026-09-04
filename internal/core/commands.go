package core

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	orbitv1 "orbit/gen/go/orbit/v1"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	maxIntentTTL      = 30 * time.Second
	maxIntentCommands = 256
)

func (e *Engine) CommandForIntent(now time.Time, intent *orbitv1.Intent) (*orbitv1.Command, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if intent == nil || intent.Metadata == nil || intent.IntentId == "" || len(intent.IntentId) > 128 ||
		intent.NodeEpoch == "" || intent.ViewRevision == 0 {
		return nil, errors.New("invalid intent")
	}
	metadata := intent.Metadata
	if metadata.MessageId == "" || metadata.ProducerId == "" || metadata.Revision == 0 {
		return nil, errors.New("intent metadata requires message id, producer id, and revision")
	}
	producedAt, err := requiredTimestamp(metadata.ProducedAt, "intent produced_at")
	if err != nil {
		return nil, err
	}
	expiresAt, err := requiredTimestamp(metadata.ExpiresAt, "intent expires_at")
	if err != nil || !expiresAt.After(producedAt) || expiresAt.Sub(producedAt) > maxIntentTTL || producedAt.After(now.Add(10*time.Second)) {
		return nil, errors.New("intent has an invalid time window")
	}
	if !expiresAt.After(now) {
		return nil, errors.New("intent is expired")
	}

	normalized, err := (proto.MarshalOptions{Deterministic: true}).Marshal(intent)
	if err != nil {
		return nil, fmt.Errorf("normalize intent: %w", err)
	}
	fingerprint := sha256.Sum256(normalized)
	cacheKey := metadata.ProducerId + "\x00" + intent.IntentId
	if cached, exists := e.intentCommands[cacheKey]; exists {
		if cached.fingerprint != fingerprint {
			return nil, errors.New("intent id was reused with different content")
		}
		return proto.Clone(cached.command).(*orbitv1.Command), nil
	}

	node, exists := e.nodes[metadata.ProducerId]
	if !exists || node.epoch != intent.NodeEpoch {
		return nil, errors.New("intent does not belong to the current node epoch")
	}
	if intent.ViewRevision > e.viewRevision[metadata.ProducerId] {
		return nil, errors.New("intent references an unknown view revision")
	}
	route := e.routeForNode(metadata.ProducerId)
	if route == nil || route.Profile != webProfile {
		return nil, errors.New("node is not allowed to open Codex sessions")
	}
	action := intent.GetOpenCodexSession()
	if action == nil || action.SessionId == "" {
		return nil, errors.New("unsupported intent action")
	}
	codexState, exists := e.codexForRoute(*route)
	if !exists || !codexState.expiresAt.After(now) {
		return nil, errors.New("Codex session state is unavailable or stale")
	}
	found := false
	for _, session := range codexState.value.Sessions {
		if session.GetSessionId() == action.SessionId {
			found = true
			break
		}
	}
	if !found {
		return nil, errors.New("Codex session is not present in the current view")
	}
	targetAgentID := ""
	for _, input := range route.Inputs {
		if input.ObservationType == orbitv1.ObservationType_OBSERVATION_TYPE_CODEX {
			targetAgentID = input.AgentID
			break
		}
	}
	if targetAgentID == "" {
		return nil, errors.New("Codex route has no target agent")
	}

	e.commandRevision++
	command := &orbitv1.Command{
		Metadata: &orbitv1.Metadata{
			MessageId:  newID(),
			ProducerId: e.config.CoreID,
			Revision:   e.commandRevision,
			ProducedAt: timestamppb.New(now),
			ExpiresAt:  timestamppb.New(minTime(expiresAt, now.Add(maxIntentTTL))),
		},
		CommandId:        newID(),
		TargetAgentId:    targetAgentID,
		IntentProducedAt: timestamppb.New(producedAt),
		IntentRef: &orbitv1.IntentRef{
			IntentId: intent.IntentId, RequesterKind: orbitv1.RequesterKind_REQUESTER_KIND_NODE, RequesterId: metadata.ProducerId,
		},
		Action: &orbitv1.Command_OpenCodexSession{
			OpenCodexSession: &orbitv1.OpenCodexSession{SessionId: action.SessionId},
		},
	}
	if len(e.intentOrder) == maxIntentCommands {
		delete(e.intentCommands, e.intentOrder[0])
		e.intentOrder = e.intentOrder[1:]
	}
	e.intentOrder = append(e.intentOrder, cacheKey)
	e.intentCommands[cacheKey] = cachedIntentCommand{
		fingerprint: fingerprint,
		command:     proto.Clone(command).(*orbitv1.Command),
	}
	return command, nil
}
