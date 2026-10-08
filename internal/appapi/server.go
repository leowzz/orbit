// Package appapi provides authenticated HTTP access to the personal inbox.
package appapi

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"orbit/internal/config"
	"orbit/internal/inbox"
)

type Server struct {
	store    *inbox.Store
	devices  map[string]config.AppDevice
	activity map[string]*deviceActivity
	status   func(string) json.RawMessage
	files    string
	mu       sync.Mutex
	wake     chan struct{}
}

func New(store *inbox.Store, cfg config.AppConfig, status func(string) json.RawMessage) (*Server, error) {
	seed, err := json.Marshal(cfg.Devices)
	if err != nil {
		return nil, err
	}
	raw, err := store.DeviceRegistry(context.Background(), seed)
	if err != nil {
		return nil, err
	}
	devices := map[string]config.AppDevice{}
	if err := json.Unmarshal(raw, &devices); err != nil {
		return nil, err
	}
	if devices == nil {
		devices = map[string]config.AppDevice{}
	}
	return &Server{store: store, devices: devices, activity: map[string]*deviceActivity{}, status: status, files: cfg.DataDir + "/attachments", wake: make(chan struct{})}, nil
}
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/status", s.auth(func(w http.ResponseWriter, r *http.Request, node string) {
		var value json.RawMessage = json.RawMessage(`null`)
		if s.status != nil {
			value = s.status(node)
		}
		s.mu.Lock()
		label := s.devices[node].Label
		s.mu.Unlock()
		write(w, 200, map[string]any{"node_id": node, "label": label, "view": value})
	}))
	mux.HandleFunc("POST /api/v1/operations", s.auth(s.operation))
	mux.HandleFunc("GET /api/v1/sync/snapshot", s.auth(s.snapshot))
	mux.HandleFunc("GET /api/v1/items", s.auth(s.snapshot))
	mux.HandleFunc("GET /api/v1/changes", s.auth(s.changes))
	mux.HandleFunc("GET /api/v1/events", s.auth(s.events))
	mux.HandleFunc("POST /api/v1/attachments", s.auth(s.upload))
	mux.HandleFunc("GET /api/v1/attachments/{id}", s.auth(s.download))
	return mux
}
func (s *Server) auth(next func(http.ResponseWriter, *http.Request, string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || len(token) < 32 || len(token) > 512 {
			failure(w, &inbox.Fault{Code: "unauthenticated"})
			return
		}
		digest := sha256.Sum256([]byte(token))
		s.mu.Lock()
		for node, device := range s.devices {
			want, err := hex.DecodeString(device.TokenSHA256)
			if err == nil && subtle.ConstantTimeCompare(digest[:], want) == 1 {
				if device.Revoked {
					s.mu.Unlock()
					failure(w, &inbox.Fault{Code: "device_revoked"})
					return
				}
				s.touchLocked(node).LastSeen = time.Now().UTC().Format(time.RFC3339)
				s.mu.Unlock()
				next(w, r, node)
				return
			}
		}
		s.mu.Unlock()
		failure(w, &inbox.Fault{Code: "unauthenticated"})
	}
}
func (s *Server) operation(w http.ResponseWriter, r *http.Request, node string) {
	s.applyOperation(w, r, node, false)
}

func (s *Server) applyOperation(w http.ResponseWriter, r *http.Request, node string, allowRestore bool) {
	r.Body = http.MaxBytesReader(w, r.Body, 32*1024)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var op inbox.Operation
	if err := dec.Decode(&op); err != nil {
		failure(w, &inbox.Fault{Code: "invalid_fields"})
		return
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		failure(w, &inbox.Fault{Code: "invalid_fields"})
		return
	}
	if op.Type == "restore" && !allowRestore {
		failure(w, &inbox.Fault{Code: "forbidden"})
		return
	}
	item, err := s.store.Apply(r.Context(), node, op)
	if err != nil {
		failure(w, err)
		return
	}
	s.notify()
	write(w, 200, item)
}
func (s *Server) snapshot(w http.ResponseWriter, r *http.Request, node string) {
	q := r.URL.Query()
	var at *int64
	if q.Has("at") {
		n, err := strconv.ParseInt(q.Get("at"), 10, 64)
		if err != nil {
			failure(w, &inbox.Fault{Code: "invalid_cursor"})
			return
		}
		at = &n
	}
	limit, _ := strconv.Atoi(q.Get("limit"))
	page, err := s.store.Snapshot(r.Context(), q.Get("generation"), at, q.Get("after_id"), limit)
	if err != nil {
		failure(w, err)
		return
	}
	s.recordSync(node, strconv.FormatInt(page.Cursor, 10))
	write(w, 200, page)
}
func (s *Server) changes(w http.ResponseWriter, r *http.Request, node string) {
	q := r.URL.Query()
	after, err := strconv.ParseInt(q.Get("after"), 10, 64)
	if err != nil {
		failure(w, &inbox.Fault{Code: "invalid_cursor"})
		return
	}
	limit, _ := strconv.Atoi(q.Get("limit"))
	page, err := s.store.Changes(r.Context(), q.Get("generation"), after, limit)
	if err != nil {
		failure(w, err)
		return
	}
	s.recordSync(node, strconv.FormatInt(page.Cursor, 10))
	write(w, 200, page)
}
func (s *Server) notify() {
	s.mu.Lock()
	defer s.mu.Unlock()
	close(s.wake)
	s.wake = make(chan struct{})
}
func (s *Server) subscribe() <-chan struct{} { s.mu.Lock(); defer s.mu.Unlock(); return s.wake }
func (s *Server) events(w http.ResponseWriter, r *http.Request, node string) {
	s.mu.Lock()
	hash := sha256.Sum256([]byte(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")))
	digest := hex.EncodeToString(hash[:])
	s.touchLocked(node).Connections++
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.touchLocked(node).Connections--; s.mu.Unlock() }()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Accel-Buffering", "no")
	rc := http.NewResponseController(w)
	tick := time.NewTicker(15 * time.Second)
	defer tick.Stop()
	for {
		// Subscribe before reading the watermark. Notifications never acknowledge
		// client data; each connection/heartbeat triggers an incremental catch-up.
		wake := s.subscribe()
		s.mu.Lock()
		device := s.devices[node]
		s.mu.Unlock()
		if device.Revoked || device.TokenSHA256 != digest {
			return
		}
		generation, cursor, err := s.store.Watermark(r.Context())
		if err != nil {
			return
		}
		_ = rc.SetWriteDeadline(time.Now().Add(10 * time.Second))
		data, _ := json.Marshal(map[string]string{"generation": generation, "cursor": cursor})
		if _, err = fmt.Fprintf(w, "event: sync\ndata: %s\n\n", data); err != nil {
			return
		}
		if rc.Flush() != nil {
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-wake:
		case <-tick.C:
		}
	}
}
func write(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func failure(w http.ResponseWriter, err error) {
	var fault *inbox.Fault
	if !errors.As(err, &fault) {
		write(w, 503, &inbox.Fault{Code: "unavailable"})
		return
	}
	status := 400
	switch fault.Code {
	case "unauthenticated":
		status = 401
	case "device_revoked", "forbidden":
		status = 403
	case "not_found", "attachment_unavailable":
		status = 404
	case "conflict", "operation_id_reused", "reset_required":
		status = 409
	}
	write(w, status, fault)
}

// Sweep runs independently of MQTT and stops with the HTTP service.
func (s *Server) Sweep(ctx context.Context) {
	timer := time.NewTicker(time.Hour)
	defer timer.Stop()
	for {
		s.cleanup(ctx)
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
	}
}
