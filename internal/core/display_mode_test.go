package core

import (
	"google.golang.org/protobuf/proto"
	orbitv1 "orbit/gen/go/orbit/v1"
	"testing"
	"time"
)

func TestDisplayModesAcrossDestinations(t *testing.T) {
	now := time.Now().UTC()
	for _, profile := range []string{usageProfile, webProfile, androidProfile, appProfile} {
		t.Run(profile, func(t *testing.T) {
			e := rotatingEngine(t, now, 3)
			route := e.config.Routes[0]
			route.Profile, route.DisplayMode = profile, "rotating"
			e.config.Routes = []Route{route}
			if _, err := New(e.config); err != nil {
				t.Fatal(err)
			}
			for _, tick := range []int{0, 3, 6} {
				views, err := e.projectRouteLocked(now.Add(time.Duration(tick)*time.Second), route)
				if err != nil || len(views) != 1 {
					t.Fatalf("tick %d: %v %v", tick, views, err)
				}
				v := views[0]
				want := "usage"
				if tick == 3 {
					want = "codex"
				}
				if v.DisplayKind != want {
					t.Fatalf("got %s want %s", v.DisplayKind, want)
				}
				if want == "codex" {
					if v.Primary.Text != "50%" || v.Usage != nil {
						t.Fatal("quota page retained usage", v)
					}
					if profile != usageProfile && v.GetCodex().WeeklyLimit == nil {
						t.Fatal("missing quota payload")
					}
				} else if v.Primary.Text != "$12.35" || v.Codex != nil {
					t.Fatal("usage page retained quota", v)
				}
			}
			// Fixed pages preserve both configured sources, even on OLED.
			route.DisplayMode, route.RotationSeconds = "usage", 0
			e.config.Routes = []Route{route}
			if _, err := New(e.config); err != nil {
				t.Fatal(err)
			}
			if len(route.Inputs) != 2 {
				t.Fatal("lost configured sources")
			}
			delete(e.usage, "usage-agent")
			views, _ := e.projectRouteLocked(now.Add(9*time.Second), route)
			if len(views) != 1 || views[0].DisplayKind != "usage" || views[0].Usage != nil || views[0].Primary.Text != "--" || views[0].Freshness != orbitv1.Freshness_FRESHNESS_STALE {
				t.Fatal("missing selected source retained old page", views)
			}
		})
	}
}

func TestEmptyAndroidPagesStillRotate(t *testing.T) {
	now := time.Now().UTC()
	e := rotatingEngine(t, now, 3)
	route := e.config.Routes[0]
	route.Profile, route.DisplayMode = androidProfile, "rotating"
	delete(e.usage, "usage-agent")
	delete(e.codex, "codex-agent")
	first, _ := e.projectRouteLocked(now, route)
	next, _ := e.projectRouteLocked(now.Add(3*time.Second), route)
	if len(first) != 1 || len(next) != 1 || next[0].DisplayKind != "codex" || next[0].Primary.Text != "--%" {
		t.Fatal("empty page transition was coalesced", first, next)
	}
}

func TestSessionSwitchIndependentOfMetrics(t *testing.T) {
	now := time.Now().UTC()
	for _, profile := range []string{webProfile, androidProfile, appProfile} {
		for _, mode := range []string{"usage", "codex", "rotating"} {
			t.Run(profile+"/"+mode, func(t *testing.T) {
				e := rotatingEngine(t, now, 3)
				source := e.codex["codex-agent"].value
				source.TotalCount, source.RunningCount = 5, 2
				source.Sessions = []*orbitv1.CodexSession{{SessionId: "session", DisplayName: "Details", ProjectName: "orbit"}}
				route := e.config.Routes[0]
				enabled := true
				route.Profile, route.DisplayMode, route.SessionsEnabled = profile, mode, &enabled
				if mode != "rotating" {
					route.RotationSeconds = 0
				}
				e.config.Routes = []Route{route}
				if _, err := New(e.config); err != nil {
					t.Fatal(err)
				}
				for _, tick := range []int{0, 3} {
					views, err := e.projectRouteLocked(now.Add(time.Duration(tick)*time.Second), route)
					if err != nil {
						t.Fatal(err)
					}
					if len(views) == 0 && profile == androidProfile && mode != "rotating" {
						continue
					}
					if len(views) != 1 {
						t.Fatal(views)
					}
					v := views[0]
					want := mode
					if mode == "rotating" {
						want = "usage"
						if tick == 3 {
							want = "codex"
						}
					}
					if v.DisplayKind != want || !v.GetSessionsEnabled() || len(v.GetCodex().GetSessions()) != 1 || v.Codex.TotalCount != 5 {
						t.Fatal("lost sessions or replaced metrics", v)
					}
					if want == "usage" && (v.Usage == nil || v.Codex.WeeklyLimit != nil || v.Primary.Text != "$12.35") {
						t.Fatal(v)
					}
					if want == "codex" && (v.Usage != nil || v.Codex.WeeklyLimit == nil || v.Primary.Text != "50%") {
						t.Fatal(v)
					}
				}
				enabled = false
				views, _ := e.projectRouteLocked(now.Add(4*time.Second), route)
				if len(views) != 1 || views[0].GetSessionsEnabled() || len(views[0].GetCodex().GetSessions()) != 0 || views[0].GetCodex().GetTotalCount() != 0 {
					t.Fatal("disabled sessions retained details", views)
				}
				if len(source.Sessions) != 1 || source.WeeklyLimit == nil {
					t.Fatal("canonical source mutated")
				}
				enabled = true
				delete(e.codex, "codex-agent")
				views, _ = e.projectRouteLocked(now.Add(5*time.Second), route)
				if len(views) != 1 || !views[0].GetSessionsEnabled() || views[0].Codex != nil {
					t.Fatal("missing source retained old sessions", views)
				}
			})
		}
	}
}

func TestLegacySessionModeBecomesQuotaWithSessionDetails(t *testing.T) {
	now := time.Now().UTC()
	e := rotatingEngine(t, now, 3)
	route := e.config.Routes[0]
	route.Profile = webProfile
	route.DisplayMode = "sessions"
	route.RotationSeconds = 0
	views, _ := e.projectRouteLocked(now, route)
	if len(views) != 1 || views[0].DisplayKind != "codex" || !views[0].GetSessionsEnabled() {
		t.Fatal(views)
	}
}

func TestNFCDPaddingPublishesOverrideAndRemoval(t *testing.T) {
	now := time.Now().UTC()
	e := rotatingEngine(t, now, 3)
	route := e.config.Routes[0]
	route.Profile, route.DisplayMode, route.RotationSeconds = appProfile, "codex", 0
	var previous *orbitv1.DeviceView
	for i, padding := range []*uint32{nil, proto.Uint32(0), proto.Uint32(64), nil} {
		route.NFCDMetricsLeftPadding = padding
		views, err := e.projectRouteLocked(now.Add(time.Duration(i)*time.Second), route)
		if err != nil || len(views) != 1 {
			t.Fatal(views, err)
		}
		view := views[0]
		if (view.NfcdMetricsLeftPadding == nil) != (padding == nil) || (padding != nil && view.GetNfcdMetricsLeftPadding() != *padding) {
			t.Fatal("missing device override", view)
		}
		if previous != nil && !publishAndroid(now.Add(time.Duration(i)*time.Second), previous, view) {
			t.Fatal("padding change delayed")
		}
		previous = view
	}
}
