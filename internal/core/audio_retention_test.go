package core

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"orbit/internal/config"
	"orbit/internal/mqtt"
)

func retentionSegment(n int, startedAt string) AudioSegment {
	s := audioFixture()
	s.SegmentID = fmt.Sprintf("00000000-0000-4000-8000-%012d", n)
	s.SessionID = fmt.Sprintf("10000000-0000-4000-8000-%012d", n)
	s.StartedAt = startedAt
	return s
}

func retentionResult(s AudioSegment) AudioResult {
	return AudioResult{SchemaVersion: 1, JobID: s.SegmentID, SegmentID: s.SegmentID, NodeID: s.NodeID, AgentID: "worker-1", Status: "completed", Text: "hello", Language: "en", Device: "cpu", CompletedAt: "2026-10-24T12:00:00Z"}
}

func assertAudioCount(t *testing.T, s *RouteStore, table string, want int) {
	t.Helper()
	var got int
	if err := s.db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("%s count = %d, want %d", table, got, want)
	}
}

func TestAudioRetentionPrunesAllStatusesAndAcknowledgesLateMessages(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "core.sqlite")
	s, err := OpenRouteStore(path, nil, config.AudioConfig{MaxSegments: 2})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	// Upload out of recording order and cross a fractional-second boundary.
	old := retentionSegment(1, "2026-10-23T12:00:00Z")
	pending := retentionSegment(2, "2026-10-23T12:00:00.1Z")
	newer := retentionSegment(3, "2026-10-23T12:00:01Z")
	newer.NodeID = "android-2" // The limit is global, not per node.
	for _, seg := range []AudioSegment{pending, old} {
		if err = s.storeAudioSegment(ctx, seg, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	if err = s.storeAudioResult(ctx, retentionResult(old)); err != nil {
		t.Fatal(err)
	}
	assertAudioCount(t, s, "audio_results", 1)
	if err = s.storeAudioSegment(ctx, newer, time.Now()); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"audio_segments", "audio_jobs", "audio_sessions"} {
		assertAudioCount(t, s, table, 2)
	}
	assertAudioCount(t, s, "audio_results", 0)
	// Next insertion evicts a pending job as well, including its empty session.
	latest := retentionSegment(4, "2026-10-23T12:00:02Z")
	latest.SessionID = newer.SessionID
	latest.NodeID = newer.NodeID
	if err = s.storeAudioSegment(ctx, latest, time.Now()); err != nil {
		t.Fatal(err)
	}
	assertAudioCount(t, s, "audio_sessions", 1)
	var pendingCount int
	if err = s.db.QueryRow(`SELECT count(*) FROM audio_jobs WHERE job_id=?`, pending.SegmentID).Scan(&pendingCount); err != nil || pendingCount != 0 {
		t.Fatalf("evicted pending job still present: %d, %v", pendingCount, err)
	}
	// The cutoff survives a restart and a larger configured limit.
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = OpenRouteStore(path, nil, config.AudioConfig{MaxSegments: 10})
	if err != nil {
		t.Fatal(err)
	}
	tr := &audioTransport{}
	runner, err := NewAudioRunner(s, tr, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, seg := range []AudioSegment{old, pending} {
		raw, _ := json.Marshal(seg)
		if err = runner.handle(ctx, mqtt.Message{Topic: "orbit/v1/nodes/android-1/audio/segments", Payload: raw}); err != nil {
			t.Fatal(err)
		}
		result, _ := json.Marshal(retentionResult(seg))
		if err = runner.handle(ctx, mqtt.Message{Topic: "orbit/v1/agents/worker-1/audio/results", Payload: result}); err != nil {
			t.Fatal(err)
		}
	}
	if len(tr.messages) != 4 {
		t.Fatalf("late uploads/results not acknowledged: %d", len(tr.messages))
	}
	for _, msg := range tr.messages {
		var ack map[string]any
		if err = json.Unmarshal(msg.Payload, &ack); err != nil || ack["status"] != "stored" {
			t.Fatalf("bad ACK: %s", msg.Payload)
		}
	}
	assertAudioCount(t, s, "audio_segments", 2)
	assertAudioCount(t, s, "audio_jobs", 2)
	assertAudioCount(t, s, "audio_results", 0)
	assertAudioCount(t, s, "audio_retention", 1)
	if err = runner.dispatch(ctx); err != nil {
		t.Fatal(err)
	}
	if len(tr.messages) != 6 {
		t.Fatal("retained pending jobs not dispatched")
	}
	if err = s.storeAudioResult(ctx, retentionResult(latest)); err != nil {
		t.Fatal(err)
	}
	assertAudioCount(t, s, "audio_results", 1)
}

func TestAudioRetentionAppliesOnOpenAndBreaksTies(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "core.sqlite")
	s, err := OpenRouteStore(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []int{3, 1, 2} {
		seg := retentionSegment(n, "2026-10-23T12:00:00Z")
		if err = s.storeAudioSegment(ctx, seg, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	// Simulate a database from before retention support.
	if _, err = s.db.Exec(`DROP TABLE audio_retention; DROP INDEX audio_segments_retention; DROP INDEX audio_segments_session`); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = OpenRouteStore(path, nil, config.AudioConfig{MaxSegments: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, table := range []string{"audio_segments", "audio_jobs", "audio_sessions"} {
		assertAudioCount(t, s, table, 1)
	}
	var id string
	if err = s.db.QueryRow(`SELECT segment_id FROM audio_segments`).Scan(&id); err != nil || id != retentionSegment(3, "").SegmentID {
		t.Fatalf("wrong retained segment: %s, %v", id, err)
	}
	// A previously unseen old upload must not displace newer recordings.
	if err = s.storeAudioSegment(ctx, retentionSegment(4, "2026-10-22T12:00:00Z"), time.Now()); err != nil {
		t.Fatal(err)
	}
	assertAudioCount(t, s, "audio_segments", 1)
}

func TestAudioRetentionDeletionFailureRollsBackInsertion(t *testing.T) {
	s, err := OpenRouteStore(filepath.Join(t.TempDir(), "core.sqlite"), nil, config.AudioConfig{MaxSegments: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	old := retentionSegment(1, "2026-10-23T12:00:00Z")
	if err = s.storeAudioSegment(ctx, old, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err = s.storeAudioResult(ctx, retentionResult(old)); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(`CREATE TRIGGER fail_audio_delete BEFORE DELETE ON audio_segments BEGIN SELECT RAISE(ABORT,'test deletion failure'); END`); err != nil {
		t.Fatal(err)
	}
	tr := &audioTransport{}
	runner, _ := NewAudioRunner(s, tr, nil, nil)
	raw, _ := json.Marshal(retentionSegment(2, "2026-10-23T12:00:01Z"))
	if err = runner.handle(ctx, mqtt.Message{Topic: "orbit/v1/nodes/android-1/audio/segments", Payload: raw}); err == nil {
		t.Fatal("failed deletion was accepted")
	}
	if len(tr.messages) != 0 {
		t.Fatal("rolled back upload acknowledged")
	}
	for _, table := range []string{"audio_segments", "audio_jobs", "audio_sessions", "audio_results"} {
		assertAudioCount(t, s, table, 1)
	}
	assertAudioCount(t, s, "audio_retention", 0)
}
