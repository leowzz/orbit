package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

func addAudioCounter(ctx context.Context, tx *sql.Tx, name, agent, status string, value float64) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO audio_counters VALUES(?,?,?,?) ON CONFLICT(name,agent_id,status) DO UPDATE SET value=value+excluded.value`, name, agent, status, value)
	return err
}

// MetricsHandler is served only on the opt-in metrics listener, never the console.
func (s *RouteStore) MetricsHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /metrics", s.audioMetrics)
	return mux
}

func (s *RouteStore) audioMetrics(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	body, err := s.collectAudioMetrics(ctx, time.Now())
	if err != nil {
		http.Error(w, "metrics unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, _ = w.Write([]byte(body))
}

// One read transaction provides a consistent snapshot. Counters are incremented
// with the corresponding write transaction and survive retention and restarts.
func (s *RouteStore) collectAudioMetrics(ctx context.Context, now time.Time) (string, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var b strings.Builder
	gauge := func(name, help string, labels map[string]string, value float64) {
		writeMetricHeader(&b, name, help, "gauge")
		writeMetric(&b, name, labels, value)
	}
	writeMetricHeader(&b, "orbit_audio_jobs", "Retained ASR jobs by status; pending includes dispatched jobs awaiting results", "gauge")
	for _, status := range []string{"pending", "completed", "failed"} {
		var count int64
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM audio_jobs WHERE status=?`, status).Scan(&count); err != nil {
			return "", err
		}
		writeMetric(&b, "orbit_audio_jobs", map[string]string{"status": status}, float64(count))
	}
	var count, duration, speech, attempts int64
	if err := tx.QueryRowContext(ctx, `SELECT count(*),COALESCE(sum(duration_ms),0),COALESCE(sum(speech_ms),0) FROM audio_segments`).Scan(&count, &duration, &speech); err != nil {
		return "", err
	}
	gauge("orbit_audio_segments_retained", "Retained audio segments", nil, float64(count))
	gauge("orbit_audio_retention_limit", "Maximum retained audio segments", nil, float64(s.audioMaxSegments))
	gauge("orbit_audio_duration_seconds_retained", "Retained audio duration", nil, float64(duration)/1000)
	gauge("orbit_audio_speech_seconds_retained", "Retained speech duration", nil, float64(speech)/1000)
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(sum(attempts),0) FROM audio_jobs`).Scan(&attempts); err != nil {
		return "", err
	}
	gauge("orbit_audio_job_attempts", "Dispatch attempts for retained jobs (not a monotonic counter)", nil, float64(attempts))
	var oldest sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT created_at FROM audio_jobs WHERE status='pending' ORDER BY julianday(created_at) LIMIT 1`).Scan(&oldest); err != nil && err != sql.ErrNoRows {
		return "", err
	}
	age := 0.0
	if oldest.Valid {
		t, err := time.Parse(time.RFC3339Nano, oldest.String)
		if err != nil {
			return "", err
		}
		age = max(0, now.Sub(t).Seconds())
	}
	gauge("orbit_audio_pending_oldest_age_seconds", "Age since Core accepted the oldest pending job", nil, age)
	for _, metric := range []struct{ name, help string }{
		{"received_total", "Unique audio segments accepted since metrics instrumentation"},
		{"results_total", "Unique results accepted since metrics instrumentation"},
		{"processed_audio_seconds_total", "Audio seconds in unique accepted results since metrics instrumentation"},
		{"processing_seconds_total", "Worker processing seconds in unique accepted results since metrics instrumentation"},
	} {
		name := "orbit_audio_" + metric.name
		writeMetricHeader(&b, name, metric.help, "counter")
		rows, err := tx.QueryContext(ctx, `SELECT agent_id,status,value FROM audio_counters WHERE name=? ORDER BY agent_id,status`, metric.name)
		if err != nil {
			return "", err
		}
		for rows.Next() {
			var agent, status string
			var value float64
			if err := rows.Scan(&agent, &status, &value); err != nil {
				rows.Close()
				return "", err
			}
			labels := map[string]string{}
			if agent != "" {
				labels["agent_id"] = agent
				labels["status"] = status
			}
			writeMetric(&b, name, labels, value)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return "", err
		}
	}
	for _, metric := range []struct{ name, help string }{
		{"info", "Last known worker identity and state"},
		{"up", "Worker has a live non-offline heartbeat within 60 seconds"},
		{"last_received_timestamp_seconds", "Core receipt time of the last worker state"},
		{"queue_reported", "Live worker reports queue telemetry; zero means unsupported or stale"},
		{"jobs", "Worker local jobs by stage; emitted only for live queue telemetry"},
		{"queue_capacity", "Worker queue capacity including outbox; emitted only for live telemetry"},
	} {
		writeMetricHeader(&b, "orbit_audio_worker_"+metric.name, metric.help, "gauge")
	}
	rows, err := tx.QueryContext(ctx, `SELECT payload,received_at,live FROM audio_workers ORDER BY agent_id`)
	if err != nil {
		return "", err
	}
	for rows.Next() {
		var raw, received string
		var live bool
		if err := rows.Scan(&raw, &received, &live); err != nil {
			rows.Close()
			return "", err
		}
		var worker AudioWorkerState
		if err := json.Unmarshal([]byte(raw), &worker); err != nil {
			rows.Close()
			return "", err
		}
		t, err := time.Parse(time.RFC3339Nano, received)
		if err != nil {
			rows.Close()
			return "", err
		}
		connection := audioWorkerConnection(worker.Status, live, t, now)
		online := connection == "online"
		labels := map[string]string{"agent_id": worker.AgentID}
		writeMetric(&b, "orbit_audio_worker_info", map[string]string{"agent_id": worker.AgentID, "host_label": worker.HostLabel, "model": worker.Model, "device": worker.Device, "status": worker.Status, "connection": connection}, 1)
		up := 0.0
		if online {
			up = 1
		}
		writeMetric(&b, "orbit_audio_worker_up", labels, up)
		writeMetric(&b, "orbit_audio_worker_last_received_timestamp_seconds", labels, float64(t.UnixMilli())/1000)
		reported := 0.0
		if online && worker.Queue != nil {
			reported = 1
			for stage, count := range map[string]int64{"queued": worker.Queue.Queued, "processing": worker.Queue.Processing, "outbox": worker.Queue.Outbox} {
				writeMetric(&b, "orbit_audio_worker_jobs", map[string]string{"agent_id": worker.AgentID, "stage": stage}, float64(count))
			}
			writeMetric(&b, "orbit_audio_worker_queue_capacity", labels, float64(worker.Queue.Capacity))
		}
		writeMetric(&b, "orbit_audio_worker_queue_reported", labels, reported)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return b.String(), nil
}

func writeMetricHeader(b *strings.Builder, name, help, typ string) {
	fmt.Fprintf(b, "# HELP %s %s\n# TYPE %s %s\n", name, help, name, typ)
}

func writeMetric(b *strings.Builder, name string, labels map[string]string, value float64) {
	b.WriteString(name)
	if len(labels) > 0 {
		keys := make([]string, 0, len(labels))
		for key := range labels {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		b.WriteByte('{')
		escape := strings.NewReplacer("\\", "\\\\", "\n", "\\n", "\"", "\\\"")
		for i, key := range keys {
			if i > 0 {
				b.WriteByte(',')
			}
			fmt.Fprintf(b, `%s="%s"`, key, escape.Replace(labels[key]))
		}
		b.WriteByte('}')
	}
	fmt.Fprintf(b, " %s\n", strconv.FormatFloat(value, 'f', -1, 64))
}
