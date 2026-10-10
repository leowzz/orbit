package codex

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFetchReadsNewestDatabasesAndMapsSessionState(t *testing.T) {
	home := t.TempDir()
	now := time.Now().Truncate(time.Second)
	oldState := filepath.Join(home, "state_old.sqlite")
	newState := filepath.Join(home, "state_new.sqlite")
	oldHistory := filepath.Join(home, "thread_history_old.sqlite")
	newHistory := filepath.Join(home, "thread_history_new.sqlite")

	createStateDB(t, oldState, true, []stateFixture{{id: "old", updatedAt: now.Add(-time.Hour)}})
	createStateDB(t, newState, true, []stateFixture{
		{id: "running", name: "Active display", title: "Active title", source: "vscode", cwd: "/tmp/project", model: "gpt", updatedAt: now},
		{id: "pending", title: "", firstMessage: "First prompt", source: "vscode", cwd: "/tmp/project", model: "gpt", updatedAt: now.Add(-5 * time.Second)},
		{id: "done", name: "Done display", title: "Done title", source: "exec", cwd: "/tmp/other", model: "gpt", updatedAt: now.Add(-2 * time.Minute)},
		{id: "archived", name: "Archived", source: "vscode", cwd: "/tmp/archive", model: "gpt", updatedAt: now.Add(-3 * time.Minute), archived: true},
	})
	createHistoryDB(t, oldHistory, []turnFixture{{threadID: "old", turnID: "old-turn", status: "completed", startedAt: now.Add(-time.Hour)}})
	createHistoryDB(t, newHistory, []turnFixture{
		{threadID: "running", turnID: "old-turn", status: "completed", startedAt: now.Add(-2 * time.Minute), completedAt: now.Add(-90 * time.Second)},
		{threadID: "running", turnID: "new-turn", status: "inProgress", startedAt: now.Add(-time.Minute)},
		{threadID: "pending", turnID: "pending-turn", status: "interrupted", startedAt: now.Add(-30 * time.Second), completedAt: now.Add(-10 * time.Second)},
		{threadID: "done", turnID: "done-turn", status: "completed", startedAt: now.Add(-2 * time.Minute), completedAt: now.Add(-90 * time.Second)},
		{threadID: "archived", turnID: "archived-turn", status: "failed", startedAt: now.Add(-3 * time.Minute), completedAt: now.Add(-2 * time.Minute)},
		{threadID: "turn-only", turnID: "turn-only-turn", status: "inProgress", startedAt: now.Add(-10 * time.Second)},
	})
	setMtime(t, oldState, now.Add(-2*time.Hour))
	setMtime(t, newState, now.Add(-time.Minute))
	setMtime(t, oldHistory, now.Add(-2*time.Hour))
	setMtime(t, newHistory, now)

	processDir := filepath.Join(home, "process_manager")
	if err := os.Mkdir(processDir, 0o755); err != nil {
		t.Fatal(err)
	}
	processes, err := json.Marshal([]map[string]any{
		{"conversationId": "running", "osPid": os.Getpid()},
		{"conversationId": "done", "osPid": 99999999},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(processDir, "chat_processes.json"), processes, 0o644); err != nil {
		t.Fatal(err)
	}

	source, err := New(Config{Home: home, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	source.now = func() time.Time { return now }
	snapshot, err := source.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.TotalCount != 3 || snapshot.RunningCount != 1 {
		t.Fatalf("counts = total %d running %d, want total 3 running 1", snapshot.TotalCount, snapshot.RunningCount)
	}
	if len(snapshot.Sessions) != 3 {
		t.Fatalf("got %d sessions, want 3", len(snapshot.Sessions))
	}
	if got := snapshot.Sessions[0]; got.ID != "running" || got.Status != "running" || !got.ProcessAlive || got.DisplayName != "Active display" || got.ProjectName != "project" {
		t.Fatalf("running session = %+v", got)
	}
	if got := snapshot.Sessions[1]; got.ID != "pending" || got.Status != "interrupted" || got.DisplayName != "First prompt" {
		t.Fatalf("pending session = %+v", got)
	}
	if got := snapshot.Sessions[2]; got.ID != "done" || got.Status != "completed" || got.ProcessAlive {
		t.Fatalf("done session = %+v", got)
	}
	for _, session := range snapshot.Sessions {
		if session.ID == "old" || session.ID == "archived" || session.ID == "turn-only" {
			t.Fatalf("unexpected session %q", session.ID)
		}
	}
}

func TestTurnStatusCodes(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{raw: "inProgress", want: "running"},
		{raw: "running", want: "running"},
		{raw: "completed", want: "completed"},
		{raw: "failed", want: "failed"},
		{raw: "error", want: "failed"},
		{raw: "interrupted", want: "interrupted"},
		{raw: "cancelled", want: "cancelled"},
		{raw: "canceled", want: "cancelled"},
		{raw: "future_status", want: "unknown"},
	}
	for _, test := range tests {
		t.Run(test.raw, func(t *testing.T) {
			if got := (turnStatus{rawStatus: test.raw}).code(); got != test.want {
				t.Fatalf("status %q = %q, want %q", test.raw, got, test.want)
			}
		})
	}
}

func TestFetchMetadataUpdatePreservesTerminalStatus(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	for _, status := range []string{"completed", "interrupted", "failed", "cancelled"} {
		for _, withRollout := range []bool{false, true} {
			name := status + "/missing-rollout"
			if withRollout {
				name = status + "/matching-rollout"
			}
			t.Run(name, func(t *testing.T) {
				home := t.TempDir()
				startedAt := now.Add(-26 * time.Hour)
				completedAt := startedAt.Add(time.Minute)
				rolloutPath := ""
				if withRollout {
					rolloutPath = filepath.Join(home, "rollout.jsonl")
					eventType := "task_complete"
					if status != "completed" {
						eventType = "turn_aborted"
					}
					writeRolloutEvents(t, rolloutPath, []map[string]any{{
						"type": "event_msg", "payload": map[string]any{
							"type": eventType, "turn_id": "done-turn", "reason": status,
							"started_at": startedAt.Unix(), "completed_at": completedAt.Unix(),
						},
					}, {
						"type": "event_msg", "payload": map[string]any{"type": "thread_settings_applied"},
					}})
				}
				createStateDB(t, filepath.Join(home, "state_1.sqlite"), true, []stateFixture{{
					id: "done", source: "vscode", updatedAt: now, rolloutPath: rolloutPath,
				}})
				createHistoryDB(t, filepath.Join(home, "thread_history_1.sqlite"), []turnFixture{{
					threadID: "done", turnID: "done-turn", status: status,
					startedAt: startedAt, completedAt: completedAt,
				}})
				source, err := New(Config{Home: home})
				if err != nil {
					t.Fatal(err)
				}
				source.now = func() time.Time { return now }
				snapshot, err := source.Fetch(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				if snapshot.RunningCount != 0 || len(snapshot.Sessions) != 1 || snapshot.Sessions[0].Status != status {
					t.Fatalf("metadata update snapshot = %+v, want status %s", snapshot, status)
				}
			})
		}
	}
}

func TestFetchReconcilesStaleHistoryFromRollout(t *testing.T) {
	home := t.TempDir()
	now := time.Date(2026, 9, 4, 16, 4, 21, 0, time.Local)
	rolloutPath := filepath.Join(home, "rollout.jsonl")
	writeRolloutEvents(t, rolloutPath, []map[string]any{
		{
			"type": "event_msg",
			"payload": map[string]any{
				"type":       "task_started",
				"turn_id":    "current-turn",
				"started_at": now.Add(-14 * time.Minute).Unix(),
			},
		},
		{
			"type": "response_item",
			"payload": map[string]any{
				"type":   "custom_tool_call_output",
				"output": strings.Repeat("x", int(rolloutTailBytes)),
			},
		},
		{
			"type": "event_msg",
			"payload": map[string]any{
				"type":         "task_complete",
				"turn_id":      "current-turn",
				"started_at":   now.Add(-14 * time.Minute).Unix(),
				"completed_at": now.Unix(),
			},
		},
	})
	createStateDB(t, filepath.Join(home, "state_1.sqlite"), true, []stateFixture{{
		id: "stale", title: "Stale projection", source: "vscode", cwd: "/tmp/project", model: "gpt",
		updatedAt: now, rolloutPath: rolloutPath,
	}})
	createHistoryDB(t, filepath.Join(home, "thread_history_1.sqlite"), []turnFixture{{
		threadID: "stale", turnID: "old-turn", status: "interrupted",
		startedAt: now.Add(-15 * time.Minute), completedAt: now.Add(-14*time.Minute - 30*time.Second),
	}})

	source, err := New(Config{Home: home, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	source.now = func() time.Time { return now }
	snapshot, err := source.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.RunningCount != 0 || len(snapshot.Sessions) != 1 || snapshot.Sessions[0].Status != "completed" {
		t.Fatalf("snapshot with stale history projection = %+v", snapshot)
	}
}

func TestFetchKeepsCurrentRolloutTurnRunning(t *testing.T) {
	home := t.TempDir()
	now := time.Date(2026, 9, 4, 16, 4, 21, 0, time.Local)
	rolloutPath := filepath.Join(home, "rollout.jsonl")
	writeRolloutEvents(t, rolloutPath, []map[string]any{
		{
			"type": "event_msg",
			"payload": map[string]any{
				"type":         "task_complete",
				"turn_id":      "old-turn",
				"started_at":   now.Add(-2 * time.Minute).Unix(),
				"completed_at": now.Add(-time.Minute).Unix(),
			},
		},
		{
			"type": "event_msg",
			"payload": map[string]any{
				"type":       "task_started",
				"turn_id":    "current-turn",
				"started_at": now.Unix(),
			},
		},
	})
	createStateDB(t, filepath.Join(home, "state_1.sqlite"), true, []stateFixture{{
		id: "running", title: "Running", source: "vscode", cwd: "/tmp/project", model: "gpt",
		updatedAt: now, rolloutPath: rolloutPath,
	}})
	createHistoryDB(t, filepath.Join(home, "thread_history_1.sqlite"), []turnFixture{{
		threadID: "running", turnID: "old-turn", status: "completed",
		startedAt: now.Add(-2 * time.Minute), completedAt: now.Add(-time.Minute),
	}})

	source, err := New(Config{Home: home, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	source.now = func() time.Time { return now }
	snapshot, err := source.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.RunningCount != 1 || len(snapshot.Sessions) != 1 || snapshot.Sessions[0].Status != "running" {
		t.Fatalf("snapshot with current rollout turn = %+v", snapshot)
	}
}

func TestFetchReconcilesLegacyTerminalEvents(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	for _, test := range []struct {
		name          string
		turnID        string
		eventType     string
		completedAt   time.Time
		includeStart  bool
		timestampOnly bool
		want          string
	}{
		{name: "start retained", turnID: "new-turn", eventType: "task_complete", completedAt: now, includeStart: true, want: "completed"},
		{name: "start outside tail", turnID: "new-turn", eventType: "task_complete", completedAt: now, want: "completed"},
		{name: "same turn", turnID: "current-turn", eventType: "task_complete", completedAt: now, want: "completed"},
		{name: "aborted timestamp", turnID: "new-turn", eventType: "turn_aborted", completedAt: now, timestampOnly: true, want: "interrupted"},
		{name: "older completion", turnID: "old-turn", eventType: "task_complete", completedAt: now.Add(-2 * time.Minute), want: "running"},
	} {
		t.Run(test.name, func(t *testing.T) {
			home := t.TempDir()
			rolloutPath := filepath.Join(home, "rollout.jsonl")
			events := []map[string]any{{
				"type": "event_msg",
				"payload": map[string]any{
					"type": "task_started", "turn_id": test.turnID, "started_at": now.Add(-30 * time.Second).Unix(),
				},
			}}
			if !test.includeStart {
				// Force the start outside the 1 MiB tail, as happens with large tool output.
				events = append(events, map[string]any{
					"type": "response_item", "payload": map[string]any{"output": strings.Repeat("x", int(rolloutTailBytes))},
				})
			}
			payload := map[string]any{"type": test.eventType, "turn_id": test.turnID}
			if !test.timestampOnly {
				payload["completed_at"] = test.completedAt.Unix()
			}
			events = append(events, map[string]any{
				"type": "event_msg", "timestamp": test.completedAt.Format(time.RFC3339Nano), "payload": payload,
			})
			writeRolloutEvents(t, rolloutPath, events)
			createStateDB(t, filepath.Join(home, "state_1.sqlite"), true, []stateFixture{{
				id: "one", source: "vscode", updatedAt: now, rolloutPath: rolloutPath,
			}})
			createHistoryDB(t, filepath.Join(home, "thread_history_1.sqlite"), []turnFixture{{
				threadID: "one", turnID: "current-turn", status: "inProgress", startedAt: now.Add(-time.Minute),
			}})
			source, err := New(Config{Home: home})
			if err != nil {
				t.Fatal(err)
			}
			source.now = func() time.Time { return now }
			snapshot, err := source.Fetch(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			wantRunning := 0
			if test.want == "running" {
				wantRunning = 1
			}
			if len(snapshot.Sessions) != 1 || snapshot.Sessions[0].Status != test.want || snapshot.RunningCount != wantRunning {
				t.Fatalf("legacy rollout snapshot = %+v, want status %s running %d", snapshot, test.want, wantRunning)
			}
		})
	}
}

func TestFetchExpiresOnlyInactiveUnclosedTurnsBeforeCountsAndLimit(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	old := now.Add(-30 * 24 * time.Hour)
	for _, test := range []struct {
		name       string
		updatedAt  time.Time
		startedAt  time.Time
		rolloutAt  time.Time
		processPID int
		want       string
	}{
		{name: "orphaned", updatedAt: old, startedAt: old, rolloutAt: old, want: "unknown"},
		{name: "dead process", updatedAt: old, startedAt: old, rolloutAt: old, processPID: 99999999, want: "unknown"},
		{name: "live process", updatedAt: old, startedAt: old, rolloutAt: old, processPID: os.Getpid(), want: "running"},
		{name: "recent state", updatedAt: now.Add(-time.Hour), startedAt: old, rolloutAt: old, want: "running"},
		{name: "recent start", updatedAt: old, startedAt: now.Add(-time.Hour), rolloutAt: old, want: "running"},
		{name: "ongoing rollout", updatedAt: old, startedAt: old, rolloutAt: now.Add(-time.Hour), want: "running"},
	} {
		t.Run(test.name, func(t *testing.T) {
			home := t.TempDir()
			rolloutPath := filepath.Join(home, "rollout.jsonl")
			writeRolloutEvents(t, rolloutPath, []map[string]any{{
				"type": "event_msg",
				"payload": map[string]any{
					"type": "task_started", "turn_id": "unclosed-turn", "started_at": test.startedAt.Unix(),
				},
			}})
			setMtime(t, rolloutPath, test.rolloutAt)
			createStateDB(t, filepath.Join(home, "state_1.sqlite"), true, []stateFixture{
				{id: "current", source: "vscode", updatedAt: now},
				{id: "unclosed", source: "vscode", updatedAt: test.updatedAt, rolloutPath: rolloutPath},
			})
			createHistoryDB(t, filepath.Join(home, "thread_history_1.sqlite"), []turnFixture{
				{threadID: "current", turnID: "current-turn", status: "inProgress", startedAt: now},
				{threadID: "unclosed", turnID: "unclosed-turn", status: "inProgress", startedAt: test.startedAt},
			})
			if test.processPID != 0 {
				processDir := filepath.Join(home, "process_manager")
				if err := os.Mkdir(processDir, 0o755); err != nil {
					t.Fatal(err)
				}
				processes, err := json.Marshal([]map[string]any{{"conversationId": "unclosed", "osPid": test.processPID}})
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(processDir, "chat_processes.json"), processes, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			source, err := New(Config{Home: home, Limit: 1})
			if err != nil {
				t.Fatal(err)
			}
			source.now = func() time.Time { return now }
			snapshot, err := source.Fetch(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			wantRunning := 1
			if test.want == "running" {
				wantRunning++
			}
			if snapshot.TotalCount != 2 || snapshot.RunningCount != wantRunning || len(snapshot.Sessions) != 1 || snapshot.Sessions[0].ID != "current" {
				t.Fatalf("limited snapshot = %+v, want running %d", snapshot, wantRunning)
			}
			source.limit = 2
			snapshot, err = source.Fetch(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if snapshot.Sessions[1].Status != test.want {
				t.Fatalf("unclosed status = %s, want %s", snapshot.Sessions[1].Status, test.want)
			}
		})
	}
}

func TestFetchAppliesFiltersAndArchivedOptionBeforeLimit(t *testing.T) {
	home := t.TempDir()
	now := time.Now().Truncate(time.Second)
	statePath := filepath.Join(home, "state_1.sqlite")
	historyPath := filepath.Join(home, "thread_history_1.sqlite")
	createStateDB(t, statePath, true, []stateFixture{
		{id: "cwd", title: "CWD", source: "vscode", cwd: "/tmp/filtered/project", model: "gpt", updatedAt: now},
		{id: "source", title: "Source", source: "exec", cwd: "/tmp/other", model: "gpt", updatedAt: now.Add(-time.Minute)},
		{id: "archived", title: "Archived", source: "vscode", cwd: "/tmp/other", model: "gpt", updatedAt: now.Add(-2 * time.Minute), archived: true},
		{id: "kept", title: "Kept", source: "vscode", cwd: "/tmp/other", model: "gpt", updatedAt: now.Add(-3 * time.Minute)},
	})
	createHistoryDB(t, historyPath, []turnFixture{
		{threadID: "cwd", status: "completed", startedAt: now, completedAt: now},
		{threadID: "source", status: "completed", startedAt: now.Add(-time.Minute), completedAt: now.Add(-time.Minute)},
		{threadID: "archived", status: "cancelled", startedAt: now.Add(-2 * time.Minute), completedAt: now.Add(-2 * time.Minute)},
		{threadID: "kept", status: "completed", startedAt: now.Add(-3 * time.Minute), completedAt: now.Add(-3 * time.Minute)},
	})

	source, err := New(Config{
		Home:            home,
		Limit:           1,
		IgnoreCWD:       []string{"filtered"},
		IgnoreSource:    []string{"exec"},
		IncludeArchived: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := source.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.TotalCount != 1 || len(snapshot.Sessions) != 1 || snapshot.Sessions[0].ID != "kept" {
		t.Fatalf("filtered snapshot = %+v", snapshot)
	}

	source.includeArchived = true
	snapshot, err = source.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.TotalCount != 2 || len(snapshot.Sessions) != 1 || snapshot.Sessions[0].ID != "archived" {
		t.Fatalf("archived snapshot = %+v", snapshot)
	}
}

func TestFetchSkipsSubagentAndDayflowChatCLISessionsBeforeCountsAndLimit(t *testing.T) {
	home := t.TempDir()
	now := time.Now().Truncate(time.Second)
	statePath := filepath.Join(home, "state_1.sqlite")
	historyPath := filepath.Join(home, "thread_history_1.sqlite")
	createStateDB(t, statePath, true, []stateFixture{
		{id: "structured-subagent", title: "Structured", source: `{"subagent":{"other":"guardian"}}`, cwd: "/tmp/subagent", model: "gpt", updatedAt: now},
		{id: "spawned-subagent", title: "Spawned", source: "vscode", cwd: "/tmp/subagent", model: "gpt", updatedAt: now.Add(-time.Minute)},
		{id: "dayflow-chatcli", title: "Dayflow", source: "exec", cwd: "/Users/test/Library/Application Support/Dayflow/chatcli", model: "gpt", updatedAt: now.Add(-2 * time.Minute)},
		{id: "ordinary-chatcli", title: "Ordinary", source: "vscode", cwd: "/Users/test/work/chatcli", model: "gpt", updatedAt: now.Add(-3 * time.Minute)},
		{id: "root", title: "Root", source: "vscode", cwd: "/tmp/root", model: "gpt", updatedAt: now.Add(-4 * time.Minute)},
	})
	createSpawnEdges(t, statePath, "spawned-subagent")
	createHistoryDB(t, historyPath, []turnFixture{
		{threadID: "structured-subagent", status: "inProgress", startedAt: now},
		{threadID: "spawned-subagent", status: "inProgress", startedAt: now.Add(-time.Minute)},
		{threadID: "dayflow-chatcli", status: "inProgress", startedAt: now.Add(-2 * time.Minute)},
		{threadID: "ordinary-chatcli", status: "completed", startedAt: now.Add(-3 * time.Minute), completedAt: now.Add(-3 * time.Minute)},
		{threadID: "root", status: "completed", startedAt: now.Add(-4 * time.Minute), completedAt: now.Add(-4 * time.Minute)},
	})

	source, err := New(Config{Home: home, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := source.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.TotalCount != 2 || snapshot.RunningCount != 0 || len(snapshot.Sessions) != 1 || snapshot.Sessions[0].ID != "ordinary-chatcli" {
		t.Fatalf("snapshot with subagents = %+v", snapshot)
	}
}

func TestFetchSupportsThreadsWithoutNameColumn(t *testing.T) {
	home := t.TempDir()
	now := time.Now().Truncate(time.Second)
	createStateDB(t, filepath.Join(home, "state_1.sqlite"), false, []stateFixture{
		{id: "title", title: "Title fallback", source: "vscode", cwd: "/tmp/title", model: "gpt", updatedAt: now},
		{id: "message", firstMessage: "Message fallback", source: "vscode", cwd: "/tmp/message", model: "gpt", updatedAt: now.Add(-time.Minute)},
	})
	createHistoryDB(t, filepath.Join(home, "thread_history_1.sqlite"), []turnFixture{
		{threadID: "title", status: "completed", startedAt: now, completedAt: now},
		{threadID: "message", status: "failed", startedAt: now.Add(-time.Minute), completedAt: now.Add(-time.Minute)},
	})

	source, err := New(Config{Home: home, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := source.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Sessions[0].DisplayName != "Title fallback" || snapshot.Sessions[1].DisplayName != "Message fallback" {
		t.Fatalf("fallback names = %+v", snapshot.Sessions)
	}
}

func TestFetchReturnsTypedErrorsForCriticalProjectionFailures(t *testing.T) {
	home := t.TempDir()
	source, err := New(Config{Home: home})
	if err != nil {
		t.Fatal(err)
	}
	_, err = source.Fetch(context.Background())
	if !IsKind(err, ErrorStateUnavailable) {
		t.Fatalf("missing state error = %v, want %s", err, ErrorStateUnavailable)
	}

	createStateDB(t, filepath.Join(home, "state_1.sqlite"), true, nil)
	_, err = source.Fetch(context.Background())
	if !IsKind(err, ErrorHistoryUnavailable) {
		t.Fatalf("missing history error = %v, want %s", err, ErrorHistoryUnavailable)
	}

	if _, err := New(Config{Home: home, Limit: -1}); !IsKind(err, ErrorInvalidConfig) {
		t.Fatalf("invalid config error = %v, want %s", err, ErrorInvalidConfig)
	}
}

func TestProcessFileIsNonFatalAndSQLiteIsReadOnly(t *testing.T) {
	home := t.TempDir()
	now := time.Now().Truncate(time.Second)
	statePath := filepath.Join(home, "state_1.sqlite")
	historyPath := filepath.Join(home, "thread_history_1.sqlite")
	createStateDB(t, statePath, true, []stateFixture{{id: "one", title: "One", source: "vscode", cwd: "/tmp/one", model: "gpt", updatedAt: now}})
	createHistoryDB(t, historyPath, []turnFixture{{threadID: "one", status: "completed", startedAt: now, completedAt: now}})
	processDir := filepath.Join(home, "process_manager")
	if err := os.Mkdir(processDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(processDir, "chat_processes.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}

	source, err := New(Config{Home: home})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}

	readOnly, err := sql.Open("sqlite", sqliteDSN(statePath))
	if err != nil {
		t.Fatal(err)
	}
	defer readOnly.Close()
	var busyTimeout int
	if err := readOnly.QueryRow("PRAGMA busy_timeout").Scan(&busyTimeout); err != nil {
		t.Fatal(err)
	}
	if busyTimeout != int(readBusyTimeout.Milliseconds()) {
		t.Fatalf("busy_timeout = %d, want %d", busyTimeout, readBusyTimeout.Milliseconds())
	}
	if _, err := readOnly.Exec("CREATE TABLE should_fail (id INTEGER)"); err == nil {
		t.Fatal("read-only SQLite connection accepted a write")
	}
}

func TestLiveSmoke(t *testing.T) {
	if os.Getenv("ORBIT_CODEX_LIVE_TEST") != "1" {
		t.Skip("set ORBIT_CODEX_LIVE_TEST=1 to read the local Codex projections")
	}
	source, err := New(Config{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := source.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.TotalCount <= 0 || len(snapshot.Sessions) <= 0 {
		t.Fatalf("live Codex snapshot is empty: sessions=%d total=%d", len(snapshot.Sessions), snapshot.TotalCount)
	}
	t.Logf("live Codex snapshot: sessions=%d total=%d running=%d", len(snapshot.Sessions), snapshot.TotalCount, snapshot.RunningCount)
}

type stateFixture struct {
	id           string
	name         string
	title        string
	source       string
	cwd          string
	model        string
	firstMessage string
	updatedAt    time.Time
	archived     bool
	rolloutPath  string
}

type turnFixture struct {
	threadID    string
	turnID      string
	status      string
	startedAt   time.Time
	completedAt time.Time
}

func createStateDB(t *testing.T, path string, withName bool, fixtures []stateFixture) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	nameColumn := ""
	if withName {
		nameColumn = ", name TEXT"
	}
	if _, err := db.Exec("CREATE TABLE threads (id TEXT, title TEXT" + nameColumn + ", source TEXT, cwd TEXT, model TEXT, updated_at_ms INTEGER, first_user_message TEXT, archived INTEGER, rollout_path TEXT)"); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		args := []any{fixture.id, fixture.title}
		if withName {
			args = append(args, fixture.name)
		}
		args = append(args, fixture.source, fixture.cwd, fixture.model, fixture.updatedAt.UnixMilli(), fixture.firstMessage, fixture.archived, fixture.rolloutPath)
		query := "INSERT INTO threads (id, title"
		if withName {
			query += ", name"
		}
		query += ", source, cwd, model, updated_at_ms, first_user_message, archived, rollout_path) VALUES ("
		query += strings.TrimRight(strings.Repeat("?,", len(args)), ",") + ")"
		if _, err := db.Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
}

func writeRolloutEvents(t *testing.T, path string, events []map[string]any) {
	t.Helper()
	var contents []byte
	for _, event := range events {
		line, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		contents = append(contents, line...)
		contents = append(contents, '\n')
	}
	if err := os.WriteFile(path, contents, 0o644); err != nil {
		t.Fatal(err)
	}
}

func createHistoryDB(t *testing.T, path string, fixtures []turnFixture) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("CREATE TABLE thread_turns (thread_id TEXT, turn_id TEXT, status TEXT, started_at INTEGER, completed_at INTEGER)"); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		var startedAt, completedAt any
		if !fixture.startedAt.IsZero() {
			startedAt = fixture.startedAt.Unix()
		}
		if !fixture.completedAt.IsZero() {
			completedAt = fixture.completedAt.Unix()
		}
		if _, err := db.Exec("INSERT INTO thread_turns (thread_id, turn_id, status, started_at, completed_at) VALUES (?, ?, ?, ?, ?)", fixture.threadID, fixture.turnID, fixture.status, startedAt, completedAt); err != nil {
			t.Fatal(err)
		}
	}
}

func createSpawnEdges(t *testing.T, path string, childIDs ...string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("CREATE TABLE thread_spawn_edges (parent_thread_id TEXT NOT NULL, child_thread_id TEXT NOT NULL PRIMARY KEY, status TEXT NOT NULL)"); err != nil {
		t.Fatal(err)
	}
	for _, childID := range childIDs {
		if _, err := db.Exec("INSERT INTO thread_spawn_edges (parent_thread_id, child_thread_id, status) VALUES (?, ?, ?)", "parent", childID, "open"); err != nil {
			t.Fatal(err)
		}
	}
}

func setMtime(t *testing.T, path string, value time.Time) {
	t.Helper()
	if err := os.Chtimes(path, value, value); err != nil {
		t.Fatal(err)
	}
}
