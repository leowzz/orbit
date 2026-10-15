package core

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"strings"
)

// Device names belong to Core and do not change the identity reported by a Node.
func (s *RouteStore) nodeLabels(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT node_id,label FROM node_labels`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	labels := map[string]string{}
	for rows.Next() {
		var id, label string
		if err := rows.Scan(&id, &label); err != nil {
			return nil, err
		}
		labels[id] = label
	}
	return labels, rows.Err()
}

func (s *RouteStore) renameNode(w http.ResponseWriter, r *http.Request, engine *Engine) {
	media, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if media != "application/json" {
		http.Error(w, "expected application/json", http.StatusUnsupportedMediaType)
		return
	}
	var input struct {
		Label string `json:"label"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	dec.DisallowUnknownFields()
	if dec.Decode(&input) != nil || dec.Decode(&struct{}{}) != io.EOF {
		http.Error(w, "invalid device name", http.StatusBadRequest)
		return
	}
	input.Label = strings.TrimSpace(input.Label)
	if input.Label == "" || len(input.Label) > 120 {
		http.Error(w, "invalid device name", http.StatusBadRequest)
		return
	}
	id := r.PathValue("id")
	engine.mu.Lock()
	known := engine.nodeProducts[id] != nil
	engine.mu.Unlock()
	if !known {
		document, err := s.Load()
		if err != nil {
			http.Error(w, "cannot load devices", http.StatusServiceUnavailable)
			return
		}
		route, ok := document.Routes[id]
		known = ok && route.Profile != appProfile
	}
	if !known {
		http.NotFound(w, r)
		return
	}
	_, err := s.db.ExecContext(r.Context(), `INSERT INTO node_labels(node_id,label) VALUES(?,?) ON CONFLICT(node_id) DO UPDATE SET label=excluded.label`, id, input.Label)
	if err != nil {
		http.Error(w, "cannot save device name", http.StatusServiceUnavailable)
		return
	}
	writeConsoleJSON(w, map[string]string{"id": id, "label": input.Label})
}
