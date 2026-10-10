package core

import (
	"errors"
	"fmt"
	"math"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"
	orbitv1 "orbit/gen/go/orbit/v1"
)

func validateWeeklyLimit(now time.Time, skew time.Duration, limit *orbitv1.CodexWeeklyLimit) error {
	if limit == nil {
		return nil
	}
	if math.IsNaN(limit.RemainingPercent) || math.IsInf(limit.RemainingPercent, 0) || limit.RemainingPercent < 0 || limit.RemainingPercent > 100 {
		return errors.New("invalid Codex weekly remaining percent")
	}
	if _, err := requiredTimestamp(limit.ResetsAt, "Codex weekly resets_at"); err != nil {
		return err
	}
	observedAt, err := requiredTimestamp(limit.ObservedAt, "Codex weekly observed_at")
	if err != nil {
		return err
	}
	freshUntil, err := requiredTimestamp(limit.FreshUntil, "Codex weekly fresh_until")
	if err != nil {
		return err
	}
	if observedAt.After(now.Add(skew)) || !freshUntil.After(observedAt) || freshUntil.After(observedAt.Add(5*time.Minute)) {
		return errors.New("invalid Codex weekly freshness window")
	}
	return nil
}

func projectWeeklyLimit(now time.Time, limit *orbitv1.CodexWeeklyLimit, view *orbitv1.DeviceView) {
	view.Primary = &orbitv1.DisplaySlot{Text: "--%", Emphasis: orbitv1.Emphasis_EMPHASIS_STRONG}
	view.Secondary = &orbitv1.DisplaySlot{Text: "-- --/--", Emphasis: orbitv1.Emphasis_EMPHASIS_NORMAL}
	view.Footer = &orbitv1.DisplaySlot{Text: "-- --:--", Emphasis: orbitv1.Emphasis_EMPHASIS_DIM}
	if limit == nil {
		view.Freshness = orbitv1.Freshness_FRESHNESS_STALE
		view.FreshUntil = timestamppb.New(now)
		return
	}
	expiresAt := minTime(view.FreshUntil.AsTime(), limit.FreshUntil.AsTime(), limit.ResetsAt.AsTime())
	view.FreshUntil = timestamppb.New(expiresAt)
	if !expiresAt.After(now) {
		view.Freshness = orbitv1.Freshness_FRESHNESS_STALE
	}
	// A passed reset cannot be presented as the current quota without another read.
	if !limit.ResetsAt.AsTime().After(now) {
		return
	}
	view.Primary.Text = fmt.Sprintf("%.0f%%", limit.RemainingPercent)
	reset := limit.ResetsAt.AsTime().In(time.FixedZone("Asia/Shanghai", 8*60*60))
	days, hours := weeklyRemaining(now, reset)
	view.Secondary.Text = fmt.Sprintf("%-2d %s", days, reset.Format("01/02"))
	view.Footer.Text = fmt.Sprintf("%-2d %s", hours, reset.Format("15:04"))
}

func weeklyRemaining(now, reset time.Time) (days, hours int64) {
	remaining := reset.Sub(now)
	if remaining <= 0 {
		return 0, 0
	}
	wholeHours := int64(remaining / time.Hour)
	return wholeHours / 24, wholeHours % 24
}
