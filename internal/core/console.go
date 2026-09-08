package core

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"
	_ "modernc.org/sqlite"
	orbitv1 "orbit/gen/go/orbit/v1"
	"orbit/internal/config"
)

//go:embed console/*
var consoleAssets embed.FS

// RouteStore stores one atomic, versioned routing document. An empty document is
// intentional and must never cause YAML routes to be imported again.
type RouteStore struct{ db *sql.DB }
type RouteDocument struct {
	Revision int64                             `json:"revision"`
	Routes   map[string]config.ProjectionRoute `json:"routes"`
}

func OpenRouteStore(path string, seed map[string]config.ProjectionRoute) (*RouteStore, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	store := &RouteStore{db: db}
	fail := func(err error) (*RouteStore, error) { db.Close(); return nil, err }
	if _, err = db.Exec(`PRAGMA busy_timeout=5000; PRAGMA journal_mode=WAL; CREATE TABLE IF NOT EXISTS routing_config (id INTEGER PRIMARY KEY CHECK (id=1), revision INTEGER NOT NULL, document TEXT NOT NULL);`); err != nil {
		return fail(err)
	}
	if seed == nil {
		seed = map[string]config.ProjectionRoute{}
	}
	data, err := json.Marshal(seed)
	if err != nil {
		return fail(err)
	}
	if _, err = db.Exec(`INSERT OR IGNORE INTO routing_config(id,revision,document) VALUES(1,1,?)`, string(data)); err != nil {
		return fail(err)
	}
	return store, nil
}
func (s *RouteStore) Close() error { return s.db.Close() }
func (s *RouteStore) Load() (RouteDocument, error) {
	var d RouteDocument
	var data string
	err := s.db.QueryRow(`SELECT revision,document FROM routing_config WHERE id=1`).Scan(&d.Revision, &data)
	if err != nil {
		return d, err
	}
	err = json.Unmarshal([]byte(data), &d.Routes)
	return d, err
}

var errRouteConflict = errors.New("rules changed; reload before saving")

func (s *RouteStore) save(d RouteDocument) error {
	data, err := json.Marshal(d.Routes)
	if err != nil {
		return err
	}
	result, err := s.db.Exec(`UPDATE routing_config SET revision=revision+1,document=? WHERE id=1 AND revision=?`, string(data), d.Revision)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return errRouteConflict
	}
	return nil
}

func RoutesFromConfig(routes map[string]config.ProjectionRoute) []Route {
	result := make([]Route, 0, len(routes))
	for id, r := range routes {
		item := Route{NodeID: id, Profile: r.Profile}
		for _, input := range r.Inputs {
			kind := orbitv1.ObservationType_OBSERVATION_TYPE_USAGE
			if input.ObservationType == "codex" {
				kind = orbitv1.ObservationType_OBSERVATION_TYPE_CODEX
			}
			item.Inputs = append(item.Inputs, RouteInput{AgentID: input.AgentID, ObservationType: kind})
		}
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].NodeID < result[j].NodeID })
	return result
}

// replaceRoutes holds the engine lock across validation and durable commit. The
// runner serializes this with MQTT handling and publication to preserve ordering.
func (e *Engine) replaceRoutes(now time.Time, routes []Route, persist func() error) ([]*orbitv1.DeviceView, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	next := e.config
	next.Routes = routes
	if _, err := New(next); err != nil {
		return nil, err
	}
	for _, route := range routes {
		if node := e.nodeProducts[route.NodeID]; node != nil {
			model := map[string]string{usageProfile: oledModel, webProfile: webModel, androidProfile: androidModel}[route.Profile]
			if node.ModelId != model {
				return nil, fmt.Errorf("node %s model %s does not match %s", route.NodeID, node.ModelId, route.Profile)
			}
		}
	}
	if err := persist(); err != nil {
		return nil, err
	}
	old := e.config.Routes
	e.config.Routes = routes
	e.lastFresh = make(map[string]string)
	e.lastAndroid = make(map[string]*orbitv1.DeviceView)
	var views []*orbitv1.DeviceView
	ids := map[string]bool{}
	for _, r := range old {
		ids[r.NodeID] = true
	}
	for _, r := range routes {
		ids[r.NodeID] = true
	}
	for id := range ids {
		projected, _ := e.projectNodeLocked(now, id)
		if len(projected) > 0 {
			views = append(views, projected...)
			continue
		}
		// An explicit empty stale view clears old upstream data on removed routes
		// and on routes whose new source has not published yet.
		views = append(views, e.emptyNodeViewLocked(now, id))
	}
	for _, view := range views {
		if view.Primary == nil {
			view.Primary = &orbitv1.DisplaySlot{Text: "--"}
		}
		if view.Secondary == nil {
			view.Secondary = &orbitv1.DisplaySlot{Text: "--"}
		}
		if view.Footer == nil {
			view.Footer = &orbitv1.DisplaySlot{Text: "--"}
		}
		if view.Usage == nil {
			view.Usage = &orbitv1.UsageView{Freshness: orbitv1.Freshness_FRESHNESS_STALE, FreshUntil: timestamppb.New(now)}
		}
		if view.Codex == nil {
			view.Codex = &orbitv1.CodexView{Freshness: orbitv1.Freshness_FRESHNESS_STALE, FreshUntil: timestamppb.New(now)}
		}
	}
	return views, nil
}

func (e *Engine) consoleState(now time.Time) map[string]any {
	e.mu.Lock()
	defer e.mu.Unlock()
	agents := make([]map[string]any, 0, len(e.agentDetails))
	nodes := make([]json.RawMessage, 0, len(e.nodeProducts))
	for id, state := range e.agentDetails {
		data, _ := protojson.Marshal(state)
		agents = append(agents, map[string]any{"id": id, "state": json.RawMessage(data), "usage_fresh": e.usage[id].expiresAt.After(now), "codex_fresh": e.codex[id].expiresAt.After(now)})
	}
	sort.Slice(agents, func(i, j int) bool { return agents[i]["id"].(string) < agents[j]["id"].(string) })
	ids := make([]string, 0, len(e.nodeProducts))
	for id := range e.nodeProducts {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		data, _ := protojson.Marshal(e.nodeProducts[id])
		nodes = append(nodes, data)
	}
	return map[string]any{"core_id": e.config.CoreID, "core_epoch": e.config.CoreEpoch, "agents": agents, "nodes": nodes, "now": now}
}

func ConsoleHandler(runner *Runner, store *RouteStore, cfg *config.CoreConfig) http.Handler {
	assets, _ := fs.Sub(consoleAssets, "console")
	mux := http.NewServeMux()
	mux.Handle("GET /", http.FileServer(http.FS(assets)))
	mux.HandleFunc("GET /api/state", func(w http.ResponseWriter, r *http.Request) {
		writeConsoleJSON(w, runner.engine.consoleState(runner.now()))
	})
	mux.HandleFunc("GET /api/routes", func(w http.ResponseWriter, r *http.Request) {
		d, err := store.Load()
		if err != nil {
			http.Error(w, "cannot load rules", 500)
			return
		}
		writeConsoleJSON(w, d)
	})
	mux.HandleFunc("PUT /api/routes", func(w http.ResponseWriter, r *http.Request) {
		media, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if media != "application/json" {
			http.Error(w, "expected application/json", 415)
			return
		}
		var d RouteDocument
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&d); err != nil {
			http.Error(w, "invalid routing document", 400)
			return
		}
		if decoder.Decode(&struct{}{}) != io.EOF || d.Routes == nil {
			http.Error(w, "invalid routing document", 400)
			return
		}
		if err := cfg.ValidateRoutes(d.Routes); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		runner.operations.Lock()
		defer runner.operations.Unlock()
		views, err := runner.engine.replaceRoutes(runner.now(), RoutesFromConfig(d.Routes), func() error { return store.save(d) })
		if err != nil {
			status := 400
			if errors.Is(err, errRouteConflict) {
				status = 409
			}
			http.Error(w, err.Error(), status)
			return
		}
		// Use a bounded context independent of a browser disconnect after commit.
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		publishErr := runner.publishViews(ctx, views)
		d.Revision++
		writeConsoleJSON(w, map[string]any{"revision": d.Revision, "routes": d.Routes, "publish_pending": publishErr != nil})
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'none'; base-uri 'none'")
		if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			if err != nil || u.Host != r.Host {
				http.Error(w, "cross-origin request rejected", 403)
				return
			}
		}
		if cfg.Console.Password == "" {
			http.Error(w, "console authentication is not configured", http.StatusServiceUnavailable)
			return
		}
		{
			_, password, ok := r.BasicAuth()
			actual := sha256.Sum256([]byte(password))
			expected := sha256.Sum256([]byte(cfg.Console.Password))
			if !ok || subtle.ConstantTimeCompare(actual[:], expected[:]) != 1 {
				w.Header().Set("WWW-Authenticate", `Basic realm="Orbit Core", charset="UTF-8"`)
				http.Error(w, "authentication required", 401)
				return
			}
		}
		mux.ServeHTTP(w, r)
	})
}
func writeConsoleJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}
