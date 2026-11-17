package core

import (
	"bytes"
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/common/expfmt"
	"orbit/internal/inbox"
	"orbit/internal/mqtt"
)

// CoreMetrics owns the process-local counters. ASR counters still come from
// SQLite so they preserve their existing semantics across deploys and retention.
type CoreMetrics struct {
	store          *RouteStore
	registry       *prometheus.Registry
	httpRequests   *prometheus.CounterVec
	httpDuration   *prometheus.HistogramVec
	httpActive     *prometheus.GaugeVec
	mqttConnected  prometheus.Gauge
	mqttOperations *prometheus.CounterVec
	pendingViews   prometheus.Gauge
	workerRuns     *prometheus.CounterVec
	workerSuccess  *prometheus.GaugeVec
	reviewRequests *prometheus.CounterVec
	reviewDuration prometheus.Histogram
	reviewTokens   *prometheus.CounterVec
	sqliteRows     sqliteTableMetrics
}

// Call before starting the Core workers. A nil recorder means metrics are disabled.
func NewCoreMetrics(store *RouteStore) *CoreMetrics {
	m := &CoreMetrics{
		store:    store,
		registry: prometheus.NewRegistry(),
		httpRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "orbit_core_http_requests_total", Help: "API responses started by surface, route pattern and status code.",
		}, []string{"surface", "route", "code"}),
		httpDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "orbit_core_http_request_duration_seconds", Help: "API request duration excluding event streams.",
		}, []string{"surface", "route"}),
		httpActive: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "orbit_core_http_active_requests", Help: "In-flight API requests and event streams.",
		}, []string{"surface", "kind"}),
		mqttConnected: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "orbit_core_mqtt_connected", Help: "Whether Core is connected to its MQTT broker.",
		}),
		mqttOperations: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "orbit_core_mqtt_operations_total", Help: "MQTT subscribe attempts, outbound acknowledgements or failures, and inbound handler outcomes.",
		}, []string{"operation", "kind", "result"}),
		pendingViews: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "orbit_core_pending_views", Help: "Latest device views still waiting for MQTT acknowledgement.",
		}),
		workerRuns: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "orbit_core_worker_runs_total", Help: "Background cycles or analysis attempts by outcome.",
		}, []string{"worker", "result"}),
		workerSuccess: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "orbit_core_worker_last_success_timestamp_seconds", Help: "Last successful background cycle or analysis attempt, or zero before the first success.",
		}, []string{"worker"}),
		reviewRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "orbit_core_review_model_requests_total", Help: "Review model requests by outcome.",
		}, []string{"result"}),
		reviewDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name: "orbit_core_review_model_request_duration_seconds", Help: "Time spent on one review model request.",
			Buckets: []float64{0.1, 0.25, 0.5, 1, 2, 5, 10, 20, 30, 60, 120},
		}),
		reviewTokens: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "orbit_core_review_model_tokens_total", Help: "Input and output tokens reported by successful review model responses.",
		}, []string{"direction"}),
	}
	m.registry.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		m.httpRequests, m.httpDuration, m.httpActive, m.mqttConnected, m.mqttOperations, m.pendingViews, m.workerRuns, m.workerSuccess,
		m.reviewRequests, m.reviewDuration, m.reviewTokens)
	for _, worker := range []string{"audio_dispatch", "review_cycle", "review_analysis"} {
		m.workerSuccess.WithLabelValues(worker)
		for _, result := range []string{"ok", "error"} {
			m.workerRuns.WithLabelValues(worker, result)
		}
	}
	for _, result := range []string{"ok", "error"} {
		m.reviewRequests.WithLabelValues(result)
	}
	for _, direction := range []string{"input", "output"} {
		m.reviewTokens.WithLabelValues(direction)
	}
	store.metrics = m
	return m
}

// Handler is served only on the optional private listener.
func (m *CoreMetrics) Handler(engine *Engine, now func() time.Time, app *inbox.Store, devices func(time.Time) (int, int, int), mqttEnabled bool) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /metrics", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		var body bytes.Buffer
		if err := m.collect(ctx, &body, engine, now(), app, devices, mqttEnabled); err != nil {
			http.Error(w, "metrics unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		_, _ = body.WriteTo(w)
	})
	return mux
}

func (m *CoreMetrics) collect(ctx context.Context, body *bytes.Buffer, engine *Engine, now time.Time, app *inbox.Store, devices func(time.Time) (int, int, int), mqttEnabled bool) error {
	wallNow := time.Now()
	audio, err := m.store.collectAudioMetrics(ctx, wallNow)
	if err != nil {
		return err
	}
	body.WriteString(audio)
	var snapshot strings.Builder
	writeGauge(&snapshot, "orbit_core_mqtt_enabled", "Whether Core has MQTT configured.", mqttEnabled)
	writeGauge(&snapshot, "orbit_core_app_enabled", "Whether Core has the App API enabled.", app != nil)
	if engine != nil {
		engine.writeMetrics(&snapshot, now)
	}
	if devices != nil {
		authorized, revoked, recent := devices(wallNow)
		writeMetricHeader(&snapshot, "orbit_core_app_devices", "App devices authorized, revoked, or recently active. Active is a subset of authorized.", "gauge")
		writeMetric(&snapshot, "orbit_core_app_devices", map[string]string{"state": "authorized"}, float64(authorized))
		writeMetric(&snapshot, "orbit_core_app_devices", map[string]string{"state": "revoked"}, float64(revoked))
		writeMetric(&snapshot, "orbit_core_app_devices", map[string]string{"state": "recent"}, float64(recent))
	}
	if err := m.store.writeMetrics(ctx, &snapshot, app); err != nil {
		return err
	}
	m.sqliteRows.write(&snapshot, app != nil)
	body.WriteString(snapshot.String())
	families, err := m.registry.Gather()
	if err != nil {
		return err
	}
	for _, family := range families {
		if _, err := expfmt.MetricFamilyToText(body, family); err != nil {
			return err
		}
	}
	return nil
}

func writeGauge(b *strings.Builder, name, help string, enabled bool) {
	writeMetricHeader(b, name, help, "gauge")
	value := 0.0
	if enabled {
		value = 1
	}
	writeMetric(b, name, nil, value)
}

func (m *CoreMetrics) InstrumentHTTP(surface string, next http.Handler) http.Handler {
	if m == nil {
		return next
	}
	for _, kind := range []string{"request", "events"} {
		m.httpActive.WithLabelValues(surface, kind)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		events := r.URL.Path == "/api/v1/events" || r.URL.Path == "/api/app/events"
		kind := "request"
		if events {
			kind = "events"
		}
		active := m.httpActive.WithLabelValues(surface, kind)
		active.Inc()
		defer active.Dec()
		start := time.Now()
		response := &metricsResponse{ResponseWriter: w, onStatus: func(code int) {
			m.httpRequests.WithLabelValues(surface, routePattern(r), strconv.Itoa(code)).Inc()
		}}
		next.ServeHTTP(response, r)
		if !response.sent {
			response.record(http.StatusOK)
		}
		if !events {
			m.httpDuration.WithLabelValues(surface, routePattern(r)).Observe(time.Since(start).Seconds())
		}
	})
}

func routePattern(r *http.Request) string {
	if r.Pattern != "" {
		return r.Pattern
	}
	return "unmatched"
}

type metricsResponse struct {
	http.ResponseWriter
	onStatus func(int)
	sent     bool
}

func (w *metricsResponse) record(code int) {
	if !w.sent && (code >= 200 || code == http.StatusSwitchingProtocols) {
		w.sent = true
		w.onStatus(code)
	}
}

func (w *metricsResponse) WriteHeader(code int) {
	w.record(code)
	w.ResponseWriter.WriteHeader(code)
}

func (w *metricsResponse) Write(b []byte) (int, error) {
	w.record(http.StatusOK)
	return w.ResponseWriter.Write(b)
}

// ResponseController must still reach the original writer for SSE deadlines
// and flushes.
func (w *metricsResponse) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (m *CoreMetrics) MQTTConnection(connected bool) {
	if m != nil {
		v := 0.0
		if connected {
			v = 1
		}
		m.mqttConnected.Set(v)
	}
}

func (m *CoreMetrics) InstrumentTransport(t Transport) Transport {
	if m == nil {
		return t
	}
	return &metricsTransport{Transport: t, metrics: m}
}

type metricsTransport struct {
	Transport
	metrics *CoreMetrics
}

func (t *metricsTransport) Publish(ctx context.Context, msg mqtt.Message) error {
	err := t.Transport.Publish(ctx, msg)
	t.metrics.mqttOperations.WithLabelValues("publish", mqttMetricKind(msg.Topic), metricResult(err)).Inc()
	return err
}

func (t *metricsTransport) Subscribe(ctx context.Context, filter string, handler mqtt.Handler) error {
	kind := mqttMetricKind(filter)
	err := t.Transport.Subscribe(ctx, filter, func(ctx context.Context, msg mqtt.Message) error {
		err := handler(ctx, msg)
		t.metrics.mqttOperations.WithLabelValues("receive", kind, metricResult(err)).Inc()
		return err
	})
	t.metrics.mqttOperations.WithLabelValues("subscribe", kind, metricResult(err)).Inc()
	return err
}

// Never label a metric with a raw topic, which contains device identities.
func mqttMetricKind(topic string) string {
	parts := strings.Split(topic, "/")
	if len(parts) < 4 || parts[0] != "orbit" || parts[1] != "v1" {
		return "other"
	}
	switch len(parts) {
	case 4:
		if parts[2] == "audio" && parts[3] == "jobs" {
			return "audio_job"
		}
	case 5:
		switch parts[2] + "/" + parts[4] {
		case "agents/state", "nodes/state":
			return parts[2] + "_state"
		case "nodes/view", "nodes/intents", "nodes/navigation", "agents/commands", "agents/shortcuts":
			return parts[4]
		}
	case 6:
		if parts[2] == "agents" && parts[4] == "observations" {
			switch parts[5] {
			case "usage", "codex", "system":
				return parts[5]
			}
		}
		switch parts[2] + "/" + parts[4] + "/" + parts[5] {
		case "nodes/audio/segments":
			return "audio_segment"
		case "agents/audio/results":
			return "audio_result"
		case "agents/audio/state":
			return "audio_worker"
		}
	}
	return "other"
}

func metricResult(err error) string {
	if err != nil {
		return "error"
	}
	return "ok"
}

func (m *CoreMetrics) recordWorker(worker string, err error) {
	if m != nil {
		m.workerRuns.WithLabelValues(worker, metricResult(err)).Inc()
		if err == nil {
			m.workerSuccess.WithLabelValues(worker).SetToCurrentTime()
		}
	}
}

func (m *CoreMetrics) recordReviewRequest(start time.Time, result reviewResult, err error) {
	m.reviewRequests.WithLabelValues(metricResult(err)).Inc()
	m.reviewDuration.Observe(time.Since(start).Seconds())
	if err == nil {
		m.reviewTokens.WithLabelValues("input").Add(float64(max(0, result.InputTokens)))
		m.reviewTokens.WithLabelValues("output").Add(float64(max(0, result.OutputTokens)))
	}
}
