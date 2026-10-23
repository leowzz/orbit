package core

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"orbit/internal/mqtt"
)

const audioSchema = `
CREATE TABLE IF NOT EXISTS audio_sessions(session_id TEXT PRIMARY KEY,node_id TEXT NOT NULL,created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS audio_segments(segment_id TEXT PRIMARY KEY,session_id TEXT NOT NULL,node_id TEXT NOT NULL,sequence INTEGER NOT NULL,started_at TEXT NOT NULL,duration_ms INTEGER NOT NULL,speech_ms INTEGER NOT NULL,payload TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS audio_segments_node_time ON audio_segments(node_id,started_at DESC,segment_id);
CREATE INDEX IF NOT EXISTS audio_segments_time ON audio_segments(started_at DESC,segment_id);
CREATE INDEX IF NOT EXISTS audio_segments_retention ON audio_segments(julianday(started_at),segment_id);
CREATE INDEX IF NOT EXISTS audio_segments_session ON audio_segments(session_id);
CREATE TABLE IF NOT EXISTS audio_retention(id INTEGER PRIMARY KEY CHECK(id=1),started_at TEXT NOT NULL,segment_id TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS audio_jobs(job_id TEXT PRIMARY KEY,status TEXT NOT NULL DEFAULT 'pending',attempts INTEGER NOT NULL DEFAULT 0,last_dispatched_at TEXT NOT NULL DEFAULT '',created_at TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS audio_jobs_pending ON audio_jobs(status,last_dispatched_at);
CREATE TABLE IF NOT EXISTS audio_results(job_id TEXT PRIMARY KEY,agent_id TEXT NOT NULL,text TEXT NOT NULL,language TEXT NOT NULL,payload TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS audio_workers(agent_id TEXT PRIMARY KEY,payload TEXT NOT NULL,received_at TEXT NOT NULL,live INTEGER NOT NULL);
`
const AudioMaxMessageBytes = 128 * 1024

var audioIdentity = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

type AudioSegment struct {
	SchemaVersion int      `json:"schema_version"`
	SegmentID     string   `json:"segment_id"`
	SessionID     string   `json:"session_id"`
	NodeID        string   `json:"node_id"`
	Sequence      int64    `json:"sequence"`
	StartedAt     string   `json:"started_at"`
	DurationMS    int      `json:"duration_ms"`
	SpeechMS      int      `json:"speech_ms"`
	SampleRate    int      `json:"sample_rate"`
	Channels      int      `json:"channels"`
	Codec         string   `json:"codec"`
	FrameMS       int      `json:"frame_ms"`
	Packets       []string `json:"packets,omitempty"`
}
type AudioTextSegment struct {
	StartMS int    `json:"start_ms"`
	EndMS   int    `json:"end_ms"`
	Text    string `json:"text"`
}
type AudioResult struct {
	SchemaVersion int                `json:"schema_version"`
	JobID         string             `json:"job_id"`
	SegmentID     string             `json:"segment_id"`
	NodeID        string             `json:"node_id"`
	AgentID       string             `json:"agent_id"`
	Status        string             `json:"status"`
	Text          string             `json:"text"`
	Language      string             `json:"language"`
	Segments      []AudioTextSegment `json:"segments"`
	Model         string             `json:"model"`
	Device        string             `json:"device"`
	ProcessingMS  int64              `json:"processing_ms"`
	CompletedAt   string             `json:"completed_at"`
	ErrorCode     string             `json:"error_code"`
	ErrorMessage  string             `json:"error_message"`
}
type AudioWorkerState struct {
	SchemaVersion int    `json:"schema_version"`
	AgentID       string `json:"agent_id"`
	HostLabel     string `json:"host_label"`
	Model         string `json:"model"`
	Device        string `json:"device"`
	Status        string `json:"status"`
	UpdatedAt     string `json:"updated_at"`
}

func audioUUID(s string) bool {
	u, e := uuid.Parse(s)
	return e == nil && u.String() == strings.ToLower(s)
}
func audioTime(s string) bool {
	t, e := time.Parse(time.RFC3339Nano, s)
	return e == nil && !t.IsZero() && t.Location() == time.UTC
}
func decodeAudio(data []byte, v any) error {
	if len(data) == 0 || len(data) > AudioMaxMessageBytes || !utf8.Valid(data) {
		return errors.New("invalid audio message size or encoding")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return e
	}
	if d.Decode(&struct{}{}) != io.EOF {
		return errors.New("trailing JSON")
	}
	return nil
}
func (s AudioSegment) validate(node string) error {
	if s.SchemaVersion != 1 || s.NodeID != node || !audioIdentity.MatchString(node) || !audioUUID(s.SegmentID) || !audioUUID(s.SessionID) || s.Sequence < 0 || !audioTime(s.StartedAt) || s.SampleRate != 16000 || s.Channels != 1 || s.Codec != "opus" || s.FrameMS != 20 || len(s.Packets) < 1 || len(s.Packets) > 500 || s.DurationMS != len(s.Packets)*20 || s.SpeechMS < 0 || s.SpeechMS > s.DurationMS {
		return errors.New("invalid audio segment")
	}
	for _, p := range s.Packets {
		b, e := base64.StdEncoding.Strict().DecodeString(p)
		if e != nil || len(b) == 0 || len(b) > 1275 {
			return errors.New("invalid opus packet")
		}
	}
	return nil
}
func (s *RouteStore) storeAudioSegment(ctx context.Context, segment AudioSegment, now time.Time) error {
	raw, _ := json.Marshal(segment)
	jobRaw, _ := json.Marshal(map[string]any{"schema_version": 1, "job_id": segment.SegmentID, "segment": segment})
	if len(jobRaw) > AudioMaxMessageBytes {
		return errors.New("audio job exceeds message limit")
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var existing string
	e = tx.QueryRowContext(ctx, `SELECT payload FROM audio_segments WHERE segment_id=?`, segment.SegmentID).Scan(&existing)
	if e == nil {
		if existing != string(raw) {
			return errors.New("audio segment ID conflict")
		}
		return tx.Commit()
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return e
	}
	// A constant-size persisted cutoff prevents evicted uploads from coming back,
	// even after restart or increasing the limit. ACK these retries normally.
	var expired bool
	if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM audio_retention WHERE (julianday(?),?) <= (julianday(started_at),segment_id))`, segment.StartedAt, segment.SegmentID).Scan(&expired); e != nil {
		return e
	}
	if expired {
		return tx.Commit()
	}
	var node string
	e = tx.QueryRowContext(ctx, `SELECT node_id FROM audio_sessions WHERE session_id=?`, segment.SessionID).Scan(&node)
	if e == nil && node != segment.NodeID {
		return errors.New("audio session identity conflict")
	}
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return e
	}
	if _, e = tx.ExecContext(ctx, `INSERT OR IGNORE INTO audio_sessions VALUES(?,?,?)`, segment.SessionID, segment.NodeID, now.UTC().Format(time.RFC3339Nano)); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO audio_segments VALUES(?,?,?,?,?,?,?,?)`, segment.SegmentID, segment.SessionID, segment.NodeID, segment.Sequence, segment.StartedAt, segment.DurationMS, segment.SpeechMS, string(raw)); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO audio_jobs(job_id,created_at) VALUES(?,?)`, segment.SegmentID, now.UTC().Format(time.RFC3339Nano)); e != nil {
		return e
	}
	if e = s.pruneAudio(ctx, tx); e != nil {
		return e
	}
	return tx.Commit()
}

// pruneAudio runs in the same transaction as insertion, including pending jobs.
// SQLite reuses freed pages; this does not VACUUM the shared console database.
func (s *RouteStore) pruneAudio(ctx context.Context, tx *sql.Tx) error {
	var startedAt, segmentID string
	err := tx.QueryRowContext(ctx, `SELECT started_at,segment_id FROM audio_segments ORDER BY julianday(started_at) DESC,segment_id DESC LIMIT 1 OFFSET ?`, s.audioMaxSegments).Scan(&startedAt, &segmentID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO audio_retention VALUES(1,?,?) ON CONFLICT(id) DO UPDATE SET started_at=excluded.started_at,segment_id=excluded.segment_id`, startedAt, segmentID); err != nil {
		return err
	}
	const expiredIDs = `SELECT segment_id FROM audio_segments WHERE (julianday(started_at),segment_id) <= (julianday(?),?)`
	for _, table := range []string{"audio_results", "audio_jobs"} {
		if _, err = tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE job_id IN (`+expiredIDs+`)`, startedAt, segmentID); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM audio_segments WHERE segment_id IN (`+expiredIDs+`)`, startedAt, segmentID); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `DELETE FROM audio_sessions WHERE NOT EXISTS(SELECT 1 FROM audio_segments WHERE audio_segments.session_id=audio_sessions.session_id)`)
	return err
}

func (s *RouteStore) storeAudioResult(ctx context.Context, r AudioResult) error {
	if r.SchemaVersion != 1 || !audioIdentity.MatchString(r.AgentID) || !audioUUID(r.JobID) || r.JobID != r.SegmentID || !audioTime(r.CompletedAt) || (r.Status != "completed" && r.Status != "failed") || (r.Device != "cpu" && r.Device != "cuda") || r.ProcessingMS < 0 || r.ProcessingMS > 86400000 || len(r.Text) > 64000 || len(r.Language) > 64 || len(r.Model) > 256 || len(r.ErrorCode) > 128 || len(r.ErrorMessage) > 2048 || len(r.Segments) > 1000 {
		return errors.New("invalid audio result")
	}
	if r.Status == "completed" && (r.ErrorCode != "" || r.ErrorMessage != "") {
		return errors.New("completed result contains error")
	}
	if r.Status == "failed" && (r.Text != "" || len(r.Segments) > 0) {
		return errors.New("failed result contains transcript")
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var node string
	var duration int
	if e = tx.QueryRowContext(ctx, `SELECT node_id,duration_ms FROM audio_segments WHERE segment_id=?`, r.JobID).Scan(&node, &duration); e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			var pruned bool
			if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM audio_retention)`).Scan(&pruned); e != nil {
				return e
			}
			if pruned {
				// Results do not carry the recording time. Once retention has run,
				// ACK missing jobs without storing them so workers release outboxes.
				return tx.Commit()
			}
			return sql.ErrNoRows
		}
		return e
	}
	if node != r.NodeID {
		return errors.New("result node mismatch")
	}
	for _, v := range r.Segments {
		if v.StartMS < 0 || v.EndMS < v.StartMS || v.EndMS > duration || len(v.Text) > 64000 {
			return errors.New("invalid transcript bounds")
		}
	}
	raw, _ := json.Marshal(r)
	if _, e = tx.ExecContext(ctx, `INSERT OR IGNORE INTO audio_results VALUES(?,?,?,?,?)`, r.JobID, r.AgentID, r.Text, r.Language, string(raw)); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE audio_jobs SET status=(SELECT json_extract(payload,'$.status') FROM audio_results WHERE job_id=?) WHERE job_id=?`, r.JobID, r.JobID); e != nil {
		return e
	}
	return tx.Commit()
}

type AudioRunner struct {
	store     *RouteStore
	transport Transport
	logger    *zap.Logger
	now       func() time.Time
	wake      chan struct{}
}

func NewAudioRunner(store *RouteStore, transport Transport, logger *zap.Logger, now func() time.Time) (*AudioRunner, error) {
	if store == nil || transport == nil {
		return nil, errors.New("audio store and transport required")
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	if now == nil {
		now = time.Now
	}
	return &AudioRunner{store, transport, logger, now, make(chan struct{}, 1)}, nil
}
func (r *AudioRunner) publish(ctx context.Context, topic string, v any) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	if len(b) > AudioMaxMessageBytes {
		return errors.New("audio publication exceeds limit")
	}
	publishCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return r.transport.Publish(publishCtx, mqtt.Message{Topic: topic, Payload: b, ContentType: "application/json"})
}
func (r *AudioRunner) handle(ctx context.Context, m mqtt.Message) error {
	p := strings.Split(m.Topic, "/")
	if len(p) != 6 || p[0] != "orbit" || p[1] != "v1" || p[4] != "audio" || !audioIdentity.MatchString(p[3]) {
		return errors.New("invalid audio topic")
	}
	switch {
	case p[2] == "nodes" && p[5] == "segments":
		if m.Retain {
			return errors.New("retained audio segment")
		}
		var s AudioSegment
		if e := decodeAudio(m.Payload, &s); e != nil {
			return e
		}
		if e := s.validate(p[3]); e != nil {
			return e
		}
		if e := r.store.storeAudioSegment(ctx, s, r.now()); e != nil {
			return e
		}
		select {
		case r.wake <- struct{}{}:
		default:
		}
		return r.publish(ctx, fmt.Sprintf("orbit/v1/nodes/%s/audio/acks", p[3]), map[string]any{"schema_version": 1, "segment_id": s.SegmentID, "status": "stored"})
	case p[2] == "agents" && p[5] == "results":
		if m.Retain {
			return errors.New("retained audio result")
		}
		var result AudioResult
		if e := decodeAudio(m.Payload, &result); e != nil {
			return e
		}
		if result.AgentID != p[3] {
			return errors.New("result agent mismatch")
		}
		if e := r.store.storeAudioResult(ctx, result); e != nil {
			return e
		}
		return r.publish(ctx, fmt.Sprintf("orbit/v1/agents/%s/audio/acks", p[3]), map[string]any{"schema_version": 1, "job_id": result.JobID, "status": "stored"})
	case p[2] == "agents" && p[5] == "state":
		var w AudioWorkerState
		if e := decodeAudio(m.Payload, &w); e != nil {
			return e
		}
		if w.SchemaVersion != 1 || w.AgentID != p[3] || !audioTime(w.UpdatedAt) || len(w.HostLabel) > 256 || len(w.Model) > 256 || (w.Device != "cpu" && w.Device != "cuda") || !strings.Contains("|starting|ready|processing|error|offline|", "|"+w.Status+"|") {
			return errors.New("invalid audio worker state")
		}
		raw, _ := json.Marshal(w)
		_, e := r.store.db.ExecContext(ctx, `INSERT INTO audio_workers VALUES(?,?,?,?) ON CONFLICT(agent_id) DO UPDATE SET payload=excluded.payload,received_at=excluded.received_at,live=excluded.live`, w.AgentID, string(raw), r.now().UTC().Format(time.RFC3339Nano), !m.Retain)
		return e
	}
	return errors.New("invalid audio topic")
}
func (r *AudioRunner) dispatch(ctx context.Context) error {
	rows, e := r.store.db.QueryContext(ctx, `SELECT s.payload FROM audio_jobs j JOIN audio_segments s ON s.segment_id=j.job_id WHERE j.status='pending' AND (j.last_dispatched_at='' OR j.last_dispatched_at<=?) ORDER BY j.last_dispatched_at LIMIT 32`, r.now().Add(-30*time.Second).UTC().Format(time.RFC3339Nano))
	if e != nil {
		return e
	}
	var segments []AudioSegment
	for rows.Next() {
		var raw string
		if e = rows.Scan(&raw); e != nil {
			rows.Close()
			return e
		}
		var s AudioSegment
		if e = json.Unmarshal([]byte(raw), &s); e != nil {
			rows.Close()
			return e
		}
		segments = append(segments, s)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, s := range segments {
		pubctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		e = r.publish(pubctx, "orbit/v1/audio/jobs", map[string]any{"schema_version": 1, "job_id": s.SegmentID, "segment": s})
		cancel()
		if e != nil {
			return e
		}
		if _, e = r.store.db.ExecContext(ctx, `UPDATE audio_jobs SET attempts=attempts+1,last_dispatched_at=? WHERE job_id=? AND status='pending'`, r.now().UTC().Format(time.RFC3339Nano), s.SegmentID); e != nil {
			return e
		}
	}
	return nil
}
func (r *AudioRunner) Run(ctx context.Context) error {
	// Reset dispatch timestamps so a restart promptly recovers every pending job.
	if _, e := r.store.db.ExecContext(ctx, `UPDATE audio_jobs SET last_dispatched_at='' WHERE status='pending'`); e != nil {
		return e
	}
	for _, topic := range []string{"orbit/v1/nodes/+/audio/segments", "orbit/v1/agents/+/audio/results", "orbit/v1/agents/+/audio/state"} {
		if e := r.transport.Subscribe(ctx, topic, r.handle); e != nil {
			return e
		}
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if e := r.dispatch(ctx); e != nil && ctx.Err() == nil {
			r.logger.Warn("audio dispatch failed", zap.Error(e))
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		case <-r.wake:
		}
	}
}
