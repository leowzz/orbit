package core

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestAudioSortingAcrossPagesAndFilters(t *testing.T) {
	store, err := OpenRouteStore(filepath.Join(t.TempDir(), "core.sqlite"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	fixtures := []struct {
		startedAt, node, status string
		duration, speech        int
	}{
		{"2026-10-23T12:00:00Z", "android-1", "completed", 4000, 1000},
		{"2026-10-23T12:00:00.2Z", "android-1", "pending", 2000, 1200},
		{"2026-10-23T12:00:01Z", "android-2", "completed", 2000, 1000},
		{"2026-10-23T12:00:01Z", "android-1", "completed", 2000, 1000},
		{"2026-10-23T11:59:59Z", "android-1", "failed", 1000, 400},
	}
	for i, fixture := range fixtures {
		segment := audioFixture()
		segment.SegmentID = fmt.Sprintf("00000000-0000-4000-8000-%012d", i+1)
		segment.SessionID = segment.SegmentID
		segment.Sequence = int64(i + 1)
		segment.StartedAt, segment.NodeID = fixture.startedAt, fixture.node
		segment.DurationMS, segment.SpeechMS = fixture.duration, fixture.speech
		segment.Packets = make([]string, fixture.duration/20)
		for j := range segment.Packets {
			segment.Packets[j] = "AQ=="
		}
		if err := store.storeAudioSegment(ctx, segment, time.Now()); err != nil {
			t.Fatal(err)
		}
		if fixture.status != "pending" {
			result := AudioResult{SchemaVersion: 1, JobID: segment.SegmentID, SegmentID: segment.SegmentID,
				NodeID: segment.NodeID, AgentID: "worker-1", Status: fixture.status, Device: "cpu",
				CompletedAt: "2026-10-23T13:00:00Z"}
			if fixture.status == "completed" {
				result.Text = "匹配的转写文字"
			}
			if err := store.storeAudioResult(ctx, result); err != nil {
				t.Fatal(err)
			}
		}
	}
	mux := http.NewServeMux()
	store.audioRoutes(mux)
	for _, tc := range []struct {
		name, query string
		want        []int64
	}{
		{"default newest", "", []int64{3, 4, 2, 1, 5}},
		{"time ascending", "sort_by=started_at&order=asc", []int64{5, 1, 2, 3, 4}},
		{"time descending", "sort_by=started_at&order=desc", []int64{3, 4, 2, 1, 5}},
		{"duration ascending", "sort_by=duration_ms&order=asc", []int64{5, 3, 4, 2, 1}},
		{"duration descending", "sort_by=duration_ms&order=desc", []int64{1, 3, 4, 2, 5}},
		{"speech ascending", "sort_by=speech_ms&order=asc", []int64{5, 3, 4, 1, 2}},
		{"speech descending", "sort_by=speech_ms&order=desc", []int64{2, 3, 4, 1, 5}},
		{"combined filters", "sort_by=duration_ms&order=asc&node_id=android-1&status=completed&q=" + url.QueryEscape("匹配"), []int64{4, 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got []int64
			for offset := 0; offset < len(tc.want); offset += 2 {
				w := httptest.NewRecorder()
				mux.ServeHTTP(w, httptest.NewRequest("GET", fmt.Sprintf("/api/audio/segments?%s&limit=2&offset=%d", tc.query, offset), nil))
				if w.Code != 200 {
					t.Fatalf("status %d: %s", w.Code, w.Body.String())
				}
				var page struct {
					Items  []audioListItem `json:"items"`
					Total  int             `json:"total"`
					Offset int             `json:"offset"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
					t.Fatal(err)
				}
				if page.Total != len(tc.want) || page.Offset != offset || len(page.Items) > 2 {
					t.Fatalf("incorrect page: %+v", page)
				}
				for _, item := range page.Items {
					got = append(got, item.Sequence)
				}
			}
			if !slices.Equal(got, tc.want) {
				t.Fatalf("order = %v, want %v", got, tc.want)
			}
		})
	}
	for _, query := range []string{
		"sort_by=unknown", "order=random", "order=ASC",
		"sort_by=" + url.QueryEscape("s.started_at DESC; DROP TABLE audio_segments;--"),
		"sort_by=duration_ms&order=" + url.QueryEscape("desc,s.node_id"),
	} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/audio/segments?"+query, nil))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("invalid sort %q returned %d", query, w.Code)
		}
	}
}
