package inbox

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image/png"
	"reflect"
	"regexp"
	"strings"

	v "orbit/gen/go/orbit/v1"
)

type NavigationItem struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Kind       string `json:"kind"`
	Target     string `json:"target,omitempty"`
	AgentID    string `json:"agent_id,omitempty"`
	ShortcutID string `json:"shortcut_id,omitempty"`
	IconURL    string `json:"icon_url,omitempty"`
	IconName   string `json:"icon_name,omitempty"`
}
type Navigation struct {
	Revision   int64            `json:"revision"`
	ShowTitles bool             `json:"show_titles"`
	Items      []NavigationItem `json:"items"`
}
type ShortcutCatalog struct {
	AgentID    string           `json:"agent_id"`
	HostLabel  string           `json:"host_label"`
	Revision   uint64           `json:"revision"`
	ShowTitles bool             `json:"show_titles"`
	Items      []NavigationItem `json:"items"`
}

var navID = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`)

func navigationFault() error { return &Fault{Code: "invalid_navigation"} }
func (s *Store) Navigation(ctx context.Context, node string) (Navigation, error) {
	d := Navigation{Items: []NavigationItem{}}
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT revision,payload FROM navigation_configs WHERE node_id=?`, node).Scan(&d.Revision, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return d, nil
	}
	if err != nil {
		return d, err
	}
	err = json.Unmarshal([]byte(raw), &d)
	return d, err
}
func (s *Store) NavigationNodes(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT node_id FROM navigation_configs ORDER BY node_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// ProjectNavigation persists a complete display snapshot derived exclusively from Agent settings.
func (s *Store) ProjectNavigation(ctx context.Context, node string, agents []string) (Navigation, error) {
	catalogs, err := s.ShortcutCatalogs(ctx)
	if err != nil {
		return Navigation{}, err
	}
	d := Navigation{Items: []NavigationItem{}}
	seen := map[string]bool{}
	for _, agent := range agents {
		if seen[agent] {
			continue
		}
		seen[agent] = true
		for _, cat := range catalogs {
			if cat.AgentID != agent {
				continue
			}
			d.ShowTitles = d.ShowTitles || cat.ShowTitles
			for _, entry := range cat.Items {
				item := entry
				sum := sha256.Sum256([]byte(agent + "\x00" + entry.ID))
				item.ID = hex.EncodeToString(sum[:])
				item.Kind, item.AgentID, item.ShortcutID = "agent", agent, entry.ID
				d.Items = append(d.Items, item)
			}
		}
	}
	old, err := s.Navigation(ctx, node)
	if err != nil {
		return d, err
	}
	d.Revision = old.Revision
	if reflect.DeepEqual(d, old) {
		return old, nil
	}
	d.Revision++
	raw, _ := json.Marshal(d)
	_, err = s.db.ExecContext(ctx, `INSERT INTO navigation_configs VALUES(?,?,?) ON CONFLICT(node_id) DO UPDATE SET revision=excluded.revision,payload=excluded.payload`, node, d.Revision, string(raw))
	return d, err
}
func (s *Store) SaveNavigationIcon(ctx context.Context, b []byte) (string, error) {
	if len(b) == 0 || len(b) > 32*1024 {
		return "", navigationFault()
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(b))
	if err != nil || cfg.Width > 256 || cfg.Height > 256 || cfg.Width < 1 || cfg.Height < 1 {
		return "", navigationFault()
	}
	if _, err = png.Decode(bytes.NewReader(b)); err != nil {
		return "", navigationFault()
	}
	sum := sha256.Sum256(b)
	id := hex.EncodeToString(sum[:])
	_, err = s.db.ExecContext(ctx, `INSERT OR IGNORE INTO navigation_icons VALUES(?,?)`, id, b)
	return "/api/v1/navigation/icons/" + id, err
}
func (s *Store) NavigationIcon(ctx context.Context, id string) ([]byte, error) {
	var b []byte
	err := s.db.QueryRowContext(ctx, `SELECT png FROM navigation_icons WHERE id=?`, id).Scan(&b)
	return b, err
}
func (s *Store) AcceptShortcutCatalog(ctx context.Context, c *v.AgentShortcuts) error {
	if !navID.MatchString(c.AgentId) || c.Revision == 0 || c.Revision > 1<<63-1 || len(c.Items) > 24 || len(c.HostLabel) > 192 {
		return navigationFault()
	}
	d := ShortcutCatalog{AgentID: c.AgentId, HostLabel: c.HostLabel, Revision: c.Revision, ShowTitles: c.ShowTitles, Items: []NavigationItem{}}
	seen := map[string]bool{}
	for _, item := range c.Items {
		if item == nil || !navID.MatchString(item.Id) || seen[item.Id] || strings.TrimSpace(item.Title) == "" || len(item.Title) > 160 || (item.Kind != "app" && item.Kind != "url") {
			return navigationFault()
		}
		seen[item.Id] = true
		icon := ""
		if len(item.IconPng) > 0 {
			var err error
			icon, err = s.SaveNavigationIcon(ctx, item.IconPng)
			if err != nil {
				return err
			}
		}
		d.Items = append(d.Items, NavigationItem{ID: item.Id, Title: item.Title, Kind: item.Kind, IconURL: icon, IconName: map[string]string{"app": "app", "url": "globe"}[item.Kind]})
	}
	raw, _ := json.Marshal(d)
	_, err := s.db.ExecContext(ctx, `INSERT INTO agent_shortcuts VALUES(?,?,?) ON CONFLICT(agent_id) DO UPDATE SET revision=excluded.revision,payload=excluded.payload WHERE excluded.revision>agent_shortcuts.revision`, c.AgentId, c.Revision, string(raw))
	return err
}
func (s *Store) ShortcutCatalogs(ctx context.Context) ([]ShortcutCatalog, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT payload FROM agent_shortcuts ORDER BY agent_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ShortcutCatalog{}
	for rows.Next() {
		var raw string
		var c ShortcutCatalog
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
func (s *Store) ResolveShortcut(ctx context.Context, node, id string) (string, string, uint64, error) {
	d, err := s.Navigation(ctx, node)
	if err != nil {
		return "", "", 0, err
	}
	catalogs, err := s.ShortcutCatalogs(ctx)
	if err != nil {
		return "", "", 0, err
	}
	for _, item := range d.Items {
		if item.ID == id && item.Kind == "agent" {
			for _, cat := range catalogs {
				if cat.AgentID == item.AgentID {
					for _, a := range cat.Items {
						if a.ID == item.ShortcutID {
							return cat.AgentID, a.ID, cat.Revision, nil
						}
					}
				}
			}
		}
	}
	return "", "", 0, &Fault{Code: "shortcut_unavailable"}
}
func (d Navigation) Proto(node string) *v.Navigation {
	out := &v.Navigation{NodeId: node, Revision: uint64(d.Revision), ShowTitles: d.ShowTitles}
	for _, i := range d.Items {
		out.Items = append(out.Items, &v.NavigationItem{Id: i.ID, Title: i.Title, Kind: i.Kind, Target: i.Target, AgentId: i.AgentID, ShortcutId: i.ShortcutID, IconUrl: i.IconURL, IconName: i.IconName})
	}
	return out
}
