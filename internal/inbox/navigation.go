package inbox

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"reflect"
	"regexp"
	"strings"

	v "orbit/gen/go/orbit/v1"
)

type NavigationItem struct {
	ExecutorKind  string `json:"executor_kind,omitempty"`
	ExecutorID    string `json:"executor_id,omitempty"`
	ExecutorLabel string `json:"executor_label,omitempty"`
	ID            string `json:"id"`
	Title         string `json:"title"`
	Kind          string `json:"kind"`
	Target        string `json:"target,omitempty"`
	AgentID       string `json:"agent_id,omitempty"`
	ShortcutID    string `json:"shortcut_id,omitempty"`
	IconURL       string `json:"icon_url,omitempty"`
	IconName      string `json:"icon_name,omitempty"`
}
type Navigation struct {
	NFCDMetricsLeftPadding *uint32          `json:"nfcd_metrics_left_padding,omitempty"`
	Revision               int64            `json:"revision"`
	ShowTitles             bool             `json:"show_titles"`
	AtTop                  bool             `json:"at_top"`
	Items                  []NavigationItem `json:"items"`
}
type ShortcutCatalog struct {
	ExecutorKind string           `json:"executor_kind"`
	ExecutorID   string           `json:"executor_id"`
	AgentID      string           `json:"agent_id,omitempty"`
	HostLabel    string           `json:"host_label"`
	Revision     uint64           `json:"revision"`
	Items        []NavigationItem `json:"items"`
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
	rows, err := s.db.QueryContext(ctx, `SELECT node_id FROM navigation_configs UNION SELECT node_id FROM navigation_settings ORDER BY node_id`)
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

// Pairing is ordered within a receiving device. Nil Items follows the entire
// endpoint catalog; an explicit empty list selects no entries.
type Pairing struct {
	ExecutorKind string   `json:"executor_kind"`
	ExecutorID   string   `json:"executor_id"`
	Enabled      bool     `json:"enabled"`
	Items        []string `json:"items"`
}
type NavigationSettings struct {
	Revision               int64     `json:"revision"`
	ShowTitles             bool      `json:"show_titles"`
	AtTop                  bool      `json:"at_top"`
	NFCDMetricsLeftPadding uint32    `json:"nfcd_metrics_left_padding"`
	Pairs                  []Pairing `json:"pairs"`
}

// NavigationSettings freezes the previous effective snapshot on first access.
// Subsequent projections never consult data routes or Agent display defaults.
func (s *Store) NavigationSettings(ctx context.Context, node string) (NavigationSettings, error) {
	return s.navigationSettings(ctx, node, nil)
}
func (s *Store) navigationSettings(ctx context.Context, node string, padding *uint32) (NavigationSettings, error) {
	d := NavigationSettings{Revision: 1, NFCDMetricsLeftPadding: 16, Pairs: []Pairing{}}
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT payload FROM navigation_settings WHERE node_id=?`, node).Scan(&raw)
	if err == nil {
		err = json.Unmarshal([]byte(raw), &d)
		return d, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return d, err
	}
	old, err := s.Navigation(ctx, node)
	if err != nil {
		return d, err
	}
	d.ShowTitles, d.AtTop = old.ShowTitles, old.AtTop
	if old.NFCDMetricsLeftPadding != nil {
		d.NFCDMetricsLeftPadding = *old.NFCDMetricsLeftPadding
	}
	if padding != nil {
		d.NFCDMetricsLeftPadding = *padding
	}
	seen := map[string]bool{}
	for _, item := range old.Items {
		if item.AgentID != "" && !seen[item.AgentID] {
			seen[item.AgentID] = true
			d.Pairs = append(d.Pairs, Pairing{ExecutorKind: "agent", ExecutorID: item.AgentID, Enabled: true})
		}
	}
	data, _ := json.Marshal(d)
	if _, err = s.db.ExecContext(ctx, `INSERT OR IGNORE INTO navigation_settings VALUES(?,?,?)`, node, d.Revision, string(data)); err != nil {
		return d, err
	}
	// Another caller may have created the settings concurrently.
	err = s.db.QueryRowContext(ctx, `SELECT payload FROM navigation_settings WHERE node_id=?`, node).Scan(&raw)
	if err == nil {
		err = json.Unmarshal([]byte(raw), &d)
	}
	return d, err
}
func (s *Store) MigrateNavigationSettings(ctx context.Context, paddings map[string]*uint32) error {
	nodes, err := s.NavigationNodes(ctx)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, id := range nodes {
		seen[id] = true
	}
	for id := range paddings {
		seen[id] = true
	}
	for id := range seen {
		if _, err := s.navigationSettings(ctx, id, paddings[id]); err != nil {
			return err
		}
	}
	return nil
}
func (s *Store) SaveNavigationSettings(ctx context.Context, node string, d NavigationSettings) (NavigationSettings, error) {
	if !navID.MatchString(node) || d.NFCDMetricsLeftPadding > 64 || len(d.Pairs) > 32 {
		return d, navigationFault()
	}
	catalogs, err := s.ShortcutCatalogs(ctx)
	if err != nil {
		return d, err
	}
	known := map[string]ShortcutCatalog{}
	for _, c := range catalogs {
		known[c.ExecutorKind+"/"+c.ExecutorID] = c
	}
	seen := map[string]bool{}
	for _, p := range d.Pairs {
		key := p.ExecutorKind + "/" + p.ExecutorID
		if (p.ExecutorKind != "agent" && p.ExecutorKind != "node") || !navID.MatchString(p.ExecutorID) || seen[key] || len(p.Items) > 24 {
			return d, navigationFault()
		}
		seen[key] = true
		cat, ok := known[key]
		if !ok {
			if !p.Enabled {
				continue
			}
			return d, &Fault{Code: "shortcut_unavailable"}
		}
		items := map[string]bool{}
		for _, item := range cat.Items {
			items[item.ID] = true
		}
		selected := map[string]bool{}
		for _, id := range p.Items {
			if !items[id] || selected[id] {
				return d, navigationFault()
			}
			selected[id] = true
		}
	}
	if d.Pairs == nil {
		d.Pairs = []Pairing{}
	}
	// Ensure the empty configuration exists before the compare-and-swap.
	if _, err = s.NavigationSettings(ctx, node); err != nil {
		return d, err
	}
	previous := d.Revision
	d.Revision++
	raw, _ := json.Marshal(d)
	result, err := s.db.ExecContext(ctx, `UPDATE navigation_settings SET revision=?,payload=? WHERE node_id=? AND revision=?`, d.Revision, string(raw), node, previous)
	if err != nil {
		return d, err
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return d, &Fault{Code: "navigation_conflict"}
	}
	return d, nil
}

func (s *Store) ProjectNavigation(ctx context.Context, node string) (Navigation, error) {
	cfg, err := s.NavigationSettings(ctx, node)
	if err != nil {
		return Navigation{}, err
	}
	catalogs, err := s.ShortcutCatalogs(ctx)
	if err != nil {
		return Navigation{}, err
	}
	d := Navigation{Items: []NavigationItem{}, ShowTitles: cfg.ShowTitles, AtTop: cfg.AtTop, NFCDMetricsLeftPadding: &cfg.NFCDMetricsLeftPadding}
	for _, pair := range cfg.Pairs {
		if !pair.Enabled {
			continue
		}
		for _, cat := range catalogs {
			if cat.ExecutorKind != pair.ExecutorKind || cat.ExecutorID != pair.ExecutorID {
				continue
			}
			entries := cat.Items
			if pair.Items != nil {
				entries = []NavigationItem{}
				for _, id := range pair.Items {
					for _, item := range cat.Items {
						if item.ID == id {
							entries = append(entries, item)
						}
					}
				}
			}
			for _, entry := range entries {
				item := entry
				identity := cat.ExecutorID
				if cat.ExecutorKind != "agent" {
					identity = cat.ExecutorKind + "/" + identity
				}
				sum := sha256.Sum256([]byte(identity + "\x00" + entry.ID))
				item.ID = hex.EncodeToString(sum[:])
				item.Kind = cat.ExecutorKind
				item.ExecutorKind, item.ExecutorID, item.ExecutorLabel = cat.ExecutorKind, cat.ExecutorID, cat.HostLabel
				if cat.ExecutorKind == "agent" {
					item.AgentID = cat.ExecutorID
				}
				item.ShortcutID = entry.ID
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
	if (c.NfcdMetricsLeftPadding != nil && *c.NfcdMetricsLeftPadding > 64) || !navID.MatchString(c.AgentId) || c.Revision == 0 || c.Revision > 1<<63-1 || len(c.Items) > 24 || len(c.HostLabel) > 192 {
		return navigationFault()
	}
	d := ShortcutCatalog{ExecutorKind: "agent", ExecutorID: c.AgentId, AgentID: c.AgentId, HostLabel: c.HostLabel, Revision: c.Revision, Items: []NavigationItem{}}
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
func (s *Store) AcceptNodeCatalog(ctx context.Context, node, label string, revision uint64, items []NavigationItem) error {
	if !navID.MatchString(node) || len(label) > 192 || revision == 0 || revision > 1<<63-1 || len(items) > 24 {
		return navigationFault()
	}
	seen := map[string]bool{}
	for _, item := range items {
		// Execution targets remain local to the Node, just as application paths stay on Agents.
		if !navID.MatchString(item.ID) || seen[item.ID] || strings.TrimSpace(item.Title) == "" || len(item.Title) > 160 || (item.Kind != "app" && item.Kind != "url") || item.Target != "" || item.AgentID != "" || item.ExecutorID != "" || item.ExecutorKind != "" || item.ExecutorLabel != "" || len(item.IconName) > 32 || item.ShortcutID != "" || item.IconURL != "" {
			return navigationFault()
		}
		seen[item.ID] = true
	}
	if items == nil {
		items = []NavigationItem{}
	}
	c := ShortcutCatalog{ExecutorKind: "node", ExecutorID: node, HostLabel: label, Revision: revision, Items: items}
	raw, _ := json.Marshal(c)
	result, err := s.db.ExecContext(ctx, `INSERT INTO node_shortcuts VALUES(?,?,?) ON CONFLICT(node_id) DO UPDATE SET revision=excluded.revision,payload=excluded.payload WHERE excluded.revision>node_shortcuts.revision`, node, revision, string(raw))
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		var current string
		if err := s.db.QueryRowContext(ctx, `SELECT payload FROM node_shortcuts WHERE node_id=?`, node).Scan(&current); err != nil {
			return err
		}
		if current == string(raw) {
			return nil
		}
		return &Fault{Code: "navigation_conflict"}
	}
	return nil
}
func (s *Store) ShortcutCatalogs(ctx context.Context) ([]ShortcutCatalog, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT 'agent',agent_id,payload FROM agent_shortcuts UNION ALL SELECT 'node',node_id,payload FROM node_shortcuts ORDER BY 1,2`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ShortcutCatalog{}
	for rows.Next() {
		var kind, id, raw string
		var c ShortcutCatalog
		if err := rows.Scan(&kind, &id, &raw); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(raw), &c); err != nil {
			return nil, err
		}
		c.ExecutorKind, c.ExecutorID = kind, id
		out = append(out, c)
	}
	return out, rows.Err()
}

type ResolvedShortcut struct {
	Kind       string
	ID         string
	ShortcutID string
	Revision   uint64
}

func (s *Store) ResolveShortcut(ctx context.Context, node, id string) (ResolvedShortcut, error) {
	// Reproject at invocation time so a removed pair or catalog entry immediately revokes access.
	d, err := s.ProjectNavigation(ctx, node)
	if err != nil {
		return ResolvedShortcut{}, err
	}
	catalogs, err := s.ShortcutCatalogs(ctx)
	if err != nil {
		return ResolvedShortcut{}, err
	}
	for _, item := range d.Items {
		if item.ID != id {
			continue
		}
		for _, cat := range catalogs {
			if cat.ExecutorKind == item.ExecutorKind && cat.ExecutorID == item.ExecutorID {
				for _, a := range cat.Items {
					if a.ID == item.ShortcutID {
						return ResolvedShortcut{cat.ExecutorKind, cat.ExecutorID, a.ID, cat.Revision}, nil
					}
				}
			}
		}
	}
	return ResolvedShortcut{}, &Fault{Code: "shortcut_unavailable"}
}

// DeleteNodeNavigation removes both receiving and executing relationships atomically.
func (s *Store) DeleteNodeNavigation(ctx context.Context, node string, registry []byte) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE app_registry SET payload=? WHERE id=1`, string(registry)); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT node_id,payload FROM navigation_settings`)
	if err != nil {
		return err
	}
	updates := map[string]NavigationSettings{}
	for rows.Next() {
		var id, raw string
		var cfg NavigationSettings
		if err = rows.Scan(&id, &raw); err != nil {
			rows.Close()
			return err
		}
		if err = json.Unmarshal([]byte(raw), &cfg); err != nil {
			rows.Close()
			return err
		}
		pairs := []Pairing{}
		for _, p := range cfg.Pairs {
			if p.ExecutorKind != "node" || p.ExecutorID != node {
				pairs = append(pairs, p)
			}
		}
		if len(pairs) != len(cfg.Pairs) {
			cfg.Pairs = pairs
			cfg.Revision++
			updates[id] = cfg
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for id, cfg := range updates {
		raw, _ := json.Marshal(cfg)
		if _, err = tx.ExecContext(ctx, `UPDATE navigation_settings SET revision=?,payload=? WHERE node_id=?`, cfg.Revision, string(raw), id); err != nil {
			return err
		}
	}
	for _, table := range []string{"navigation_settings", "navigation_configs", "node_shortcuts"} {
		if _, err = tx.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s WHERE node_id=?", table), node); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (d Navigation) Proto(node string) *v.Navigation {
	out := &v.Navigation{NodeId: node, Revision: uint64(d.Revision), ShowTitles: d.ShowTitles, AtTop: d.AtTop, NfcdMetricsLeftPadding: d.NFCDMetricsLeftPadding}
	for _, i := range d.Items {
		out.Items = append(out.Items, &v.NavigationItem{Id: i.ID, Title: i.Title, Kind: i.Kind, Target: i.Target, AgentId: i.AgentID, ShortcutId: i.ShortcutID, IconUrl: i.IconURL, IconName: i.IconName, ExecutorKind: i.ExecutorKind, ExecutorId: i.ExecutorID, ExecutorLabel: i.ExecutorLabel})
	}
	return out
}
