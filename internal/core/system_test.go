package core

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	orbitv1 "orbit/gen/go/orbit/v1"
	"orbit/internal/config"
	"orbit/internal/mqtt"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func testSystemAgentState(now time.Time, agentID string) *orbitv1.AgentState {
	return &orbitv1.AgentState{
		Metadata:     &orbitv1.Metadata{MessageId: "state-" + agentID, ProducerId: agentID, Revision: 1, ProducedAt: timestamppb.New(now)},
		AgentId:      agentID,
		AgentEpoch:   "epoch-" + agentID,
		AgentVersion: "0.4.3",
		HostLabel:    "Workstation",
		Sources:      []*orbitv1.SourceStatus{{ObservationType: orbitv1.ObservationType_OBSERVATION_TYPE_SYSTEM, Enabled: true, Health: orbitv1.SourceHealth_SOURCE_HEALTH_HEALTHY}},
	}
}

func testSystemObservation(now time.Time, agentID string, rev uint64) *orbitv1.Observation {
	return &orbitv1.Observation{
		Metadata: &orbitv1.Metadata{
			MessageId:  "obs-sys",
			ProducerId: agentID,
			Revision:   rev,
			ProducedAt: timestamppb.New(now),
			ExpiresAt:  timestamppb.New(now.Add(30 * time.Second)),
		},
		AgentEpoch: "epoch-" + agentID,
		Payload: &orbitv1.Observation_System{
			System: &orbitv1.SystemObservation{
				ObservedAt:       timestamppb.New(now),
				CpuUsagePermille: proto.Uint32(244),
				CpuCores:         proto.Uint32(12),
				MemoryUsedBytes:  proto.Uint64(13314398618), // ~12.4 GiB
				MemoryTotalBytes: proto.Uint64(32 << 30),
				SwapUsedBytes:    proto.Uint64(1 << 30),
				SwapTotalBytes:   proto.Uint64(4 << 30),
				Load_1MMilli:     proto.Uint32(1420),
				UptimeSeconds:    proto.Uint64(86400),
			},
		},
	}
}

func TestSystemObservationValidationAndProjectionToOLEDAndXiaozhi(t *testing.T) {
	now := time.Date(2026, 11, 16, 8, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name          string
		profile       string
		model         string
		variant       string
		systemPrimary string
		wantPrimary   string
		wantSecondary string
		wantFooter    string
	}{
		{"oled cpu primary", usageProfile, oledModel, ydVariant, "", "24%", "MEM 39%", "12.4/32G"},
		{"oled memory primary", usageProfile, oledModel, ydVariant, "memory", "39%", "CPU 24%", "12.4/32G"},
		{"xiaozhi cpu primary", xiaozhiProfile, xiaozhiModel, xiaozhiVariant, "cpu", "24%", "MEM 39%", "12.4/32G"},
		{"xiaozhi memory primary", xiaozhiProfile, xiaozhiModel, xiaozhiVariant, "memory", "39%", "CPU 24%", "12.4/32G"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, err := New(Config{
				CoreID:    "core",
				CoreEpoch: "epoch",
				Routes: []Route{{
					NodeID:        "screen-1",
					Profile:       tc.profile,
					DisplayMode:   "system",
					SystemPrimary: tc.systemPrimary,
					Inputs:        []RouteInput{{AgentID: "agent-a", ObservationType: orbitv1.ObservationType_OBSERVATION_TYPE_SYSTEM}},
				}},
			})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if err := e.ApplyAgentState(testSystemAgentState(now, "agent-a")); err != nil {
				t.Fatal(err)
			}
			node := &orbitv1.NodeState{
				Metadata:        &orbitv1.Metadata{MessageId: "ns", ProducerId: "screen-1", Revision: 1, ProducedAt: timestamppb.New(now)},
				NodeId:          "screen-1",
				NodeEpoch:       "ne",
				SeriesId:        displaySeries,
				ModelId:         tc.model,
				VariantId:       tc.variant,
				FirmwareVersion: "0.1.0",
			}
			missingViews, err := e.ApplyNodeState(now, node)
			if err != nil || len(missingViews) != 1 {
				t.Fatalf("ApplyNodeState missingViews=%v err=%v", missingViews, err)
			}
			if missingViews[0].Primary.Text != "--%" || missingViews[0].Footer.Text != "--/--G" || missingViews[0].Freshness != orbitv1.Freshness_FRESHNESS_STALE {
				t.Fatalf("unexpected missing placeholder view: %#v", missingViews[0])
			}

			views, err := e.ApplyObservation(now, testSystemObservation(now, "agent-a", 1))
			if err != nil || len(views) != 1 {
				t.Fatalf("ApplyObservation: views=%v err=%v", views, err)
			}
			v := views[0]
			if v.DisplayKind != "system" || v.Freshness != orbitv1.Freshness_FRESHNESS_FRESH {
				t.Fatalf("unexpected kind/freshness: %#v", v)
			}
			if v.Primary.Text != tc.wantPrimary || v.Secondary.Text != tc.wantSecondary || v.Footer.Text != tc.wantFooter {
				t.Fatalf("slots = (%q, %q, %q), want (%q, %q, %q)", v.Primary.Text, v.Secondary.Text, v.Footer.Text, tc.wantPrimary, tc.wantSecondary, tc.wantFooter)
			}
			if v.Usage != nil || v.Codex != nil || v.System != nil {
				t.Fatal("physical screen view attached heavy nested payloads")
			}
			if proto.Size(v) > 1024 {
				t.Fatalf("physical screen payload size %d exceeds 1024", proto.Size(v))
			}
			for _, r := range v.Primary.Text {
				if !strings.ContainsRune(" $%+,-.0123456789", r) {
					t.Fatalf("xiaozhi primary font missing rune %q in %q", r, v.Primary.Text)
				}
			}

			// Invalid CPU > 1000 or memory used > total are rejected.
			bad := testSystemObservation(now, "agent-a", 2)
			bad.GetSystem().CpuUsagePermille = proto.Uint32(1001)
			if _, err := e.ApplyObservation(now, bad); err == nil {
				t.Fatal("accepted >100% cpu usage")
			}
			badMem := testSystemObservation(now, "agent-a", 2)
			badMem.GetSystem().MemoryUsedBytes = proto.Uint64((32 << 30) + 1)
			if _, err := e.ApplyObservation(now, badMem); err == nil {
				t.Fatal("accepted used memory > total memory")
			}
		})
	}
}

func TestSystemProjectionRichProfilesAndAndroidCoalescing(t *testing.T) {
	now := time.Date(2026, 11, 16, 8, 0, 0, 0, time.UTC)
	e, err := New(Config{
		CoreID:    "core",
		CoreEpoch: "epoch",
		Routes: []Route{{
			NodeID:      "phone",
			Profile:     appProfile,
			DisplayMode: "system",
			Inputs:      []RouteInput{{AgentID: "agent-a", ObservationType: orbitv1.ObservationType_OBSERVATION_TYPE_SYSTEM}},
		}, {
			NodeID:      "android-1",
			Profile:     androidProfile,
			DisplayMode: "system",
			Inputs:      []RouteInput{{AgentID: "agent-a", ObservationType: orbitv1.ObservationType_OBSERVATION_TYPE_SYSTEM}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = e.ApplyAgentState(testSystemAgentState(now, "agent-a"))
	_, _ = e.ApplyNodeState(now, &orbitv1.NodeState{
		Metadata: &orbitv1.Metadata{MessageId: "na", ProducerId: "android-1", Revision: 1, ProducedAt: timestamppb.New(now)},
		NodeId:   "android-1", NodeEpoch: "ne", SeriesId: displaySeries, ModelId: androidModel, VariantId: androidVariant, FirmwareVersion: "0.1",
	})
	views, err := e.ApplyObservation(now, testSystemObservation(now, "agent-a", 1))
	if err != nil || len(views) != 1 || views[0].System == nil {
		t.Fatalf("expected SystemView on android profile: views=%v err=%v", views, err)
	}
	appView := e.AppView(now, "phone")
	if appView == nil || appView.System == nil || appView.System.GetCpuUsagePermille() != 244 || appView.Primary.Text != "24%" {
		t.Fatalf("unexpected AppView: %#v", appView)
	}
	// Sub-minute content change is coalesced on android, but freshness expiry emits immediately.
	obs2 := testSystemObservation(now.Add(10*time.Second), "agent-a", 2)
	obs2.GetSystem().CpuUsagePermille = proto.Uint32(420)
	obs2.Metadata.ExpiresAt = timestamppb.New(now.Add(25 * time.Second))
	views, err = e.ApplyObservation(now.Add(10*time.Second), obs2)
	if err != nil || len(views) != 0 {
		t.Fatalf("expected android sub-minute coalesce, got %v %v", views, err)
	}
	staleViews, err := e.Refresh(now.Add(26 * time.Second))
	if err != nil || len(staleViews) != 1 || staleViews[0].System.GetFreshness() != orbitv1.Freshness_FRESHNESS_STALE {
		t.Fatalf("expected stale transition on android refresh, got %v %v", staleViews, err)
	}
}

func TestRotatingGeneralizedAcrossTwoAndThreeInputs(t *testing.T) {
	now := time.Date(2026, 11, 16, 8, 0, 0, 0, time.UTC)
	e := rotatingEngine(t, now, 3)
	_ = e.ApplyAgentState(testSystemAgentState(now, "sys-agent"))
	e.system["sys-agent"] = canonicalSystem{
		value:     testSystemObservation(now, "sys-agent", 1).GetSystem(),
		expiresAt: now.Add(time.Hour),
	}
	route := Route{
		NodeID:          "node-a",
		Profile:         rotatingOLEDProfile,
		RotationSeconds: 3,
		Inputs: []RouteInput{
			{AgentID: "usage-agent", ObservationType: orbitv1.ObservationType_OBSERVATION_TYPE_USAGE},
			{AgentID: "codex-agent", ObservationType: orbitv1.ObservationType_OBSERVATION_TYPE_CODEX},
			{AgentID: "sys-agent", ObservationType: orbitv1.ObservationType_OBSERVATION_TYPE_SYSTEM},
		},
	}
	e.config.Routes = []Route{route}
	wantModes := []string{"usage", "codex", "system", "usage"}
	wantPrimaries := []string{"$12.35", "50%", "24%", "$12.35"}
	for i, tick := range []int{0, 3, 6, 9} {
		views, err := e.Refresh(now.Add(time.Duration(tick) * time.Second))
		if err != nil || len(views) != 1 {
			t.Fatalf("tick %d: views=%v err=%v", tick, views, err)
		}
		if views[0].DisplayKind != wantModes[i] || views[0].Primary.Text != wantPrimaries[i] {
			t.Fatalf("tick %d: got (%s, %s), want (%s, %s)", tick, views[0].DisplayKind, views[0].Primary.Text, wantModes[i], wantPrimaries[i])
		}
	}

	// Usage + System two-way rotation without Codex.
	route.Inputs = []RouteInput{
		{AgentID: "usage-agent", ObservationType: orbitv1.ObservationType_OBSERVATION_TYPE_USAGE},
		{AgentID: "sys-agent", ObservationType: orbitv1.ObservationType_OBSERVATION_TYPE_SYSTEM},
	}
	views, err := e.replaceRoutes(now, []Route{route}, func() error { return nil })
	if err != nil || len(views) != 1 || views[0].Primary.Text != "$12.35" {
		t.Fatalf("2-way tick 0 replaceRoutes: views=%v err=%v", views, err)
	}
	views, err = e.Refresh(now.Add(3 * time.Second))
	if err != nil || len(views) != 1 || views[0].Primary.Text != "24%" {
		t.Fatalf("2-way tick 3: views=%v err=%v", views, err)
	}
}

func TestConsoleStateIncludesSystemSummaryAndReadOnlyNodeViews(t *testing.T) {
	now := time.Date(2026, 11, 16, 8, 0, 0, 0, time.UTC)
	e, err := New(Config{
		CoreID:    "core",
		CoreEpoch: "epoch",
		Routes: []Route{{
			NodeID:          "node-a",
			Profile:         usageProfile,
			DisplayMode:     "rotating",
			RotationSeconds: 3,
			Inputs: []RouteInput{
				{AgentID: "agent-a", ObservationType: orbitv1.ObservationType_OBSERVATION_TYPE_USAGE},
				{AgentID: "agent-a", ObservationType: orbitv1.ObservationType_OBSERVATION_TYPE_SYSTEM},
			},
		}},
		UsagePolicy: UsagePolicy{MaxTTL: time.Minute},
	})
	if err != nil {
		t.Fatal(err)
	}
	transport := &fakeTransport{}
	runner, err := NewRunner(e, transport, nil, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := proto.Marshal(testSystemObservation(now, "agent-a", 1))
	_ = e.ApplyAgentState(testSystemAgentState(now, "agent-a"))
	if err := runner.handleSystemObservation(context.Background(), mqtt.Message{
		Topic:   "orbit/v1/agents/agent-a/observations/system",
		Payload: payload,
	}); err != nil {
		t.Fatalf("handleSystemObservation: %v", err)
	}

	// Inspecting consoleState must not mutate rotationStarted or viewRevision.
	state := e.consoleState(now)
	if len(e.rotationStarted) != 0 || e.viewRevision["node-a"] != 0 {
		t.Fatalf("consoleState mutated engine state: rotation=%v rev=%d", e.rotationStarted, e.viewRevision["node-a"])
	}
	raw, _ := json.Marshal(state)
	var decoded struct {
		Agents []struct {
			ID          string         `json:"id"`
			SystemFresh bool           `json:"system_fresh"`
			System      map[string]any `json:"system"`
		} `json:"agents"`
		NodeViews map[string]struct {
			DisplayKind   string `json:"display_kind"`
			Primary       string `json:"primary"`
			Secondary     string `json:"secondary"`
			Footer        string `json:"footer"`
			RotationStep  int    `json:"rotation_step"`
			RotationTotal int    `json:"rotation_total"`
		} `json:"node_views"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Agents) != 1 || !decoded.Agents[0].SystemFresh || decoded.Agents[0].System["cpu_usage_permille"].(float64) != 244 {
		t.Fatalf("unexpected agent system state: %+v", decoded.Agents)
	}
	preview := decoded.NodeViews["node-a"]
	if preview.DisplayKind != "usage" || preview.RotationStep != 1 || preview.RotationTotal != 2 {
		t.Fatalf("unexpected node_views preview: %+v", preview)
	}

	// /api/system includes effective system policy even if omitted in YAML.
	store := newTestConsoleStore(t)
	handler := ConsoleHandler(runner, store, &config.CoreConfig{Console: config.ConsoleConfig{Password: "pw", SessionHours: 24}})
	login := httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(`{"password":"pw"}`))
	login.Header.Set("Content-Type", "application/json")
	lw := httptest.NewRecorder()
	handler.ServeHTTP(lw, login)
	req := httptest.NewRequest("GET", "/api/system", nil)
	for _, c := range lw.Result().Cookies() {
		req.AddCookie(c)
	}
	sw := httptest.NewRecorder()
	handler.ServeHTTP(sw, req)
	if sw.Code != 200 || !strings.Contains(sw.Body.String(), `"system"`) {
		t.Fatalf("/api/system response = %d %s", sw.Code, sw.Body.String())
	}
}

func TestFormatMemoryRatioRanges(t *testing.T) {
	const mib = 1024 * 1024
	const gib = 1024 * mib
	for _, tc := range []struct {
		used, total uint64
		want        string
	}{
		{0, 0, "--/--G"},
		{512 * mib, 980 * mib, "512/980M"},
		{2200 * mib, 8 * gib, "2.1/8G"},
		{13314398618, 32 * gib, "12.4/32G"},
		{1288490189, 1610612736, "1.2/1.5G"},
		{64 * gib, 128 * gib, "64/128G"},
		{40 * gib, 32 * gib, "32.0/32G"},
	} {
		if got := formatMemoryRatio(tc.used, tc.total); got != tc.want {
			t.Errorf("formatMemoryRatio(%d, %d) = %q, want %q", tc.used, tc.total, got, tc.want)
		}
	}
}
