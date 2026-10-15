package appapi

import (
	"context"
	"errors"
	"net/http"
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
	var agents []string
	if s.navSources != nil {
		agents = s.navSources()[node]
	}
	return s.store.ProjectNavigation(ctx, node, agents)
}
func (s *Server) openShortcut(w http.ResponseWriter, r *http.Request, node string) {
	s.navMu.Lock()
	defer s.navMu.Unlock()
	if _, err := s.projectNavigationLocked(r.Context(), node); err != nil {
		failure(w, err)
		return
	}
	agent, id, rev, err := s.store.ResolveShortcut(r.Context(), node, r.PathValue("id"))
	if err != nil {
		failure(w, err)
		return
	}
	if s.navTransport == nil {
		failure(w, &inbox.Fault{Code: "agent_unavailable"})
		return
	}
	now := time.Now().UTC()
	s.navRevision++
	command := &v.Command{Metadata: &v.Metadata{MessageId: uuid.NewString(), ProducerId: s.navCoreID, Revision: s.navRevision, ProducedAt: timestamppb.New(now), ExpiresAt: timestamppb.New(now.Add(15 * time.Second))}, CommandId: uuid.NewString(), TargetAgentId: agent, IntentProducedAt: timestamppb.New(now), IntentRef: &v.IntentRef{IntentId: uuid.NewString(), RequesterKind: v.RequesterKind_REQUESTER_KIND_NODE, RequesterId: node}, Action: &v.Command_OpenShortcut{OpenShortcut: &v.OpenShortcut{ShortcutId: id, CatalogRevision: rev}}}
	payload, err := proto.Marshal(command)
	if err == nil {
		err = s.navTransport.Publish(r.Context(), mqtt.Message{Topic: "orbit/v1/agents/" + agent + "/commands", Payload: payload})
	}
	if err != nil {
		failure(w, err)
		return
	}
	write(w, 202, map[string]string{"command_id": command.CommandId, "status": "sent"})
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
}, coreID string, sources func() map[string][]string) error {
	s.navMu.Lock()
	s.navTransport = t
	s.navCoreID = coreID
	s.navSources = sources
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
	if s.navSources != nil {
		for id := range s.navSources() {
			seen[id] = true
		}
	}
	for id := range seen {
		d, err := s.projectNavigationLocked(ctx, id)
		if err == nil {
			_ = s.publishNavigationLocked(ctx, id, d)
		}
	}
}
