package integration

import (
	"context"
	"sync"
	"testing"
	"time"

	orbitv1 "orbit/gen/go/orbit/v1"
	"orbit/internal/agent"
	"orbit/internal/core"
	codexsource "orbit/internal/sources/codex"
	webnode "orbit/nodes/web"

	"go.uber.org/zap"
)

const integrationSessionID = "01a066af-69d4-77d1-a21b-26d84534a817"

type commandCodexSource struct{}

func (*commandCodexSource) Fetch(context.Context) (codexsource.Snapshot, error) {
	return codexsource.Snapshot{
		Sessions: []codexsource.Session{{
			ID: integrationSessionID, DisplayName: "Open this task", ProjectName: "orbit",
			Model: "gpt-5.6-sol", Status: "running", UpdatedAt: time.Now().UTC(), ProcessAlive: true,
		}},
		TotalCount: 1, RunningCount: 1,
	}, nil
}

type commandOpener struct {
	mu       sync.Mutex
	sessions []string
}

func (opener *commandOpener) Open(_ context.Context, sessionID string) error {
	opener.mu.Lock()
	defer opener.mu.Unlock()
	opener.sessions = append(opener.sessions, sessionID)
	return nil
}

func (opener *commandOpener) Sessions() []string {
	opener.mu.Lock()
	defer opener.mu.Unlock()
	return append([]string(nil), opener.sessions...)
}

func TestWebIntentOpensCodexSessionThroughCoreAndAgent(t *testing.T) {
	broker := newMemoryBroker()
	engine, err := core.New(core.Config{
		CoreID: "core-test", CoreEpoch: "core-epoch-test",
		Routes: []core.Route{{NodeID: "web-test", Profile: "overview-web", Inputs: []core.RouteInput{{
			AgentID: "agent-test", ObservationType: orbitv1.ObservationType_OBSERVATION_TYPE_CODEX,
		}}}},
		CodexPolicy: core.CodexPolicy{MaxTTL: time.Minute, MaxFutureSkew: 10 * time.Second},
		RetainFor:   time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	coreRunner, err := core.NewRunner(engine, broker, zap.NewNop(), nil)
	if err != nil {
		t.Fatal(err)
	}
	opener := &commandOpener{}
	agentRunner, err := agent.New(agent.Config{
		AgentID: "agent-test", AgentEpoch: "agent-epoch-test", AgentVersion: "test", HostLabel: "test host",
		CodexPollInterval: time.Minute, CodexObservationTTL: time.Minute,
	}, agent.Sources{Codex: &commandCodexSource{}}, agent.Capabilities{OpenCodexSession: opener}, broker, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	store := webnode.NewStore()
	webRunner, err := webnode.NewRunner(webnode.RunnerConfig{
		NodeID: "web-test", NodeEpoch: "node-epoch-test", FirmwareVersion: "test",
	}, broker, store, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 3)
	go func() { done <- coreRunner.Run(ctx) }()
	waitForBrokerSubscriptions(t, broker, 5)
	go func() { done <- agentRunner.Run(ctx) }()
	go func() { done <- webRunner.Run(ctx) }()

	var revision uint64
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		snapshot, snapshotErr := store.Snapshot()
		if snapshotErr != nil {
			t.Fatal(snapshotErr)
		}
		if snapshot != nil && snapshot.Codex != nil && len(snapshot.Codex.Sessions) == 1 {
			revision = snapshot.Revision
			break
		}
		time.Sleep(time.Millisecond)
	}
	if revision == 0 {
		t.Fatal("Web Node did not receive the Codex view")
	}
	if _, err := webRunner.OpenCodexSession(ctx, integrationSessionID, revision); err != nil {
		t.Fatal(err)
	}
	if got := opener.Sessions(); len(got) != 1 || got[0] != integrationSessionID {
		t.Fatalf("opened sessions = %v, want [%s]", got, integrationSessionID)
	}

	cancel()
	for range 3 {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(time.Second):
			t.Fatal("runner did not stop")
		}
	}
}

func waitForBrokerSubscriptions(t *testing.T, broker *memoryBroker, count int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		broker.mu.RLock()
		got := len(broker.subscriptions)
		broker.mu.RUnlock()
		if got >= count {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("broker did not receive %d subscriptions", count)
}
