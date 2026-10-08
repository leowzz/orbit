package core

import (
	"context"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"
)

const consoleSessionSchema = `
CREATE TABLE IF NOT EXISTS console_auth_state (
 id INTEGER PRIMARY KEY CHECK(id=1), salt BLOB NOT NULL, verifier BLOB NOT NULL
);
CREATE TABLE IF NOT EXISTS console_sessions (
 token_hash TEXT PRIMARY KEY, expires_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS console_sessions_expiry ON console_sessions(expires_at);
`

var errSessionLimit = errors.New("too many active sessions")
var errConsoleCredentialChanged = errors.New("console credential changed")

// A salted, slow verifier detects configuration changes without persisting the
// password. Changing it atomically invalidates every previously issued session.
func (s *RouteStore) consoleCredential(password string) ([]byte, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var salt, previous []byte
	err = tx.QueryRow(`SELECT salt,verifier FROM console_auth_state WHERE id=1`).Scan(&salt, &previous)
	if errors.Is(err, sql.ErrNoRows) {
		salt = make([]byte, 32)
		if _, err = rand.Read(salt); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	verifier, err := pbkdf2.Key(sha256.New, password, salt, 600000, 32)
	if err != nil {
		return nil, err
	}
	if subtle.ConstantTimeCompare(previous, verifier) != 1 {
		if _, err = tx.Exec(`DELETE FROM console_sessions`); err != nil {
			return nil, err
		}
		if _, err = tx.Exec(`INSERT INTO console_auth_state(id,salt,verifier) VALUES(1,?,?)
 ON CONFLICT(id) DO UPDATE SET salt=excluded.salt,verifier=excluded.verifier`, salt, verifier); err != nil {
			return nil, err
		}
	}
	return verifier, tx.Commit()
}

func consoleTokenHash(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}

func (s *RouteStore) createConsoleSession(ctx context.Context, token string, verifier []byte, now, until time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var current []byte
	if err = tx.QueryRowContext(ctx, `SELECT verifier FROM console_auth_state WHERE id=1`).Scan(&current); err != nil {
		return err
	}
	if subtle.ConstantTimeCompare(current, verifier) != 1 {
		return errConsoleCredentialChanged
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM console_sessions WHERE expires_at<=?`, now.UnixNano()); err != nil {
		return err
	}
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM console_sessions`).Scan(&count); err != nil {
		return err
	}
	if count >= 256 {
		return errSessionLimit
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO console_sessions(token_hash,expires_at) VALUES(?,?)`, consoleTokenHash(token), until.UnixNano()); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *RouteStore) validConsoleSession(ctx context.Context, token string, verifier []byte, now time.Time) (bool, error) {
	var valid bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(
 SELECT 1 FROM console_sessions WHERE token_hash=? AND expires_at>?
 AND EXISTS(SELECT 1 FROM console_auth_state WHERE id=1 AND verifier=?)
)`, consoleTokenHash(token), now.UnixNano(), verifier).Scan(&valid)
	return valid, err
}

func (s *RouteStore) deleteConsoleSession(ctx context.Context, token string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM console_sessions WHERE token_hash=?`, consoleTokenHash(token))
	return err
}
