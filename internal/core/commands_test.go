package core

import (
	"testing"
	"time"

	orbitv1 "orbit/gen/go/orbit/v1"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestCommandForIntentRoutesCurrentCodexSessionAndDeduplicates(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	engine := newCodexCommandEngine(t, now)
	intent := testOpenCodexIntent(now, "intent-1", testCodexSessionID, 1)
	intentProducedAt := now.Add(-75 * time.Millisecond)
	intent.Metadata.ProducedAt = timestamppb.New(intentProducedAt)

	command, err := engine.CommandForIntent(now, intent)
	if err != nil {
		t.Fatal(err)
	}
	if command.TargetAgentId != "agent-a" || command.GetOpenCodexSession().GetSessionId() != testCodexSessionID {
		t.Fatalf("unexpected command: %+v", command)
	}
	if command.IntentRef.GetRequesterKind() != orbitv1.RequesterKind_REQUESTER_KIND_NODE || command.IntentRef.GetRequesterId() != "web-a" {
		t.Fatalf("unexpected intent ref: %+v", command.IntentRef)
	}
	if command.GetIntentProducedAt() == nil || !command.GetIntentProducedAt().AsTime().Equal(intentProducedAt) {
		t.Fatalf("intent produced_at = %v, want %v", command.GetIntentProducedAt(), intentProducedAt)
	}
	duplicate, err := engine.CommandForIntent(now.Add(time.Second), proto.Clone(intent).(*orbitv1.Intent))
	if err != nil {
		t.Fatal(err)
	}
	if duplicate.CommandId != command.CommandId {
		t.Fatalf("duplicate intent produced command %q, want %q", duplicate.CommandId, command.CommandId)
	}
}

func TestCommandForIntentRejectsUnknownSessionAndFutureView(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	engine := newCodexCommandEngine(t, now)
	unknown := testOpenCodexIntent(now, "intent-unknown", "01a06712-3498-74b3-b787-532466bf8eb8", 1)
	if _, err := engine.CommandForIntent(now, unknown); err == nil {
		t.Fatal("accepted a session absent from the current Codex view")
	}
	future := testOpenCodexIntent(now, "intent-future", testCodexSessionID, 2)
	if _, err := engine.CommandForIntent(now, future); err == nil {
		t.Fatal("accepted an unknown future view revision")
	}
}

func newCodexCommandEngine(t *testing.T, now time.Time) *Engine {
	t.Helper()
	engine, err := New(Config{
		CoreID: "core-a", CoreEpoch: "core-epoch-a",
		Routes: []Route{{NodeID: "web-a", Profile: webProfile, Inputs: []RouteInput{{
			AgentID: "agent-a", ObservationType: orbitv1.ObservationType_OBSERVATION_TYPE_CODEX,
		}}}},
		CodexPolicy: CodexPolicy{MaxTTL: time.Minute, MaxFutureSkew: time.Second},
		RetainFor:   time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	agent := testAgentState(now)
	agent.Sources = []*orbitv1.SourceStatus{{
		ObservationType: orbitv1.ObservationType_OBSERVATION_TYPE_CODEX, Enabled: true,
	}}
	if err := engine.ApplyAgentState(agent); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.ApplyNodeState(now, testWebNodeState(now)); err != nil {
		t.Fatal(err)
	}
	views, err := engine.ApplyObservation(now, testCodexObservation(now))
	if err != nil || len(views) != 1 || views[0].GetMetadata().GetRevision() != 1 {
		t.Fatalf("prepare command view: views=%d err=%v", len(views), err)
	}
	return engine
}

func testOpenCodexIntent(now time.Time, intentID, sessionID string, viewRevision uint64) *orbitv1.Intent {
	return &orbitv1.Intent{
		Metadata: &orbitv1.Metadata{
			MessageId: "message-" + intentID, ProducerId: "web-a", Revision: 1,
			ProducedAt: timestamppb.New(now), ExpiresAt: timestamppb.New(now.Add(20 * time.Second)),
		},
		IntentId: intentID, NodeEpoch: "node-epoch-a", ViewRevision: viewRevision,
		Action: &orbitv1.Intent_OpenCodexSession{
			OpenCodexSession: &orbitv1.OpenCodexSessionIntent{SessionId: sessionID},
		},
	}
}
