package core

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"regexp"
)

var archivedNodeID = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

func (s *RouteStore) archivedNodes(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT node_id FROM node_archives ORDER BY node_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// Archiving is a presentation preference keyed by stable ID. It also survives a
// device being absent from discovery, without touching its route or credentials.
func (s *RouteStore) archiveNode(w http.ResponseWriter, r *http.Request) {
	media, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if media != "application/json" {
		http.Error(w, "expected application/json", http.StatusUnsupportedMediaType)
		return
	}
	var input struct {
		Archived *bool `json:"archived"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	dec.DisallowUnknownFields()
	id := r.PathValue("id")
	if !archivedNodeID.MatchString(id) || dec.Decode(&input) != nil || dec.Decode(&struct{}{}) != io.EOF || input.Archived == nil {
		http.Error(w, "invalid archive preference", http.StatusBadRequest)
		return
	}
	var err error
	if *input.Archived {
		_, err = s.db.ExecContext(r.Context(), `INSERT OR IGNORE INTO node_archives(node_id) VALUES(?)`, id)
	} else {
		_, err = s.db.ExecContext(r.Context(), `DELETE FROM node_archives WHERE node_id=?`, id)
	}
	if err != nil {
		http.Error(w, "cannot save archive preference", http.StatusServiceUnavailable)
		return
	}
	writeConsoleJSON(w, map[string]any{"id": id, "archived": *input.Archived})
}
