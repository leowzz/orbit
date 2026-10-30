package core

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"orbit/internal/config"
)

func reviewTestStore(t *testing.T, limit int) *RouteStore {
	t.Helper()
	s, err := OpenRouteStore(filepath.Join(t.TempDir(), "core.sqlite"), nil, config.AudioConfig{MaxSegments: config.SegmentLimit(limit), Analysis: config.AnalysisConfig{Enabled: true, Model: "test-model", BaseURL: "https://example.invalid/v1", APIKey: "test-secret"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}
func reviewSeed(t *testing.T, s *RouteStore, id, stamp, status, text string) {
	t.Helper()
	_, err := s.db.Exec(`INSERT INTO review_inputs VALUES(?,'android-1',?,strftime('%Y-%m-%dT%H:00:00Z',?),date(?,'+8 hours'),?,?)`, id, stamp, stamp, stamp, status, text)
	if err != nil {
		t.Fatal(err)
	}
}
func reviewFake(t *testing.T, id string, onCall func()) *reviewModel {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" || r.Header.Get("Authorization") != "Bearer test-secret" {
			t.Error("incorrect request")
		}
		if onCall != nil {
			onCall()
		}
		result := reviewResult{Title: "快递进展", Summary: "已查询快递，收货未知。", Items: []reviewItem{{Kind: "progress", Text: "已查到昨天发出", Source: "conversation", Owner: "未确认", Status: "reported_done", EvidenceIDs: []string{id}}}}
		b, _ := json.Marshal(result)
		json.NewEncoder(w).Encode(map[string]any{"status": "completed", "output": []any{map[string]any{"type": "message", "content": []any{map[string]string{"type": "output_text", "text": string(b)}}}}, "usage": map[string]int{"input_tokens": 100, "output_tokens": 40}})
	}))
	t.Cleanup(server.Close)
	return &reviewModel{client: server.Client(), endpoint: server.URL + "/responses", key: "test-secret", model: "test-model"}
}
func TestReviewCaptureSurvivesAudioPruning(t *testing.T) {
	s := reviewTestStore(t, 1)
	ctx := context.Background()
	seg := audioFixture()
	if err := s.storeAudioSegment(ctx, seg, time.Now()); err != nil {
		t.Fatal(err)
	}
	result := AudioResult{SchemaVersion: 1, JobID: seg.SegmentID, SegmentID: seg.SegmentID, NodeID: seg.NodeID, AgentID: "worker", Status: "completed", Text: "已经寄了", Device: "cpu", CompletedAt: seg.StartedAt}
	if err := s.storeAudioResult(ctx, result); err != nil {
		t.Fatal(err)
	}
	seg.SegmentID = "00000000-0000-4000-8000-000000000002"
	seg.StartedAt = "2026-10-23T12:01:00Z"
	if err := s.storeAudioSegment(ctx, seg, time.Now()); err != nil {
		t.Fatal(err)
	}
	seg.SegmentID = "00000000-0000-4000-8000-000000000003"
	seg.StartedAt = "2026-10-23T12:02:00Z"
	if err := s.storeAudioSegment(ctx, seg, time.Now()); err != nil {
		t.Fatal(err)
	}
	rows, err := s.reviewInputs(ctx, "android-1", "2026-10-23T12:00:00Z", "hour")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 || rows[0].Text != "已经寄了" || rows[1].Status != "lost" || rows[2].Status != "pending" {
		t.Fatalf("lost archive: %+v", rows)
	}
	// Duplicate final results must preserve the original captured evidence.
	result.Text = "changed"
	if err := s.storeAudioResult(ctx, result); err != nil {
		t.Fatal(err)
	}
	rows, _ = s.reviewInputs(ctx, "android-1", "2026-10-23T12:00:00Z", "hour")
	if rows[0].Text != "已经寄了" {
		t.Fatal("changed expired evidence")
	}
}
func TestReviewNoCaptureWhenDisabled(t *testing.T) {
	s, err := OpenRouteStore(filepath.Join(t.TempDir(), "core.sqlite"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.storeAudioSegment(context.Background(), audioFixture(), time.Now()); err != nil {
		t.Fatal(err)
	}
	var n int
	s.db.QueryRow(`SELECT count(*) FROM review_inputs`).Scan(&n)
	if n != 0 {
		t.Fatal("captured disabled input")
	}
}
func TestReviewSchedulingLateResultsAndDailyRevision(t *testing.T) {
	s := reviewTestStore(t, 100)
	ctx := context.Background()
	now := time.Date(2026, 10, 25, 1, 0, 0, 0, time.UTC)
	reviewSeed(t, s, "a", "2026-10-24T04:00:00Z", "completed", "已经查到快递昨天发出")
	reviewSeed(t, s, "b", "2026-10-24T04:01:00Z", "pending", "")
	calls := 0
	model := reviewFake(t, "a", func() { calls++ })
	if err := s.reviewCycle(ctx, model, now); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("want hour + day, got %d", calls)
	}
	if err := s.reviewCycle(ctx, model, now); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatal("unchanged inputs reprocessed")
	}
	s.db.Exec(`UPDATE review_inputs SET text='已经签收',status='completed' WHERE segment_id='b'`)
	if err := s.reviewCycle(ctx, model, now); err != nil {
		t.Fatal(err)
	}
	if calls != 4 {
		t.Fatal("late result did not refresh hour/day")
	}
	w, err := scanReview(s.db.QueryRow(`SELECT ` + reviewColumns + ` FROM review_windows WHERE kind='day'`))
	if err != nil {
		t.Fatal(err)
	}
	if w.Start != "2026-10-24" || w.Counts.Completed != 2 || w.Stale || len(w.Result.Evidence) != 1 {
		t.Fatalf("bad daily result: %+v", w)
	}
}
func TestReviewHourDelayAndShanghaiDay(t *testing.T) {
	s := reviewTestStore(t, 100)
	ctx := context.Background()
	reviewSeed(t, s, "a", "2026-10-24T16:00:00Z", "completed", "hello")
	before := time.Date(2026, 10, 24, 17, 9, 0, 0, time.UTC)
	if err := s.syncReviewHours(ctx, before); err != nil {
		t.Fatal(err)
	}
	var n int
	s.db.QueryRow(`SELECT count(*) FROM review_windows`).Scan(&n)
	if n != 0 {
		t.Fatal("ran before settling delay")
	}
	if err := s.syncReviewHours(ctx, before.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	s.db.QueryRow(`SELECT count(*) FROM review_windows`).Scan(&n)
	if n != 1 {
		t.Fatal("hour missing after delay")
	}
	inputs, err := s.reviewInputs(ctx, "android-1", "2026-10-25", "day")
	if err != nil || len(inputs) != 1 {
		t.Fatal("UTC+8 day boundary incorrect")
	}
}
func TestReviewRejectsInventedEvidenceAndRetries(t *testing.T) {
	s := reviewTestStore(t, 100)
	ctx := context.Background()
	now := time.Date(2026, 10, 24, 6, 0, 0, 0, time.UTC)
	reviewSeed(t, s, "a", "2026-10-24T04:00:00Z", "completed", "hello")
	s.syncReviewHours(ctx, now)
	calls := 0
	model := reviewFake(t, "invented-id", func() { calls++ })
	if _, err := s.processReview(ctx, model, "hour", now); err != nil {
		t.Fatal(err)
	}
	w, err := scanReview(s.db.QueryRow(`SELECT ` + reviewColumns + ` FROM review_windows`))
	if err != nil {
		t.Fatal(err)
	}
	if w.Status != "failed" || w.AnalyzedHash != "" || strings.Contains(w.Error, "test-secret") {
		t.Fatalf("bad failure: %+v", w)
	}
	if _, err := s.processReview(ctx, model, "hour", time.Now()); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("retry backoff ignored")
	}
}
func TestReviewLateArrivalDuringRequestDoesNotPublish(t *testing.T) {
	s := reviewTestStore(t, 100)
	ctx := context.Background()
	now := time.Date(2026, 10, 24, 6, 0, 0, 0, time.UTC)
	reviewSeed(t, s, "a", "2026-10-24T04:00:00Z", "completed", "hello")
	reviewSeed(t, s, "b", "2026-10-24T04:01:00Z", "pending", "")
	s.syncReviewHours(ctx, now)
	model := reviewFake(t, "a", func() {
		_, err := s.db.Exec(`UPDATE review_inputs SET text='late',status='completed' WHERE segment_id='b'`)
		if err != nil {
			t.Error(err)
		}
	})
	if _, err := s.processReview(ctx, model, "hour", now); err != nil {
		t.Fatal(err)
	}
	w, err := scanReview(s.db.QueryRow(`SELECT ` + reviewColumns + ` FROM review_windows`))
	if err != nil {
		t.Fatal(err)
	}
	if w.Status != "queued" || w.AnalyzedHash != "" {
		t.Fatal("published superseded analysis")
	}
}
func TestReviewEmptyWindowDoesNotCallModel(t *testing.T) {
	s := reviewTestStore(t, 100)
	ctx := context.Background()
	now := time.Date(2026, 10, 24, 6, 0, 0, 0, time.UTC)
	reviewSeed(t, s, "a", "2026-10-24T04:00:00Z", "completed", "")
	s.syncReviewHours(ctx, now)
	model := reviewFake(t, "a", func() { t.Error("empty input called model") })
	if _, err := s.processReview(ctx, model, "hour", now); err != nil {
		t.Fatal(err)
	}
	w, _ := scanReview(s.db.QueryRow(`SELECT ` + reviewColumns + ` FROM review_windows`))
	if w.Status != "completed" || w.Counts.Nonempty != 0 {
		t.Fatal("bad empty result")
	}
}
func TestReviewAPIUsesConsoleAuthentication(t *testing.T) {
	s := reviewTestStore(t, 100)
	handler := ConsoleHandler(&Runner{}, s, &config.CoreConfig{Console: config.ConsoleConfig{Password: "secret", SessionHours: 72}})
	for _, path := range []string{"/api/reviews?date=2026-10-24", "/api/reviews/1"} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 401 {
			t.Fatalf("%s: %d", path, w.Code)
		}
	}
	mux := http.NewServeMux()
	s.reviewRoutes(mux)
	for _, path := range []string{"/api/reviews?date=bad", "/api/reviews?date=2026-10-24&view=year", "/api/reviews/nope"} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 400 {
			t.Fatalf("%s: %d", path, w.Code)
		}
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/reviews?date=2026-10-24&view=week", nil))
	if w.Code != 200 || strings.Contains(w.Body.String(), "test-secret") || strings.Contains(w.Body.String(), "example.invalid") {
		t.Fatal("bad public config")
	}
}
