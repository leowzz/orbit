package core

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	orbitv1 "orbit/gen/go/orbit/v1"
	"orbit/internal/inbox"
)

func TestCoreMetricsPreserveSSEAndBoundHTTPLabels(t *testing.T) {
	store, err := OpenRouteStore(filepath.Join(t.TempDir(), "core.sqlite"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	metrics := NewCoreMetrics(store)
	scrape := func() string {
		res := httptest.NewRecorder()
		metrics.Handler(nil, time.Now, nil, nil, false).ServeHTTP(res, httptest.NewRequest("GET", "/metrics", nil))
		if res.Code != 200 {
			t.Fatalf("scrape: %d %s", res.Code, res.Body.String())
		}
		return res.Body.String()
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/attachments/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	mux.HandleFunc("GET /api/v1/events", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("event: sync\n\n"))
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Fatal(err)
		}
		body := scrape()
		if !strings.Contains(body, `orbit_core_http_active_requests{kind="events",surface="app"} 1`) ||
			!strings.Contains(body, `orbit_core_http_requests_total{code="200",route="GET /api/v1/events",surface="app"} 1`) {
			t.Fatal("open event stream was not observable until disconnect")
		}
	})
	handler := metrics.InstrumentHTTP("app", mux)
	stream := httptest.NewRecorder()
	handler.ServeHTTP(stream, httptest.NewRequest("GET", "/api/v1/events", nil))
	if !stream.Flushed {
		t.Fatal("event stream did not flush")
	}
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/v1/attachments/private-attachment-id", nil))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/unmatched-private-path", nil))
	body := scrape()
	for _, want := range []string{
		`orbit_core_http_requests_total{code="404",route="GET /api/v1/attachments/{id}",surface="app"} 1`,
		`orbit_core_http_requests_total{code="404",route="unmatched",surface="app"} 1`,
		`orbit_core_http_active_requests{kind="events",surface="app"} 0`,
		`orbit_core_http_request_duration_seconds_count{route="GET /api/v1/attachments/{id}",surface="app"} 1`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing metric %q", want)
		}
	}
	for _, unwanted := range []string{"private-attachment-id", "unmatched-private-path", `orbit_core_http_request_duration_seconds_count{route="GET /api/v1/events"`} {
		if strings.Contains(body, unwanted) {
			t.Errorf("unexpected metric %q", unwanted)
		}
	}
}

func TestCoreMetricsConfiguredSourcesAndInbox(t *testing.T) {
	store, err := OpenRouteStore(filepath.Join(t.TempDir(), "core.sqlite"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	app, err := inbox.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	now := time.Now()
	active := RouteInput{AgentID: "observed", ObservationType: orbitv1.ObservationType_OBSERVATION_TYPE_USAGE}
	engine := &Engine{
		config: Config{Routes: []Route{
			{Inputs: []RouteInput{active, {AgentID: "missing", ObservationType: orbitv1.ObservationType_OBSERVATION_TYPE_CODEX}}},
			{Inputs: []RouteInput{active, {AgentID: "expired", ObservationType: orbitv1.ObservationType_OBSERVATION_TYPE_SYSTEM}}},
		}},
		usage:  map[string]canonicalUsage{"observed": {expiresAt: now.Add(time.Minute)}, "unconfigured": {expiresAt: now.Add(time.Minute)}},
		system: map[string]canonicalSystem{"expired": {expiresAt: now.Add(-time.Minute)}},
	}
	metrics := NewCoreMetrics(store)
	res := httptest.NewRecorder()
	metrics.Handler(engine, func() time.Time { return now }, app, nil, true).ServeHTTP(res, httptest.NewRequest("GET", "/metrics", nil))
	if res.Code != 200 {
		t.Fatalf("scrape: %d %s", res.Code, res.Body.String())
	}
	body := res.Body.String()
	for _, want := range []string{
		`orbit_core_source_state{agent_id="observed",source="usage"} 2`,
		`orbit_core_source_state{agent_id="expired",source="system"} 1`,
		`orbit_core_source_state{agent_id="missing",source="codex"} 0`,
		`orbit_core_sqlite_allocated_bytes{database="inbox"} `,
		`orbit_core_mqtt_enabled 1`,
		`orbit_core_reviews_enabled 0`,
	} {
		if strings.Count(body, want) != 1 {
			t.Errorf("want exactly one sample for %q", want)
		}
	}
	if strings.Contains(body, "unconfigured") || strings.Contains(body, "orbit_core_review_windows{") {
		t.Fatal("scrape exposed an unconfigured source or a disabled review queue")
	}
}
