package core

import (
	"context"
	"database/sql"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

type sqliteTableMetrics struct {
	core, inbox atomic.Pointer[tableRowSnapshot]
}

type tableRowSnapshot struct {
	at     time.Time
	tables []tableRows
}

type tableRows struct {
	name string
	rows int64
}

// RunSQLiteTableMetrics counts rows once a minute on separate read-only
// connections, outside scrapes and the pools used for application traffic.
func (m *CoreMetrics) RunSQLiteTableMetrics(ctx context.Context, corePath, inboxPath string, logger *zap.Logger) {
	type source struct {
		name string
		db   *sql.DB
		dest *atomic.Pointer[tableRowSnapshot]
	}
	var sources []source
	for _, input := range []struct {
		name, path string
		dest       *atomic.Pointer[tableRowSnapshot]
	}{
		{"core", corePath, &m.sqliteRows.core},
		{"inbox", inboxPath, &m.sqliteRows.inbox},
	} {
		if input.path == "" {
			continue
		}
		db, err := openSQLiteMetricsReader(input.path)
		if err != nil {
			logger.Warn("open SQLite table metrics reader", zap.String("database", input.name), zap.Error(err))
			continue
		}
		defer db.Close()
		sources = append(sources, source{input.name, db, input.dest})
	}
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for ctx.Err() == nil {
		for _, src := range sources {
			readCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			rows, err := collectTableRows(readCtx, src.db)
			cancel()
			if err != nil {
				if ctx.Err() == nil {
					logger.Warn("collect SQLite table rows", zap.String("database", src.name), zap.Error(err))
				}
				continue
			}
			src.dest.Store(&tableRowSnapshot{at: time.Now(), tables: rows})
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func openSQLiteMetricsReader(path string) (*sql.DB, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	path = filepath.ToSlash(path)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path // SQLite file URIs use /C:/... on Windows.
	}
	dsn := url.URL{Scheme: "file", Path: path, RawQuery: "mode=ro"}
	db, err := sql.Open("sqlite", dsn.String())
	if err == nil {
		db.SetMaxOpenConns(1)
	}
	return db, err
}

func collectTableRows(ctx context.Context, db *sql.DB) ([]tableRows, error) {
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	tables, err := tx.QueryContext(ctx, `SELECT name FROM pragma_table_list WHERE schema='main' AND type='table' AND name NOT GLOB 'sqlite_*' ORDER BY name`)
	if err != nil {
		return nil, err
	}
	var counts []tableRows
	for tables.Next() {
		var table tableRows
		if err := tables.Scan(&table.name); err != nil {
			tables.Close()
			return nil, err
		}
		counts = append(counts, table)
	}
	if err := tables.Err(); err != nil {
		tables.Close()
		return nil, err
	}
	if err := tables.Close(); err != nil {
		return nil, err
	}
	for i := range counts {
		table := &counts[i]
		// The identifier comes from SQLite's own schema; escape quotes as well.
		ident := `"` + strings.ReplaceAll(table.name, `"`, `""`) + `"`
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM main.`+ident).Scan(&table.rows); err != nil {
			return nil, err
		}
	}
	return counts, tx.Commit()
}

func (s *sqliteTableMetrics) write(b *strings.Builder, inboxEnabled bool) {
	writeMetricHeader(b, "orbit_core_sqlite_table_rows", "Exact row count in an application table at the last successful sample.", "gauge")
	writeMetricHeader(b, "orbit_core_sqlite_table_rows_last_success_timestamp_seconds", "Last successful table row count snapshot per database, or zero before the first success.", "gauge")
	for _, db := range []struct {
		name string
		snap *tableRowSnapshot
	}{{"core", s.core.Load()}, {"inbox", s.inbox.Load()}} {
		if db.name == "inbox" && !inboxEnabled {
			continue
		}
		at := 0.0
		if db.snap != nil {
			at = float64(db.snap.at.Unix())
			for _, t := range db.snap.tables {
				writeMetric(b, "orbit_core_sqlite_table_rows", map[string]string{"database": db.name, "table": t.name}, float64(t.rows))
			}
		}
		writeMetric(b, "orbit_core_sqlite_table_rows_last_success_timestamp_seconds", map[string]string{"database": db.name}, at)
	}
}
