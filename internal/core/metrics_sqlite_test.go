package core

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSQLiteTableMetricsCountWALRowsAndExcludeInternalTables(t *testing.T) {
	path := filepath.Join(t.TempDir(), "core #metrics.sqlite")
	store, err := OpenRouteStore(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.db.Exec(`
		CREATE TABLE "quoted""table" (id INTEGER PRIMARY KEY AUTOINCREMENT);
		INSERT INTO "quoted""table" DEFAULT VALUES;
		INSERT INTO "quoted""table" DEFAULT VALUES;
		CREATE VIEW sample_view AS SELECT * FROM "quoted""table";
	`); err != nil {
		t.Fatal(err)
	}
	reader, err := openSQLiteMetricsReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	rows, err := collectTableRows(context.Background(), reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.Exec(`DELETE FROM "quoted""table"`); err == nil {
		t.Fatal("metrics connection allowed writes")
	}
	recorder := NewCoreMetrics(store)
	recorder.sqliteRows.core.Store(&tableRowSnapshot{at: time.Unix(1700000000, 0), tables: rows})
	res := httptest.NewRecorder()
	recorder.Handler(nil, time.Now, nil, nil, false).ServeHTTP(res, httptest.NewRequest("GET", "/metrics", nil))
	if res.Code != 200 {
		t.Fatalf("scrape: %d %s", res.Code, res.Body.String())
	}
	body := res.Body.String()
	for _, want := range []string{
		`orbit_core_sqlite_table_rows{database="core",table="quoted\"table"} 2`,
		`orbit_core_sqlite_table_rows{database="core",table="audio_jobs"} 0`,
		`orbit_core_sqlite_table_rows_last_success_timestamp_seconds{database="core"} 1700000000`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
	for _, unwanted := range []string{`table="sqlite_sequence"`, `table="sample_view"`, `database="inbox"`} {
		if strings.Contains(body, unwanted) {
			t.Errorf("unexpected %q", unwanted)
		}
	}
}
