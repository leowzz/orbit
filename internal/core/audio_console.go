package core

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type audioListItem struct {
	SegmentID  string `json:"segment_id"`
	NodeID     string `json:"node_id"`
	SessionID  string `json:"session_id"`
	Sequence   int64  `json:"sequence"`
	StartedAt  string `json:"started_at"`
	DurationMS int    `json:"duration_ms"`
	SpeechMS   int    `json:"speech_ms"`
	Status     string `json:"status"`
	Text       string `json:"text"`
	Language   string `json:"language"`
	AgentID    string `json:"agent_id"`
}

type audioMetrics struct {
	Total     int `json:"total"`
	Pending   int `json:"pending"`
	Completed int `json:"completed"`
	Failed    int `json:"failed"`
}

func (s *RouteStore) audioRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/audio/segments", s.listAudio)
	mux.HandleFunc("GET /api/audio/segments/{id}", s.getAudio)
	mux.HandleFunc("GET /api/audio/workers", s.listAudioWorkers)
}
func audioPage(r *http.Request) (int, int, error) {
	limit, offset := 50, 0
	var e error
	if v := r.URL.Query().Get("limit"); v != "" {
		limit, e = strconv.Atoi(v)
		if e != nil || limit < 1 || limit > 200 {
			return 0, 0, errors.New("invalid limit")
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		offset, e = strconv.Atoi(v)
		if e != nil || offset < 0 {
			return 0, 0, errors.New("invalid offset")
		}
	}
	return limit, offset, nil
}

func audioOrder(r *http.Request) (string, error) {
	field, direction := r.URL.Query().Get("sort_by"), r.URL.Query().Get("order")
	if field == "" {
		field = "started_at"
	}
	if direction == "" {
		direction = "desc"
	}
	// Normalize RFC3339 timestamps with and without fractional seconds.
	column := map[string]string{
		"started_at":  "julianday(s.started_at)",
		"duration_ms": "s.duration_ms",
		"speech_ms":   "s.speech_ms",
	}[field]
	if column == "" || (direction != "asc" && direction != "desc") {
		return "", errors.New("invalid audio sort")
	}
	order := column + " " + strings.ToUpper(direction)
	if field != "started_at" {
		order += ",julianday(s.started_at) DESC"
	}
	// Equal sort values must stay in a deterministic order across pages.
	return order + ",s.segment_id ASC", nil
}

func (s *RouteStore) listAudio(w http.ResponseWriter, r *http.Request) {
	limit, offset, e := audioPage(r)
	if e != nil {
		http.Error(w, e.Error(), 400)
		return
	}
	order, e := audioOrder(r)
	if e != nil {
		http.Error(w, e.Error(), 400)
		return
	}
	q := r.URL.Query()
	status, node, search := q.Get("status"), q.Get("node_id"), q.Get("q")
	hideEmpty := q.Get("hide_empty")
	if status != "" && status != "pending" && status != "completed" && status != "failed" || node != "" && !audioIdentity.MatchString(node) || len(search) > 1000 || hideEmpty != "" && hideEmpty != "true" && hideEmpty != "false" {
		http.Error(w, "invalid audio filter", 400)
		return
	}
	where := ` WHERE (?='' OR s.node_id=?) AND (?='' OR j.status=?) AND (?='' OR instr(lower(COALESCE(a.text,'')),lower(?))>0)`
	args := []any{node, node, status, status, search, search}
	if hideEmpty == "true" {
		// Keep pending/failed jobs visible. Filter before both counting and paging.
		where += ` AND (j.status<>'completed' OR trim(COALESCE(a.text,''),?)<>'')`
		args = append(args, "\t\n\v\f\r \u0085\u00a0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a\u2028\u2029\u202f\u205f\u3000")
	}
	joins := ` FROM audio_segments s JOIN audio_jobs j ON j.job_id=s.segment_id LEFT JOIN audio_results a ON a.job_id=j.job_id`
	var total int
	if e = s.db.QueryRowContext(r.Context(), `SELECT count(*)`+joins+where, args...).Scan(&total); e != nil {
		http.Error(w, "cannot load audio", 500)
		return
	}
	rows, e := s.db.QueryContext(r.Context(), `SELECT s.segment_id,s.node_id,s.session_id,s.sequence,s.started_at,s.duration_ms,s.speech_ms,j.status,COALESCE(a.text,''),COALESCE(a.language,''),COALESCE(a.agent_id,'')`+joins+where+` ORDER BY `+order+` LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if e != nil {
		http.Error(w, "cannot load audio", 500)
		return
	}
	defer rows.Close()
	items := []audioListItem{}
	for rows.Next() {
		var item audioListItem
		if e = rows.Scan(&item.SegmentID, &item.NodeID, &item.SessionID, &item.Sequence, &item.StartedAt, &item.DurationMS, &item.SpeechMS, &item.Status, &item.Text, &item.Language, &item.AgentID); e != nil {
			http.Error(w, "cannot load audio", 500)
			return
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		http.Error(w, "cannot load audio", 500)
		return
	}
	if e = rows.Close(); e != nil {
		http.Error(w, "cannot load audio", 500)
		return
	}
	metrics := audioMetrics{}
	metricRows, e := s.db.QueryContext(r.Context(), `SELECT status,count(*) FROM audio_jobs GROUP BY status`)
	if e != nil {
		http.Error(w, "cannot load audio", 500)
		return
	}
	defer metricRows.Close()
	for metricRows.Next() {
		var status string
		var count int
		if e = metricRows.Scan(&status, &count); e != nil {
			http.Error(w, "cannot load audio", 500)
			return
		}
		metrics.Total += count
		switch status {
		case "pending":
			metrics.Pending = count
		case "completed":
			metrics.Completed = count
		case "failed":
			metrics.Failed = count
		}
	}
	if e = metricRows.Err(); e != nil {
		http.Error(w, "cannot load audio", 500)
		return
	}
	writeConsoleJSON(w, map[string]any{"items": items, "total": total, "limit": limit, "offset": offset, "metrics": metrics})
}
func (s *RouteStore) getAudio(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !audioUUID(id) {
		http.Error(w, "invalid segment ID", 400)
		return
	}
	var segmentRaw, status, last, created string
	var attempts int
	var resultRaw sql.NullString
	e := s.db.QueryRowContext(r.Context(), `SELECT s.payload,j.status,j.attempts,j.last_dispatched_at,j.created_at,a.payload FROM audio_segments s JOIN audio_jobs j ON j.job_id=s.segment_id LEFT JOIN audio_results a ON a.job_id=j.job_id WHERE s.segment_id=?`, id).Scan(&segmentRaw, &status, &attempts, &last, &created, &resultRaw)
	if errors.Is(e, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if e != nil {
		http.Error(w, "cannot load audio", 500)
		return
	}
	var segment AudioSegment
	if json.Unmarshal([]byte(segmentRaw), &segment) != nil {
		http.Error(w, "cannot load audio", 500)
		return
	}
	segment.Packets = nil
	var result any
	if resultRaw.Valid {
		result = json.RawMessage(resultRaw.String)
	}
	writeConsoleJSON(w, map[string]any{"segment": segment, "job": map[string]any{"job_id": id, "status": status, "attempts": attempts, "last_dispatched_at": last, "created_at": created}, "result": result})
}
func (s *RouteStore) listAudioWorkers(w http.ResponseWriter, r *http.Request) {
	rows, e := s.db.QueryContext(r.Context(), `SELECT payload,received_at,live FROM audio_workers ORDER BY agent_id`)
	if e != nil {
		http.Error(w, "cannot load audio workers", 500)
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var raw, received string
		var live bool
		if rows.Scan(&raw, &received, &live) != nil {
			http.Error(w, "cannot load audio workers", 500)
			return
		}
		item := map[string]any{}
		if json.Unmarshal([]byte(raw), &item) != nil {
			http.Error(w, "cannot load audio workers", 500)
			return
		}
		t, _ := time.Parse(time.RFC3339Nano, received)
		age := time.Since(t)
		item["received_at"] = received
		item["online"] = live && age >= 0 && age < 60*time.Second && !strings.EqualFold(item["status"].(string), "offline")
		items = append(items, item)
	}
	if rows.Err() != nil {
		http.Error(w, "cannot load audio workers", 500)
		return
	}
	writeConsoleJSON(w, map[string]any{"items": items})
}
