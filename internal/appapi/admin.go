package appapi

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"orbit/internal/config"
	"orbit/internal/inbox"
)

const consoleNode = "@console"

type deviceActivity struct {
	LastSeen    string `json:"last_seen,omitempty"`
	LastSync    string `json:"last_sync,omitempty"`
	Cursor      string `json:"served_cursor,omitempty"`
	Connections int    `json:"connections"`
}

func (s *Server) touchLocked(node string) *deviceActivity {
	if s.activity[node] == nil {
		s.activity[node] = &deviceActivity{}
	}
	return s.activity[node]
}
func (s *Server) recordSync(node, cursor string) {
	if node == consoleNode {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	a := s.touchLocked(node)
	a.LastSync = time.Now().UTC().Format(time.RFC3339)
	a.Cursor = cursor
}

// AdminHandler must be mounted behind the console's session and origin checks.
// It is deliberately not reachable from the public bearer-token API listener.
func (s *Server) AdminHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/app/devices", s.listDevices)
	mux.HandleFunc("POST /api/app/devices", s.createDevice)
	mux.HandleFunc("POST /api/app/devices/{id}/{action}", s.changeDevice)
	mux.HandleFunc("GET /api/app/items", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if len(q.Get("q")) > 200 || (q.Get("deleted") != "" && q.Get("deleted") != "false" && q.Get("deleted") != "true") {
			failure(w, &inbox.Fault{Code: "invalid_fields"})
			return
		}
		items, err := s.store.ListItems(r.Context(), q.Get("after"), q.Get("kind"), q.Get("q"), q.Get("deleted") == "true")
		if err != nil {
			failure(w, err)
			return
		}
		more := len(items) > 50
		if more {
			items = items[:50]
		}
		next := ""
		if more {
			next = items[len(items)-1].ID
		}
		write(w, 200, map[string]any{"items": items, "next": next})
	})
	mux.HandleFunc("POST /api/app/operations", func(w http.ResponseWriter, r *http.Request) { s.applyOperation(w, r, consoleNode, true) })
	mux.HandleFunc("POST /api/app/attachments", func(w http.ResponseWriter, r *http.Request) { s.upload(w, r, consoleNode) })
	mux.HandleFunc("GET /api/app/attachments/{id}", func(w http.ResponseWriter, r *http.Request) { s.downloadAttachment(w, r, consoleNode, true) })
	return mux
}
func (s *Server) listDevices(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	devices := []map[string]any{}
	ids := make([]string, 0, len(s.devices))
	for id := range s.devices {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		d := s.devices[id]
		a := s.touchLocked(id)
		devices = append(devices, map[string]any{"id": id, "label": d.Label, "platform": d.Platform, "revoked": d.Revoked, "activity": *a})
	}
	write(w, 200, map[string]any{"enabled": true, "devices": devices})
}
func decodeAdmin(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	dec.DisallowUnknownFields()
	if dec.Decode(dst) != nil || dec.Decode(&struct{}{}) != io.EOF {
		failure(w, &inbox.Fault{Code: "invalid_fields"})
		return false
	}
	return true
}
func tokenPair() (string, string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	token := hex.EncodeToString(b)
	digest := sha256.Sum256([]byte(token))
	return token, hex.EncodeToString(digest[:]), nil
}
func (s *Server) saveDevice(r *http.Request, id string, device config.AppDevice) error {
	next := make(map[string]config.AppDevice, len(s.devices)+1)
	for k, v := range s.devices {
		next[k] = v
	}
	next[id] = device
	raw, err := json.Marshal(next)
	if err != nil {
		return err
	}
	if err = s.store.SaveDevices(r.Context(), raw); err != nil {
		return err
	}
	s.devices = next
	return nil
}
func (s *Server) createDevice(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Label    string `json:"label"`
		Platform string `json:"platform"`
	}
	if !decodeAdmin(w, r, &input) {
		return
	}
	input.Label = strings.TrimSpace(input.Label)
	if input.Label == "" || len(input.Label) > 120 || (input.Platform != "android" && input.Platform != "macos" && input.Platform != "windows") {
		failure(w, &inbox.Fault{Code: "invalid_fields"})
		return
	}
	token, digest, err := tokenPair()
	if err != nil {
		failure(w, err)
		return
	}
	id := "app-" + uuid.NewString()
	s.mu.Lock()
	err = s.saveDevice(r, id, config.AppDevice{Label: input.Label, Platform: input.Platform, TokenSHA256: digest})
	s.mu.Unlock()
	if err != nil {
		failure(w, err)
		return
	}
	write(w, 201, map[string]string{"id": id, "token": token})
}
func (s *Server) changeDevice(w http.ResponseWriter, r *http.Request) {
	id, action := r.PathValue("id"), r.PathValue("action")
	if action != "revoke" && action != "rotate" {
		http.NotFound(w, r)
		return
	}
	s.mu.Lock()
	device, ok := s.devices[id]
	if !ok {
		s.mu.Unlock()
		failure(w, &inbox.Fault{Code: "not_found"})
		return
	}
	token := ""
	var err error
	if action == "revoke" {
		device.Revoked = true
	} else {
		token, device.TokenSHA256, err = tokenPair()
		device.Revoked = false
	}
	if err == nil {
		err = s.saveDevice(r, id, device)
	}
	s.mu.Unlock()
	if err != nil {
		failure(w, err)
		return
	}
	s.notify()
	write(w, 200, map[string]string{"id": id, "token": token})
}
