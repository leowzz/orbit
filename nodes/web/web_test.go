package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	orbitv1 "orbit/gen/go/orbit/v1"
	"orbit/internal/mqtt"

	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type recordingTransport struct {
	mu        sync.Mutex
	published mqtt.Message
	filter    string
	handler   mqtt.Handler
}

func (t *recordingTransport) Publish(_ context.Context, message mqtt.Message) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.published = message
	return nil
}

func (t *recordingTransport) Subscribe(_ context.Context, filter string, handler mqtt.Handler) error {
	t.mu.Lock()
	t.filter = filter
	t.handler = handler
	t.mu.Unlock()
	return nil
}

func (t *recordingTransport) snapshot() (mqtt.Message, string, mqtt.Handler) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.published, t.filter, t.handler
}

func TestStoreExposesLatestRichView(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)
	store := NewStore()
	if err := store.Update(testView(now), now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := store.Update(testView(now), now.Add(2*time.Second)); err == nil {
		t.Fatal("accepted duplicate view revision")
	}

	request := httptest.NewRequest(http.MethodGet, "/api/state", nil)
	response := httptest.NewRecorder()
	Handler(store).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
	var snapshot Snapshot
	if err := json.Unmarshal(response.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Usage == nil || snapshot.Usage.TokenCount != 1234 || snapshot.Codex == nil || len(snapshot.Codex.Sessions) != 1 {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}
	if snapshot.Codex.Sessions[0].DisplayName != "Web node" || snapshot.Freshness != "fresh" {
		t.Fatalf("unexpected projected fields: %+v", snapshot)
	}
}

func TestStoreKeepsCachedSectionsAcrossPartialViews(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)
	store := NewStore()
	if err := store.Update(testView(now), now); err != nil {
		t.Fatal(err)
	}

	usageOnly := proto.Clone(testView(now.Add(time.Second))).(*orbitv1.DeviceView)
	usageOnly.Metadata.Revision = 2
	usageOnly.Codex = nil
	if err := store.Update(usageOnly, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}

	for client := 1; client <= 2; client++ {
		request := httptest.NewRequest(http.MethodGet, "/api/state", nil)
		response := httptest.NewRecorder()
		Handler(store).ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("client %d status = %d", client, response.Code)
		}
		var snapshot Snapshot
		if err := json.Unmarshal(response.Body.Bytes(), &snapshot); err != nil {
			t.Fatal(err)
		}
		if snapshot.Revision != 2 || snapshot.Usage == nil || snapshot.Codex == nil {
			t.Fatalf("client %d received incomplete cached snapshot: %+v", client, snapshot)
		}
	}
}

func TestStoreDoesNotCarryCacheAcrossCoreEpochs(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)
	store := NewStore()
	if err := store.Update(testView(now), now); err != nil {
		t.Fatal(err)
	}

	nextEpoch := proto.Clone(testView(now.Add(time.Second))).(*orbitv1.DeviceView)
	nextEpoch.Metadata.Revision = 1
	nextEpoch.CoreEpoch = "next-core-epoch"
	nextEpoch.Codex = nil
	if err := store.Update(nextEpoch, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}

	snapshot, err := store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Codex != nil {
		t.Fatalf("carried cached Codex state across Core epochs: %+v", snapshot.Codex)
	}
}

func TestStoreMarksRetainedSectionStale(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)
	store := NewStore()
	if err := store.Update(testView(now), now); err != nil {
		t.Fatal(err)
	}

	usageOnly := proto.Clone(testView(now.Add(2 * time.Minute))).(*orbitv1.DeviceView)
	usageOnly.Metadata.Revision = 2
	usageOnly.Codex = nil
	if err := store.Update(usageOnly, now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}

	snapshot, err := store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Codex == nil || snapshot.Codex.Freshness != "stale" {
		t.Fatalf("cached Codex state was not retained as stale: %+v", snapshot.Codex)
	}
}

func TestRunnerRegistersAndAcceptsOnlyItsView(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)
	transport := &recordingTransport{}
	store := NewStore()
	runner, err := NewRunner(RunnerConfig{
		NodeID: "web-a", NodeEpoch: "node-epoch", FirmwareVersion: "test",
	}, transport, store, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	runner.now = func() time.Time { return now }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runner.Run(ctx) }()
	var published mqtt.Message
	var filter string
	var handler mqtt.Handler
	for handler == nil || published.Topic == "" {
		time.Sleep(time.Millisecond)
		published, filter, handler = transport.snapshot()
	}
	if filter != "orbit/v1/nodes/web-a/view" || published.Topic != "orbit/v1/nodes/web-a/state" || !published.Retain {
		t.Fatalf("unexpected transport setup: filter=%q publish=%q retained=%t", filter, published.Topic, published.Retain)
	}
	var state orbitv1.NodeState
	if err := proto.Unmarshal(published.Payload, &state); err != nil {
		t.Fatal(err)
	}
	if state.ModelId != "web" || state.VariantId != "browser" || state.NodeId != "web-a" {
		t.Fatalf("unexpected node state: %+v", &state)
	}
	payload, _ := proto.Marshal(testView(now))
	if err := handler(ctx, mqtt.Message{Topic: filter, Payload: payload, Retain: true}); err != nil {
		t.Fatal(err)
	}
	snapshot, _ := store.Snapshot()
	if snapshot == nil || snapshot.Revision != 1 {
		t.Fatalf("view not stored: %+v", snapshot)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func testView(now time.Time) *orbitv1.DeviceView {
	cost := int64(1_250_000)
	tokens := uint64(1234)
	tpm := uint64(56)
	return &orbitv1.DeviceView{
		Metadata: &orbitv1.Metadata{
			MessageId: "view-a", ProducerId: "core-a", Revision: 1,
			ProducedAt: timestamppb.New(now), ExpiresAt: timestamppb.New(now.Add(time.Hour)),
		},
		NodeId: "web-a", CoreEpoch: "core-epoch", Freshness: orbitv1.Freshness_FRESHNESS_FRESH,
		FreshUntil: timestamppb.New(now.Add(time.Minute)), RetainUntil: timestamppb.New(now.Add(time.Hour)),
		Usage: &orbitv1.UsageView{
			Freshness: orbitv1.Freshness_FRESHNESS_FRESH, FreshUntil: timestamppb.New(now.Add(time.Minute)),
			ActualCostMicros: &cost, CurrencyCode: "USD", TokenCount: &tokens, Tpm: &tpm,
			ObservedAt: timestamppb.New(now),
		},
		Codex: &orbitv1.CodexView{
			Freshness: orbitv1.Freshness_FRESHNESS_FRESH, FreshUntil: timestamppb.New(now.Add(time.Minute)),
			TotalCount: 1, RunningCount: 1, ObservedAt: timestamppb.New(now),
			Sessions: []*orbitv1.CodexSessionView{{
				SessionId: "session-a", DisplayName: "Web node", ProjectName: "orbit", Model: "gpt-5",
				Status: orbitv1.CodexSessionStatus_CODEX_SESSION_STATUS_RUNNING, UpdatedAt: timestamppb.New(now), ProcessAlive: true,
			}},
		},
	}
}
