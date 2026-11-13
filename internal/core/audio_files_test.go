package core

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"orbit/internal/config"
)

func TestOggOpusRoundTrip(t *testing.T) {
	for _, count := range []int{1, 50, 300, 1000} {
		t.Run(fmt.Sprintf("packets_%d", count), func(t *testing.T) {
			packets := make([]string, count)
			for i := range packets {
				packets[i] = "AQIDBAUGBwg="
			}
			encoded, err := encodeOggOpus(packets, 16000, 1)
			if err != nil {
				t.Fatalf("encodeOggOpus failed: %v", err)
			}
			decoded, err := decodeOggOpus(encoded)
			if err != nil {
				t.Fatalf("decodeOggOpus failed: %v", err)
			}
			if len(decoded) != len(packets) {
				t.Fatalf("decoded length = %d, want %d", len(decoded), len(packets))
			}
			for i := range packets {
				if decoded[i] != packets[i] {
					t.Fatalf("packet[%d] = %q, want %q", i, decoded[i], packets[i])
				}
			}
		})
	}
}

func TestAudioFilePersistenceAndSizeCleanup(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "core.sqlite")
	audioDir := filepath.Join(dir, "audio")

	// Measure one encoded segment file size so we can set a tight 90% budget.
	sample := audioFixture()
	sample.Packets = make([]string, 20)
	for i := range sample.Packets {
		sample.Packets[i] = "AQIDBAUGBwg="
	}
	sampleBytes, err := encodeOggOpus(sample.Packets, sample.SampleRate, sample.Channels)
	if err != nil {
		t.Fatal(err)
	}
	oneSize := int64(len(sampleBytes))
	// With MaxBytes = 3 * oneSize and 90% threshold (2.7 * oneSize):
	// 2 files = 2 * oneSize (< 2.7 * oneSize)
	// 3 files = 3 * oneSize (>= 2.7 * oneSize) -> triggers cleanup of earliest file -> 2 files remain.
	maxBytes := oneSize * 3

	s, err := OpenRouteStore(dbPath, nil, config.AudioConfig{
		MaxSegments:             100,
		MaxBytes:                maxBytes,
		CleanupThresholdPercent: 90,
		DataDir:                 audioDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	seg1 := retentionSegment(1, "2026-11-13T10:00:00Z")
	seg1.Packets = sample.Packets
	seg1.DurationMS = 20 * 20
	seg1.SpeechMS = 20 * 20

	seg2 := retentionSegment(2, "2026-11-13T10:01:00Z")
	seg2.Packets = sample.Packets
	seg2.DurationMS = 20 * 20
	seg2.SpeechMS = 20 * 20

	seg3 := retentionSegment(3, "2026-11-13T10:02:00Z")
	seg3.Packets = sample.Packets
	seg3.DurationMS = 20 * 20
	seg3.SpeechMS = 20 * 20

	for _, seg := range []AudioSegment{seg1, seg2} {
		if err := s.storeAudioSegment(ctx, seg, time.Now()); err != nil {
			t.Fatal(err)
		}
	}

	count, totalBytes, err := s.audioFiles.stats()
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 || totalBytes != 2*oneSize {
		t.Fatalf("after 2 segments: count=%d totalBytes=%d, want count=2 totalBytes=%d", count, totalBytes, 2*oneSize)
	}

	// Verify payload in SQLite does not retain packets.
	var payload string
	if err := s.db.QueryRow(`SELECT payload FROM audio_segments WHERE segment_id=?`, seg1.SegmentID).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(payload, `"packets"`) {
		t.Fatalf("expected SQLite payload to omit packets, got %s", payload)
	}

	// Verify dispatch reconstructs packets from persisted .opus file.
	tr := &audioTransport{}
	runner, err := NewAudioRunner(s, tr, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.dispatch(ctx); err != nil {
		t.Fatal(err)
	}
	if len(tr.messages) != 2 {
		t.Fatalf("dispatched %d messages, want 2", len(tr.messages))
	}
	var job struct {
		Segment AudioSegment `json:"segment"`
	}
	if err := json.Unmarshal(tr.messages[0].Payload, &job); err != nil {
		t.Fatal(err)
	}
	if len(job.Segment.Packets) != len(sample.Packets) {
		t.Fatalf("dispatched packet count = %d, want %d", len(job.Segment.Packets), len(sample.Packets))
	}

	// Adding seg3 reaches 100% (>= 90%), so earliest file (seg1) must be removed automatically.
	if err := s.storeAudioSegment(ctx, seg3, time.Now()); err != nil {
		t.Fatal(err)
	}
	count, totalBytes, err = s.audioFiles.stats()
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 || totalBytes >= s.audioFiles.triggerBytes() {
		t.Fatalf("after cleanup: count=%d totalBytes=%d trigger=%d", count, totalBytes, s.audioFiles.triggerBytes())
	}
	if _, err := os.Stat(s.audioFiles.pathFor(seg1.SegmentID)); !os.IsNotExist(err) {
		t.Fatalf("expected earliest file %s to be deleted, stat err=%v", seg1.SegmentID, err)
	}
	for _, kept := range []string{seg2.SegmentID, seg3.SegmentID} {
		if _, err := os.Stat(s.audioFiles.pathFor(kept)); err != nil {
			t.Fatalf("expected %s to remain, err=%v", kept, err)
		}
	}

	// Reopening with a smaller MaxBytes cleans up on startup as well.
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s2, err := OpenRouteStore(dbPath, nil, config.AudioConfig{
		MaxSegments:             100,
		MaxBytes:                oneSize * 2, // 90% = 1.8 * oneSize -> 2 files exceed 90% -> keep 1
		CleanupThresholdPercent: 90,
		DataDir:                 audioDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	count, _, err = s2.audioFiles.stats()
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("after reopen with tighter limit: count=%d, want 1", count)
	}
	if _, err := os.Stat(s2.audioFiles.pathFor(seg3.SegmentID)); err != nil {
		t.Fatalf("expected latest segment %s to remain after reopen: %v", seg3.SegmentID, err)
	}
}
