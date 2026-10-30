package core

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"orbit/internal/mqtt"
)

func TestAudioWorkerConnectionEvidence(t *testing.T) {
	now := time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name, status string
		live         bool
		age          time.Duration
		want         string
	}{
		{"processing", "processing", true, 20 * time.Second, "online"},
		{"ready", "ready", true, 0, "online"},
		{"error is connected", "error", true, time.Second, "online"},
		{"timeout boundary", "ready", true, time.Minute, "stale"},
		{"before timeout", "processing", true, time.Minute - time.Nanosecond, "online"},
		{"future receipt", "ready", true, -time.Second, "stale"},
		{"retained replay", "ready", false, 0, "unconfirmed"},
		{"last will", "offline", true, 0, "offline"},
		{"retained last will", "offline", false, 0, "offline"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := audioWorkerConnection(tc.status, tc.live, now.Add(-tc.age), now); got != tc.want {
				t.Fatalf("got %s, want %s", got, tc.want)
			}
		})
	}
}

func TestAudioWorkerLiveHeartbeatAfterDiscovery(t *testing.T) {
	store, err := OpenRouteStore(filepath.Join(t.TempDir(), "core.sqlite"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	runner, _ := NewAudioRunner(store, &audioTransport{}, nil, nil)
	state := AudioWorkerState{SchemaVersion: 1, AgentID: "win-1", HostLabel: "Windows", Model: "Qwen", Device: "vulkan", Status: "processing", UpdatedAt: "2020-01-01T00:00:00Z"}
	for _, step := range []struct {
		retained           bool
		status, connection string
		online             bool
	}{
		{true, "processing", "unconfirmed", false}, {false, "processing", "online", true}, {false, "offline", "offline", false}, {false, "ready", "online", true},
	} {
		state.Status = step.status
		raw, _ := json.Marshal(state)
		if err := runner.handle(context.Background(), mqtt.Message{Topic: "orbit/v1/agents/win-1/audio/state", Payload: raw, Retain: step.retained}); err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		store.listAudioWorkers(w, httptest.NewRequest("GET", "/api/audio/workers", nil))
		var response struct {
			Items []struct {
				Online     bool   `json:"online"`
				Connection string `json:"connection_state"`
			} `json:"items"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if len(response.Items) != 1 || response.Items[0].Online != step.online || response.Items[0].Connection != step.connection {
			t.Fatal(w.Body.String())
		}
	}
}

func TestAudioFilterByWorker(t *testing.T) {
	store, err := OpenRouteStore(filepath.Join(t.TempDir(), "core.sqlite"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	segment := audioFixture()
	if err := store.storeAudioSegment(context.Background(), segment, time.Now()); err != nil {
		t.Fatal(err)
	}
	result := AudioResult{SchemaVersion: 1, JobID: segment.SegmentID, SegmentID: segment.SegmentID, NodeID: segment.NodeID, AgentID: "win-1", Status: "completed", Text: "文字", Device: "vulkan", CompletedAt: "2026-10-31T00:00:00Z"}
	if err := store.storeAudioResult(context.Background(), result); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		query       string
		count, code int
	}{
		{"agent_id=win-1", 1, 200}, {"agent_id=other", 0, 200}, {"agent_id=win-1&status=pending", 0, 200}, {"agent_id=win-1&hide_empty=true", 1, 200}, {"agent_id=bad%2Fid", 0, 400},
	} {
		w := httptest.NewRecorder()
		store.listAudio(w, httptest.NewRequest("GET", "/api/audio/segments?"+tc.query, nil))
		if w.Code != tc.code {
			t.Fatalf("%s: %d %s", tc.query, w.Code, w.Body.String())
		}
		if tc.code != 200 {
			continue
		}
		var page struct {
			Items []audioListItem
			Total int
		}
		if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		if page.Total != tc.count || len(page.Items) != tc.count {
			t.Fatal(w.Body.String())
		}
		if strings.Contains(w.Body.String(), "packets") {
			t.Fatal("audio packets leaked")
		}
	}
}
