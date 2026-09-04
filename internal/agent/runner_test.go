package agent

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	orbitv1 "orbit/gen/go/orbit/v1"
	"orbit/internal/mqtt"
	"orbit/internal/sources/codex"
	"orbit/internal/sources/sub2api"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"google.golang.org/protobuf/proto"
)

type stubSource struct {
	mu    sync.Mutex
	usage sub2api.Usage
	err   error
}

func (source *stubSource) FetchUsage(context.Context) (sub2api.Usage, error) {
	source.mu.Lock()
	defer source.mu.Unlock()
	return source.usage, source.err
}

type stubCodexSource struct {
	mu       sync.Mutex
	snapshot codex.Snapshot
	err      error
}

func (source *stubCodexSource) Fetch(context.Context) (codex.Snapshot, error) {
	source.mu.Lock()
	defer source.mu.Unlock()
	return source.snapshot, source.err
}

type recordingPublisher struct {
	mu       sync.Mutex
	messages []mqtt.Message
	filter   string
	handler  mqtt.Handler
}

func (publisher *recordingPublisher) Subscribe(_ context.Context, filter string, handler mqtt.Handler) error {
	publisher.mu.Lock()
	defer publisher.mu.Unlock()
	publisher.filter = filter
	publisher.handler = handler
	return nil
}

func (publisher *recordingPublisher) Subscription() (string, mqtt.Handler) {
	publisher.mu.Lock()
	defer publisher.mu.Unlock()
	return publisher.filter, publisher.handler
}

func (publisher *recordingPublisher) Publish(_ context.Context, message mqtt.Message) error {
	publisher.mu.Lock()
	defer publisher.mu.Unlock()
	message.Payload = append([]byte(nil), message.Payload...)
	publisher.messages = append(publisher.messages, message)
	return nil
}

func (publisher *recordingPublisher) Messages() []mqtt.Message {
	publisher.mu.Lock()
	defer publisher.mu.Unlock()
	messages := make([]mqtt.Message, len(publisher.messages))
	for index, message := range publisher.messages {
		messages[index] = message
		messages[index].Payload = append([]byte(nil), message.Payload...)
	}
	return messages
}

func (publisher *recordingPublisher) Reset() {
	publisher.mu.Lock()
	defer publisher.mu.Unlock()
	publisher.messages = nil
}

func TestPollOncePublishesUsageAndHealthyState(t *testing.T) {
	t.Parallel()
	source := &stubSource{usage: sub2api.Usage{TodayActualCostMicros: 1_500_000, TodayTokens: 2_000, TPM: 30}}
	publisher := &recordingPublisher{}
	runner := newTestRunner(t, source, publisher)
	var logs bytes.Buffer
	runner.logger = zap.New(zapcore.NewCore(
		zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()),
		zapcore.AddSync(&logs),
		zapcore.InfoLevel,
	))
	now := time.Date(2026, 9, 3, 16, 30, 0, 0, time.UTC)
	runner.now = func() time.Time { return now }

	if err := runner.PollOnce(context.Background()); err != nil {
		t.Fatalf("PollOnce: %v", err)
	}
	messages := publisher.Messages()
	if len(messages) != 2 {
		t.Fatalf("got %d messages, want observation and state", len(messages))
	}
	var observation orbitv1.Observation
	if err := proto.Unmarshal(messages[0].Payload, &observation); err != nil {
		t.Fatalf("unmarshal observation: %v", err)
	}
	usage := observation.GetUsage()
	if usage.GetActualCostMicros() != 1_500_000 || usage.GetTokenCount() != 2_000 || usage.GetTpm() != 30 {
		t.Fatalf("unexpected usage: %+v", usage)
	}
	wantStart := time.Date(2026, 9, 3, 16, 0, 0, 0, time.UTC)
	if !usage.WindowStart.AsTime().Equal(wantStart) {
		t.Fatalf("window start=%s, want %s", usage.WindowStart.AsTime(), wantStart)
	}
	var state orbitv1.AgentState
	if err := proto.Unmarshal(messages[1].Payload, &state); err != nil {
		t.Fatalf("unmarshal state: %v", err)
	}
	if !messages[1].Retain || state.Sources[0].Health != orbitv1.SourceHealth_SOURCE_HEALTH_HEALTHY {
		t.Fatalf("unexpected agent state: %+v", state.Sources[0])
	}
	for _, want := range []string{"observation published", `"source_type":"usage"`, "agent state published"} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("info log missing %q: %s", want, logs.String())
		}
	}
	if strings.Contains(logs.String(), "1500000") {
		t.Errorf("usage value leaked into logs: %s", logs.String())
	}
}

func TestPollFailurePublishesDegradedStateWithoutReplacingObservation(t *testing.T) {
	t.Parallel()
	source := &stubSource{usage: sub2api.Usage{}}
	publisher := &recordingPublisher{}
	runner := newTestRunner(t, source, publisher)
	runner.now = func() time.Time { return time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC) }
	if err := runner.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	publisher.Reset()
	source.mu.Lock()
	source.err = &sub2api.Error{Kind: sub2api.ErrorRateLimited, Operation: "usage"}
	source.mu.Unlock()
	if err := runner.PollOnce(context.Background()); err == nil {
		t.Fatal("PollOnce returned nil for source failure")
	}
	messages := publisher.Messages()
	if len(messages) != 1 || !messages[0].Retain {
		t.Fatalf("got messages: %+v", messages)
	}
	var state orbitv1.AgentState
	if err := proto.Unmarshal(messages[0].Payload, &state); err != nil {
		t.Fatal(err)
	}
	if state.Sources[0].Health != orbitv1.SourceHealth_SOURCE_HEALTH_DEGRADED || state.Sources[0].ErrorCode != string(sub2api.ErrorRateLimited) {
		t.Fatalf("unexpected source status: %+v", state.Sources[0])
	}
	if state.Sources[0].LastSuccessAt == nil {
		t.Fatal("failure replaced last_success with an empty timestamp")
	}
}

func TestPollCodexOncePublishesSanitizedObservationAndHealthyState(t *testing.T) {
	t.Parallel()
	updated := time.Date(2026, 9, 3, 8, 0, 0, 0, time.FixedZone("UTC+8", 8*60*60))
	source := &stubCodexSource{snapshot: codex.Snapshot{
		Sessions: []codex.Session{{
			ID:           "session-public",
			DisplayName:  "title-secret",
			ProjectName:  "project-secret",
			Model:        "gpt-5.6-luna",
			Status:       "running",
			UpdatedAt:    updated,
			ProcessAlive: true,
		}},
		TotalCount:   1,
		RunningCount: 1,
	}}
	publisher := &recordingPublisher{}
	runner := newCodexTestRunner(t, source, publisher, false, false)
	var logs bytes.Buffer
	runner.logger = zap.New(zapcore.NewCore(
		zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()),
		zapcore.AddSync(&logs),
		zapcore.InfoLevel,
	))
	now := time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)
	runner.now = func() time.Time { return now }

	if err := runner.PollCodexOnce(context.Background()); err != nil {
		t.Fatalf("PollCodexOnce: %v", err)
	}
	messages := publisher.Messages()
	if len(messages) != 2 || messages[0].Topic != "orbit/v1/agents/agent-a/observations/codex" || messages[0].Retain {
		t.Fatalf("unexpected Codex messages: %+v", messages)
	}
	var observation orbitv1.Observation
	if err := proto.Unmarshal(messages[0].Payload, &observation); err != nil {
		t.Fatalf("unmarshal Codex observation: %v", err)
	}
	if observation.GetMetadata().GetRevision() != 1 || observation.GetAgentEpoch() != "epoch-a" {
		t.Fatalf("unexpected metadata: %+v", observation.GetMetadata())
	}
	if !observation.GetMetadata().GetProducedAt().AsTime().Equal(now) || !observation.GetMetadata().GetExpiresAt().AsTime().Equal(now.Add(2*time.Minute)) {
		t.Fatalf("unexpected observation timestamps: %+v", observation.GetMetadata())
	}
	payload := observation.GetCodex()
	if payload.GetTotalCount() != 1 || payload.GetRunningCount() != 1 || len(payload.GetSessions()) != 1 {
		t.Fatalf("unexpected Codex payload: %+v", payload)
	}
	session := payload.GetSessions()[0]
	if session.GetDisplayName() != "" || session.GetProjectName() != "" {
		t.Fatalf("privacy defaults exposed names: %+v", session)
	}
	if session.GetSessionId() != "session-public" || session.GetModel() != "gpt-5.6-luna" || session.GetStatus() != orbitv1.CodexSessionStatus_CODEX_SESSION_STATUS_RUNNING || !session.GetProcessAlive() || !session.GetUpdatedAt().AsTime().Equal(updated) {
		t.Fatalf("unexpected Codex session: %+v", session)
	}
	for _, forbidden := range []string{"title-secret", "project-secret"} {
		if bytes.Contains(messages[0].Payload, []byte(forbidden)) {
			t.Errorf("forbidden value %q appeared in observation payload", forbidden)
		}
		if strings.Contains(logs.String(), forbidden) {
			t.Errorf("forbidden value %q appeared in info logs: %s", forbidden, logs.String())
		}
	}
	for _, want := range []string{`"total_count":1`, `"running_count":1`, `"session_count":1`} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("Codex aggregate count missing from info logs %q: %s", want, logs.String())
		}
	}
	var state orbitv1.AgentState
	if err := proto.Unmarshal(messages[1].Payload, &state); err != nil {
		t.Fatal(err)
	}
	if !messages[1].Retain || len(state.Sources) != 1 || state.Sources[0].ObservationType != orbitv1.ObservationType_OBSERVATION_TYPE_CODEX || state.Sources[0].Health != orbitv1.SourceHealth_SOURCE_HEALTH_HEALTHY {
		t.Fatalf("unexpected Codex source state: %+v", &state)
	}
}

func TestCodexPrivacyFieldsAndUTF8Bounds(t *testing.T) {
	t.Parallel()
	source := &stubCodexSource{snapshot: codex.Snapshot{Sessions: []codex.Session{{
		ID:          strings.Repeat("会", 100),
		DisplayName: strings.Repeat("显示🙂", 100),
		ProjectName: strings.Repeat("项目", 100),
		Model:       strings.Repeat("模型🙂", 100),
		Status:      "completed",
	}}}}
	publisher := &recordingPublisher{}
	runner := newCodexTestRunner(t, source, publisher, true, true)
	if err := runner.PollCodexOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	var observation orbitv1.Observation
	if err := proto.Unmarshal(publisher.Messages()[0].Payload, &observation); err != nil {
		t.Fatal(err)
	}
	session := observation.GetCodex().GetSessions()[0]
	checks := []struct {
		name  string
		value string
		limit int
	}{
		{name: "session_id", value: session.GetSessionId(), limit: codexSessionIDMaxBytes},
		{name: "display_name", value: session.GetDisplayName(), limit: codexDisplayNameMaxBytes},
		{name: "project_name", value: session.GetProjectName(), limit: codexProjectNameMaxBytes},
		{name: "model", value: session.GetModel(), limit: codexModelMaxBytes},
	}
	for _, check := range checks {
		if len(check.value) > check.limit || !utf8.ValidString(check.value) {
			t.Errorf("%s violates UTF-8 byte bound: bytes=%d limit=%d valid=%t", check.name, len(check.value), check.limit, utf8.ValidString(check.value))
		}
	}
	if session.GetStatus() != orbitv1.CodexSessionStatus_CODEX_SESSION_STATUS_COMPLETED {
		t.Fatalf("unexpected status: %s", session.GetStatus())
	}
}

func TestCodexFailureUsesTypedErrorAndPreservesLastSuccess(t *testing.T) {
	t.Parallel()
	source := &stubCodexSource{snapshot: codex.Snapshot{TotalCount: 1}}
	publisher := &recordingPublisher{}
	runner := newCodexTestRunner(t, source, publisher, false, false)
	runner.now = func() time.Time { return time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC) }
	source.mu.Lock()
	source.err = &codex.Error{Kind: codex.ErrorHistoryRead, Operation: "read history", Cause: errors.New("fixture")}
	source.mu.Unlock()
	if err := runner.PollCodexOnce(context.Background()); err == nil || sourceErrorCode(err) != string(codex.ErrorHistoryRead) {
		t.Fatalf("expected typed Codex error, got %v", err)
	}
	messages := publisher.Messages()
	if len(messages) != 1 || !messages[0].Retain {
		t.Fatalf("failed poll published an observation: %+v", messages)
	}
	var failedState orbitv1.AgentState
	if err := proto.Unmarshal(messages[0].Payload, &failedState); err != nil {
		t.Fatal(err)
	}
	if failedState.Sources[0].Health != orbitv1.SourceHealth_SOURCE_HEALTH_FAILED || failedState.Sources[0].ErrorCode != string(codex.ErrorHistoryRead) || failedState.Sources[0].LastSuccessAt != nil {
		t.Fatalf("unexpected first failure state: %+v", failedState.Sources[0])
	}

	source.mu.Lock()
	source.err = nil
	source.mu.Unlock()
	publisher.Reset()
	if err := runner.PollCodexOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	publisher.Reset()
	source.mu.Lock()
	source.err = &codex.Error{Kind: codex.ErrorStateRead, Operation: "read state"}
	source.mu.Unlock()
	if err := runner.PollCodexOnce(context.Background()); err == nil {
		t.Fatal("expected failure after a successful observation")
	}
	messages = publisher.Messages()
	if len(messages) != 1 {
		t.Fatalf("expected only state after failed poll, got %d messages", len(messages))
	}
	var degradedState orbitv1.AgentState
	if err := proto.Unmarshal(messages[0].Payload, &degradedState); err != nil {
		t.Fatal(err)
	}
	status := degradedState.Sources[0]
	if status.Health != orbitv1.SourceHealth_SOURCE_HEALTH_DEGRADED || status.ErrorCode != string(codex.ErrorStateRead) || status.LastSuccessAt == nil {
		t.Fatalf("unexpected degraded state: %+v", status)
	}
}

func TestUsageAndCodexRevisionsAreIndependent(t *testing.T) {
	t.Parallel()
	usageSource := &stubSource{usage: sub2api.Usage{TodayTokens: 1}}
	codexSource := &stubCodexSource{snapshot: codex.Snapshot{TotalCount: 1}}
	publisher := &recordingPublisher{}
	runner := newDualTestRunner(t, usageSource, codexSource, publisher)
	if err := runner.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := runner.PollCodexOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := runner.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := runner.PollCodexOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	var usageRevisions, codexRevisions, stateRevisions []uint64
	for _, message := range publisher.Messages() {
		switch message.Topic {
		case "orbit/v1/agents/agent-a/observations/usage":
			var observation orbitv1.Observation
			if err := proto.Unmarshal(message.Payload, &observation); err != nil {
				t.Fatal(err)
			}
			usageRevisions = append(usageRevisions, observation.GetMetadata().GetRevision())
		case "orbit/v1/agents/agent-a/observations/codex":
			var observation orbitv1.Observation
			if err := proto.Unmarshal(message.Payload, &observation); err != nil {
				t.Fatal(err)
			}
			codexRevisions = append(codexRevisions, observation.GetMetadata().GetRevision())
		case "orbit/v1/agents/agent-a/state":
			var state orbitv1.AgentState
			if err := proto.Unmarshal(message.Payload, &state); err != nil {
				t.Fatal(err)
			}
			stateRevisions = append(stateRevisions, state.GetMetadata().GetRevision())
		}
	}
	if !equalUint64s(usageRevisions, []uint64{1, 2}) || !equalUint64s(codexRevisions, []uint64{1, 2}) {
		t.Fatalf("source revisions were not independent: usage=%v codex=%v", usageRevisions, codexRevisions)
	}
	if !equalUint64s(stateRevisions, []uint64{1, 2, 3, 4}) {
		t.Fatalf("state revisions were not ordered: %v", stateRevisions)
	}
}

func TestRunPublishesInitialStateBeforeIndependentSources(t *testing.T) {
	usageSource := &stubSource{usage: sub2api.Usage{TodayTokens: 1}}
	codexSource := &stubCodexSource{snapshot: codex.Snapshot{TotalCount: 1}}
	publisher := &recordingPublisher{}
	runner := newDualTestRunner(t, usageSource, codexSource, publisher)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runner.Run(ctx) }()
	messages := waitForMessages(t, publisher, 5)
	if messages[0].Topic != "orbit/v1/agents/agent-a/state" || !messages[0].Retain {
		t.Fatalf("first message was not retained AgentState: %+v", messages[0])
	}
	var initial orbitv1.AgentState
	if err := proto.Unmarshal(messages[0].Payload, &initial); err != nil {
		t.Fatal(err)
	}
	if initial.GetMetadata().GetRevision() != 1 || len(initial.Sources) != 2 {
		t.Fatalf("unexpected initial state: %+v", &initial)
	}
	for _, source := range initial.Sources {
		if source.Health != orbitv1.SourceHealth_SOURCE_HEALTH_UNSPECIFIED || source.LastSuccessAt != nil || source.ErrorCode != "" {
			t.Fatalf("initial source was already collected: %+v", source)
		}
	}
	var stateRevisions []uint64
	for _, message := range messages {
		if message.Topic != "orbit/v1/agents/agent-a/state" {
			continue
		}
		var state orbitv1.AgentState
		if err := proto.Unmarshal(message.Payload, &state); err != nil {
			t.Fatal(err)
		}
		stateRevisions = append(stateRevisions, state.GetMetadata().GetRevision())
	}
	if len(stateRevisions) != 3 || !equalUint64s(stateRevisions, []uint64{1, 2, 3}) {
		t.Fatalf("unexpected concurrent state revisions: %v", stateRevisions)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not stop")
	}
}

func TestNewRejectsIncompleteConfig(t *testing.T) {
	t.Parallel()
	_, err := New(Config{}, Sources{Usage: &stubSource{}}, Capabilities{}, &recordingPublisher{}, zap.NewNop())
	if err == nil || errors.Is(err, context.Canceled) {
		t.Fatalf("New returned unexpected error: %v", err)
	}
}

func TestNewRejectsUnsupportedCurrency(t *testing.T) {
	t.Parallel()
	runnerConfig := usageConfig()
	runnerConfig.CurrencyCode = "EUR"
	_, err := New(runnerConfig, Sources{Usage: &stubSource{}}, Capabilities{}, &recordingPublisher{}, zap.NewNop())
	if err == nil {
		t.Fatal("New accepted a currency that V1 cannot render correctly")
	}
}

func TestNewSupportsCodexOnlyAndDualSources(t *testing.T) {
	t.Parallel()
	config := codexConfig(false, false)
	if _, err := New(config, Sources{Codex: &stubCodexSource{}}, Capabilities{}, &recordingPublisher{}, zap.NewNop()); err != nil {
		t.Fatalf("Codex-only New: %v", err)
	}
	config = usageConfig()
	config.CodexPollInterval = time.Minute
	config.CodexObservationTTL = 2 * time.Minute
	if _, err := New(config, Sources{Usage: &stubSource{}, Codex: &stubCodexSource{}}, Capabilities{}, &recordingPublisher{}, zap.NewNop()); err != nil {
		t.Fatalf("dual-source New: %v", err)
	}
}

func newTestRunner(t *testing.T, source UsageSource, publisher Transport) *Runner {
	t.Helper()
	runner, err := New(usageConfig(), Sources{Usage: source}, Capabilities{}, publisher, zap.NewNop())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return runner
}

func newCodexTestRunner(t *testing.T, source CodexSource, publisher Transport, displayName, projectName bool) *Runner {
	t.Helper()
	config := codexConfig(displayName, projectName)
	runner, err := New(config, Sources{Codex: source}, Capabilities{}, publisher, zap.NewNop())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return runner
}

func newDualTestRunner(t *testing.T, usageSource UsageSource, codexSource CodexSource, publisher Transport) *Runner {
	t.Helper()
	config := usageConfig()
	config.CodexPollInterval = time.Minute
	config.CodexObservationTTL = 2 * time.Minute
	runner, err := New(config, Sources{Usage: usageSource, Codex: codexSource}, Capabilities{}, publisher, zap.NewNop())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return runner
}

func usageConfig() Config {
	return Config{
		AgentID:        "agent-a",
		AgentEpoch:     "epoch-a",
		AgentVersion:   "test",
		HostLabel:      "test host",
		CurrencyCode:   "USD",
		Location:       time.FixedZone("UTC+8", 8*60*60),
		PollInterval:   time.Minute,
		ObservationTTL: 2 * time.Minute,
	}
}

func codexConfig(displayName, projectName bool) Config {
	return Config{
		AgentID:             "agent-a",
		AgentEpoch:          "epoch-a",
		AgentVersion:        "test",
		HostLabel:           "test host",
		CodexPollInterval:   time.Minute,
		CodexObservationTTL: 2 * time.Minute,
		CodexDisplayName:    displayName,
		CodexProjectName:    projectName,
	}
}

func waitForMessages(t *testing.T, publisher *recordingPublisher, count int) []mqtt.Message {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		messages := publisher.Messages()
		if len(messages) >= count {
			return messages
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d messages; got %d", count, len(publisher.Messages()))
	return nil
}

func equalUint64s(left, right []uint64) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
