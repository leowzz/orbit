package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"go.uber.org/zap"
)

// One Core-owned worker keeps scheduling/commits deterministic. HTTP/model work
// happens outside SQLite transactions and never blocks ingestion.
func (s *RouteStore) RunReviews(ctx context.Context, logger *zap.Logger) {
	model := s.newReviewModel()
	for ctx.Err() == nil {
		if err := s.reviewCycle(ctx, model, time.Now()); err != nil && ctx.Err() == nil {
			logger.Warn("review cycle failed", zap.Error(err))
		}
		timer := time.NewTimer(5 * time.Minute)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
func (s *RouteStore) reviewCycle(ctx context.Context, model *reviewModel, now time.Time) error {
	if err := s.pruneReviews(ctx, now); err != nil {
		return err
	}
	if err := s.syncReviewHours(ctx, now); err != nil {
		return err
	}
	for i := 0; i < 8; i++ {
		worked, err := s.processReview(ctx, model, "hour", now)
		if err != nil {
			return err
		}
		if !worked {
			break
		}
	}
	if err := s.syncReviewDays(ctx, now); err != nil {
		return err
	}
	for i := 0; i < 4; i++ {
		worked, err := s.processReview(ctx, model, "day", now)
		if err != nil {
			return err
		}
		if !worked {
			break
		}
	}
	return nil
}
func (s *RouteStore) processReview(ctx context.Context, model *reviewModel, kind string, now time.Time) (bool, error) {
	w, err := scanReview(s.db.QueryRowContext(ctx, `SELECT `+reviewColumns+` FROM review_windows WHERE kind=? AND status IN ('queued','failed') AND (retry_at='' OR retry_at<=?) ORDER BY start DESC,id LIMIT 1`, kind, now.UTC().Format(time.RFC3339)))
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	claim, err := s.db.ExecContext(ctx, `UPDATE review_windows SET status='running' WHERE id=? AND input_hash=? AND status IN ('queued','failed')`, w.ID, w.InputHash)
	if err != nil {
		return false, err
	}
	n, _ := claim.RowsAffected()
	if n == 0 {
		return false, nil
	}
	inputs, err := s.reviewInputs(ctx, w.NodeID, w.Start, w.Kind)
	var hours []reviewWindow
	if err == nil && w.Kind == "day" {
		hours, err = s.reviewHours(ctx, w.NodeID, w.Start)
	}
	var result reviewResult
	if err == nil {
		// A bounded lifetime also prevents one pathological hour starving all work.
		callCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
		result, err = model.analyze(callCtx, w, inputs, hours)
		cancel()
	}
	if ctx.Err() != nil {
		return true, ctx.Err()
	} // startup resets interrupted jobs
	if err != nil {
		_, saveErr := s.db.ExecContext(ctx, `UPDATE review_windows SET status='failed',attempts=attempts+1,retry_at=?,error=? WHERE id=? AND input_hash=?`, reviewFailure(w.Attempts, time.Now()), err.Error(), w.ID, w.InputHash)
		return true, saveErr
	}
	// Recheck source input after the network request. Never publish an analysis
	// against a newer input revision or silently lose late arriving results.
	current, err := s.reviewInputs(ctx, w.NodeID, w.Start, w.Kind)
	if err != nil {
		return true, err
	}
	if reviewHash(current) != reviewHash(inputs) {
		_, err = s.db.ExecContext(ctx, `UPDATE review_windows SET status='queued' WHERE id=? AND input_hash=?`, w.ID, w.InputHash)
		return true, err
	}
	if w.Kind == "day" {
		latest, err := s.reviewHours(ctx, w.NodeID, w.Start)
		if err != nil {
			return true, err
		}
		if reviewHash(latest) != reviewHash(hours) {
			_, err = s.db.ExecContext(ctx, `UPDATE review_windows SET status='queued' WHERE id=? AND input_hash=?`, w.ID, w.InputHash)
			return true, err
		}
	}
	raw, _ := json.Marshal(result)
	_, err = s.db.ExecContext(ctx, `UPDATE review_windows SET status='completed',analyzed_hash=?,result=?,attempts=0,retry_at='',error='',updated_at=? WHERE id=? AND input_hash=?`, w.InputHash, string(raw), time.Now().UTC().Format(time.RFC3339), w.ID, w.InputHash)
	return true, err
}
