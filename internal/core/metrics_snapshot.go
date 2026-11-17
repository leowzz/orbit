package core

import (
	"context"
	"database/sql"
	"strings"
	"time"

	orbitv1 "orbit/gen/go/orbit/v1"
	"orbit/internal/inbox"
)

func (e *Engine) writeMetrics(b *strings.Builder, now time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()
	writeMetricHeader(b, "orbit_core_mqtt_participants", "Participants with accepted MQTT state since Core start (not necessarily connected).", "gauge")
	writeMetric(b, "orbit_core_mqtt_participants", map[string]string{"kind": "agent"}, float64(len(e.agents)))
	writeMetric(b, "orbit_core_mqtt_participants", map[string]string{"kind": "node"}, float64(len(e.nodes)))
	writeMetricHeader(b, "orbit_core_projection_routes", "Configured projection routes.", "gauge")
	writeMetric(b, "orbit_core_projection_routes", nil, float64(len(e.config.Routes)))

	writeMetricHeader(b, "orbit_core_source_state", "Configured source state: 0 missing, 1 stale, 2 fresh.", "gauge")
	seen := map[RouteInput]bool{}
	for _, route := range e.config.Routes {
		for _, input := range route.Inputs {
			if seen[input] {
				continue
			}
			seen[input] = true
			id := input.AgentID
			var source string
			var expiry time.Time
			switch input.ObservationType {
			case orbitv1.ObservationType_OBSERVATION_TYPE_USAGE:
				source, expiry = "usage", e.usage[id].expiresAt
			case orbitv1.ObservationType_OBSERVATION_TYPE_CODEX:
				source, expiry = "codex", e.codex[id].expiresAt
			case orbitv1.ObservationType_OBSERVATION_TYPE_SYSTEM:
				source, expiry = "system", e.system[id].expiresAt
			default:
				continue
			}
			state := 0.0
			if expiry.After(now) {
				state = 2
			} else if !expiry.IsZero() {
				state = 1
			}
			writeMetric(b, "orbit_core_source_state", map[string]string{"agent_id": id, "source": source}, state)
		}
	}
}

func (s *RouteStore) writeMetrics(ctx context.Context, b *strings.Builder, app *inbox.Store) error {
	writeGauge(b, "orbit_core_reviews_enabled", "Whether Core has review analysis enabled.", s.analysis.Enabled)
	if s.analysis.Enabled {
		if err := s.writeReviewMetrics(ctx, b); err != nil {
			return err
		}
	}

	var coreBytes int64
	if err := s.db.QueryRowContext(ctx, `SELECT page_count * page_size FROM pragma_page_count(), pragma_page_size()`).Scan(&coreBytes); err != nil {
		return err
	}
	writeMetricHeader(b, "orbit_core_sqlite_allocated_bytes", "SQLite logical database size including free pages, excluding the WAL file.", "gauge")
	writeMetricHeader(b, "orbit_core_sqlite_waits_total", "Times an operation waited for a database connection.", "counter")
	writeMetricHeader(b, "orbit_core_sqlite_wait_seconds_total", "Time spent waiting for database connections.", "counter")
	writeDatabaseMetrics(b, "core", coreBytes, s.db.Stats())
	if app != nil {
		appBytes, stats, err := app.StorageStats(ctx)
		if err != nil {
			return err
		}
		writeDatabaseMetrics(b, "inbox", appBytes, stats)
	}
	return nil
}

func writeDatabaseMetrics(b *strings.Builder, name string, allocated int64, stats sql.DBStats) {
	labels := map[string]string{"database": name}
	writeMetric(b, "orbit_core_sqlite_allocated_bytes", labels, float64(allocated))
	writeMetric(b, "orbit_core_sqlite_waits_total", labels, float64(stats.WaitCount))
	writeMetric(b, "orbit_core_sqlite_wait_seconds_total", labels, stats.WaitDuration.Seconds())
}

func (s *RouteStore) writeReviewMetrics(ctx context.Context, b *strings.Builder) error {
	rows, err := s.db.QueryContext(ctx, `SELECT kind,status,count(*) FROM review_windows GROUP BY kind,status`)
	if err != nil {
		return err
	}
	defer rows.Close()
	counts := map[[2]string]int64{}
	for rows.Next() {
		var kind, status string
		var count int64
		if err := rows.Scan(&kind, &status, &count); err != nil {
			return err
		}
		counts[[2]string{kind, status}] = count
	}
	if err := rows.Err(); err != nil {
		return err
	}
	writeMetricHeader(b, "orbit_core_review_windows", "Retained review windows by period and status.", "gauge")
	for _, kind := range []string{"hour", "day"} {
		for _, status := range []string{"queued", "running", "completed", "failed"} {
			writeMetric(b, "orbit_core_review_windows", map[string]string{"kind": kind, "status": status}, float64(counts[[2]string{kind, status}]))
		}
	}
	return nil
}
