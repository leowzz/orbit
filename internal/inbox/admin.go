package inbox

import (
	"context"
	"encoding/json"
)

// DeviceRegistry imports the deployment seed only once, including an empty seed.
func (s *Store) DeviceRegistry(ctx context.Context, seed []byte) ([]byte, error) {
	if _, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO app_registry VALUES(1,?)`, string(seed)); err != nil {
		return nil, err
	}
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT payload FROM app_registry WHERE id=1`).Scan(&raw)
	return []byte(raw), err
}
func (s *Store) SaveDevices(ctx context.Context, raw []byte) error {
	_, err := s.db.ExecContext(ctx, `UPDATE app_registry SET payload=? WHERE id=1`, string(raw))
	return err
}

func (s *Store) ListItems(ctx context.Context, after, kind, query string, deleted bool) ([]Item, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT payload FROM items WHERE id>? AND (COALESCE(json_extract(payload,'$.deleted_at'),'')!='')=? AND (?='' OR json_extract(payload,'$.kind')=?) AND instr(lower(json_extract(payload,'$.body')),lower(?))>0 ORDER BY id LIMIT 51`, after, deleted, kind, kind, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Item{}
	for rows.Next() {
		var raw string
		var item Item
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(raw), &item); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
