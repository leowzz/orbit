// Package inbox owns durable personal items, their operation receipts and sync history.
package inbox

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

type Item struct {
	ID           string `json:"id"`
	Kind         string `json:"kind"`
	Body         string `json:"body"`
	Completed    bool   `json:"completed"`
	AttachmentID string `json:"attachment_id,omitempty"`
	CreatedBy    string `json:"created_by_node_id"`
	Revision     int64  `json:"revision,string"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
	DeletedAt    string `json:"deleted_at,omitempty"`
}

type Operation struct {
	ID               string `json:"operation_id"`
	Type             string `json:"type"`
	ItemID           string `json:"item_id"`
	ExpectedRevision int64  `json:"expected_revision,string"`
	Kind             string `json:"kind,omitempty"`
	Body             string `json:"body,omitempty"`
	Completed        *bool  `json:"completed,omitempty"`
	AttachmentID     string `json:"attachment_id,omitempty"`
}

type Fault struct {
	Code    string `json:"code"`
	Current *Item  `json:"current,omitempty"`
}

func (f *Fault) Error() string { return f.Code }

type Change struct {
	Seq  int64 `json:"seq,string"`
	Item Item  `json:"item"`
}

type Page struct {
	Generation string   `json:"generation"`
	Cursor     int64    `json:"cursor,string"`
	Items      []Item   `json:"items"`
	Changes    []Change `json:"changes"`
	HasMore    bool     `json:"has_more"`
	NextID     string   `json:"next_id,omitempty"`
}

type Store struct{ db *sql.DB }

func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "inbox.sqlite"))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err = s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}
func (s *Store) Close() error { return s.db.Close() }
func (s *Store) migrate() error {
	if _, err := s.db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000; PRAGMA synchronous=FULL;`); err != nil {
		return err
	}
	var version int
	if err := s.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return err
	}
	if version > 2 {
		return errors.New("inbox database is newer than this Core")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.Exec(`
 CREATE TABLE IF NOT EXISTS metadata (id INTEGER PRIMARY KEY CHECK(id=1), generation TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS items (id TEXT PRIMARY KEY, payload TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS changes (seq INTEGER PRIMARY KEY AUTOINCREMENT, item_id TEXT NOT NULL, payload TEXT NOT NULL);
 CREATE INDEX IF NOT EXISTS changes_item_seq ON changes(item_id,seq);
 CREATE TABLE IF NOT EXISTS receipts (node_id TEXT NOT NULL, operation_id TEXT NOT NULL, digest TEXT NOT NULL, payload TEXT NOT NULL, PRIMARY KEY(node_id,operation_id));
 CREATE TABLE IF NOT EXISTS attachments (id TEXT PRIMARY KEY, owner TEXT NOT NULL, mime TEXT NOT NULL, size INTEGER NOT NULL, width INTEGER NOT NULL, height INTEGER NOT NULL, created_at INTEGER NOT NULL);
 CREATE TABLE IF NOT EXISTS app_registry (id INTEGER PRIMARY KEY CHECK(id=1), payload TEXT NOT NULL);
 PRAGMA user_version=2;`)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(`INSERT OR IGNORE INTO metadata VALUES(1,?)`, uuid.NewString()); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ResetGeneration(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `UPDATE metadata SET generation=? WHERE id=1`, uuid.NewString())
	return err
}

func (s *Store) Apply(ctx context.Context, node string, op Operation) (Item, error) {
	var item Item
	if !validID(op.ID) || !validID(op.ItemID) || len(op.Body) > 16000 {
		return item, &Fault{Code: "invalid_fields"}
	}
	raw := mustJSON(op)
	hash := sha256.Sum256(raw)
	digest := hex.EncodeToString(hash[:])
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return item, err
	}
	defer tx.Rollback()
	var previous, previousDigest string
	err = tx.QueryRowContext(ctx, `SELECT digest,payload FROM receipts WHERE node_id=? AND operation_id=?`, node, op.ID).Scan(&previousDigest, &previous)
	if err == nil {
		if previousDigest != digest {
			return item, &Fault{Code: "operation_id_reused"}
		}
		err = json.Unmarshal([]byte(previous), &item)
		return item, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return item, err
	}
	var stored string
	err = tx.QueryRowContext(ctx, `SELECT payload FROM items WHERE id=?`, op.ItemID).Scan(&stored)
	exists := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return item, err
	}
	if exists {
		if err = json.Unmarshal([]byte(stored), &item); err != nil {
			return item, err
		}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if op.Type == "create" {
		if exists {
			return item, &Fault{Code: "conflict", Current: &item}
		}
		if op.ExpectedRevision != 0 || (op.Kind != "text" && op.Kind != "todo" && op.Kind != "image") || (op.Kind != "image" && strings.TrimSpace(op.Body) == "") || (op.Kind != "image" && op.AttachmentID != "") || op.Completed != nil {
			return item, &Fault{Code: "invalid_fields"}
		}
		if op.Kind == "image" {
			var owner string
			if err = tx.QueryRowContext(ctx, `SELECT owner FROM attachments WHERE id=?`, op.AttachmentID).Scan(&owner); err != nil || owner != node {
				return item, &Fault{Code: "attachment_unavailable"}
			}
		}
		item = Item{ID: op.ItemID, Kind: op.Kind, Body: op.Body, AttachmentID: op.AttachmentID, CreatedBy: node, CreatedAt: now}
	} else {
		if !exists {
			return item, &Fault{Code: "not_found"}
		}
		if item.Revision != op.ExpectedRevision || (item.DeletedAt != "" && op.Type != "restore") {
			return item, &Fault{Code: "conflict", Current: &item}
		}
		switch op.Type {
		case "restore":
			if item.DeletedAt == "" {
				return item, &Fault{Code: "conflict", Current: &item}
			}
			if op.Body != "" || op.Kind != "" || op.AttachmentID != "" || op.Completed != nil {
				return item, &Fault{Code: "invalid_fields"}
			}
			if item.AttachmentID != "" {
				var id string
				if err := tx.QueryRowContext(ctx, `SELECT id FROM attachments WHERE id=?`, item.AttachmentID).Scan(&id); err != nil {
					if errors.Is(err, sql.ErrNoRows) {
						return item, &Fault{Code: "attachment_unavailable"}
					}
					return item, err
				}
			}
			item.DeletedAt = ""
		case "update":
			if (item.Kind != "image" && strings.TrimSpace(op.Body) == "") || op.Kind != "" || op.AttachmentID != "" || op.Completed != nil {
				return item, &Fault{Code: "invalid_fields"}
			}
			item.Body = op.Body
		case "set_kind":
			if (item.Kind != "text" && item.Kind != "todo") || (op.Kind != "text" && op.Kind != "todo") || op.Body != "" || op.AttachmentID != "" || op.Completed != nil {
				return item, &Fault{Code: "invalid_fields"}
			}
			if item.Kind != op.Kind {
				item.Kind = op.Kind
				item.Completed = false
			}
		case "set_completed":
			if item.Kind != "todo" || op.Completed == nil || op.Body != "" || op.Kind != "" || op.AttachmentID != "" {
				return item, &Fault{Code: "invalid_fields"}
			}
			item.Completed = *op.Completed
		case "delete":
			if op.Body != "" || op.Kind != "" || op.AttachmentID != "" || op.Completed != nil {
				return item, &Fault{Code: "invalid_fields"}
			}
			item.DeletedAt = now
		default:
			return item, &Fault{Code: "invalid_fields"}
		}
	}
	item.Revision++
	item.UpdatedAt = now
	payload := string(mustJSON(item))
	if _, err = tx.ExecContext(ctx, `INSERT INTO items(id,payload) VALUES(?,?) ON CONFLICT(id) DO UPDATE SET payload=excluded.payload`, item.ID, payload); err != nil {
		return item, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO changes(item_id,payload) VALUES(?,?)`, item.ID, payload); err != nil {
		return item, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO receipts VALUES(?,?,?,?)`, node, op.ID, digest, payload); err != nil {
		return item, err
	}
	return item, tx.Commit()
}

func validID(s string) bool { _, err := uuid.Parse(s); return err == nil && len(s) == 36 }
func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// Snapshot pages reconstruct the immutable history at a fixed high-water mark.
// Edits/deletes during pagination cannot move an item out of that snapshot.
func (s *Store) Snapshot(ctx context.Context, generation string, at *int64, after string, limit int) (Page, error) {
	p := Page{Items: []Item{}, Changes: []Change{}}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return p, err
	}
	defer tx.Rollback()
	if err = tx.QueryRowContext(ctx, `SELECT generation,(SELECT COALESCE(MAX(seq),0) FROM changes) FROM metadata WHERE id=1`).Scan(&p.Generation, &p.Cursor); err != nil {
		return p, err
	}
	if at != nil {
		if generation != p.Generation || *at < 0 || *at > p.Cursor {
			return p, &Fault{Code: "reset_required"}
		}
		p.Cursor = *at
	} else if after != "" {
		return p, &Fault{Code: "invalid_cursor"}
	}
	rows, err := tx.QueryContext(ctx, `SELECT payload FROM changes WHERE seq IN (SELECT MAX(seq) FROM changes WHERE seq<=? AND item_id>? GROUP BY item_id) ORDER BY item_id LIMIT ?`, p.Cursor, after, bounded(limit)+1)
	if err != nil {
		return p, err
	}
	defer rows.Close()
	for rows.Next() {
		var b string
		var item Item
		if err = rows.Scan(&b); err != nil {
			return p, err
		}
		if err = json.Unmarshal([]byte(b), &item); err != nil {
			return p, err
		}
		p.Items = append(p.Items, item)
	}
	if err = rows.Err(); err != nil {
		return p, err
	}
	if len(p.Items) > bounded(limit) {
		p.HasMore = true
		p.Items = p.Items[:bounded(limit)]
	}
	if len(p.Items) > 0 {
		p.NextID = p.Items[len(p.Items)-1].ID
	}
	return p, nil
}
func (s *Store) Changes(ctx context.Context, generation string, after int64, limit int) (Page, error) {
	p := Page{Items: []Item{}, Changes: []Change{}}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return p, err
	}
	defer tx.Rollback()
	var high int64
	if err = tx.QueryRowContext(ctx, `SELECT generation,(SELECT COALESCE(MAX(seq),0) FROM changes) FROM metadata WHERE id=1`).Scan(&p.Generation, &high); err != nil {
		return p, err
	}
	if generation != p.Generation || after < 0 || after > high {
		return p, &Fault{Code: "reset_required"}
	}
	p.Cursor = after
	rows, err := tx.QueryContext(ctx, `SELECT seq,payload FROM changes WHERE seq>? ORDER BY seq LIMIT ?`, after, bounded(limit)+1)
	if err != nil {
		return p, err
	}
	defer rows.Close()
	for rows.Next() {
		var c Change
		var b string
		if err = rows.Scan(&c.Seq, &b); err != nil {
			return p, err
		}
		if err = json.Unmarshal([]byte(b), &c.Item); err != nil {
			return p, err
		}
		p.Changes = append(p.Changes, c)
	}
	if err = rows.Err(); err != nil {
		return p, err
	}
	if len(p.Changes) > bounded(limit) {
		p.HasMore = true
		p.Changes = p.Changes[:bounded(limit)]
	}
	if len(p.Changes) > 0 {
		p.Cursor = p.Changes[len(p.Changes)-1].Seq
	}
	return p, nil
}
func (s *Store) Watermark(ctx context.Context) (string, string, error) {
	var generation string
	var cursor int64
	err := s.db.QueryRowContext(ctx, `SELECT generation,(SELECT COALESCE(MAX(seq),0) FROM changes) FROM metadata WHERE id=1`).Scan(&generation, &cursor)
	return generation, strconv.FormatInt(cursor, 10), err
}
func bounded(n int) int {
	if n < 1 || n > 200 {
		return 100
	}
	return n
}
