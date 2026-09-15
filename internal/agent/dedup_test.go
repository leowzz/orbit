package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	orbitv1 "orbit/gen/go/orbit/v1"
	"orbit/internal/mqtt"
	"orbit/internal/sources/codex"
	"orbit/internal/sources/sub2api"
)

func TestUsageDedupRefreshAndDayChange(t *testing.T) {
	source := &stubSource{usage: sub2api.Usage{TodayTokens: 10}}
	publisher := &recordingPublisher{}
	runner := newTestRunner(t, source, publisher)
	now := time.Date(2026, 9, 15, 1, 0, 0, 0, time.UTC)
	runner.now = func() time.Time { return now }
	runner.config.ObservationTTL = 5 * time.Minute
	poll := func(want int) {
		t.Helper()
		publisher.Reset()
		if err := runner.PollOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
		if got := len(publisher.Messages()); got != want {
			t.Fatalf("got %d messages, want %d", got, want)
		}
	}
	poll(2)
	now = now.Add(time.Minute)
	poll(0)
	source.usage.TPM++
	poll(1)
	now = now.Add(3 * time.Minute)
	poll(0)
	now = now.Add(time.Minute)
	poll(1) // last poll before expiry
	now = now.AddDate(0, 0, 1)
	poll(1) // changed accounting window
	if runner.usageRevision != 4 || runner.stateRevision != 1 {
		t.Fatalf("unexpected revisions: %d/%d", runner.usageRevision, runner.stateRevision)
	}
}

func TestCodexDedupIgnoresTimesAndOrderButSendsFacts(t *testing.T) {
	now := time.Date(2026, 9, 15, 1, 0, 0, 0, time.UTC)
	source := &stubCodexSource{snapshot: codex.Snapshot{TotalCount: 2, Sessions: []codex.Session{{ID: "a", UpdatedAt: now}, {ID: "b"}}}}
	publisher := &recordingPublisher{}
	runner := newCodexTestRunner(t, source, publisher, true, true)
	runner.now = func() time.Time { return now }
	if err := runner.PollCodexOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	publisher.Reset()
	now = now.Add(time.Second)
	source.snapshot.Sessions[0].UpdatedAt = now
	source.snapshot.Sessions[0], source.snapshot.Sessions[1] = source.snapshot.Sessions[1], source.snapshot.Sessions[0]
	if err := runner.PollCodexOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(publisher.Messages()) != 0 {
		t.Fatal("dynamic fields triggered publish")
	}
	for _, change := range []func(){
		func() { source.snapshot.Sessions[0].Status = "running" },
		func() { source.snapshot.Sessions[0].ProcessAlive = true },
		func() { source.snapshot.Sessions[0].Model = "new-model" },
		func() { source.snapshot.Sessions[0].DisplayName = "new-name" },
		func() { source.snapshot.Sessions[0].ProjectName = "new-project" },
		func() { source.snapshot.RunningCount++ },
		func() { source.snapshot.Sessions = source.snapshot.Sessions[:1] },
	} {
		publisher.Reset()
		change()
		if err := runner.PollCodexOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
		if len(publisher.Messages()) != 1 {
			t.Fatal("fact change did not publish exactly one observation")
		}
	}
}

type failingPublisher struct {
	recordingPublisher
	fail bool
}

func (p *failingPublisher) Publish(ctx context.Context, message mqtt.Message) error {
	if p.fail {
		return errors.New("transport unavailable")
	}
	return p.recordingPublisher.Publish(ctx, message)
}

func TestDedupRetriesFailedPublishAndReportsHealthTransitions(t *testing.T) {
	source := &stubSource{}
	publisher := &failingPublisher{fail: true}
	runner := newTestRunner(t, source, publisher)
	if err := runner.PollOnce(context.Background()); err == nil {
		t.Fatal("expected failure")
	}
	publisher.fail = false
	if err := runner.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(publisher.Messages()) != 2 {
		t.Fatal("failed send incorrectly cached")
	}
	publisher.Reset()
	source.err = errors.New("source unavailable")
	for i := 0; i < 2; i++ {
		if err := runner.PollOnce(context.Background()); err == nil {
			t.Fatal("expected source failure")
		}
	}
	if len(publisher.Messages()) != 1 {
		t.Fatal("repeated failure sent duplicate state")
	}
	publisher.Reset()
	source.err = nil
	if err := runner.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	messages := publisher.Messages()
	if len(messages) != 1 || !messages[0].Retain {
		t.Fatal("expected recovered state only")
	}
	var state orbitv1.AgentState
	if err := proto.Unmarshal(messages[0].Payload, &state); err != nil {
		t.Fatal(err)
	}
	if state.Sources[0].Health != orbitv1.SourceHealth_SOURCE_HEALTH_HEALTHY {
		t.Fatal("health did not recover")
	}
}
