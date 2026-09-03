package integration

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	orbitv1 "orbit/gen/go/orbit/v1"
	"orbit/internal/agent"
	"orbit/internal/core"
	"orbit/internal/mqtt"
	"orbit/internal/sources/codex"
	"orbit/internal/sources/sub2api"

	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestSub2APIToRetainedDeviceView(t *testing.T) {
	t.Parallel()
	var loginCalls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/api/v1/auth/login":
			loginCalls.Add(1)
			fmt.Fprint(response, `{"code":0,"message":"ok","data":{"access_token":"access","refresh_token":"refresh","expires_in":3600}}`)
		case "/api/v1/usage/dashboard/stats":
			if request.Header.Get("Authorization") != "Bearer access" {
				http.Error(response, "unauthorized", http.StatusUnauthorized)
				return
			}
			fmt.Fprint(response, `{"code":0,"message":"ok","data":{"today_actual_cost":12.345678,"today_tokens":1234567,"tpm":4567}}`)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	source, err := sub2api.New(sub2api.Config{
		LoginEndpoint:   server.URL + "/api/v1/auth/login",
		RefreshEndpoint: server.URL + "/api/v1/auth/refresh",
		UsageEndpoint:   server.URL + "/api/v1/usage/dashboard/stats",
		Email:           "test@example.com",
		Password:        "secret",
		Timeout:         time.Second,
		HTTPClient:      server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	engine, err := core.New(core.Config{
		CoreID:    "core-test",
		CoreEpoch: "core-epoch-test",
		Routes: []core.Route{{
			NodeID: "node-test", AgentID: "agent-test", Profile: "usage-oled-128x32",
		}},
		UsagePolicy: core.UsagePolicy{MaxTTL: 5 * time.Minute, MaxFutureSkew: 10 * time.Second},
		RetainFor:   time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	broker := newMemoryBroker()
	coreRunner, err := core.NewRunner(engine, broker, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	agentRunner, err := agent.New(agent.Config{
		AgentID:        "agent-test",
		AgentEpoch:     "agent-epoch-test",
		AgentVersion:   "test",
		HostLabel:      "integration test",
		CurrencyCode:   "USD",
		Location:       location,
		PollInterval:   time.Minute,
		ObservationTTL: 2 * time.Minute,
	}, agent.Sources{Usage: source}, agent.Capabilities{}, broker, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	coreDone := make(chan error, 1)
	go func() { coreDone <- coreRunner.Run(ctx) }()
	select {
	case <-broker.ready:
	case <-time.After(time.Second):
		t.Fatal("core subscriptions were not registered")
	}

	viewReceived := make(chan mqtt.Message, 1)
	if err := broker.Subscribe(ctx, "orbit/v1/nodes/node-test/view", func(_ context.Context, message mqtt.Message) error {
		viewReceived <- message
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	nodePayload, err := proto.Marshal(&orbitv1.NodeState{
		Metadata: &orbitv1.Metadata{
			MessageId: "node-state", ProducerId: "node-test", Revision: 1, ProducedAt: timestamppb.New(now),
		},
		NodeId: "node-test", NodeEpoch: "node-epoch-test", SeriesId: "display",
		ModelId: "oled-128x32", VariantId: "yd-esp32-s3", FirmwareVersion: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := broker.Publish(ctx, mqtt.Message{Topic: "orbit/v1/nodes/node-test/state", Payload: nodePayload, Retain: true}); err != nil {
		t.Fatal(err)
	}
	agentDone := make(chan error, 1)
	go func() { agentDone <- agentRunner.Run(ctx) }()

	select {
	case message := <-viewReceived:
		if !message.Retain {
			t.Fatal("DeviceView was not retained")
		}
		var view orbitv1.DeviceView
		if err := proto.Unmarshal(message.Payload, &view); err != nil {
			t.Fatal(err)
		}
		if view.Freshness != orbitv1.Freshness_FRESHNESS_FRESH || view.Primary.Text != "$12.35" || view.Secondary.Text != "1M" || view.Footer.Text != "4K" {
			t.Fatalf("unexpected DeviceView: %+v", &view)
		}
		if strings.Contains(view.String(), "agent-test") || loginCalls.Load() != 1 {
			t.Fatalf("privacy/auth invariant failed: view=%s login_calls=%d", view.String(), loginCalls.Load())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("node did not receive DeviceView")
	}

	cancel()
	for name, done := range map[string]<-chan error{"agent": agentDone, "core": coreDone} {
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("%s runner returned error: %v", name, err)
			}
		case <-time.After(time.Second):
			t.Fatalf("%s runner did not stop", name)
		}
	}
}

type memorySubscription struct {
	filter  string
	handler mqtt.Handler
}

type memoryBroker struct {
	mu            sync.RWMutex
	subscriptions []memorySubscription
	retained      map[string]mqtt.Message
	ready         chan struct{}
	readyOnce     sync.Once
}

func newMemoryBroker() *memoryBroker {
	return &memoryBroker{retained: make(map[string]mqtt.Message), ready: make(chan struct{})}
}

func (broker *memoryBroker) Publish(ctx context.Context, message mqtt.Message) error {
	broker.mu.Lock()
	if message.Retain {
		broker.retained[message.Topic] = cloneMessage(message)
	}
	subscriptions := append([]memorySubscription(nil), broker.subscriptions...)
	broker.mu.Unlock()
	for _, subscription := range subscriptions {
		if topicMatches(subscription.filter, message.Topic) {
			if err := subscription.handler(ctx, cloneMessage(message)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (broker *memoryBroker) Subscribe(ctx context.Context, filter string, handler mqtt.Handler) error {
	broker.mu.Lock()
	broker.subscriptions = append(broker.subscriptions, memorySubscription{filter: filter, handler: handler})
	retained := make([]mqtt.Message, 0)
	for topic, message := range broker.retained {
		if topicMatches(filter, topic) {
			retained = append(retained, cloneMessage(message))
		}
	}
	count := len(broker.subscriptions)
	broker.mu.Unlock()
	if count >= 3 {
		broker.readyOnce.Do(func() { close(broker.ready) })
	}
	for _, message := range retained {
		if err := handler(ctx, message); err != nil {
			return err
		}
	}
	return nil
}

func cloneMessage(message mqtt.Message) mqtt.Message {
	message.Payload = append([]byte(nil), message.Payload...)
	return message
}

func topicMatches(filter, topic string) bool {
	filterParts := strings.Split(filter, "/")
	topicParts := strings.Split(topic, "/")
	if len(filterParts) != len(topicParts) {
		return false
	}
	for index := range filterParts {
		if filterParts[index] != "+" && filterParts[index] != topicParts[index] {
			return false
		}
	}
	return true
}

type integrationCodexSource struct {
	snapshot codex.Snapshot
}

func (source *integrationCodexSource) Fetch(context.Context) (codex.Snapshot, error) {
	return source.snapshot, nil
}

func TestCodexAgentPublishesSanitizedObservation(t *testing.T) {
	updated := time.Date(2026, 9, 3, 16, 0, 0, 0, time.UTC)
	source := &integrationCodexSource{snapshot: codex.Snapshot{
		Sessions: []codex.Session{{
			ID:           "codex-session",
			DisplayName:  "prompt-title-must-not-travel",
			ProjectName:  "secret-repo",
			Model:        "gpt-5.6-luna",
			Status:       "running",
			UpdatedAt:    updated,
			ProcessAlive: true,
		}},
		TotalCount:   1,
		RunningCount: 1,
	}}
	broker := newMemoryBroker()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := make(chan mqtt.Message, 3)
	stateTopic := "orbit/v1/agents/codex-agent/state"
	observationTopic := "orbit/v1/agents/codex-agent/observations/codex"
	if err := broker.Subscribe(ctx, stateTopic, func(_ context.Context, message mqtt.Message) error {
		events <- message
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := broker.Subscribe(ctx, observationTopic, func(_ context.Context, message mqtt.Message) error {
		events <- message
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	runner, err := agent.New(agent.Config{
		AgentID:             "codex-agent",
		AgentEpoch:          "codex-epoch",
		AgentVersion:        "test",
		HostLabel:           "integration test",
		CodexPollInterval:   time.Hour,
		CodexObservationTTL: 2 * time.Hour,
	}, agent.Sources{Codex: source}, agent.Capabilities{}, broker, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- runner.Run(ctx) }()
	first := receiveMessage(t, events)
	if first.Topic != stateTopic || !first.Retain {
		t.Fatalf("first Agent message was not retained state: %+v", first)
	}
	var initial orbitv1.AgentState
	if err := proto.Unmarshal(first.Payload, &initial); err != nil {
		t.Fatal(err)
	}
	if len(initial.Sources) != 1 || initial.Sources[0].ObservationType != orbitv1.ObservationType_OBSERVATION_TYPE_CODEX || initial.Sources[0].Health != orbitv1.SourceHealth_SOURCE_HEALTH_UNSPECIFIED {
		t.Fatalf("unexpected initial Codex state: %+v", &initial)
	}

	observationMessage := receiveMessage(t, events)
	if observationMessage.Topic != observationTopic || observationMessage.Retain {
		t.Fatalf("unexpected Codex observation delivery: %+v", observationMessage)
	}
	var observation orbitv1.Observation
	if err := proto.Unmarshal(observationMessage.Payload, &observation); err != nil {
		t.Fatal(err)
	}
	if observation.GetMetadata().GetRevision() != 1 || observation.GetAgentEpoch() != "codex-epoch" || observation.GetCodex().GetRunningCount() != 1 {
		t.Fatalf("unexpected Codex observation: %+v", &observation)
	}
	if observation.GetMetadata().GetExpiresAt().AsTime().Sub(observation.GetMetadata().GetProducedAt().AsTime()) != 2*time.Hour {
		t.Fatalf("unexpected Codex observation TTL: %+v", observation.GetMetadata())
	}
	session := observation.GetCodex().GetSessions()[0]
	if session.GetDisplayName() != "" || session.GetProjectName() != "" || session.GetModel() != "gpt-5.6-luna" || session.GetStatus() != orbitv1.CodexSessionStatus_CODEX_SESSION_STATUS_RUNNING || !session.GetProcessAlive() || !session.GetUpdatedAt().AsTime().Equal(updated) {
		t.Fatalf("unexpected sanitized Codex session: %+v", session)
	}
	for _, forbidden := range []string{"prompt-title-must-not-travel", "secret-repo"} {
		if strings.Contains(string(observationMessage.Payload), forbidden) {
			t.Fatalf("forbidden Codex value %q leaked into payload", forbidden)
		}
	}

	finalStateMessage := receiveMessage(t, events)
	if finalStateMessage.Topic != stateTopic || !finalStateMessage.Retain {
		t.Fatalf("unexpected final AgentState delivery: %+v", finalStateMessage)
	}
	var finalState orbitv1.AgentState
	if err := proto.Unmarshal(finalStateMessage.Payload, &finalState); err != nil {
		t.Fatal(err)
	}
	if finalState.Sources[0].Health != orbitv1.SourceHealth_SOURCE_HEALTH_HEALTHY || finalState.Sources[0].LastSuccessAt == nil {
		t.Fatalf("Codex source did not become healthy: %+v", finalState.Sources[0])
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("agent runner returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("agent runner did not stop")
	}
}

func receiveMessage(t *testing.T, events <-chan mqtt.Message) mqtt.Message {
	t.Helper()
	select {
	case message := <-events:
		return message
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for Agent MQTT message")
		return mqtt.Message{}
	}
}
