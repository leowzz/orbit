package agent

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	orbitv1 "orbit/gen/go/orbit/v1"
	"orbit/internal/mqtt"
	"orbit/internal/sources/sub2api"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"google.golang.org/protobuf/proto"
)

type stubSource struct {
	usage sub2api.Usage
	err   error
}

func (source *stubSource) FetchUsage(context.Context) (sub2api.Usage, error) {
	return source.usage, source.err
}

type recordingPublisher struct {
	messages []mqtt.Message
}

func (publisher *recordingPublisher) Publish(_ context.Context, message mqtt.Message) error {
	publisher.messages = append(publisher.messages, message)
	return nil
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
		zapcore.DebugLevel,
	))
	now := time.Date(2026, 9, 3, 16, 30, 0, 0, time.UTC)
	runner.now = func() time.Time { return now }

	if err := runner.PollOnce(context.Background()); err != nil {
		t.Fatalf("PollOnce: %v", err)
	}
	if len(publisher.messages) != 2 {
		t.Fatalf("got %d messages, want observation and state", len(publisher.messages))
	}
	var observation orbitv1.Observation
	if err := proto.Unmarshal(publisher.messages[0].Payload, &observation); err != nil {
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
	if err := proto.Unmarshal(publisher.messages[1].Payload, &state); err != nil {
		t.Fatalf("unmarshal state: %v", err)
	}
	if !publisher.messages[1].Retain || state.Sources[0].Health != orbitv1.SourceHealth_SOURCE_HEALTH_HEALTHY {
		t.Fatalf("unexpected agent state: %+v", state.Sources[0])
	}
	for _, want := range []string{"sub2api usage fetched", `"cost_micros":1500000`, "usage observation published", "agent state published"} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("debug log missing %q: %s", want, logs.String())
		}
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
	publisher.messages = nil
	source.err = &sub2api.Error{Kind: sub2api.ErrorRateLimited, Operation: "usage"}
	if err := runner.PollOnce(context.Background()); err == nil {
		t.Fatal("PollOnce returned nil for source failure")
	}
	if len(publisher.messages) != 1 || !publisher.messages[0].Retain {
		t.Fatalf("got messages: %+v", publisher.messages)
	}
	var state orbitv1.AgentState
	if err := proto.Unmarshal(publisher.messages[0].Payload, &state); err != nil {
		t.Fatal(err)
	}
	if state.Sources[0].Health != orbitv1.SourceHealth_SOURCE_HEALTH_DEGRADED || state.Sources[0].ErrorCode != string(sub2api.ErrorRateLimited) {
		t.Fatalf("unexpected source status: %+v", state.Sources[0])
	}
}

func TestNewRejectsIncompleteConfig(t *testing.T) {
	t.Parallel()
	_, err := New(Config{}, &stubSource{}, &recordingPublisher{}, zap.NewNop())
	if err == nil || errors.Is(err, context.Canceled) {
		t.Fatalf("New returned unexpected error: %v", err)
	}
}

func TestNewRejectsUnsupportedCurrency(t *testing.T) {
	t.Parallel()
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	_, err = New(Config{
		AgentID:        "agent-a",
		AgentEpoch:     "epoch-a",
		AgentVersion:   "test",
		HostLabel:      "test host",
		CurrencyCode:   "EUR",
		Location:       location,
		PollInterval:   time.Minute,
		ObservationTTL: 2 * time.Minute,
	}, &stubSource{}, &recordingPublisher{}, zap.NewNop())
	if err == nil {
		t.Fatal("New accepted a currency that V1 cannot render correctly")
	}
}

func newTestRunner(t *testing.T, source UsageSource, publisher Publisher) *Runner {
	t.Helper()
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	runner, err := New(Config{
		AgentID:        "agent-a",
		AgentEpoch:     "epoch-a",
		AgentVersion:   "test",
		HostLabel:      "test host",
		CurrencyCode:   "USD",
		Location:       location,
		PollInterval:   time.Minute,
		ObservationTTL: 2 * time.Minute,
	}, source, publisher, zap.NewNop())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return runner
}
