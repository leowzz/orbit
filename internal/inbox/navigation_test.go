package inbox

import (
	"context"
	v "orbit/gen/go/orbit/v1"
	"testing"
)

func TestNavigationTracksAgentOrderTitlesAndRouteRemoval(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	c := &v.AgentShortcuts{AgentId: "mac", Revision: 1, Items: []*v.AgentShortcut{
		{Id: "one", Title: "One", Kind: "app"}, {Id: "two", Title: "Two", Kind: "url"},
	}}
	if err := s.AcceptShortcutCatalog(ctx, c); err != nil {
		t.Fatal(err)
	}
	first, err := s.ProjectNavigation(ctx, "node", []string{"mac", "mac"})
	if err != nil || len(first.Items) != 2 || first.ShowTitles {
		t.Fatal(first, err)
	}
	c.Revision++
	c.ShowTitles = true
	c.Items[0], c.Items[1] = c.Items[1], c.Items[0]
	if err := s.AcceptShortcutCatalog(ctx, c); err != nil {
		t.Fatal(err)
	}
	next, err := s.ProjectNavigation(ctx, "node", []string{"mac"})
	if err != nil || next.Revision != first.Revision+1 || !next.ShowTitles || next.Items[0].ID != first.Items[1].ID {
		t.Fatal(next, err)
	}
	again, err := s.ProjectNavigation(ctx, "node", []string{"mac"})
	if err != nil || again.Revision != next.Revision {
		t.Fatal(again, err)
	}
	cleared, err := s.ProjectNavigation(ctx, "node", nil)
	if err != nil || len(cleared.Items) != 0 || cleared.Revision != next.Revision+1 {
		t.Fatal(cleared, err)
	}
	if _, _, _, err := s.ResolveShortcut(ctx, "node", first.Items[0].ID); err == nil {
		t.Fatal("deleted route retained open access")
	}
}

func TestNavigationPositionChangesWithoutChangingItems(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	c := &v.AgentShortcuts{AgentId: "mac", Items: []*v.AgentShortcut{{Id: "one", Title: "One", Kind: "app"}}}
	for i, atTop := range []bool{false, true, false} {
		c.Revision = uint64(i + 1)
		c.AtTop = atTop
		if err := s.AcceptShortcutCatalog(ctx, c); err != nil {
			t.Fatal(err)
		}
		d, err := s.ProjectNavigation(ctx, "node", []string{"mac"})
		if err != nil || d.AtTop != atTop || d.Revision != int64(i+1) || d.Proto("node").AtTop != atTop {
			t.Fatal("position update did not reach navigation", d, err)
		}
		if saved, err := s.Navigation(ctx, "node"); err != nil || saved.AtTop != atTop {
			t.Fatal("position not persisted", saved, err)
		}
	}
}
