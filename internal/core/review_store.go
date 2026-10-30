package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

// Text is captured in the audio transaction, before the independent audio cap
// can remove it. Only enabled installations capture new review inputs.
const reviewSchema = `
CREATE TABLE IF NOT EXISTS review_settings(id INTEGER PRIMARY KEY CHECK(id=1),enabled INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS review_inputs(
 segment_id TEXT PRIMARY KEY,node_id TEXT NOT NULL,started_at TEXT NOT NULL,
 hour TEXT NOT NULL,day TEXT NOT NULL,status TEXT NOT NULL,text TEXT NOT NULL DEFAULT '');
CREATE INDEX IF NOT EXISTS review_inputs_time ON review_inputs(hour,node_id);
CREATE INDEX IF NOT EXISTS review_inputs_day ON review_inputs(day,node_id);
CREATE TABLE IF NOT EXISTS review_windows(
 id INTEGER PRIMARY KEY,kind TEXT NOT NULL,node_id TEXT NOT NULL,start TEXT NOT NULL,
 input_hash TEXT NOT NULL,analyzed_hash TEXT NOT NULL DEFAULT '',status TEXT NOT NULL DEFAULT 'queued',
 counts TEXT NOT NULL,result TEXT NOT NULL DEFAULT '{}',attempts INTEGER NOT NULL DEFAULT 0,
 retry_at TEXT NOT NULL DEFAULT '',error TEXT NOT NULL DEFAULT '',updated_at TEXT NOT NULL DEFAULT '',
 UNIQUE(kind,node_id,start));
CREATE INDEX IF NOT EXISTS review_windows_time ON review_windows(kind,start);
CREATE TRIGGER IF NOT EXISTS review_capture_segment AFTER INSERT ON audio_segments
WHEN (SELECT enabled FROM review_settings WHERE id=1)=1 BEGIN
 INSERT OR IGNORE INTO review_inputs(segment_id,node_id,started_at,hour,day,status)
 VALUES(NEW.segment_id,NEW.node_id,NEW.started_at,strftime('%Y-%m-%dT%H:00:00Z',NEW.started_at),date(NEW.started_at,'+8 hours'),'pending');
END;
CREATE TRIGGER IF NOT EXISTS review_capture_result AFTER INSERT ON audio_results
WHEN (SELECT enabled FROM review_settings WHERE id=1)=1 BEGIN
 UPDATE review_inputs SET text=NEW.text,status=json_extract(NEW.payload,'$.status') WHERE segment_id=NEW.job_id;
END;
CREATE TRIGGER IF NOT EXISTS review_capture_loss BEFORE DELETE ON audio_segments BEGIN
 UPDATE review_inputs SET status='lost' WHERE segment_id=OLD.segment_id AND status='pending';
END;
`

type reviewCounts struct {
	Total     int `json:"total"`
	Completed int `json:"completed"`
	Pending   int `json:"pending"`
	Failed    int `json:"failed"`
	Lost      int `json:"lost"`
	Nonempty  int `json:"nonempty"`
}
type reviewInput struct {
	ID     string `json:"id"`
	Time   string `json:"time"`
	Status string `json:"status"`
	Text   string `json:"text"`
}
type reviewItem struct {
	Kind        string   `json:"kind"`
	Text        string   `json:"text"`
	Source      string   `json:"source"`
	Owner       string   `json:"owner"`
	Status      string   `json:"status"`
	EvidenceIDs []string `json:"evidence_ids"`
	Uncertainty string   `json:"uncertainty"`
}
type reviewResult struct {
	Title        string        `json:"title"`
	Summary      string        `json:"summary"`
	Items        []reviewItem  `json:"items"`
	Evidence     []reviewInput `json:"evidence"`
	Model        string        `json:"model"`
	InputTokens  int           `json:"input_tokens"`
	OutputTokens int           `json:"output_tokens"`
}
type reviewWindow struct {
	ID           int64        `json:"id"`
	Kind         string       `json:"kind"`
	NodeID       string       `json:"node_id"`
	Start        string       `json:"start"`
	InputHash    string       `json:"-"`
	AnalyzedHash string       `json:"-"`
	Status       string       `json:"status"`
	Counts       reviewCounts `json:"counts"`
	Result       reviewResult `json:"result"`
	Attempts     int          `json:"attempts"`
	RetryAt      string       `json:"retry_at,omitempty"`
	Error        string       `json:"error,omitempty"`
	UpdatedAt    string       `json:"updated_at,omitempty"`
	Stale        bool         `json:"stale"`
}

const reviewColumns = `id,kind,node_id,start,input_hash,analyzed_hash,status,counts,result,attempts,retry_at,error,updated_at`

type reviewScanner interface{ Scan(...any) error }

func scanReview(row reviewScanner) (reviewWindow, error) {
	var w reviewWindow
	var counts, result string
	err := row.Scan(&w.ID, &w.Kind, &w.NodeID, &w.Start, &w.InputHash, &w.AnalyzedHash, &w.Status, &counts, &result, &w.Attempts, &w.RetryAt, &w.Error, &w.UpdatedAt)
	if err != nil {
		return w, err
	}
	if err = json.Unmarshal([]byte(counts), &w.Counts); err != nil {
		return w, err
	}
	err = json.Unmarshal([]byte(result), &w.Result)
	if w.Result.Items == nil {
		w.Result.Items = []reviewItem{}
	}
	if w.Result.Evidence == nil {
		w.Result.Evidence = []reviewInput{}
	}
	w.Stale = w.AnalyzedHash != "" && w.AnalyzedHash != w.InputHash
	return w, err
}
func (s *RouteStore) initReviews() error {
	if _, err := s.db.Exec(reviewSchema); err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`INSERT INTO review_settings VALUES(1,?) ON CONFLICT(id) DO UPDATE SET enabled=excluded.enabled`, s.analysis.Enabled); err != nil {
		return err
	}
	if s.analysis.Enabled {
		// Snapshot existing retained records before startup audio pruning.
		_, err = tx.Exec(`INSERT OR IGNORE INTO review_inputs SELECT s.segment_id,s.node_id,s.started_at,strftime('%Y-%m-%dT%H:00:00Z',s.started_at),date(s.started_at,'+8 hours'),COALESCE(j.status,'pending'),COALESCE(r.text,'') FROM audio_segments s LEFT JOIN audio_jobs j ON j.job_id=s.segment_id LEFT JOIN audio_results r ON r.job_id=s.segment_id WHERE julianday(s.started_at)>=julianday('now','-30 days')`)
		if err != nil {
			return err
		}
		// Refresh records completed while analysis was disabled, without losing
		// captured records whose original audio has already expired.
		_, err = tx.Exec(`UPDATE review_inputs SET status=(SELECT json_extract(payload,'$.status') FROM audio_results WHERE job_id=segment_id),text=(SELECT text FROM audio_results WHERE job_id=segment_id) WHERE EXISTS(SELECT 1 FROM audio_results WHERE job_id=segment_id)`)
		if err != nil {
			return err
		}
		_, err = tx.Exec(`UPDATE review_windows SET status='queued' WHERE status='running'`)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}
func reviewHash(value any) string {
	b, _ := json.Marshal(value)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func reviewCount(inputs []reviewInput) reviewCounts {
	c := reviewCounts{Total: len(inputs)}
	for _, r := range inputs {
		switch r.Status {
		case "completed":
			c.Completed++
		case "pending":
			c.Pending++
		case "failed":
			c.Failed++
		case "lost":
			c.Lost++
		}
		if r.Text != "" {
			c.Nonempty++
		}
	}
	return c
}
func (s *RouteStore) reviewInputs(ctx context.Context, node, start, kind string) ([]reviewInput, error) {
	column := "hour"
	if kind == "day" {
		column = "day"
	}
	rows, err := s.db.QueryContext(ctx, `SELECT segment_id,started_at,status,text FROM review_inputs WHERE node_id=? AND `+column+`=? ORDER BY julianday(started_at),segment_id`, node, start)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []reviewInput{}
	for rows.Next() {
		var r reviewInput
		if err := rows.Scan(&r.ID, &r.Time, &r.Status, &r.Text); err != nil {
			return nil, err
		}
		items = append(items, r)
	}
	return items, rows.Err()
}
func (s *RouteStore) queueReview(ctx context.Context, kind, node, start, hash string, c reviewCounts) error {
	counts, _ := json.Marshal(c)
	_, err := s.db.ExecContext(ctx, `INSERT INTO review_windows(kind,node_id,start,input_hash,counts) VALUES(?,?,?,?,?) ON CONFLICT(kind,node_id,start) DO UPDATE SET input_hash=excluded.input_hash,counts=excluded.counts,status='queued',attempts=0,retry_at='',error='' WHERE review_windows.input_hash<>excluded.input_hash`, kind, node, start, hash, string(counts))
	return err
}
func (s *RouteStore) syncReviewHours(ctx context.Context, now time.Time) error {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT node_id,hour FROM review_inputs WHERE hour>=? AND hour<? ORDER BY hour DESC`, now.AddDate(0, 0, -30).UTC().Format(time.RFC3339), now.Add(-10*time.Minute).UTC().Truncate(time.Hour).Format(time.RFC3339))
	if err != nil {
		return err
	}
	type key struct{ node, hour string }
	keys := []key{}
	for rows.Next() {
		var k key
		if err = rows.Scan(&k.node, &k.hour); err != nil {
			rows.Close()
			return err
		}
		keys = append(keys, k)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, k := range keys {
		inputs, err := s.reviewInputs(ctx, k.node, k.hour, "hour")
		if err != nil {
			return err
		}
		hash := reviewHash([]any{reviewPromptVersion, s.analysis.Model, s.analysis.BaseURL, inputs})
		if err = s.queueReview(ctx, "hour", k.node, k.hour, hash, reviewCount(inputs)); err != nil {
			return err
		}
	}
	return nil
}
func (s *RouteStore) reviewHours(ctx context.Context, node, day string) ([]reviewWindow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+reviewColumns+` FROM review_windows WHERE kind='hour' AND node_id=? AND date(start,'+8 hours')=? ORDER BY start`, node, day)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []reviewWindow{}
	for rows.Next() {
		w, err := scanReview(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, w)
	}
	return items, rows.Err()
}
func (s *RouteStore) syncReviewDays(ctx context.Context, now time.Time) error {
	// Yesterday's first report becomes due at 00:30 Shanghai; late hourly
	// revisions naturally produce a new daily input hash, even days later.
	local := now.In(time.FixedZone("Asia/Shanghai", 8*3600)).Add(-30 * time.Minute)
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT node_id,day FROM review_inputs WHERE day<? AND day>=? ORDER BY day DESC`, local.Format("2006-01-02"), local.AddDate(0, 0, -30).Format("2006-01-02"))
	if err != nil {
		return err
	}
	type key struct{ node, day string }
	keys := []key{}
	for rows.Next() {
		var k key
		if err = rows.Scan(&k.node, &k.day); err != nil {
			rows.Close()
			return err
		}
		keys = append(keys, k)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, k := range keys {
		hours, err := s.reviewHours(ctx, k.node, k.day)
		if err != nil {
			return err
		}
		ready := len(hours) > 0
		dependencies := []any{reviewPromptVersion, s.analysis.Model, s.analysis.BaseURL}
		for _, h := range hours {
			if h.AnalyzedHash != h.InputHash || h.Status != "completed" {
				ready = false
			}
			dependencies = append(dependencies, []any{h.Start, h.InputHash, h.Result})
		}
		if !ready {
			continue
		}
		inputs, err := s.reviewInputs(ctx, k.node, k.day, "day")
		if err != nil {
			return err
		}
		if err = s.queueReview(ctx, "day", k.node, k.day, reviewHash(dependencies), reviewCount(inputs)); err != nil {
			return err
		}
	}
	return nil
}
func (s *RouteStore) pruneReviews(ctx context.Context, now time.Time) error {
	// Keep a whole Shanghai day so daily coverage does not drift on expiry.
	cutoff := now.In(time.FixedZone("Asia/Shanghai", 8*3600)).AddDate(0, 0, -30).Format("2006-01-02")
	if _, err := s.db.ExecContext(ctx, `DELETE FROM review_inputs WHERE day<?`, cutoff); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM review_windows WHERE CASE WHEN kind='day' THEN start ELSE date(start,'+8 hours') END <?`, cutoff)
	return err
}
func reviewFailure(attempt int, now time.Time) string {
	minutes := 5 * (1 << min(attempt, 8))
	return now.Add(time.Duration(minutes) * time.Minute).UTC().Format(time.RFC3339)
}
func reviewErrorCode(status int) error { return fmt.Errorf("model endpoint returned HTTP %d", status) }
