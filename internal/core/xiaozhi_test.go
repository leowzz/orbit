package core

import (
	"google.golang.org/protobuf/proto"
	orbitv1 "orbit/gen/go/orbit/v1"
	"testing"
	"time"
)

func TestXiaozhiProductAndRouting(t *testing.T) {
	now := time.Now().UTC()
	e := rotatingEngine(t, now, 3)
	route := e.config.Routes[0]
	route.Profile, route.DisplayMode = xiaozhiProfile, "rotating"
	e.config.Routes = []Route{route}
	delete(e.nodeProducts, route.NodeID)
	state := testNodeState(now)
	state.NodeId, state.Metadata.ProducerId = route.NodeID, route.NodeID
	state.ModelId, state.VariantId = xiaozhiModel, xiaozhiVariant
	views, err := e.ApplyNodeState(now, state)
	if err != nil || len(views) != 1 || views[0].Primary.Text != "$12.3" {
		t.Fatal(views, err)
	}
	views, err = e.Refresh(now.Add(3 * time.Second))
	if err != nil || len(views) != 1 || views[0].DisplayKind != "codex" || views[0].Primary.Text != "50%" {
		t.Fatal(views, err)
	}
	if len(views[0].GetCodex().GetSessions()) != 0 || proto.Size(views[0]) > 1024 {
		t.Fatal("physical display payload exceeded contract")
	}
	wrong := proto.Clone(state).(*orbitv1.NodeState)
	wrong.ModelId, wrong.VariantId = oledModel, ydVariant
	wrong.Metadata.Revision++
	if _, err = e.ApplyNodeState(now, wrong); err == nil {
		t.Fatal("accepted wrong hardware for xiaozhi route")
	}
	badRoute := route
	badRoute.Profile = rotatingOLEDProfile
	if _, err = e.replaceRoutes(now, []Route{badRoute}, func() error { t.Fatal("persisted incompatible route"); return nil }); err == nil {
		t.Fatal("accepted incompatible route")
	}
	route.DisplayMode, route.RotationSeconds = "codex", 0
	if _, err = e.replaceRoutes(now, []Route{route}, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	delete(e.codex, "codex-agent")
	views, err = e.Refresh(now.Add(4 * time.Second))
	if err != nil || len(views) != 1 || views[0].Primary.Text != "--%" || views[0].Freshness != orbitv1.Freshness_FRESHNESS_STALE {
		t.Fatal("missing quota is not stale", views, err)
	}
}

func TestXiaozhiCostRoundsSourceToOneDecimal(t *testing.T) {
	now := time.Now().UTC()
	e := rotatingEngine(t, now, 3)
	route := e.config.Routes[0]
	route.Profile, route.DisplayMode, route.RotationSeconds = xiaozhiProfile, "usage", 0
	for _, tc := range []struct {
		micros int64
		want   string
	}{
		{0, "$0.0"}, {20_230_000, "$20.2"},
		{20_249_999, "$20.2"}, {20_250_000, "$20.3"},
		{9_950_000, "$10.0"}, {99_950_000, "$100.0"},
		{100_000_000_000, "$99999+"},
	} {
		e.usage["usage-agent"].value.ActualCostMicros = proto.Int64(tc.micros)
		views, err := e.projectRouteLocked(now, route)
		if err != nil || len(views) != 1 || views[0].Primary.Text != tc.want {
			t.Fatalf("micros=%d want=%s views=%v err=%v", tc.micros, tc.want, views, err)
		}
	}
}

func TestXiaozhiRejectsUnsupportedDisplayModes(t *testing.T) {
	e := rotatingEngine(t, time.Now(), 3)
	e.config.Routes[0].Profile = xiaozhiProfile
	for _, mode := range []string{"", "sessions"} {
		e.config.Routes[0].DisplayMode = mode
		if _, err := New(e.config); err == nil {
			t.Fatal("accepted unsupported mode", mode)
		}
	}
	e.config.Routes[0].DisplayMode = "rotating"
	e.config.Routes[0].SessionsEnabled = proto.Bool(true)
	if _, err := New(e.config); err == nil {
		t.Fatal("accepted unbounded sessions on physical screen")
	}
}
