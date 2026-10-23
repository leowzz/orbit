package core

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"orbit/internal/config"
	"orbit/internal/mqtt"
)

type audioTransport struct {
	messages []mqtt.Message
	fail     bool
}

func (t *audioTransport) Publish(_ context.Context, m mqtt.Message) error {
	if t.fail {
		return errors.New("offline")
	}
	t.messages = append(t.messages, m)
	return nil
}
func (*audioTransport) Subscribe(context.Context, string, mqtt.Handler) error { return nil }
func audioFixture() AudioSegment {
	return AudioSegment{SchemaVersion: 1, SegmentID: "b60d0429-5235-45fb-bb4d-24a25f1f8ef7", SessionID: "828bacb4-291b-44bf-a897-86f6023957cf", NodeID: "android-1", Sequence: 0, StartedAt: "2026-10-23T12:00:00Z", DurationMS: 20, SpeechMS: 20, SampleRate: 16000, Channels: 1, Codec: "opus", FrameMS: 20, Packets: []string{"AQ=="}}
}
func TestAudioTwentySecondSegmentLimit(t *testing.T) {
	for _, frames := range []int{500, 501, 999, 1000, 1001} {
		segment := audioFixture()
		segment.Packets = make([]string, frames)
		for i := range segment.Packets {
			segment.Packets[i] = "AQ=="
		}
		segment.DurationMS = frames * 20
		if err := segment.validate(segment.NodeID); (err != nil) != (frames > 1000) {
			t.Errorf("frames=%d: unexpected validation result: %v", frames, err)
		}
	}
}

func TestAudioDurabilityDuplicatesAndResult(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "core.sqlite")
	s, e := OpenRouteStore(path, nil)
	if e != nil {
		t.Fatal(e)
	}
	tr := &audioTransport{}
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	r, _ := NewAudioRunner(s, tr, nil, func() time.Time { return now })
	seg := audioFixture()
	raw, _ := json.Marshal(seg)
	m := mqtt.Message{Topic: "orbit/v1/nodes/android-1/audio/segments", Payload: raw}
	for range 2 {
		if e = r.handle(ctx, m); e != nil {
			t.Fatal(e)
		}
	}
	var n int
	s.db.QueryRow(`SELECT count(*) FROM audio_jobs`).Scan(&n)
	if n != 1 || len(tr.messages) != 2 {
		t.Fatalf("duplicate jobs/acks: %d %d", n, len(tr.messages))
	}
	seg.Sequence++
	m.Payload, _ = json.Marshal(seg)
	if r.handle(ctx, m) == nil {
		t.Fatal("ID reuse accepted")
	}
	tr.fail = true
	if r.dispatch(ctx) == nil {
		t.Fatal("publish failure lost")
	}
	s.Close()
	s, e = OpenRouteStore(path, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	r.store = s
	tr.fail = false
	if e = r.dispatch(ctx); e != nil {
		t.Fatal(e)
	}
	if tr.messages[len(tr.messages)-1].Topic != "orbit/v1/audio/jobs" {
		t.Fatal("pending job not recovered")
	}
	result := AudioResult{SchemaVersion: 1, JobID: seg.SegmentID, SegmentID: seg.SegmentID, NodeID: seg.NodeID, AgentID: "worker-1", Status: "completed", Text: "hello", Language: "en", Device: "cpu", CompletedAt: now.Format(time.RFC3339), Segments: []AudioTextSegment{{StartMS: 0, EndMS: 20, Text: "hello"}}}
	rm := mqtt.Message{Topic: "orbit/v1/agents/worker-1/audio/results"}
	rm.Payload, _ = json.Marshal(result)
	if e = r.handle(ctx, rm); e != nil {
		t.Fatal(e)
	}
	result.Text = "changed"
	rm.Payload, _ = json.Marshal(result)
	if e = r.handle(ctx, rm); e != nil {
		t.Fatal(e)
	}
	var text string
	s.db.QueryRow(`SELECT text FROM audio_results`).Scan(&text)
	if text != "hello" {
		t.Fatal("first terminal result overwritten")
	}
	if e = r.dispatch(ctx); e != nil {
		t.Fatal(e)
	}
}
func TestAudioRejectsUntrustedMessages(t *testing.T) {
	s, e := OpenRouteStore(filepath.Join(t.TempDir(), "core.sqlite"), nil)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	tr := &audioTransport{}
	r, _ := NewAudioRunner(s, tr, nil, nil)
	seg := audioFixture()
	raw, _ := json.Marshal(seg)
	base := mqtt.Message{Topic: "orbit/v1/nodes/android-1/audio/segments", Payload: raw}
	cases := []mqtt.Message{{Topic: base.Topic, Payload: []byte(strings.Repeat("x", AudioMaxMessageBytes+1))}, {Topic: "orbit/v1/nodes/other/audio/segments", Payload: raw}, {Topic: base.Topic, Payload: raw, Retain: true}}
	for _, m := range cases {
		if r.handle(context.Background(), m) == nil {
			t.Fatal("invalid message accepted")
		}
	}
	seg.Packets = []string{"!"}
	raw, _ = json.Marshal(seg)
	base.Payload = raw
	if r.handle(context.Background(), base) == nil {
		t.Fatal("invalid base64 accepted")
	}
	if len(tr.messages) != 0 {
		t.Fatal("rejection acknowledged")
	}
}
func TestAudioAPIPageAndMetadata(t *testing.T) {
	s, e := OpenRouteStore(filepath.Join(t.TempDir(), "core.sqlite"), nil)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	seg := audioFixture()
	if e = s.storeAudioSegment(context.Background(), seg, time.Now()); e != nil {
		t.Fatal(e)
	}
	mux := http.NewServeMux()
	s.audioRoutes(mux)
	for _, tc := range []struct {
		path string
		code int
	}{{"/api/audio/segments?limit=1&offset=0", 200}, {"/api/audio/segments?limit=201", 400}, {"/api/audio/segments?offset=-1", 400}, {"/api/audio/segments?status=bad", 400}, {"/api/audio/segments/" + seg.SegmentID, 200}, {"/api/audio/workers", 200}} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", tc.path, nil))
		if w.Code != tc.code {
			t.Fatalf("%s: %d %s", tc.path, w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "packets") {
			t.Fatal("API leaked packets")
		}
	}
}
func TestAudioWorkerRetainedIsNotOnline(t *testing.T) {
	s, e := OpenRouteStore(filepath.Join(t.TempDir(), "core.sqlite"), nil)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	r, _ := NewAudioRunner(s, &audioTransport{}, nil, nil)
	state := AudioWorkerState{SchemaVersion: 1, AgentID: "worker-1", HostLabel: "host", Model: "model", Device: "cpu", Status: "ready", UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	raw, _ := json.Marshal(state)
	if e = r.handle(context.Background(), mqtt.Message{Topic: "orbit/v1/agents/worker-1/audio/state", Payload: raw, Retain: true}); e != nil {
		t.Fatal(e)
	}
	w := httptest.NewRecorder()
	s.listAudioWorkers(w, httptest.NewRequest("GET", "/api/audio/workers", nil))
	if !strings.Contains(w.Body.String(), `"online":false`) {
		t.Fatal(w.Body.String())
	}
}

func TestAudioConsoleAuthentication(t *testing.T) {
	store := newTestConsoleStore(t)
	handler := ConsoleHandler(nil, store, &config.CoreConfig{Console: config.ConsoleConfig{Password: "password", SessionHours: 12}})
	for _, path := range []string{"/api/audio/segments", "/api/audio/workers", "/api/audio/segments/" + audioFixture().SegmentID} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 401 {
			t.Fatalf("%s exposed without auth: %d", path, w.Code)
		}
	}
	login := httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(`{"password":"password"}`))
	login.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, login)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	req := httptest.NewRequest("GET", "/api/audio/segments", nil)
	req.AddCookie(w.Result().Cookies()[0])
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
}
