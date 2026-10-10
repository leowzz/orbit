package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"time"
)

const weeklyWindowMinutes = 7 * 24 * 60

// WeeklyLimit contains only the account quota needed by the display.
type WeeklyLimit struct {
	RemainingPercent float64
	ResetsAt         time.Time
	ObservedAt       time.Time
	FreshUntil       time.Time
}

type rateLimitWindow struct {
	UsedPercent        *float64 `json:"usedPercent"`
	WindowDurationMins int      `json:"windowDurationMins"`
	ResetsAt           int64    `json:"resetsAt"`
}

type rateLimitBucket struct {
	LimitID   string           `json:"limitId"`
	Primary   *rateLimitWindow `json:"primary"`
	Secondary *rateLimitWindow `json:"secondary"`
}

func parseWeeklyLimit(data json.RawMessage) (*WeeklyLimit, error) {
	var response struct {
		RateLimits          rateLimitBucket            `json:"rateLimits"`
		RateLimitsByLimitID map[string]rateLimitBucket `json:"rateLimitsByLimitId"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, errors.New("invalid Codex rate limit response")
	}
	bucket := response.RateLimits
	if response.RateLimitsByLimitID != nil {
		var ok bool
		bucket, ok = response.RateLimitsByLimitID["codex"]
		if !ok {
			return nil, errors.New("Codex rate limit bucket unavailable")
		}
	}
	if bucket.LimitID != "" && bucket.LimitID != "codex" {
		return nil, errors.New("Codex rate limit bucket unavailable")
	}
	for _, window := range []*rateLimitWindow{bucket.Primary, bucket.Secondary} {
		if window == nil || window.WindowDurationMins != weeklyWindowMinutes {
			continue
		}
		if window.UsedPercent == nil || math.IsNaN(*window.UsedPercent) || math.IsInf(*window.UsedPercent, 0) || *window.UsedPercent < 0 || window.ResetsAt <= 0 {
			return nil, errors.New("invalid Codex weekly limit")
		}
		return &WeeklyLimit{RemainingPercent: math.Max(0, 100-*window.UsedPercent), ResetsAt: time.Unix(window.ResetsAt, 0).UTC()}, nil
	}
	return nil, errors.New("Codex weekly limit unavailable")
}

// readWeeklyLimit uses Codex's authenticated RPC, without reading or exporting tokens.
func readWeeklyLimit(ctx context.Context, binary, home string) (*WeeklyLimit, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "app-server", "--listen", "stdio://")
	cmd.Env = append(os.Environ(), "CODEX_HOME="+home)
	cmd.Stderr = io.Discard
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, errors.New("Codex app-server input unavailable")
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, errors.New("Codex app-server output unavailable")
	}
	if err := cmd.Start(); err != nil {
		return nil, errors.New("Codex app-server could not start")
	}
	defer func() {
		_ = stdin.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()
	encoder := json.NewEncoder(stdin)
	if err := encoder.Encode(map[string]any{"id": 1, "method": "initialize", "params": map[string]any{"clientInfo": map[string]string{"name": "orbit", "title": "Orbit", "version": "0.1.0"}}}); err != nil {
		return nil, errors.New("Codex app-server initialization failed")
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	for scanner.Scan() {
		var response struct {
			ID     int             `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  json.RawMessage `json:"error"`
		}
		if json.Unmarshal(scanner.Bytes(), &response) != nil {
			continue
		}
		if response.ID != 1 && response.ID != 2 {
			continue
		}
		if len(response.Error) > 0 && string(response.Error) != "null" {
			return nil, errors.New("Codex account rate limit RPC failed")
		}
		if response.ID == 2 {
			return parseWeeklyLimit(response.Result)
		}
		if err := encoder.Encode(map[string]any{"method": "initialized"}); err != nil {
			return nil, errors.New("Codex app-server initialization failed")
		}
		if err := encoder.Encode(map[string]any{"id": 2, "method": "account/rateLimits/read"}); err != nil {
			return nil, errors.New("Codex account rate limit request failed")
		}
	}
	if ctx.Err() != nil {
		return nil, fmt.Errorf("Codex account rate limit request: %w", ctx.Err())
	}
	return nil, errors.New("Codex app-server closed before rate limit response")
}

func (s *Source) weeklyLimit(ctx context.Context, now time.Time) *WeeklyLimit {
	if !s.rateLimitsEnabled {
		return nil
	}
	if !now.Before(s.nextRateLimitsRead) {
		s.nextRateLimitsRead = now.Add(time.Minute)
		if limit, err := readWeeklyLimit(ctx, s.codexBinary, s.home); err == nil && limit.ResetsAt.After(now) {
			limit.ObservedAt = now
			limit.FreshUntil = now.Add(3 * time.Minute)
			s.lastWeeklyLimit = limit
		}
	}
	if s.lastWeeklyLimit == nil {
		return nil
	}
	copy := *s.lastWeeklyLimit
	return &copy
}
