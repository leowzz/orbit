package integration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
	"google.golang.org/protobuf/encoding/protojson"
	orbitv1 "orbit/gen/go/orbit/v1"
	"orbit/internal/agent"
	"orbit/internal/appapi"
	"orbit/internal/config"
	"orbit/internal/core"
	"orbit/internal/inbox"
	codexsource "orbit/internal/sources/codex"
)

const integrationSessionID = "01a066af-69d4-77d1-a21b-26d84534a817"

type commandCodexSource struct{}

func (*commandCodexSource) Fetch(context.Context) (codexsource.Snapshot, error) {
	return codexsource.Snapshot{Sessions: []codexsource.Session{{ID: integrationSessionID, DisplayName: "Open this task", ProjectName: "orbit", Model: "test", Status: "running", UpdatedAt: time.Now().UTC(), ProcessAlive: true}}, TotalCount: 1, RunningCount: 1}, nil
}

type commandOpener struct {
	mu       sync.Mutex
	sessions []string
}

func (o *commandOpener) Open(_ context.Context, id string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.sessions = append(o.sessions, id)
	return nil
}
func (o *commandOpener) Sessions() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.sessions...)
}

// Retains the real Agent -> Core -> authenticated client -> command -> Agent
// round trip after retiring the separate Web Node process.
func TestAppAPIOpensCodexSessionThroughCoreAndAgent(t *testing.T) {
	broker := newMemoryBroker()
	engine, err := core.New(core.Config{CoreID: "core-test", CoreEpoch: "core-epoch-test", Routes: []core.Route{{NodeID: "app-test", Profile: "overview-app", Inputs: []core.RouteInput{{AgentID: "agent-test", ObservationType: orbitv1.ObservationType_OBSERVATION_TYPE_CODEX}}}}, CodexPolicy: core.CodexPolicy{MaxTTL: time.Minute, MaxFutureSkew: 10 * time.Second}, RetainFor: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	runner, err := core.NewRunner(engine, broker, zap.NewNop(), nil)
	if err != nil {
		t.Fatal(err)
	}
	opener := &commandOpener{}
	agentRunner, err := agent.New(agent.Config{AgentID: "agent-test", AgentEpoch: "agent-epoch-test", AgentVersion: "test", HostLabel: "test host", CodexPollInterval: time.Minute, CodexObservationTTL: time.Minute}, agent.Sources{Codex: &commandCodexSource{}}, agent.Capabilities{OpenCodexSession: opener}, broker, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	store, err := inbox.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	token := strings.Repeat("a", 64)
	digest := sha256.Sum256([]byte(token))
	api, err := appapi.New(store, config.AppConfig{DataDir: t.TempDir(), Devices: map[string]config.AppDevice{"app-test": {TokenSHA256: hex.EncodeToString(digest[:])}}}, func(node string) json.RawMessage {
		view := engine.AppView(time.Now(), node)
		if view == nil {
			return nil
		}
		data, _ := protojson.Marshal(view)
		return data
	})
	if err != nil {
		t.Fatal(err)
	}
	api.SetSessionOpener(runner.OpenAppCodexSession)
	handler := api.Handler()
	call := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 2)
	go func() { done <- runner.Run(ctx) }()
	waitForBrokerSubscriptions(t, broker, 5)
	go func() { done <- agentRunner.Run(ctx) }()
	var view orbitv1.DeviceView
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		response := call("GET", "/api/v1/status", "")
		var status struct {
			View json.RawMessage `json:"view"`
		}
		if response.Code == http.StatusOK && json.Unmarshal(response.Body.Bytes(), &status) == nil && protojson.Unmarshal(status.View, &view) == nil && len(view.GetCodex().GetSessions()) == 1 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if view.GetMetadata().GetRevision() == 0 {
		t.Fatal("App did not receive the Codex view")
	}
	response := call("POST", "/api/v1/sessions/"+integrationSessionID+"/open", fmt.Sprintf(`{"core_epoch":%q,"view_revision":"%d"}`, view.CoreEpoch, view.GetMetadata().Revision))
	if response.Code != http.StatusAccepted {
		t.Fatal(response.Code, response.Body.String())
	}
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && len(opener.Sessions()) == 0 {
		time.Sleep(time.Millisecond)
	}
	if got := opener.Sessions(); len(got) != 1 || got[0] != integrationSessionID {
		t.Fatalf("opened sessions = %v", got)
	}
	cancel()
	for range 2 {
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
