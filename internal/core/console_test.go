package core

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	orbitv1 "orbit/gen/go/orbit/v1"
	"orbit/internal/config"
	"orbit/internal/mqtt"
	webnode "orbit/nodes/web"
)

func seedRoutes() map[string]config.ProjectionRoute {
	return map[string]config.ProjectionRoute{"node-a": {Profile: usageProfile, Inputs: []config.ProjectionInput{{AgentID: "agent-a", ObservationType: "usage"}}}}
}
func TestRouteStorePersistsEmptyAndRejectsConflicts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "core.sqlite")
	store, err := OpenRouteStore(path, seedRoutes())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.save(RouteDocument{Revision: 1, Routes: map[string]config.ProjectionRoute{}}); err != nil {
		t.Fatal(err)
	}
	if err := store.save(RouteDocument{Revision: 1, Routes: seedRoutes()}); !errors.Is(err, errRouteConflict) {
		t.Fatalf("conflict: %v", err)
	}
	store.Close()
	store, err = OpenRouteStore(path, seedRoutes())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	d, err := store.Load()
	if err != nil || d.Revision != 2 || len(d.Routes) != 0 {
		t.Fatalf("reopened: %+v %v", d, err)
	}
}

func TestRouteReplacementAtomicAndClearsCachedData(t *testing.T) {
	now := time.Now().UTC()
	engine := newTestEngine(t)
	if err := engine.ApplyAgentState(testAgentState(now)); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.ApplyNodeState(now, testNodeState(now)); err != nil {
		t.Fatal(err)
	}
	views, err := engine.ApplyObservation(now, testObservation(now))
	if err != nil {
		t.Fatal(err)
	}
	// Simulate the Web Node's previously cached sections as well as OLED slots.
	cost := int64(12345000)
	views[0].Usage = &orbitv1.UsageView{ActualCostMicros: &cost, CurrencyCode: "USD", Freshness: orbitv1.Freshness_FRESHNESS_FRESH, FreshUntil: views[0].FreshUntil}
	views[0].Codex = &orbitv1.CodexView{Sessions: []*orbitv1.CodexSessionView{{SessionId: "old-session"}}, Freshness: orbitv1.Freshness_FRESHNESS_FRESH, FreshUntil: views[0].FreshUntil}
	cache := webnode.NewStore()
	if err := cache.Update(views[0], now); err != nil {
		t.Fatal(err)
	}
	oldRevision := engine.viewRevision["node-a"]
	failure := errors.New("disk unavailable")
	if _, err := engine.replaceRoutes(now, nil, func() error { return failure }); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	if len(engine.config.Routes) != 1 || engine.viewRevision["node-a"] != oldRevision {
		t.Fatal("failed commit mutated runtime")
	}
	views, err = engine.replaceRoutes(now, nil, func() error { return nil })
	if err != nil || len(views) != 1 {
		t.Fatalf("delete: %v %v", views, err)
	}
	view := views[0]
	if view.Usage == nil || view.Codex == nil || view.Primary == nil || view.Freshness != orbitv1.Freshness_FRESHNESS_STALE || view.Metadata.Revision <= oldRevision {
		t.Fatalf("incomplete clearing view: %v", view)
	}
	if err := cache.Update(view, now); err != nil {
		t.Fatal(err)
	}
	snapshot, err := cache.Snapshot()
	if err != nil || snapshot.Usage.ActualCostMicros != 0 || len(snapshot.Codex.Sessions) != 0 {
		t.Fatalf("old upstream data survived clearing: %+v %v", snapshot, err)
	}
	if views, err := engine.ApplyObservation(now.Add(time.Second), testObservationRevision(now.Add(time.Second), 2)); err != nil || len(views) != 0 {
		t.Fatalf("deleted route still forwards: %v %v", views, err)
	}
}
func testObservationRevision(now time.Time, revision uint64) *orbitv1.Observation {
	o := testObservation(now)
	o.Metadata.Revision = revision
	return o
}

func TestConsoleRoutesValidationConflictAuthAndHotApply(t *testing.T) {
	now := time.Now().UTC()
	engine := newTestEngine(t)
	transport := &fakeTransport{}
	runner, _ := NewRunner(engine, transport, nil, func() time.Time { return now })
	store, err := OpenRouteStore(filepath.Join(t.TempDir(), "core.sqlite"), seedRoutes())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	cfg := &config.CoreConfig{Console: config.ConsoleConfig{Password: "test-password"}, ObservationPolicies: map[string]config.ObservationPolicy{"usage": {MaxTTL: config.Duration{Duration: time.Minute}}}}
	handler := ConsoleHandler(runner, store, cfg)
	request := func(method, path, body, password, origin string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if password != "" {
			login := httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(`{"password":"`+password+`"}`))
			login.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, login)
			if response.Code != 200 {
				t.Fatalf("login: %s", response.Body.String())
			}
			for _, cookie := range response.Result().Cookies() {
				r.AddCookie(cookie)
			}
		}
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	if w := request("GET", "/api/state", "", "", ""); w.Code != 401 {
		t.Fatal(w.Code)
	}
	if w := request("GET", "/", "", "test-password", ""); w.Code != 200 || !strings.Contains(w.Body.String(), "Core Console") {
		t.Fatal(w.Code)
	}
	if w := request("PUT", "/api/routes", `{"revision":1,"routes":{}}`, "test-password", "https://evil.example"); w.Code != 403 {
		t.Fatal(w.Code)
	}
	if w := request("PUT", "/api/routes", `{"revision":1,"routes":{"bad/node":{"profile":"overview-web","inputs":[]}}}`, "test-password", ""); w.Code != 400 {
		t.Fatal(w.Code)
	}
	if w := request("PUT", "/api/routes", `{"revision":1,"routes":{}} {}`, "test-password", ""); w.Code != 400 {
		t.Fatal(w.Code)
	}
	changed := seedRoutes()
	r := changed["node-a"]
	r.Inputs[0].AgentID = "agent-b"
	changed["node-a"] = r
	body, _ := json.Marshal(RouteDocument{Revision: 1, Routes: changed})
	if w := request("PUT", "/api/routes", string(body), "test-password", ""); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if engine.config.Routes[0].Inputs[0].AgentID != "agent-b" {
		t.Fatal("runtime not updated")
	}
	if w := request("PUT", "/api/routes", string(body), "test-password", ""); w.Code != 409 {
		t.Fatal(w.Code)
	}
	d, _ := store.Load()
	if d.Revision != 2 || d.Routes["node-a"].Inputs[0].AgentID != "agent-b" {
		t.Fatal(d)
	}
}

type retryTransport struct {
	failed   bool
	messages []mqtt.Message
}

func (t *retryTransport) Subscribe(context.Context, string, mqtt.Handler) error { return nil }
func (t *retryTransport) Publish(_ context.Context, m mqtt.Message) error {
	if t.failed {
		return errors.New("offline")
	}
	t.messages = append(t.messages, m)
	return nil
}
func TestRunnerRetriesLatestViewAfterFailure(t *testing.T) {
	transport := &retryTransport{failed: true}
	runner, _ := NewRunner(newTestEngine(t), transport, nil, nil)
	view := &orbitv1.DeviceView{NodeId: "node-a", Metadata: &orbitv1.Metadata{Revision: 1}}
	if runner.publishViews(context.Background(), []*orbitv1.DeviceView{view}) == nil {
		t.Fatal("expected offline")
	}
	updated := proto.Clone(view).(*orbitv1.DeviceView)
	updated.Metadata.Revision = 2
	if runner.publishViews(context.Background(), []*orbitv1.DeviceView{updated}) == nil {
		t.Fatal("expected offline")
	}
	transport.failed = false
	if err := runner.publishViews(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if len(transport.messages) != 1 || len(runner.pendingViews) != 0 {
		t.Fatal("retry not drained")
	}
	var received orbitv1.DeviceView
	_ = proto.Unmarshal(transport.messages[0].Payload, &received)
	if received.Metadata.Revision != 2 {
		t.Fatal("replayed obsolete view")
	}
}

func TestRouteReplacementRejectsKnownProductMismatch(t *testing.T) {
	e := newTestEngine(t)
	_, err := e.ApplyNodeState(time.Now(), testNodeState(time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	persisted := false
	routes := RoutesFromConfig(seedRoutes())
	routes[0].Profile = webProfile
	if _, err := e.replaceRoutes(time.Now(), routes, func() error { persisted = true; return nil }); err == nil || persisted {
		t.Fatal("accepted mismatched product")
	}
}

func TestCachedIntentCannotUseRemovedRoute(t *testing.T) {
	now := time.Now()
	engine := newCodexCommandEngine(t, now)
	intent := testOpenCodexIntent(now, "intent-1", testCodexSessionID, 1)
	if _, err := engine.CommandForIntent(now, intent); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.replaceRoutes(now, nil, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.CommandForIntent(now, intent); err == nil {
		t.Fatal("cached intent used deleted route")
	}
}

func TestRouteReplacementForwardsOnlyNewAgent(t *testing.T) {
	now := time.Now().UTC()
	e := newTestEngine(t)
	if err := e.ApplyAgentState(testAgentState(now)); err != nil {
		t.Fatal(err)
	}
	b := testAgentState(now)
	b.AgentId = "agent-b"
	b.Metadata.ProducerId = "agent-b"
	if err := e.ApplyAgentState(b); err != nil {
		t.Fatal(err)
	}
	if _, err := e.ApplyNodeState(now, testNodeState(now)); err != nil {
		t.Fatal(err)
	}
	if _, err := e.ApplyObservation(now, testObservation(now)); err != nil {
		t.Fatal(err)
	}
	routes := RoutesFromConfig(seedRoutes())
	routes[0].Inputs[0].AgentID = "agent-b"
	clearing, err := e.replaceRoutes(now, routes, func() error { return nil })
	if err != nil || len(clearing) != 1 || clearing[0].Freshness != orbitv1.Freshness_FRESHNESS_STALE {
		t.Fatalf("new source should clear old: %v %v", clearing, err)
	}
	views, err := e.ApplyObservation(now, testObservationRevision(now, 2))
	if err != nil || len(views) != 0 {
		t.Fatalf("old agent still forwarded: %v %v", views, err)
	}
	observation := testObservation(now)
	observation.Metadata.ProducerId = "agent-b"
	views, err = e.ApplyObservation(now, observation)
	if err != nil || len(views) != 1 || views[0].Freshness != orbitv1.Freshness_FRESHNESS_FRESH {
		t.Fatalf("new agent did not forward: %v %v", views, err)
	}
}

func TestUnroutedNodeDiscoveryClearsOldRetainedView(t *testing.T) {
	now := time.Now()
	e, err := New(Config{CoreID: "core-a", CoreEpoch: "restarted"})
	if err != nil {
		t.Fatal(err)
	}
	views, err := e.ApplyNodeState(now, testNodeState(now))
	if err != nil || len(views) != 1 || views[0].Codex == nil || views[0].Usage == nil || views[0].Freshness != orbitv1.Freshness_FRESHNESS_STALE {
		t.Fatalf("unrouted node not cleared: %v %v", views, err)
	}
}
