package web

import (
	"encoding/json"
	"errors"
	"sync"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"
	orbitv1 "orbit/gen/go/orbit/v1"
)

type Snapshot struct {
	NodeID      string         `json:"node_id"`
	CoreEpoch   string         `json:"core_epoch"`
	Revision    uint64         `json:"revision"`
	Freshness   string         `json:"freshness"`
	ProducedAt  *time.Time     `json:"produced_at,omitempty"`
	ReceivedAt  time.Time      `json:"received_at"`
	FreshUntil  *time.Time     `json:"fresh_until,omitempty"`
	RetainUntil *time.Time     `json:"retain_until,omitempty"`
	Usage       *UsageSnapshot `json:"usage,omitempty"`
	Codex       *CodexSnapshot `json:"codex,omitempty"`
}

type UsageSnapshot struct {
	Freshness        string     `json:"freshness"`
	FreshUntil       *time.Time `json:"fresh_until,omitempty"`
	ActualCostMicros int64      `json:"actual_cost_micros"`
	CurrencyCode     string     `json:"currency_code"`
	TokenCount       uint64     `json:"token_count"`
	TPM              uint64     `json:"tpm"`
	ObservedAt       *time.Time `json:"observed_at,omitempty"`
}

type CodexSnapshot struct {
	Freshness    string            `json:"freshness"`
	FreshUntil   *time.Time        `json:"fresh_until,omitempty"`
	TotalCount   uint32            `json:"total_count"`
	RunningCount uint32            `json:"running_count"`
	ObservedAt   *time.Time        `json:"observed_at,omitempty"`
	Sessions     []SessionSnapshot `json:"sessions"`
}

type SessionSnapshot struct {
	ID           string     `json:"id"`
	DisplayName  string     `json:"display_name"`
	ProjectName  string     `json:"project_name"`
	Model        string     `json:"model"`
	Status       string     `json:"status"`
	UpdatedAt    *time.Time `json:"updated_at,omitempty"`
	ProcessAlive bool       `json:"process_alive"`
}

type Store struct {
	mu          sync.RWMutex
	latest      *Snapshot
	subscribers map[chan []byte]struct{}
}

func NewStore() *Store {
	return &Store{subscribers: make(map[chan []byte]struct{})}
}

func (s *Store) Update(view *orbitv1.DeviceView, receivedAt time.Time) error {
	if view == nil || view.Metadata == nil {
		return errors.New("device view metadata is required")
	}
	next := snapshotFromView(view, receivedAt)

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.latest != nil && s.latest.CoreEpoch == next.CoreEpoch && next.Revision <= s.latest.Revision {
		return errors.New("stale device view revision")
	}
	mergeCachedSections(&next, s.latest, receivedAt)
	payload, err := json.Marshal(next)
	if err != nil {
		return err
	}
	s.latest = &next
	for subscriber := range s.subscribers {
		select {
		case subscriber <- payload:
		default:
			select {
			case <-subscriber:
			default:
			}
			select {
			case subscriber <- payload:
			default:
			}
		}
	}
	return nil
}

func mergeCachedSections(next, previous *Snapshot, receivedAt time.Time) {
	if previous == nil || previous.NodeID != next.NodeID || previous.CoreEpoch != next.CoreEpoch ||
		previous.RetainUntil == nil || receivedAt.After(*previous.RetainUntil) {
		return
	}
	if next.Usage == nil && previous.Usage != nil {
		usage := *previous.Usage
		usage.Freshness = cachedFreshness(usage.FreshUntil, receivedAt)
		next.Usage = &usage
	}
	if next.Codex == nil && previous.Codex != nil {
		codex := *previous.Codex
		codex.Freshness = cachedFreshness(codex.FreshUntil, receivedAt)
		codex.Sessions = append([]SessionSnapshot(nil), previous.Codex.Sessions...)
		next.Codex = &codex
	}
}

func cachedFreshness(freshUntil *time.Time, receivedAt time.Time) string {
	if freshUntil == nil || receivedAt.After(*freshUntil) {
		return "stale"
	}
	return "fresh"
}

func (s *Store) Snapshot() (*Snapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.latest == nil {
		return nil, nil
	}
	payload, err := json.Marshal(s.latest)
	if err != nil {
		return nil, err
	}
	var clone Snapshot
	if err := json.Unmarshal(payload, &clone); err != nil {
		return nil, err
	}
	return &clone, nil
}

func (s *Store) Subscribe() (<-chan []byte, func()) {
	updates := make(chan []byte, 1)
	s.mu.Lock()
	if s.latest != nil {
		if payload, err := json.Marshal(s.latest); err == nil {
			updates <- payload
		}
	}
	s.subscribers[updates] = struct{}{}
	s.mu.Unlock()
	return updates, func() {
		s.mu.Lock()
		delete(s.subscribers, updates)
		s.mu.Unlock()
	}
}

func snapshotFromView(view *orbitv1.DeviceView, receivedAt time.Time) Snapshot {
	result := Snapshot{
		NodeID: view.NodeId, CoreEpoch: view.CoreEpoch, Revision: view.Metadata.Revision,
		Freshness: freshnessName(view.Freshness), ReceivedAt: receivedAt,
		ProducedAt: timestamp(view.Metadata.ProducedAt), FreshUntil: timestamp(view.FreshUntil), RetainUntil: timestamp(view.RetainUntil),
	}
	if usage := view.Usage; usage != nil {
		result.Usage = &UsageSnapshot{
			Freshness: freshnessName(usage.Freshness), FreshUntil: timestamp(usage.FreshUntil),
			ActualCostMicros: usage.GetActualCostMicros(), CurrencyCode: usage.CurrencyCode,
			TokenCount: usage.GetTokenCount(), TPM: usage.GetTpm(), ObservedAt: timestamp(usage.ObservedAt),
		}
	}
	if codex := view.Codex; codex != nil {
		result.Codex = &CodexSnapshot{
			Freshness: freshnessName(codex.Freshness), FreshUntil: timestamp(codex.FreshUntil),
			TotalCount: codex.TotalCount, RunningCount: codex.RunningCount,
			ObservedAt: timestamp(codex.ObservedAt), Sessions: make([]SessionSnapshot, 0, len(codex.Sessions)),
		}
		for _, session := range codex.Sessions {
			result.Codex.Sessions = append(result.Codex.Sessions, SessionSnapshot{
				ID: session.SessionId, DisplayName: session.DisplayName, ProjectName: session.ProjectName,
				Model: session.Model, Status: statusName(session.Status), UpdatedAt: timestamp(session.UpdatedAt),
				ProcessAlive: session.ProcessAlive,
			})
		}
	}
	return result
}

func timestamp(value *timestamppb.Timestamp) *time.Time {
	if value == nil {
		return nil
	}
	parsed := value.AsTime()
	return &parsed
}

func freshnessName(value orbitv1.Freshness) string {
	switch value {
	case orbitv1.Freshness_FRESHNESS_FRESH:
		return "fresh"
	case orbitv1.Freshness_FRESHNESS_STALE:
		return "stale"
	case orbitv1.Freshness_FRESHNESS_OFFLINE:
		return "offline"
	default:
		return "unknown"
	}
}

func statusName(value orbitv1.CodexSessionStatus) string {
	switch value {
	case orbitv1.CodexSessionStatus_CODEX_SESSION_STATUS_RUNNING:
		return "running"
	case orbitv1.CodexSessionStatus_CODEX_SESSION_STATUS_COMPLETED:
		return "completed"
	case orbitv1.CodexSessionStatus_CODEX_SESSION_STATUS_FAILED:
		return "failed"
	case orbitv1.CodexSessionStatus_CODEX_SESSION_STATUS_INTERRUPTED:
		return "interrupted"
	case orbitv1.CodexSessionStatus_CODEX_SESSION_STATUS_CANCELLED:
		return "cancelled"
	default:
		return "unknown"
	}
}
