package inbox

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type Attachment struct {
	ID     string `json:"id"`
	MIME   string `json:"mime"`
	Size   int64  `json:"size"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

func (s *Store) AddAttachment(ctx context.Context, node string, a Attachment) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO attachments VALUES(?,?,?,?,?,?,?)`, a.ID, node, a.MIME, a.Size, a.Width, a.Height, time.Now().Unix())
	return err
}
func (s *Store) Attachment(ctx context.Context, node, id string, includeDeleted bool) (Attachment, error) {
	a := Attachment{ID: id}
	err := s.db.QueryRowContext(ctx, `SELECT mime,size,width,height FROM attachments WHERE id=? AND (owner=? OR EXISTS(SELECT 1 FROM items WHERE json_extract(payload,'$.attachment_id')=? AND (? OR COALESCE(json_extract(payload,'$.deleted_at'),'')='')))`, id, node, id, includeDeleted).Scan(&a.MIME, &a.Size, &a.Width, &a.Height)
	if errors.Is(err, sql.ErrNoRows) {
		return a, &Fault{Code: "attachment_unavailable"}
	}
	return a, err
}

// ExpiredAttachments removes metadata in the same write lock used to validate
// new references. Soft-deleted items retain their attachments for restoration.
// Files are removed after commit by the caller.
func (s *Store) ExpiredAttachments(ctx context.Context) ([]string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT id FROM attachments WHERE created_at<? AND NOT EXISTS(SELECT 1 FROM items WHERE json_extract(payload,'$.attachment_id')=attachments.id)`, time.Now().Add(-24*time.Hour).Unix())
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		if _, err = tx.ExecContext(ctx, `DELETE FROM attachments WHERE id=?`, id); err != nil {
			return nil, err
		}
	}
	return ids, tx.Commit()
}

// Seed uses ordinary idempotent operations and contains no private content.
func (s *Store) Seed(ctx context.Context) error {
	for _, op := range []Operation{
		{ID: "00000000-0000-4000-8000-000000000001", ItemID: "00000000-0000-4000-8000-000000000011", Type: "create", Kind: "text", Body: "欢迎来到 Orbit。文本可以复制，也可以在另一台设备继续编辑。"},
		{ID: "00000000-0000-4000-8000-000000000002", ItemID: "00000000-0000-4000-8000-000000000012", Type: "create", Kind: "todo", Body: "试着完成这条待办"},
	} {
		if _, err := s.Apply(ctx, "sample", op); err != nil {
			return err
		}
	}

	return nil
}

func (s *Store) HasAttachment(ctx context.Context, id string) bool {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM attachments WHERE id=?`, id).Scan(&count)
	return err != nil || count > 0 // On database failure, preserve files.
}
