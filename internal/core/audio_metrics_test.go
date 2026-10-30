package core

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"orbit/internal/config"
	"orbit/internal/mqtt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAudioMetricsExposesQueueAndWorkers(t *testing.T) {
	store, err := OpenRouteStore(filepath.Join(t.TempDir(), "core.sqlite"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	segment := audioFixture()
	if err := store.storeAudioSegment(context.Background(), segment, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`UPDATE audio_jobs SET attempts=2`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`INSERT INTO audio_workers VALUES(?,?,?,?)`, "worker-1", `{"agent_id":"worker-1","host_label":"tx","model":"qwen","device":"cuda","status":"processing"}`, time.Now().UTC().Format(time.RFC3339Nano), 1); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/metrics", nil)
	res := httptest.NewRecorder()
	store.audioMetrics(res, req)
	if res.Code != 200 {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
	body := res.Body.String()
	for _, want := range []string{
		`orbit_audio_jobs{status="pending"} 1`,
		"orbit_audio_segments_retained 1",
		"orbit_audio_job_attempts 2",
		`orbit_audio_worker_info{agent_id="worker-1",connection="online",device="cuda",host_label="tx",model="qwen",status="processing"} 1`,
		`orbit_audio_worker_up{agent_id="worker-1"} 1`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics missing %q in:\n%s", want, body)
		}
	}
}

func TestAudioMetricsWorkerFreshnessAndQueue(t *testing.T) {
	store, err := OpenRouteStore(filepath.Join(t.TempDir(), "core.sqlite"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC()
	runner, _ := NewAudioRunner(store, &audioTransport{}, nil, func() time.Time { return now })
	worker := AudioWorkerState{SchemaVersion: 1, AgentID: "worker-1", HostLabel: "test\"host\n", Model: "qwen", Device: "vulkan", Status: "processing", UpdatedAt: now.Format(time.RFC3339Nano), Queue: &AudioWorkerQueue{Queued: 2, Processing: 1, Outbox: 183, Capacity: 1000}}
	check := func(live bool, age time.Duration, state string, wantUp string, wantQueue bool) {
		t.Helper()
		worker.Status = state
		raw, _ := json.Marshal(worker)
		if err := runner.handle(context.Background(), mqtt.Message{Topic: "orbit/v1/agents/worker-1/audio/state", Payload: raw, Retain: !live}); err != nil {
			t.Fatal(err)
		}
		body, err := store.collectAudioMetrics(context.Background(), now.Add(age))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(body, `orbit_audio_worker_up{agent_id="worker-1"} `+wantUp+"\n") {
			t.Fatal(body)
		}
		if strings.Contains(body, `orbit_audio_worker_jobs{agent_id="worker-1",stage="outbox"} 183`) != wantQueue {
			t.Fatal(body)
		}
	}
	check(true, 0, "processing", "1", true)
	check(true, 60*time.Second, "processing", "0", false)
	check(false, 0, "processing", "0", false)
	check(true, 0, "offline", "0", false)
	worker.Queue = nil
	check(true, 0, "ready", "1", false)
	worker.Queue = &AudioWorkerQueue{Queued: -1, Capacity: 1000}
	raw, _ := json.Marshal(worker)
	if err := runner.handle(context.Background(), mqtt.Message{Topic: "orbit/v1/agents/worker-1/audio/state", Payload: raw}); err == nil {
		t.Fatal("accepted negative queue")
	}
}

func TestAudioCountersDeduplicateAndSurviveRetentionAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "core.sqlite")
	store, err := OpenRouteStore(path, nil, config.AudioConfig{MaxSegments: 1})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	seg := audioFixture()
	result := AudioResult{SchemaVersion: 1, JobID: seg.SegmentID, SegmentID: seg.SegmentID, NodeID: seg.NodeID, AgentID: "worker-1", Status: "completed", Device: "cpu", CompletedAt: time.Now().UTC().Format(time.RFC3339Nano), ProcessingMS: 100}
	for range 2 {
		if err := store.storeAudioSegment(ctx, seg, time.Now()); err != nil {
			t.Fatal(err)
		}
		if err := store.storeAudioResult(ctx, result); err != nil {
			t.Fatal(err)
		}
	}
	seg.SegmentID = "00000000-0000-4000-8000-000000000001"
	seg.StartedAt = "2026-10-24T00:00:00Z"
	if err := store.storeAudioSegment(ctx, seg, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = OpenRouteStore(path, nil, config.AudioConfig{MaxSegments: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	// A late result for an evicted job is ACKed but must not count again.
	if err := store.storeAudioResult(ctx, result); err != nil {
		t.Fatal(err)
	}
	body, err := store.collectAudioMetrics(ctx, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"orbit_audio_received_total 2\n", `orbit_audio_results_total{agent_id="worker-1",status="completed"} 1`, `orbit_audio_processing_seconds_total{agent_id="worker-1",status="completed"} 0.1`, `orbit_audio_processed_audio_seconds_total{agent_id="worker-1",status="completed"} 0.02`, "orbit_audio_segments_retained 1"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %s\n%s", want, body)
		}
	}
}

func TestAudioMetricsUnavailableDoesNotReturnPartialScrape(t *testing.T) {
	store, err := OpenRouteStore(filepath.Join(t.TempDir(), "core.sqlite"), nil)
	if err != nil {
		t.Fatal(err)
	}
	store.Close()
	res := httptest.NewRecorder()
	store.MetricsHandler().ServeHTTP(res, httptest.NewRequest("GET", "/metrics", nil))
	if res.Code != 503 || strings.Contains(res.Body.String(), "# HELP") {
		t.Fatal(res.Code, res.Body.String())
	}
}
