package web

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	orbitv1 "orbit/gen/go/orbit/v1"
	"orbit/internal/mqtt"

	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const webTestSessionID = "01a066af-69d4-77d1-a21b-26d84534a817"

type recordingTransport struct {
	mu        sync.Mutex
	published mqtt.Message
	filter    string
	handler   mqtt.Handler
}

func (t *recordingTransport) Publish(_ context.Context, message mqtt.Message) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.published = message
	return nil
}

func (t *recordingTransport) Subscribe(_ context.Context, filter string, handler mqtt.Handler) error {
	t.mu.Lock()
	t.filter = filter
	t.handler = handler
	t.mu.Unlock()
	return nil
}

func (t *recordingTransport) snapshot() (mqtt.Message, string, mqtt.Handler) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.published, t.filter, t.handler
}

func TestStoreExposesLatestRichView(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)
	store := NewStore()
	if err := store.Update(testView(now), now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := store.Update(testView(now), now.Add(2*time.Second)); err == nil {
		t.Fatal("accepted duplicate view revision")
	}

	request := httptest.NewRequest(http.MethodGet, "/api/state", nil)
	response := httptest.NewRecorder()
	Handler(store, nil).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
	var snapshot Snapshot
	if err := json.Unmarshal(response.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Usage == nil || snapshot.Usage.TokenCount != 1234 || snapshot.Codex == nil || len(snapshot.Codex.Sessions) != 1 {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}
	if snapshot.Codex.Sessions[0].DisplayName != "Web node" || snapshot.Freshness != "fresh" {
		t.Fatalf("unexpected projected fields: %+v", snapshot)
	}
}

func TestWebAuthProtectsAPIAndPersistsSessionCookie(t *testing.T) {
	t.Parallel()
	store := NewStore()
	now := time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)
	if err := store.Update(testView(now), now); err != nil {
		t.Fatal(err)
	}
	handler := HandlerWithAuth(store, nil, AuthConfig{Password: "web-secret", SessionTTL: time.Hour})

	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/api/state", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized state status = %d", unauthorized.Code)
	}

	badLogin := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString(`{"password":"wrong"}`))
	badLogin.Header.Set("Content-Type", "application/json")
	badResponse := httptest.NewRecorder()
	handler.ServeHTTP(badResponse, badLogin)
	if badResponse.Code != http.StatusUnauthorized {
		t.Fatalf("bad login status = %d", badResponse.Code)
	}

	login := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString(`{"password":"web-secret"}`))
	login.Header.Set("Content-Type", "application/json")
	loginResponse := httptest.NewRecorder()
	handler.ServeHTTP(loginResponse, login)
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("login status = %d, body = %s", loginResponse.Code, loginResponse.Body.String())
	}
	var session struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(loginResponse.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	if session.Token == "" {
		t.Fatal("login did not return a token")
	}
	cookies := loginResponse.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != authCookieName {
		t.Fatalf("login did not set %q cookie", authCookieName)
	}

	authorized := httptest.NewRequest(http.MethodGet, "/api/state", nil)
	authorized.Header.Set("Authorization", "Bearer "+session.Token)
	authorizedResponse := httptest.NewRecorder()
	handler.ServeHTTP(authorizedResponse, authorized)
	if authorizedResponse.Code != http.StatusOK {
		t.Fatalf("authorized state status = %d", authorizedResponse.Code)
	}

	cookieAuthorized := httptest.NewRequest(http.MethodGet, "/api/state", nil)
	cookieAuthorized.AddCookie(cookies[0])
	cookieResponse := httptest.NewRecorder()
	handler.ServeHTTP(cookieResponse, cookieAuthorized)
	if cookieResponse.Code != http.StatusOK {
		t.Fatalf("cookie-authorized state status = %d", cookieResponse.Code)
	}

	tampered := httptest.NewRequest(http.MethodGet, "/api/state", nil)
	tampered.Header.Set("Authorization", "Bearer "+session.Token+"x")
	tamperedResponse := httptest.NewRecorder()
	handler.ServeHTTP(tamperedResponse, tampered)
	if tamperedResponse.Code != http.StatusUnauthorized {
		t.Fatalf("tampered token status = %d", tamperedResponse.Code)
	}
}

func TestAppJSSupportsDeviceChrome(t *testing.T) {
	t.Parallel()
	script, err := staticFiles.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	unsupported := []struct {
		name    string
		pattern *regexp.Regexp
	}{
		{name: "optional chaining", pattern: regexp.MustCompile(`\?\.`)},
		{name: "nullish coalescing", pattern: regexp.MustCompile(`\?\?`)},
		{name: "numeric separators", pattern: regexp.MustCompile(`[0-9]_[0-9]`)},
		{name: "Element.replaceChildren", pattern: regexp.MustCompile(`\.replaceChildren\(`)},
	}
	for _, feature := range unsupported {
		if feature.pattern.Match(script) {
			t.Errorf("app.js uses %s, which is unsupported by the device's Chromium 71", feature.name)
		}
	}
}

func TestAppCSSSupportsDeviceChrome(t *testing.T) {
	t.Parallel()
	stylesheet, err := staticFiles.ReadFile("static/app.css")
	if err != nil {
		t.Fatal(err)
	}
	unsupported := []struct {
		name    string
		pattern *regexp.Regexp
	}{
		{name: "CSS min()", pattern: regexp.MustCompile(`:\s*min\(`)},
		{name: "aspect-ratio", pattern: regexp.MustCompile(`aspect-ratio\s*:`)},
	}
	for _, feature := range unsupported {
		if feature.pattern.Match(stylesheet) {
			t.Errorf("app.css uses %s, which is unsupported by the device's Chromium 71", feature.name)
		}
	}
}

func TestDevelopmentHandlerServesDiskAssetsAndSignalsReload(t *testing.T) {
	t.Parallel()
	staticDir := t.TempDir()
	indexPath := filepath.Join(staticDir, "index.html")
	if err := os.WriteFile(indexPath, []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	handler, err := DevelopmentHandler(NewStore(), nil, AuthConfig{}, staticDir)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()

	assetResponse, err := server.Client().Get(server.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	assetBody, err := io.ReadAll(assetResponse.Body)
	assetResponse.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if string(assetBody) != "before" || assetResponse.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("unexpected development asset response: body=%q cache-control=%q", assetBody, assetResponse.Header.Get("Cache-Control"))
	}

	client := server.Client()
	client.Timeout = 3 * time.Second
	eventsResponse, err := client.Get(server.URL + "/api/events")
	if err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(eventsResponse.Body)
	if !scanner.Scan() || scanner.Text() != ": connected" {
		eventsResponse.Body.Close()
		t.Fatalf("development event stream did not connect: %q", scanner.Text())
	}
	if err := os.WriteFile(indexPath, []byte("after"), 0o600); err != nil {
		eventsResponse.Body.Close()
		t.Fatal(err)
	}
	reloadSeen := false
	for scanner.Scan() {
		if scanner.Text() == "event: reload" {
			reloadSeen = true
			break
		}
	}
	eventsResponse.Body.Close()
	if !reloadSeen {
		t.Fatalf("development event stream did not signal reload: %v", scanner.Err())
	}

	updatedResponse, err := server.Client().Get(server.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	updatedBody, err := io.ReadAll(updatedResponse.Body)
	updatedResponse.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if string(updatedBody) != "after" {
		t.Fatalf("development handler served stale content %q", updatedBody)
	}
}

func TestStaticPageProvidesFullscreenToggle(t *testing.T) {
	t.Parallel()
	markup, err := staticFiles.ReadFile("static/index.html")
	if err != nil {
		t.Fatal(err)
	}
	script, err := staticFiles.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{`<button class="brand" id="fullscreen-toggle"`, `aria-pressed="false"`} {
		if !strings.Contains(string(markup), fragment) {
			t.Errorf("index.html is missing %q", fragment)
		}
	}
	for _, api := range []string{"requestFullscreen", "webkitRequestFullscreen", "exitFullscreen", "webkitExitFullscreen"} {
		if !strings.Contains(string(script), api) {
			t.Errorf("app.js is missing %s support", api)
		}
	}
}

func TestStaticPageReloadsFromConnectionStatus(t *testing.T) {
	t.Parallel()
	markup, err := staticFiles.ReadFile("static/index.html")
	if err != nil {
		t.Fatal(err)
	}
	script, err := staticFiles.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(markup), `<button class="connection" id="connection" type="button"`) {
		t.Fatal("index.html does not expose the connection status as a button")
	}
	for _, fragment := range []string{`elements.connection.addEventListener("click"`, `eventSource.addEventListener("reload"`, `window.location.reload()`} {
		if !strings.Contains(string(script), fragment) {
			t.Errorf("app.js is missing %q", fragment)
		}
	}
}

func TestStaticPageOpensSessionsThroughNodeIntent(t *testing.T) {
	t.Parallel()
	markup, err := staticFiles.ReadFile("static/index.html")
	if err != nil {
		t.Fatal(err)
	}
	script, err := staticFiles.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{`document.createElement("button")`, `document.createElement("span")`, "/api/sessions/", "view_revision"} {
		if !strings.Contains(string(script), fragment) {
			t.Errorf("app.js is missing %q", fragment)
		}
	}
	if !strings.Contains(string(markup), `id="action-status"`) {
		t.Fatal("index.html is missing the session action live region")
	}
}

func TestStoreKeepsCachedSectionsAcrossPartialViews(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)
	store := NewStore()
	if err := store.Update(testView(now), now); err != nil {
		t.Fatal(err)
	}

	usageOnly := proto.Clone(testView(now.Add(time.Second))).(*orbitv1.DeviceView)
	usageOnly.Metadata.Revision = 2
	usageOnly.Codex = nil
	if err := store.Update(usageOnly, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}

	for client := 1; client <= 2; client++ {
		request := httptest.NewRequest(http.MethodGet, "/api/state", nil)
		response := httptest.NewRecorder()
		Handler(store, nil).ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("client %d status = %d", client, response.Code)
		}
		var snapshot Snapshot
		if err := json.Unmarshal(response.Body.Bytes(), &snapshot); err != nil {
			t.Fatal(err)
		}
		if snapshot.Revision != 2 || snapshot.Usage == nil || snapshot.Codex == nil {
			t.Fatalf("client %d received incomplete cached snapshot: %+v", client, snapshot)
		}
	}
}

func TestStoreDoesNotCarryCacheAcrossCoreEpochs(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)
	store := NewStore()
	if err := store.Update(testView(now), now); err != nil {
		t.Fatal(err)
	}

	nextEpoch := proto.Clone(testView(now.Add(time.Second))).(*orbitv1.DeviceView)
	nextEpoch.Metadata.Revision = 1
	nextEpoch.CoreEpoch = "next-core-epoch"
	nextEpoch.Codex = nil
	if err := store.Update(nextEpoch, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}

	snapshot, err := store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Codex != nil {
		t.Fatalf("carried cached Codex state across Core epochs: %+v", snapshot.Codex)
	}
}

func TestStoreMarksRetainedSectionStale(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)
	store := NewStore()
	if err := store.Update(testView(now), now); err != nil {
		t.Fatal(err)
	}

	usageOnly := proto.Clone(testView(now.Add(2 * time.Minute))).(*orbitv1.DeviceView)
	usageOnly.Metadata.Revision = 2
	usageOnly.Codex = nil
	if err := store.Update(usageOnly, now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}

	snapshot, err := store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Codex == nil || snapshot.Codex.Freshness != "stale" {
		t.Fatalf("cached Codex state was not retained as stale: %+v", snapshot.Codex)
	}
}

func TestRunnerRegistersAndAcceptsOnlyItsView(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)
	transport := &recordingTransport{}
	store := NewStore()
	runner, err := NewRunner(RunnerConfig{
		NodeID: "web-a", NodeEpoch: "node-epoch", FirmwareVersion: "test",
	}, transport, store, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	runner.now = func() time.Time { return now }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runner.Run(ctx) }()
	var published mqtt.Message
	var filter string
	var handler mqtt.Handler
	for handler == nil || published.Topic == "" {
		time.Sleep(time.Millisecond)
		published, filter, handler = transport.snapshot()
	}
	if filter != "orbit/v1/nodes/web-a/view" || published.Topic != "orbit/v1/nodes/web-a/state" || !published.Retain {
		t.Fatalf("unexpected transport setup: filter=%q publish=%q retained=%t", filter, published.Topic, published.Retain)
	}
	var state orbitv1.NodeState
	if err := proto.Unmarshal(published.Payload, &state); err != nil {
		t.Fatal(err)
	}
	if state.ModelId != "web" || state.VariantId != "browser" || state.NodeId != "web-a" {
		t.Fatalf("unexpected node state: %+v", &state)
	}
	payload, _ := proto.Marshal(testView(now))
	if err := handler(ctx, mqtt.Message{Topic: filter, Payload: payload, Retain: true}); err != nil {
		t.Fatal(err)
	}
	snapshot, _ := store.Snapshot()
	if snapshot == nil || snapshot.Revision != 1 {
		t.Fatalf("view not stored: %+v", snapshot)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestOpenCodexSessionEndpointPublishesIntent(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)
	store := NewStore()
	if err := store.Update(testView(now), now); err != nil {
		t.Fatal(err)
	}
	transport := &recordingTransport{}
	runner, err := NewRunner(RunnerConfig{
		NodeID: "web-a", NodeEpoch: "node-epoch", FirmwareVersion: "test",
	}, transport, store, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	runner.now = func() time.Time { return now }
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/sessions/"+webTestSessionID+"/open",
		bytes.NewBufferString(`{"view_revision":1}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	Handler(store, runner).ServeHTTP(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	published, _, _ := transport.snapshot()
	if published.Topic != "orbit/v1/nodes/web-a/intents" || published.Retain {
		t.Fatalf("unexpected intent publish: %+v", published)
	}
	var intent orbitv1.Intent
	if err := proto.Unmarshal(published.Payload, &intent); err != nil {
		t.Fatal(err)
	}
	if intent.Metadata.GetProducerId() != "web-a" || intent.NodeEpoch != "node-epoch" || intent.ViewRevision != 1 || intent.GetOpenCodexSession().GetSessionId() != webTestSessionID {
		t.Fatalf("unexpected intent: %+v", &intent)
	}
}

func testView(now time.Time) *orbitv1.DeviceView {
	cost := int64(1_250_000)
	tokens := uint64(1234)
	tpm := uint64(56)
	return &orbitv1.DeviceView{
		Metadata: &orbitv1.Metadata{
			MessageId: "view-a", ProducerId: "core-a", Revision: 1,
			ProducedAt: timestamppb.New(now), ExpiresAt: timestamppb.New(now.Add(time.Hour)),
		},
		NodeId: "web-a", CoreEpoch: "core-epoch", Freshness: orbitv1.Freshness_FRESHNESS_FRESH,
		FreshUntil: timestamppb.New(now.Add(time.Minute)), RetainUntil: timestamppb.New(now.Add(time.Hour)),
		Usage: &orbitv1.UsageView{
			Freshness: orbitv1.Freshness_FRESHNESS_FRESH, FreshUntil: timestamppb.New(now.Add(time.Minute)),
			ActualCostMicros: &cost, CurrencyCode: "USD", TokenCount: &tokens, Tpm: &tpm,
			ObservedAt: timestamppb.New(now),
		},
		Codex: &orbitv1.CodexView{
			Freshness: orbitv1.Freshness_FRESHNESS_FRESH, FreshUntil: timestamppb.New(now.Add(time.Minute)),
			TotalCount: 1, RunningCount: 1, ObservedAt: timestamppb.New(now),
			Sessions: []*orbitv1.CodexSessionView{{
				SessionId: webTestSessionID, DisplayName: "Web node", ProjectName: "orbit", Model: "gpt-5",
				Status: orbitv1.CodexSessionStatus_CODEX_SESSION_STATUS_RUNNING, UpdatedAt: timestamppb.New(now), ProcessAlive: true,
			}},
		},
	}
}
