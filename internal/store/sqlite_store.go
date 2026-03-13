package store

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

type SQLiteStateStore struct {
	path string
	mu   sync.Mutex
}

type SQLiteEventStore struct {
	path string
	mu   sync.Mutex
}

var _ StateStore = (*SQLiteStateStore)(nil)
var _ EventStore = (*SQLiteEventStore)(nil)

func NewSQLiteStateStore(path string) (*SQLiteStateStore, error) {
	if err := ensureSQLitePath(path); err != nil {
		return nil, err
	}
	store := &SQLiteStateStore{path: path}
	if err := store.exec(schemaStateSQL); err != nil {
		return nil, err
	}
	if err := store.ensureSessionColumns(); err != nil {
		return nil, err
	}
	return store, nil
}

func NewSQLiteEventStore(path string) (*SQLiteEventStore, error) {
	if err := ensureSQLitePath(path); err != nil {
		return nil, err
	}
	store := &SQLiteEventStore{path: path}
	if err := store.exec(schemaEventSQL); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *SQLiteStateStore) GetChannel(channelID string) ScopeState {
	return s.getScopeState("channels", "channel_id", channelID)
}

func (s *SQLiteStateStore) SetChannel(channelID string, state ScopeState) error {
	return s.upsertScopeState("channels", "channel_id", channelID, state)
}

func (s *SQLiteStateStore) ClearChannel(channelID string) error {
	return s.deleteByKey("channels", "channel_id", channelID)
}

func (s *SQLiteStateStore) GetThread(threadKey string) ScopeState {
	return s.getScopeState("threads", "thread_key", threadKey)
}

func (s *SQLiteStateStore) SetThread(threadKey string, state ScopeState) error {
	return s.upsertScopeState("threads", "thread_key", threadKey, state)
}

func (s *SQLiteStateStore) ClearThread(threadKey string) error {
	return s.deleteByKey("threads", "thread_key", threadKey)
}

func (s *SQLiteStateStore) GetSession(sessionKey string) NativeSessionState {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := sqliteQuery[sessionRow](s.path, fmt.Sprintf(
		`SELECT agent, native_id, updated_at, project, thread_key, channel_id, summary, summary_updated_at FROM sessions WHERE session_key = %s LIMIT 1;`,
		sqliteString(sessionKey),
	))
	if err != nil || len(rows) == 0 {
		return NativeSessionState{}
	}
	row := rows[0]
	return NativeSessionState{
		Agent:            row.Agent,
		NativeID:         row.NativeID,
		UpdatedAt:        row.UpdatedAt,
		Project:          row.Project,
		ThreadKey:        row.ThreadKey,
		ChannelID:        row.ChannelID,
		Summary:          row.Summary,
		SummaryUpdatedAt: row.SummaryUpdatedAt,
	}
}

func (s *SQLiteStateStore) SetSession(sessionKey string, state NativeSessionState) error {
	return s.exec(fmt.Sprintf(
		`INSERT INTO sessions (session_key, agent, native_id, updated_at, project, thread_key, channel_id, summary, summary_updated_at)
VALUES (%s, %s, %s, %s, %s, %s, %s, %s, %s)
ON CONFLICT(session_key) DO UPDATE SET
agent = excluded.agent,
native_id = excluded.native_id,
updated_at = excluded.updated_at,
project = excluded.project,
thread_key = excluded.thread_key,
channel_id = excluded.channel_id,
summary = excluded.summary,
summary_updated_at = excluded.summary_updated_at;`,
		sqliteString(sessionKey),
		sqliteString(state.Agent),
		sqliteString(state.NativeID),
		sqliteString(state.UpdatedAt),
		sqliteString(state.Project),
		sqliteString(state.ThreadKey),
		sqliteString(state.ChannelID),
		sqliteString(state.Summary),
		sqliteString(state.SummaryUpdatedAt),
	))
}

func (s *SQLiteStateStore) ClearSession(sessionKey string) error {
	return s.deleteByKey("sessions", "session_key", sessionKey)
}

func (s *SQLiteStateStore) getScopeState(table, keyColumn, key string) ScopeState {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := sqliteQuery[scopeRow](s.path, fmt.Sprintf(
		`SELECT project, agent, quiet, session_active FROM %s WHERE %s = %s LIMIT 1;`,
		table,
		keyColumn,
		sqliteString(key),
	))
	if err != nil || len(rows) == 0 {
		return ScopeState{}
	}

	row := rows[0]
	state := ScopeState{
		Project:       row.Project,
		Agent:         row.Agent,
		SessionActive: row.SessionActive != 0,
	}
	if row.Quiet.Valid {
		quiet := row.Quiet.Bool
		state.Quiet = &quiet
	}
	return state
}

func (s *SQLiteStateStore) upsertScopeState(table, keyColumn, key string, state ScopeState) error {
	quiet := "NULL"
	if state.Quiet != nil {
		quiet = sqliteBool(*state.Quiet)
	}
	return s.exec(fmt.Sprintf(
		`INSERT INTO %s (%s, project, agent, quiet, session_active)
VALUES (%s, %s, %s, %s, %s)
ON CONFLICT(%s) DO UPDATE SET
project = excluded.project,
agent = excluded.agent,
quiet = excluded.quiet,
session_active = excluded.session_active;`,
		table,
		keyColumn,
		sqliteString(key),
		sqliteString(state.Project),
		sqliteString(state.Agent),
		quiet,
		sqliteBool(state.SessionActive),
		keyColumn,
	))
}

func (s *SQLiteStateStore) deleteByKey(table, keyColumn, key string) error {
	return s.exec(fmt.Sprintf(`DELETE FROM %s WHERE %s = %s;`, table, keyColumn, sqliteString(key)))
}

func (s *SQLiteStateStore) exec(sql string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return sqliteExec(s.path, sql)
}

func (s *SQLiteEventStore) Append(event Event) error {
	payloadJSON := "{}"
	if len(event.Payload) > 0 {
		data, err := json.Marshal(event.Payload)
		if err != nil {
			return fmt.Errorf("marshal event payload: %w", err)
		}
		payloadJSON = string(data)
	}
	return s.exec(fmt.Sprintf(
		`INSERT INTO events (timestamp, type, channel_id, thread_key, session_key, project, agent, payload_json)
VALUES (%s, %s, %s, %s, %s, %s, %s, %s);`,
		sqliteString(event.Timestamp),
		sqliteString(event.Type),
		sqliteString(event.ChannelID),
		sqliteString(event.ThreadKey),
		sqliteString(event.SessionKey),
		sqliteString(event.Project),
		sqliteString(event.Agent),
		sqliteString(payloadJSON),
	))
}

func (s *SQLiteEventStore) exec(sql string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return sqliteExec(s.path, sql)
}

type scopeRow struct {
	Project       string          `json:"project"`
	Agent         string          `json:"agent"`
	Quiet         sqliteBoolValue `json:"quiet"`
	SessionActive int             `json:"session_active"`
}

type sessionRow struct {
	Agent            string `json:"agent"`
	NativeID         string `json:"native_id"`
	UpdatedAt        string `json:"updated_at"`
	Project          string `json:"project"`
	ThreadKey        string `json:"thread_key"`
	ChannelID        string `json:"channel_id"`
	Summary          string `json:"summary"`
	SummaryUpdatedAt string `json:"summary_updated_at"`
}

type sqliteBoolValue struct {
	Bool  bool
	Valid bool
}

func (v *sqliteBoolValue) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "null" {
		v.Bool = false
		v.Valid = false
		return nil
	}
	value, err := strconv.Atoi(trimmed)
	if err != nil {
		return err
	}
	v.Bool = value != 0
	v.Valid = true
	return nil
}

func sqliteExec(path, sql string) error {
	cmd := exec.Command("sqlite3", path, sql)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("sqlite exec failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func sqliteQuery[T any](path, sql string) ([]T, error) {
	cmd := exec.Command("sqlite3", "-json", path, sql)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("sqlite query failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	if len(output) == 0 {
		return nil, nil
	}
	var rows []T
	if err := json.Unmarshal(output, &rows); err != nil {
		return nil, fmt.Errorf("parse sqlite json: %w", err)
	}
	return rows, nil
}

func ensureSQLitePath(path string) error {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		return fmt.Errorf("sqlite3 command is required for sqlite store: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create sqlite store dir: %w", err)
	}
	return nil
}

func (s *SQLiteStateStore) ensureSessionColumns() error {
	rows, err := sqliteQuery[struct {
		Name string `json:"name"`
	}](s.path, `PRAGMA table_info(sessions);`)
	if err != nil {
		return err
	}
	columns := map[string]struct{}{}
	for _, row := range rows {
		columns[row.Name] = struct{}{}
	}
	if _, ok := columns["summary"]; !ok {
		if err := s.exec(`ALTER TABLE sessions ADD COLUMN summary TEXT NOT NULL DEFAULT '';`); err != nil {
			return err
		}
	}
	if _, ok := columns["summary_updated_at"]; !ok {
		if err := s.exec(`ALTER TABLE sessions ADD COLUMN summary_updated_at TEXT NOT NULL DEFAULT '';`); err != nil {
			return err
		}
	}
	return nil
}

func sqliteString(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func sqliteBool(value bool) string {
	if value {
		return "1"
	}
	return "0"
}

const schemaStateSQL = `
CREATE TABLE IF NOT EXISTS channels (
	channel_id TEXT PRIMARY KEY,
	project TEXT NOT NULL DEFAULT '',
	agent TEXT NOT NULL DEFAULT '',
	quiet INTEGER NULL,
	session_active INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS threads (
	thread_key TEXT PRIMARY KEY,
	project TEXT NOT NULL DEFAULT '',
	agent TEXT NOT NULL DEFAULT '',
	quiet INTEGER NULL,
	session_active INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS sessions (
	session_key TEXT PRIMARY KEY,
	agent TEXT NOT NULL DEFAULT '',
	native_id TEXT NOT NULL DEFAULT '',
	updated_at TEXT NOT NULL DEFAULT '',
	project TEXT NOT NULL DEFAULT '',
	thread_key TEXT NOT NULL DEFAULT '',
	channel_id TEXT NOT NULL DEFAULT '',
	summary TEXT NOT NULL DEFAULT '',
	summary_updated_at TEXT NOT NULL DEFAULT ''
);`

const schemaEventSQL = `
CREATE TABLE IF NOT EXISTS events (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	timestamp TEXT NOT NULL,
	type TEXT NOT NULL,
	channel_id TEXT NOT NULL DEFAULT '',
	thread_key TEXT NOT NULL DEFAULT '',
	session_key TEXT NOT NULL DEFAULT '',
	project TEXT NOT NULL DEFAULT '',
	agent TEXT NOT NULL DEFAULT '',
	payload_json TEXT NOT NULL DEFAULT '{}'
);
CREATE INDEX IF NOT EXISTS idx_events_session_key ON events(session_key);
CREATE INDEX IF NOT EXISTS idx_events_timestamp ON events(timestamp);`
