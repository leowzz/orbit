package appapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	v "orbit/gen/go/orbit/v1"
	"orbit/internal/inbox"
	"orbit/internal/mqtt"
)

func (s *Server) navigationRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/navigation", s.auth(func(w http.ResponseWriter, r *http.Request, node string) {
		s.navMu.Lock()
		d, err := s.projectNavigationLocked(r.Context(), node)
		s.navMu.Unlock()
		if err != nil {
			failure(w, err)
			return
		}
		write(w, 200, d)
	}))
	mux.HandleFunc("GET /api/v1/navigation/icons/{id}", s.auth(func(w http.ResponseWriter, r *http.Request, _ string) {
		b, err := s.store.NavigationIcon(r.Context(), r.PathValue("id"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Write(b)
	}))
	mux.HandleFunc("POST /api/v1/navigation/{id}/open", s.auth(s.openShortcut))
}
func (s *Server) projectNavigationLocked(ctx context.Context, node string) (inbox.Navigation, error) {
	return s.store.ProjectNavigation(ctx, node)
}
func (s *Server) openShortcut(w http.ResponseWriter, r *http.Request, node string) {
	s.navMu.Lock()
	commandID, err := s.dispatchShortcutLocked(r.Context(), node, r.PathValue("id"))
	s.navMu.Unlock()
	if err != nil {
		failure(w, err)
		return
	}
	s.notify()
	write(w, 202, map[string]string{"command_id": commandID, "status": "sent"})
}
func (s *Server) dispatchShortcutLocked(ctx context.Context, node, entry string) (string, error) {
	resolved, err := s.store.ResolveShortcut(ctx, node, entry)
	if err != nil {
		return "", err
	}
	now := time.Now().UTC()
	id := uuid.NewString()
	if resolved.Kind == "node" {
		s.mu.Lock()
		device, ok := s.devices[resolved.ID]
		s.mu.Unlock()
		if !ok || device.Revoked {
			return "", &inbox.Fault{Code: "shortcut_unavailable"}
		}
		queue := s.nodeCommands[resolved.ID]
		active := []nodeCommand{}
		for _, c := range queue {
			if c.ExpiresAt.After(now) {
				active = append(active, c)
			}
		}
		if len(active) >= 32 {
			return "", errors.New("execution queue is full")
		}
		s.nodeCommands[resolved.ID] = append(active, nodeCommand{ID: id, ShortcutID: resolved.ShortcutID, CatalogRevision: resolved.Revision, ExpiresAt: now.Add(15 * time.Second), Requester: node, Entry: entry})
		return id, nil
	}
	if s.navTransport == nil {
		return "", &inbox.Fault{Code: "agent_unavailable"}
	}
	s.navRevision++
	command := &v.Command{Metadata: &v.Metadata{MessageId: uuid.NewString(), ProducerId: s.navCoreID, Revision: s.navRevision, ProducedAt: timestamppb.New(now), ExpiresAt: timestamppb.New(now.Add(15 * time.Second))}, CommandId: id, TargetAgentId: resolved.ID, IntentProducedAt: timestamppb.New(now), IntentRef: &v.IntentRef{IntentId: uuid.NewString(), RequesterKind: v.RequesterKind_REQUESTER_KIND_NODE, RequesterId: node}, Action: &v.Command_OpenShortcut{OpenShortcut: &v.OpenShortcut{ShortcutId: resolved.ShortcutID, CatalogRevision: resolved.Revision}}}
	payload, err := proto.Marshal(command)
	if err == nil {
		err = s.navTransport.Publish(ctx, mqtt.Message{Topic: "orbit/v1/agents/" + resolved.ID + "/commands", Payload: payload})
	}
	return id, err
}

type nodeCommand struct {
	ID              string    `json:"id"`
	ShortcutID      string    `json:"shortcut_id"`
	CatalogRevision uint64    `json:"catalog_revision"`
	ExpiresAt       time.Time `json:"expires_at"`
	Requester       string    `json:"-"`
	Entry           string    `json:"-"`
}

func decodeNavigation(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024))
	dec.DisallowUnknownFields()
	if dec.Decode(dst) != nil || dec.Decode(&struct{}{}) != io.EOF {
		failure(w, &inbox.Fault{Code: "invalid_navigation"})
		return false
	}
	return true
}
func (s *Server) executionRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/execution/catalog", s.auth(func(w http.ResponseWriter, r *http.Request, node string) {
		catalogs, err := s.store.ShortcutCatalogs(r.Context())
		if err != nil {
			failure(w, err)
			return
		}
		var revision uint64
		for _, c := range catalogs {
			if c.ExecutorKind == "node" && c.ExecutorID == node {
				revision = c.Revision
			}
		}
		write(w, 200, map[string]any{"revision": revision})
	}))
	mux.HandleFunc("POST /api/v1/execution/catalog", s.auth(func(w http.ResponseWriter, r *http.Request, node string) {
		var input struct {
			Revision uint64                 `json:"revision"`
			Items    []inbox.NavigationItem `json:"items"`
		}
		if !decodeNavigation(w, r, &input) {
			return
		}
		s.navMu.Lock()
		s.mu.Lock()
		label := s.devices[node].Label
		s.mu.Unlock()
		err := s.store.AcceptNodeCatalog(r.Context(), node, label, input.Revision, input.Items)
		if err == nil {
			s.publishNavigationsLocked(r.Context())
		}
		s.navMu.Unlock()
		if err != nil {
			failure(w, err)
			return
		}
		s.notify()
		write(w, 200, map[string]any{"revision": input.Revision})
	}))
	mux.HandleFunc("GET /api/v1/execution/commands", s.auth(func(w http.ResponseWriter, r *http.Request, node string) {
		s.navMu.Lock()
		defer s.navMu.Unlock()
		commands := []nodeCommand{}
		for _, c := range s.nodeCommands[node] {
			if !c.ExpiresAt.After(time.Now().UTC()) {
				continue
			}
			s.mu.Lock()
			requester, exists := s.devices[c.Requester]
			s.mu.Unlock()
			if !exists || requester.Revoked {
				continue
			}
			target, err := s.store.ResolveShortcut(r.Context(), c.Requester, c.Entry)
			if err == nil && target.Kind == "node" && target.ID == node && target.ShortcutID == c.ShortcutID && target.Revision == c.CatalogRevision {
				commands = append(commands, c)
			}
		}
		// Claim once. Requests expire rather than being replayed after reconnect/restart.
		delete(s.nodeCommands, node)
		write(w, 200, map[string]any{"commands": commands, "now": time.Now().UTC()})
	}))
}

var receiverID = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

func (s *Server) navigationAdminRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/app/executors", func(w http.ResponseWriter, r *http.Request) {
		catalogs, err := s.store.ShortcutCatalogs(r.Context())
		if err != nil {
			failure(w, err)
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		filtered := []inbox.ShortcutCatalog{}
		for _, c := range catalogs {
			if c.ExecutorKind == "node" {
				d, ok := s.devices[c.ExecutorID]
				if !ok || d.Revoked {
					continue
				}
			}
			filtered = append(filtered, c)
		}
		write(w, 200, map[string]any{"executors": filtered})
	})
	mux.HandleFunc("GET /api/app/navigation/{node}", func(w http.ResponseWriter, r *http.Request) {
		node := r.PathValue("node")
		if !receiverID.MatchString(node) {
			failure(w, &inbox.Fault{Code: "invalid_navigation"})
			return
		}
		s.navMu.Lock()
		defer s.navMu.Unlock()
		d, err := s.store.NavigationSettings(r.Context(), node)
		if err != nil {
			failure(w, err)
			return
		}
		write(w, 200, d)
	})
	mux.HandleFunc("PUT /api/app/navigation/{node}", func(w http.ResponseWriter, r *http.Request) {
		node := r.PathValue("node")
		if !receiverID.MatchString(node) {
			failure(w, &inbox.Fault{Code: "invalid_navigation"})
			return
		}
		var d inbox.NavigationSettings
		if !decodeNavigation(w, r, &d) {
			return
		}
		s.navMu.Lock()
		s.mu.Lock()
		device, exists := s.devices[node]
		invalid := exists && device.Revoked
		for _, p := range d.Pairs {
			if p.Enabled && p.ExecutorKind == "node" {
				e, ok := s.devices[p.ExecutorID]
				invalid = invalid || !ok || e.Revoked
			}
		}
		s.mu.Unlock()
		if invalid {
			s.navMu.Unlock()
			failure(w, &inbox.Fault{Code: "device_revoked"})
			return
		}
		saved, err := s.store.SaveNavigationSettings(r.Context(), node, d)
		pending := false
		if err == nil {
			view, e := s.projectNavigationLocked(r.Context(), node)
			if e == nil {
				pending = s.publishNavigationLocked(r.Context(), node, view) != nil
			} else {
				pending = true
			}
		}
		s.navMu.Unlock()
		if err != nil {
			failure(w, err)
			return
		}
		s.notify()
		write(w, 200, struct {
			inbox.NavigationSettings
			PublishPending bool `json:"publish_pending"`
		}{saved, pending})
	})
}
func (s *Server) publishNavigationLocked(ctx context.Context, node string, d inbox.Navigation) error {
	if s.navTransport == nil {
		return nil
	}
	b, err := proto.Marshal(d.Proto(node))
	if err != nil {
		return err
	}
	return s.navTransport.Publish(ctx, mqtt.Message{Topic: "orbit/v1/nodes/" + node + "/navigation", Payload: b, Retain: true})
}
func (s *Server) StartNavigation(ctx context.Context, t interface {
	Publish(context.Context, mqtt.Message) error
	Subscribe(context.Context, string, mqtt.Handler) error
}, coreID string, displayDefaults map[string]*uint32) error {
	s.navMu.Lock()
	s.navTransport = t
	s.navCoreID = coreID
	if err := s.store.MigrateNavigationSettings(ctx, displayDefaults); err != nil {
		s.navMu.Unlock()
		return err
	}
	s.navMu.Unlock()
	if err := t.Subscribe(ctx, "orbit/v1/agents/+/shortcuts", func(ctx context.Context, m mqtt.Message) error {
		parts := strings.Split(m.Topic, "/")
		if len(parts) != 5 || len(m.Payload) > 256*1024 {
			return errors.New("invalid shortcut catalog")
		}
		var c v.AgentShortcuts
		if err := proto.Unmarshal(m.Payload, &c); err != nil {
			return err
		}
		if c.AgentId != parts[3] {
			return errors.New("catalog identity mismatch")
		}
		s.navMu.Lock()
		err := s.store.AcceptShortcutCatalog(ctx, &c)
		if err == nil {
			s.publishNavigationsLocked(ctx)
		}
		s.navMu.Unlock()
		if err != nil {
			return err
		}
		s.notify()
		return nil
	}); err != nil {
		s.StopNavigation()
		return err
	}
	go func() {
		tick := time.NewTicker(15 * time.Second)
		defer tick.Stop()
		for {
			s.navMu.Lock()
			s.publishNavigationsLocked(ctx)
			s.navMu.Unlock()
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			}
		}
	}()
	return nil
}
func (s *Server) StopNavigation() { s.navMu.Lock(); defer s.navMu.Unlock(); s.navTransport = nil }

func (s *Server) publishNavigationsLocked(ctx context.Context) {
	ids, err := s.store.NavigationNodes(ctx)
	if err != nil {
		return
	}
	seen := map[string]bool{}
	for _, id := range ids {
		seen[id] = true
	}
	for id := range seen {
		d, err := s.projectNavigationLocked(ctx, id)
		if err == nil {
			_ = s.publishNavigationLocked(ctx, id, d)
		}
	}
}
