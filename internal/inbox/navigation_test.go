package inbox

import (
	"context"
	"encoding/json"
	"google.golang.org/protobuf/proto"
	v "orbit/gen/go/orbit/v1"
	"testing"
)

func navigationStore(t *testing.T) *Store {
	t.Helper()
	s, e := Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	return s
}
func savePairs(t *testing.T, s *Store, node string, pairs ...Pairing) NavigationSettings {
	t.Helper()
	c, e := s.NavigationSettings(context.Background(), node)
	if e != nil {
		t.Fatal(e)
	}
	c.Pairs = pairs
	c, e = s.SaveNavigationSettings(context.Background(), node, c)
	if e != nil {
		t.Fatal(e)
	}
	return c
}
func TestIndependentPairingsMultipleExecutorsAndRevocation(t *testing.T) {
	s := navigationStore(t)
	ctx := context.Background()
	for _, id := range []string{"mac", "other"} {
		if e := s.AcceptShortcutCatalog(ctx, &v.AgentShortcuts{AgentId: id, Revision: 1, Items: []*v.AgentShortcut{{Id: "one", Title: id, Kind: "app"}, {Id: "two", Title: "Second", Kind: "url"}}}); e != nil {
			t.Fatal(e)
		}
	}
	if e := s.AcceptNodeCatalog(ctx, "tablet", "Tablet", 3, []NavigationItem{{ID: "one", Title: "Browser", Kind: "url"}}); e != nil {
		t.Fatal(e)
	}
	// Agent and Node identities are namespaced, even for identical endpoint/item IDs.
	savePairs(t, s, "screen", Pairing{"agent", "mac", true, []string{"two", "one"}}, Pairing{"agent", "other", false, nil}, Pairing{"node", "tablet", true, nil})
	d, e := s.ProjectNavigation(ctx, "screen")
	if e != nil || len(d.Items) != 3 || d.Items[0].ShortcutID != "two" || d.Items[2].ExecutorKind != "node" {
		t.Fatal(d, e)
	}
	a, e := s.ResolveShortcut(ctx, "screen", d.Items[2].ID)
	if e != nil || a.Kind != "node" || a.ID != "tablet" || a.Revision != 3 {
		t.Fatal(a, e)
	}
	other, _ := s.ProjectNavigation(ctx, "unpaired")
	if len(other.Items) != 0 {
		t.Fatal("unpaired receiver inherited entries")
	}
	cfg, _ := s.NavigationSettings(ctx, "screen")
	cfg.Pairs[0].Enabled = false
	if _, e = s.SaveNavigationSettings(ctx, "screen", cfg); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ResolveShortcut(ctx, "screen", d.Items[0].ID); e == nil {
		t.Fatal("disabled pair remained executable")
	}
	// Removing a catalog entry revokes it without changing the pairing.
	if e = s.AcceptNodeCatalog(ctx, "tablet", "Tablet", 4, nil); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ResolveShortcut(ctx, "screen", d.Items[2].ID); e == nil {
		t.Fatal("removed entry remained executable")
	}
}
func TestNavigationMigrationFreezesEffectiveDisplayAndPairings(t *testing.T) {
	s := navigationStore(t)
	ctx := context.Background()
	if e := s.AcceptShortcutCatalog(ctx, &v.AgentShortcuts{AgentId: "mac", Revision: 1, ShowTitles: true, AtTop: true, Items: []*v.AgentShortcut{{Id: "one", Title: "One", Kind: "app"}}}); e != nil {
		t.Fatal(e)
	}
	old := Navigation{Revision: 8, ShowTitles: true, AtTop: true, NFCDMetricsLeftPadding: proto.Uint32(23), Items: []NavigationItem{{ID: "old", AgentID: "mac", ShortcutID: "one"}}}
	raw, _ := json.Marshal(old)
	if _, e := s.db.Exec(`INSERT INTO navigation_configs VALUES(?,?,?)`, "screen", 8, string(raw)); e != nil {
		t.Fatal(e)
	}
	if e := s.MigrateNavigationSettings(ctx, map[string]*uint32{"screen": proto.Uint32(0)}); e != nil {
		t.Fatal(e)
	}
	cfg, e := s.NavigationSettings(ctx, "screen")
	if e != nil || !cfg.ShowTitles || !cfg.AtTop || cfg.NFCDMetricsLeftPadding != 0 || len(cfg.Pairs) != 1 {
		t.Fatal(cfg, e)
	}
	if e = s.AcceptShortcutCatalog(ctx, &v.AgentShortcuts{AgentId: "mac", Revision: 2, Items: []*v.AgentShortcut{{Id: "one", Title: "Changed", Kind: "app"}}}); e != nil {
		t.Fatal(e)
	}
	d, e := s.ProjectNavigation(ctx, "screen")
	if e != nil || !d.ShowTitles || !d.AtTop || *d.NFCDMetricsLeftPadding != 0 || len(d.Items) != 1 {
		t.Fatal(d, e)
	}
	if e = s.MigrateNavigationSettings(ctx, map[string]*uint32{"screen": proto.Uint32(64)}); e != nil {
		t.Fatal(e)
	}
	cfg, _ = s.NavigationSettings(ctx, "screen")
	if cfg.NFCDMetricsLeftPadding != 0 {
		t.Fatal("migration ran twice")
	}
}
func TestNavigationSettingsCASSelectionAndPersistence(t *testing.T) {
	dir := t.TempDir()
	s, e := Open(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	ctx := context.Background()
	_ = s.AcceptShortcutCatalog(ctx, &v.AgentShortcuts{AgentId: "mac", Revision: 1, Items: []*v.AgentShortcut{{Id: "one", Title: "One", Kind: "app"}}})
	cfg := savePairs(t, s, "screen", Pairing{"agent", "mac", true, []string{}})
	d, _ := s.ProjectNavigation(ctx, "screen")
	if len(d.Items) != 0 {
		t.Fatal("empty selection meant all")
	}
	cfg.Pairs[0].Items = []string{"unknown"}
	if _, e = s.SaveNavigationSettings(ctx, "screen", cfg); e == nil {
		t.Fatal("unknown shortcut accepted")
	}
	cfg.Pairs[0].Items = nil
	cfg.ShowTitles = true
	cfg.NFCDMetricsLeftPadding = 64
	saved, e := s.SaveNavigationSettings(ctx, "screen", cfg)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.SaveNavigationSettings(ctx, "screen", cfg); e == nil {
		t.Fatal("stale revision overwrote settings")
	}
	cfg = saved
	cfg.NFCDMetricsLeftPadding = 65
	if _, e = s.SaveNavigationSettings(ctx, "screen", cfg); e == nil {
		t.Fatal("invalid padding accepted")
	}
	second, e := Open(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer second.Close()
	again, e := second.NavigationSettings(ctx, "screen")
	if e != nil || again.Revision != saved.Revision || !again.ShowTitles || again.NFCDMetricsLeftPadding != 64 {
		t.Fatal(again, e)
	}
}

func TestMetricsDisplaySettingsDefaultsPersistenceAndLegacyWrites(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	// Settings saved by an older Core have no metrics display fields.
	if _, err = s.db.Exec(`INSERT INTO navigation_settings VALUES(?,?,?)`, "screen", 1, `{"revision":1,"pairs":[]}`); err != nil {
		t.Fatal(err)
	}
	cfg, err := s.NavigationSettings(ctx, "screen")
	if err != nil || cfg.MetricsVisible == nil || !*cfg.MetricsVisible || cfg.MetricsWidth == nil || *cfg.MetricsWidth != 150 {
		t.Fatal(cfg, err)
	}
	for _, width := range []uint32{0, 95, 321} {
		invalid := cfg
		invalid.MetricsWidth = proto.Uint32(width)
		if _, err = s.SaveNavigationSettings(ctx, "screen", invalid); err == nil {
			t.Fatalf("accepted width %d", width)
		}
	}
	cfg.MetricsVisible, cfg.MetricsWidth = proto.Bool(false), proto.Uint32(96)
	cfg, err = s.SaveNavigationSettings(ctx, "screen", cfg)
	if err != nil {
		t.Fatal(err)
	}
	// An older console changing another setting must not restore the metrics.
	cfg.MetricsVisible, cfg.MetricsWidth = nil, nil
	cfg.ShowTitles = true
	if _, err = s.SaveNavigationSettings(ctx, "screen", cfg); err != nil {
		t.Fatal(err)
	}
	second, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	nav, err := second.ProjectNavigation(ctx, "screen")
	if err != nil || nav.MetricsVisible == nil || *nav.MetricsVisible || nav.MetricsWidth == nil || *nav.MetricsWidth != 96 {
		t.Fatal(nav, err)
	}
	wire := nav.Proto("screen")
	if wire.MetricsVisible == nil || wire.GetMetricsVisible() || wire.GetMetricsWidth() != 96 {
		t.Fatal(wire)
	}
	raw, err := json.Marshal(nav)
	var payload map[string]any
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["metrics_visible"] != false || payload["metrics_width"] != float64(96) {
		t.Fatal(payload)
	}
}
func TestNodeCatalogKeepsTargetsLocalAndRejectsStaleRevision(t *testing.T) {
	s := navigationStore(t)
	ctx := context.Background()
	if e := s.AcceptNodeCatalog(ctx, "node", "Node", 2, []NavigationItem{{ID: "one", Title: "One", Kind: "url"}}); e != nil {
		t.Fatal(e)
	}
	if e := s.AcceptNodeCatalog(ctx, "node", "Node", 2, []NavigationItem{{ID: "one", Title: "One", Kind: "url"}}); e != nil {
		t.Fatal("idempotent catalog failed", e)
	}
	if e := s.AcceptNodeCatalog(ctx, "node", "Node", 2, nil); e == nil {
		t.Fatal("same revision changed catalog")
	}
	if e := s.AcceptNodeCatalog(ctx, "node", "Node", 1, nil); e == nil {
		t.Fatal("stale catalog accepted")
	}
	if e := s.AcceptNodeCatalog(ctx, "node", "Node", 3, []NavigationItem{{ID: "one", Title: "One", Kind: "url", Target: "https://example.com"}}); e == nil {
		t.Fatal("execution target uploaded")
	}
	if _, e := s.SaveNavigationIcon(ctx, []byte("<svg/>")); e == nil {
		t.Fatal("unsafe image accepted")
	}
}
func TestDeleteExecutingNodeRemovesAllRelationships(t *testing.T) {
	s := navigationStore(t)
	ctx := context.Background()
	_ = s.AcceptNodeCatalog(ctx, "tablet", "Tablet", 1, nil)
	savePairs(t, s, "receiver", Pairing{"node", "tablet", true, nil})
	if e := s.DeleteNodeNavigation(ctx, "tablet", []byte(`{}`)); e != nil {
		t.Fatal(e)
	}
	c, _ := s.NavigationSettings(ctx, "receiver")
	if len(c.Pairs) != 0 || c.Revision != 3 {
		t.Fatal(c)
	}
	cats, _ := s.ShortcutCatalogs(ctx)
	if len(cats) != 0 {
		t.Fatal(cats)
	}
}
