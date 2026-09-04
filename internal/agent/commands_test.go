package agent

import (
	"context"
	"sync"
	"testing"
	"time"

	orbitv1 "orbit/gen/go/orbit/v1"
	"orbit/internal/mqtt"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type recordingOpener struct {
	mu        sync.Mutex
	sessions  []string
	err       error
	afterOpen func()
}

func (opener *recordingOpener) Open(_ context.Context, sessionID string) error {
	opener.mu.Lock()
	defer opener.mu.Unlock()
	opener.sessions = append(opener.sessions, sessionID)
	if opener.afterOpen != nil {
		opener.afterOpen()
	}
	return opener.err
}

func (opener *recordingOpener) Count() int {
	opener.mu.Lock()
	defer opener.mu.Unlock()
	return len(opener.sessions)
}

func TestOpenCodexCommandExecutesOnceAndPublishesResult(t *testing.T) {
	t.Parallel()
	transport := &recordingPublisher{}
	opener := &recordingOpener{}
	runner, err := New(
		codexConfig(false, false),
		Sources{Codex: &stubCodexSource{}},
		Capabilities{OpenCodexSession: opener},
		transport,
		zap.NewNop(),
	)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	runner.now = func() time.Time { return now }
	message := testOpenCodexCommand(t, now, "command-1", "01a066af-69d4-77d1-a21b-26d84534a817")

	if err := runner.handleCommand(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	if err := runner.handleCommand(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	if opener.Count() != 1 {
		t.Fatalf("opener count = %d, want 1 after duplicate delivery", opener.Count())
	}
	messages := transport.Messages()
	if len(messages) != 2 {
		t.Fatalf("published %d results, want 2", len(messages))
	}
	for _, message := range messages {
		if message.Topic != "orbit/v1/agents/agent-a/results" || message.Retain {
			t.Fatalf("unexpected result message: %+v", message)
		}
		var result orbitv1.CommandResult
		if err := proto.Unmarshal(message.Payload, &result); err != nil {
			t.Fatal(err)
		}
		if result.Status != orbitv1.CommandStatus_COMMAND_STATUS_SUCCEEDED || result.CommandId != "command-1" || result.IntentRef.GetRequesterId() != "web-a" {
			t.Fatalf("unexpected result: %+v", &result)
		}
	}
}

func TestOpenCodexCommandRejectsExpiredAndInvalidSession(t *testing.T) {
	t.Parallel()
	transport := &recordingPublisher{}
	opener := &recordingOpener{}
	runner, err := New(
		codexConfig(false, false),
		Sources{Codex: &stubCodexSource{}},
		Capabilities{OpenCodexSession: opener},
		transport,
		zap.NewNop(),
	)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	runner.now = func() time.Time { return now }

	expired := testOpenCodexCommand(t, now.Add(-time.Minute), "expired", "01a066af-69d4-77d1-a21b-26d84534a817")
	if err := runner.handleCommand(context.Background(), expired); err != nil {
		t.Fatal(err)
	}
	invalid := testOpenCodexCommand(t, now, "invalid", "../../Calculator.app")
	if err := runner.handleCommand(context.Background(), invalid); err != nil {
		t.Fatal(err)
	}
	if opener.Count() != 0 {
		t.Fatalf("opener ran %d times for rejected commands", opener.Count())
	}
	messages := transport.Messages()
	want := []orbitv1.CommandStatus{
		orbitv1.CommandStatus_COMMAND_STATUS_EXPIRED,
		orbitv1.CommandStatus_COMMAND_STATUS_REJECTED,
	}
	for index, message := range messages {
		var result orbitv1.CommandResult
		if err := proto.Unmarshal(message.Payload, &result); err != nil {
			t.Fatal(err)
		}
		if result.Status != want[index] {
			t.Fatalf("result %d status = %s, want %s", index, result.Status, want[index])
		}
	}
}

func TestOpenCodexCommandLogsDispatchAndExecutionLatency(t *testing.T) {
	t.Parallel()
	current := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	opener := &recordingOpener{afterOpen: func() { current = current.Add(20 * time.Millisecond) }}
	logCore, observed := observer.New(zap.InfoLevel)
	runner, err := New(
		codexConfig(false, false),
		Sources{Codex: &stubCodexSource{}},
		Capabilities{OpenCodexSession: opener},
		&recordingPublisher{},
		zap.New(logCore),
	)
	if err != nil {
		t.Fatal(err)
	}
	runner.now = func() time.Time { return current }
	message := testOpenCodexCommand(t, current.Add(-25*time.Millisecond), "command-latency", "01a066af-69d4-77d1-a21b-26d84534a817")

	if err := runner.handleCommand(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	entries := observed.FilterMessage("command completed").All()
	if len(entries) != 1 {
		t.Fatalf("command completed log count = %d, want 1", len(entries))
	}
	fields := entries[0].ContextMap()
	for name, want := range map[string]int64{
		"node_to_execution_ms": 80,
		"core_to_execution_ms": 25,
		"execution_ms":         20,
	} {
		if got := fields[name]; got != want {
			t.Errorf("%s = %v, want %d", name, got, want)
		}
	}
}

func testOpenCodexCommand(t *testing.T, producedAt time.Time, commandID, sessionID string) mqtt.Message {
	t.Helper()
	command := &orbitv1.Command{
		Metadata: &orbitv1.Metadata{
			MessageId:  "message-" + commandID,
			ProducerId: "core-a",
			Revision:   1,
			ProducedAt: timestamppb.New(producedAt),
			ExpiresAt:  timestamppb.New(producedAt.Add(20 * time.Second)),
		},
		CommandId:        commandID,
		TargetAgentId:    "agent-a",
		IntentProducedAt: timestamppb.New(producedAt.Add(-55 * time.Millisecond)),
		IntentRef: &orbitv1.IntentRef{
			IntentId: "intent-1", RequesterKind: orbitv1.RequesterKind_REQUESTER_KIND_NODE, RequesterId: "web-a",
		},
		Action: &orbitv1.Command_OpenCodexSession{OpenCodexSession: &orbitv1.OpenCodexSession{SessionId: sessionID}},
	}
	payload, err := proto.Marshal(command)
	if err != nil {
		t.Fatal(err)
	}
	return mqtt.Message{Topic: "orbit/v1/agents/agent-a/commands", Payload: payload}
}
