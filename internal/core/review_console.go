package core

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"time"
)

func (s *RouteStore) reviewRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/reviews", s.listReviews)
	mux.HandleFunc("GET /api/reviews/{id}", s.getReview)
	mux.HandleFunc("POST /api/reviews/{id}/retry", s.retryReview)
}
func (s *RouteStore) listReviews(w http.ResponseWriter, r *http.Request) {
	day, err := time.Parse("2006-01-02", r.URL.Query().Get("date"))
	if err != nil {
		http.Error(w, "invalid date", 400)
		return
	}
	days := 1
	if r.URL.Query().Get("view") == "week" {
		days = 7
	} else if v := r.URL.Query().Get("view"); v != "" && v != "day" {
		http.Error(w, "invalid view", 400)
		return
	}
	node := r.URL.Query().Get("node_id")
	if node != "" && !audioIdentity.MatchString(node) {
		http.Error(w, "invalid device", 400)
		return
	}
	rows, err := s.db.QueryContext(r.Context(), `SELECT `+reviewColumns+` FROM review_windows WHERE (CASE WHEN kind='day' THEN start ELSE date(start,'+8 hours') END)>=? AND (CASE WHEN kind='day' THEN start ELSE date(start,'+8 hours') END)<? AND (?='' OR node_id=?) ORDER BY start,node_id`, day.Format("2006-01-02"), day.AddDate(0, 0, days).Format("2006-01-02"), node, node)
	if err != nil {
		http.Error(w, "cannot load reviews", 500)
		return
	}
	items := []reviewWindow{}
	for rows.Next() {
		item, err := scanReview(rows)
		if err != nil {
			rows.Close()
			http.Error(w, "cannot load reviews", 500)
			return
		}
		item.Result.Evidence = nil
		items = append(items, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		http.Error(w, "cannot load reviews", 500)
		return
	}
	deviceRows, err := s.db.QueryContext(r.Context(), `SELECT DISTINCT w.node_id,COALESCE(n.label,w.node_id) FROM review_windows w LEFT JOIN node_labels n ON n.node_id=w.node_id ORDER BY 2`)
	if err != nil {
		http.Error(w, "cannot load review devices", 500)
		return
	}
	devices := []map[string]string{}
	for deviceRows.Next() {
		var id, label string
		if err = deviceRows.Scan(&id, &label); err != nil {
			break
		}
		devices = append(devices, map[string]string{"id": id, "label": label})
	}
	if err == nil {
		err = deviceRows.Err()
	}
	deviceRows.Close()
	if err != nil {
		http.Error(w, "cannot load review devices", 500)
		return
	}
	writeConsoleJSON(w, map[string]any{"items": items, "devices": devices, "enabled": s.analysis.Enabled, "timezone": "Asia/Shanghai", "model": s.analysis.Model})
}
func (s *RouteStore) getReview(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		http.Error(w, "invalid review ID", 400)
		return
	}
	item, err := scanReview(s.db.QueryRowContext(r.Context(), `SELECT `+reviewColumns+` FROM review_windows WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "cannot load review", 500)
		return
	}
	writeConsoleJSON(w, item)
}
func (s *RouteStore) retryReview(w http.ResponseWriter, r *http.Request) {
	if !s.analysis.Enabled {
		http.Error(w, "analysis is disabled", 409)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		http.Error(w, "invalid review ID", 400)
		return
	}
	result, err := s.db.ExecContext(r.Context(), `UPDATE review_windows SET status='queued',attempts=0,retry_at='',error='' WHERE id=? AND status='failed'`, id)
	if err != nil {
		http.Error(w, "cannot retry review", 500)
		return
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		http.Error(w, "only failed reviews can be retried", 409)
		return
	}
	writeConsoleJSON(w, map[string]bool{"queued": true})
}
