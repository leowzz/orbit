package codex

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	_ "modernc.org/sqlite"
)

const (
	defaultLimit            = 20
	pendingTurnGrace        = 5 * time.Minute
	pendingTurnMinimumDelta = 2 * time.Second
	readBusyTimeout         = 2 * time.Second
)

// Config controls which Codex projection files are read and how sessions are filtered.
type Config struct {
	Home            string
	Limit           int
	IncludeArchived bool
	IgnoreCWD       []string
	IgnoreSource    []string
}

// Session is the sanitized session state exposed to Orbit callers.
type Session struct {
	ID           string
	DisplayName  string
	ProjectName  string
	Model        string
	Status       string
	UpdatedAt    time.Time
	ProcessAlive bool
}

// Snapshot is one consistent read of Codex's state and turn projections.
type Snapshot struct {
	Sessions     []Session
	TotalCount   int
	RunningCount int
}

// ErrorKind is stable for callers that need to preserve the previous observation.
type ErrorKind string

const (
	ErrorInvalidConfig      ErrorKind = "invalid_config"
	ErrorStateUnavailable   ErrorKind = "state_unavailable"
	ErrorHistoryUnavailable ErrorKind = "history_unavailable"
	ErrorStateRead          ErrorKind = "state_read_error"
	ErrorHistoryRead        ErrorKind = "history_read_error"
)

// Error describes a failure without exposing process details or database contents.
type Error struct {
	Kind      ErrorKind
	Operation string
	Cause     error
}

func (e *Error) Error() string {
	message := fmt.Sprintf("codex %s: %s", e.Operation, e.Kind)
	if e.Cause != nil {
		message += ": " + e.Cause.Error()
	}
	return message
}

func (e *Error) Unwrap() error { return e.Cause }

// IsKind reports whether err or one of its wrapped causes has the requested kind.
func IsKind(err error, kind ErrorKind) bool {
	var sourceErr *Error
	return errors.As(err, &sourceErr) && sourceErr.Kind == kind
}

// Source reads Codex's local SQLite projections without modifying them.
type Source struct {
	home            string
	limit           int
	includeArchived bool
	ignoreCWD       []string
	ignoreSource    map[string]struct{}
	now             func() time.Time
}

// New validates the adapter configuration. The home directory is not accessed until Fetch.
func New(config Config) (*Source, error) {
	if config.Limit < 0 {
		return nil, &Error{Kind: ErrorInvalidConfig, Operation: "configure", Cause: errors.New("limit must not be negative")}
	}
	home, err := resolveHome(config.Home)
	if err != nil {
		return nil, &Error{Kind: ErrorInvalidConfig, Operation: "configure", Cause: err}
	}
	for _, pattern := range config.IgnoreCWD {
		if pattern == "" {
			return nil, &Error{Kind: ErrorInvalidConfig, Operation: "configure", Cause: errors.New("ignore cwd patterns must not be empty")}
		}
	}
	ignoreSource := make(map[string]struct{}, len(config.IgnoreSource))
	for _, source := range config.IgnoreSource {
		ignoreSource[source] = struct{}{}
	}
	limit := config.Limit
	if limit == 0 {
		limit = defaultLimit
	}
	return &Source{
		home:            home,
		limit:           limit,
		includeArchived: config.IncludeArchived,
		ignoreCWD:       append([]string(nil), config.IgnoreCWD...),
		ignoreSource:    ignoreSource,
		now:             time.Now,
	}, nil
}

// Fetch returns the current and recent displayable sessions.
func (s *Source) Fetch(ctx context.Context) (Snapshot, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	statePath, err := newestDatabase(s.home, "state_*.sqlite")
	if err != nil {
		return Snapshot{}, &Error{Kind: ErrorStateUnavailable, Operation: "discover state", Cause: err}
	}
	historyPath, err := newestDatabase(s.home, "thread_history_*.sqlite")
	if err != nil {
		return Snapshot{}, &Error{Kind: ErrorHistoryUnavailable, Operation: "discover history", Cause: err}
	}

	sessions, err := readSessions(ctx, statePath, s.includeArchived)
	if err != nil {
		return Snapshot{}, &Error{Kind: ErrorStateRead, Operation: "read state", Cause: err}
	}
	turns, err := readLatestTurns(ctx, historyPath)
	if err != nil {
		return Snapshot{}, &Error{Kind: ErrorHistoryRead, Operation: "read history", Cause: err}
	}
	processes := readProcesses(filepath.Join(s.home, "process_manager", "chat_processes.json"))

	visible := make([]Session, 0, len(sessions))
	now := time.Now()
	if s.now != nil {
		now = s.now()
	}
	for id, record := range sessions {
		session := record.Session
		turn, ok := turns[id]
		if ok {
			session.Status = turn.code()
			session.ProcessAlive = processes[id]
			if isPendingRunning(session.UpdatedAt, turn, now) {
				session.Status = "running"
			}
		}
		if isIgnored(record, s.ignoreCWD, s.ignoreSource) {
			continue
		}
		visible = append(visible, session)
	}

	sort.SliceStable(visible, func(i, j int) bool {
		left, right := visible[i].UpdatedAt, visible[j].UpdatedAt
		if left.IsZero() {
			return false
		}
		if right.IsZero() {
			return true
		}
		return left.After(right)
	})

	runningCount := 0
	for _, session := range visible {
		if session.Status == "running" {
			runningCount++
		}
	}
	result := Snapshot{
		TotalCount:   len(visible),
		RunningCount: runningCount,
	}
	if s.limit >= len(visible) {
		result.Sessions = visible
	} else {
		result.Sessions = visible[:s.limit]
	}
	return result, nil
}

type turnStatus struct {
	rawStatus   string
	startedAt   *time.Time
	completedAt *time.Time
}

type sessionRecord struct {
	Session
	cwd    string
	source string
}

func (turn turnStatus) code() string {
	normalized := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(turn.rawStatus)), "_", "")
	switch normalized {
	case "inprogress", "running":
		return "running"
	case "completed":
		return "completed"
	case "failed", "error":
		return "failed"
	case "interrupted":
		return "interrupted"
	case "cancelled", "canceled":
		return "cancelled"
	default:
		return "unknown"
	}
}

func isPendingRunning(updatedAt time.Time, turn turnStatus, now time.Time) bool {
	if turn.code() == "running" {
		return true
	}
	if updatedAt.IsZero() || turn.completedAt == nil {
		return false
	}
	age := now.Sub(updatedAt)
	delta := updatedAt.Sub(*turn.completedAt)
	return age >= 0 && age <= pendingTurnGrace && delta >= pendingTurnMinimumDelta
}

func resolveHome(configured string) (string, error) {
	home := strings.TrimSpace(configured)
	if home == "" {
		home = strings.TrimSpace(os.Getenv("CODEX_HOME"))
	}
	if home == "" {
		home = "~/.codex"
	}
	if home == "~" || strings.HasPrefix(home, "~/") {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home: %w", err)
		}
		home = filepath.Join(userHome, strings.TrimPrefix(home, "~/"))
	}
	abs, err := filepath.Abs(filepath.Clean(home))
	if err != nil {
		return "", fmt.Errorf("resolve home: %w", err)
	}
	return abs, nil
}

func newestDatabase(home, pattern string) (string, error) {
	paths, err := filepath.Glob(filepath.Join(home, pattern))
	if err != nil {
		return "", err
	}
	type candidate struct {
		path  string
		mtime time.Time
	}
	candidates := make([]candidate, 0, len(paths))
	for _, path := range paths {
		info, statErr := os.Stat(path)
		if statErr != nil || !info.Mode().IsRegular() {
			continue
		}
		candidates = append(candidates, candidate{path: path, mtime: info.ModTime()})
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf("no %s database found in %s", pattern, home)
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].mtime.Equal(candidates[j].mtime) {
			return candidates[i].path < candidates[j].path
		}
		return candidates[i].mtime.After(candidates[j].mtime)
	})
	return candidates[0].path, nil
}

func readSessions(ctx context.Context, path string, includeArchived bool) (map[string]sessionRecord, error) {
	db, err := openReadonly(ctx, path)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	columns, err := tableColumns(ctx, db, "threads")
	if err != nil {
		return nil, err
	}
	threadName := "NULL"
	if columns["name"] {
		threadName = "threads.name"
	}
	spawnJoin := ""
	spawnedSubagent := "0"
	hasSpawnEdges, err := tableExists(ctx, db, "thread_spawn_edges")
	if err != nil {
		return nil, err
	}
	if hasSpawnEdges {
		spawnJoin = "LEFT JOIN thread_spawn_edges AS spawn ON spawn.child_thread_id = threads.id"
		spawnedSubagent = "spawn.child_thread_id IS NOT NULL"
	}
	query := fmt.Sprintf(`
		SELECT threads.id, %s AS thread_name, threads.title, threads.source,
		       threads.cwd, threads.model, threads.updated_at_ms,
		       threads.first_user_message, threads.archived,
		       %s AS spawned_subagent
		FROM threads
		%s
		%s
		ORDER BY threads.updated_at_ms DESC`, threadName, spawnedSubagent, spawnJoin, archivedClause(includeArchived))
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	sessions := make(map[string]sessionRecord)
	for rows.Next() {
		var (
			id, name, title, source, cwd, model, firstMessage sql.NullString
			updatedAtMs, archived                             sql.NullInt64
			spawned                                           int
		)
		if err := rows.Scan(&id, &name, &title, &source, &cwd, &model, &updatedAtMs, &firstMessage, &archived, &spawned); err != nil {
			return nil, err
		}
		if !id.Valid || strings.TrimSpace(id.String) == "" {
			return nil, errors.New("threads row has no id")
		}
		if spawned != 0 || isSubagentSource(source.String) {
			continue
		}
		titleValue := cleanText(title.String)
		firstValue := cleanText(firstMessage.String)
		displayName := cleanText(name.String)
		if displayName == "" {
			displayName = titleValue
		}
		if displayName == "" {
			displayName = firstValue
		}
		if displayName == "" {
			displayName = "(无标题)"
		}
		updatedAt := time.Time{}
		if updatedAtMs.Valid {
			updatedAt = time.UnixMilli(updatedAtMs.Int64).In(time.Local)
		}
		sessions[id.String] = sessionRecord{
			Session: Session{
				ID:          id.String,
				DisplayName: displayName,
				ProjectName: projectName(cwd.String),
				Model:       model.String,
				Status:      "unknown",
				UpdatedAt:   updatedAt,
			},
			cwd:    cwd.String,
			source: source.String,
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return sessions, nil
}

func archivedClause(includeArchived bool) string {
	if includeArchived {
		return ""
	}
	return "WHERE threads.archived = 0"
}

func readLatestTurns(ctx context.Context, path string) (map[string]turnStatus, error) {
	db, err := openReadonly(ctx, path)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	query := `
		SELECT thread_id, turn_id, status, started_at, completed_at
		FROM (
			SELECT thread_id, turn_id, status, started_at, completed_at,
			       ROW_NUMBER() OVER (
				       PARTITION BY thread_id
				       ORDER BY COALESCE(started_at, 0) DESC, rowid DESC
			       ) AS rank
			FROM thread_turns
		)
		WHERE rank = 1`
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	turns := make(map[string]turnStatus)
	for rows.Next() {
		var (
			threadID, turnID, status sql.NullString
			startedAt, completedAt   sql.NullInt64
		)
		if err := rows.Scan(&threadID, &turnID, &status, &startedAt, &completedAt); err != nil {
			return nil, err
		}
		if !threadID.Valid || threadID.String == "" {
			return nil, errors.New("thread_turns row has no thread_id")
		}
		turns[threadID.String] = turnStatus{
			rawStatus:   status.String,
			startedAt:   unixSeconds(startedAt),
			completedAt: unixSeconds(completedAt),
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return turns, nil
}

func tableColumns(ctx context.Context, db *sql.DB, table string) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, "PRAGMA table_info("+table+")")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns := make(map[string]bool)
	for rows.Next() {
		var cid, notNull, pk sql.NullInt64
		var name, columnType, defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
			return nil, err
		}
		if name.Valid {
			columns[name.String] = true
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return columns, nil
}

func tableExists(ctx context.Context, db *sql.DB, table string) (bool, error) {
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?", table).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func openReadonly(ctx context.Context, path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func sqliteDSN(path string) string {
	uri := url.URL{Scheme: "file", Path: path}
	query := uri.Query()
	query.Set("mode", "ro")
	query.Set("_busy_timeout", strconv.FormatInt(readBusyTimeout.Milliseconds(), 10))
	uri.RawQuery = query.Encode()
	return uri.String()
}

func readProcesses(path string) map[string]bool {
	contents, err := os.ReadFile(path)
	if err != nil {
		return map[string]bool{}
	}
	var entries []struct {
		ConversationID string          `json:"conversationId"`
		OSPID          json.RawMessage `json:"osPid"`
	}
	if err := json.Unmarshal(contents, &entries); err != nil {
		return map[string]bool{}
	}
	processes := make(map[string]bool, len(entries))
	for _, entry := range entries {
		if entry.ConversationID == "" {
			continue
		}
		pid, err := parsePID(entry.OSPID)
		if err != nil || pid <= 0 {
			continue
		}
		processes[entry.ConversationID] = processAlive(pid)
	}
	return processes
}

func parsePID(raw json.RawMessage) (int64, error) {
	value := strings.TrimSpace(string(raw))
	if value == "" {
		return 0, errors.New("missing pid")
	}
	if pid, err := strconv.ParseInt(value, 10, 64); err == nil {
		return pid, nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return 0, err
	}
	return strconv.ParseInt(strings.TrimSpace(text), 10, 64)
}

func processAlive(pid int64) bool {
	if pid <= 0 || int64(int(pid)) != pid {
		return false
	}
	process, err := os.FindProcess(int(pid))
	if err != nil {
		return false
	}
	return process.Signal(syscall.Signal(0)) == nil
}

func unixSeconds(value sql.NullInt64) *time.Time {
	if !value.Valid {
		return nil
	}
	parsed := time.Unix(value.Int64, 0).In(time.Local)
	return &parsed
}

func projectName(cwd string) string {
	if cwd == "" {
		return ""
	}
	return filepath.Base(filepath.Clean(cwd))
}

func isIgnored(session sessionRecord, cwdPatterns []string, sourcePatterns map[string]struct{}) bool {
	for _, pattern := range cwdPatterns {
		if strings.Contains(session.cwd, pattern) {
			return true
		}
	}
	_, ignored := sourcePatterns[session.source]
	return ignored
}

func isSubagentSource(source string) bool {
	source = strings.TrimSpace(source)
	if !strings.HasPrefix(source, "{") {
		return false
	}
	var metadata map[string]json.RawMessage
	if err := json.Unmarshal([]byte(source), &metadata); err != nil {
		return false
	}
	_, ok := metadata["subagent"]
	return ok
}

func cleanText(value string) string { return strings.Join(strings.Fields(value), " ") }
