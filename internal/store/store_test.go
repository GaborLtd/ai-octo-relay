package store

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestJSONLEventStoreAppend(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")

	events, err := NewJSONLEventStore(path)
	if err != nil {
		t.Fatalf("NewJSONLEventStore() error = %v", err)
	}

	if err := events.Append(Event{Type: "session.closed", Timestamp: "2026-03-12T00:00:00Z"}); err != nil {
		t.Fatalf("Append() error = %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !strings.Contains(string(content), `"type":"session.closed"`) {
		t.Fatalf("unexpected event log content: %s", content)
	}
}

func TestSQLiteStateStoreRoundTrip(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 command not available")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "state.db")

	stateStore, err := NewSQLiteStateStore(path)
	if err != nil {
		t.Fatalf("NewSQLiteStateStore() error = %v", err)
	}

	quiet := true
	if err := stateStore.SetThread("C123:123.456", ScopeState{
		Project:       "relay",
		Agent:         "codex",
		Quiet:         &quiet,
		SessionActive: true,
	}); err != nil {
		t.Fatalf("SetThread() error = %v", err)
	}

	got := stateStore.GetThread("C123:123.456")
	if got.Project != "relay" || got.Agent != "codex" || got.Quiet == nil || !*got.Quiet || !got.SessionActive {
		t.Fatalf("unexpected thread state: %+v", got)
	}
}

func TestJSONStoreSessionSummaryRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	stateStore, err := NewJSONStore(path)
	if err != nil {
		t.Fatalf("NewJSONStore() error = %v", err)
	}

	want := NativeSessionState{
		Agent:            "gemini",
		Summary:          "Goal: 建立 CONFIG.md",
		SummaryUpdatedAt: "2026-03-13T00:00:00Z",
	}
	if err := stateStore.SetSession("session-1", want); err != nil {
		t.Fatalf("SetSession() error = %v", err)
	}

	got := stateStore.GetSession("session-1")
	if got.Summary != want.Summary || got.SummaryUpdatedAt != want.SummaryUpdatedAt {
		t.Fatalf("unexpected session summary state: %+v", got)
	}
}

func TestSQLiteStateStoreSessionSummaryRoundTrip(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 command not available")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "state.db")

	stateStore, err := NewSQLiteStateStore(path)
	if err != nil {
		t.Fatalf("NewSQLiteStateStore() error = %v", err)
	}

	want := NativeSessionState{
		Agent:            "gemini",
		Summary:          "Goal: 建立 CONFIG.md",
		SummaryUpdatedAt: "2026-03-13T00:00:00Z",
	}
	if err := stateStore.SetSession("session-1", want); err != nil {
		t.Fatalf("SetSession() error = %v", err)
	}

	got := stateStore.GetSession("session-1")
	if got.Summary != want.Summary || got.SummaryUpdatedAt != want.SummaryUpdatedAt {
		t.Fatalf("unexpected sqlite session summary state: %+v", got)
	}
}
