package agent

import (
	"strings"
	"testing"

	"github.com/match/ai-octo-relay/internal/config"
)

func TestGeminiAdapterDisablesMaxSessionTurns(t *testing.T) {
	spec := geminiAdapter{}.Build(RunRequest{AgentName: "gemini"}, config.AgentConfig{
		Model:           "gemini-2.5-flash",
		MaxSessionTurns: 8,
	})
	if spec.MaxSessionTurns != 0 {
		t.Fatalf("gemini MaxSessionTurns = %d, want 0", spec.MaxSessionTurns)
	}
}

func TestGeminiAdapterUsesAutoEditOutsideDM(t *testing.T) {
	spec := geminiAdapter{}.Build(RunRequest{AgentName: "gemini"}, config.AgentConfig{
		Model: "gemini-2.5-flash",
	})
	if !hasOptionWithValue(spec.Args, "--approval-mode", "auto_edit") {
		t.Fatalf("gemini channel args missing auto_edit: %v", spec.Args)
	}
	if containsArg(spec.Args, "--sandbox") {
		t.Fatalf("gemini channel args unexpectedly enable sandbox: %v", spec.Args)
	}
}

func TestGeminiAdapterUsesPlanModeInDM(t *testing.T) {
	spec := geminiAdapter{}.Build(RunRequest{
		AgentName:  "gemini",
		DMReadOnly: true,
	}, config.AgentConfig{
		Model: "gemini-2.5-flash",
	})
	if !hasOptionWithValue(spec.Args, "--approval-mode", "plan") {
		t.Fatalf("gemini DM args missing plan mode: %v", spec.Args)
	}
	if !containsArg(spec.Args, "--sandbox") {
		t.Fatalf("gemini DM args missing sandbox: %v", spec.Args)
	}
	if got := spec.Env["SEATBELT_PROFILE"]; got != "strict-open" {
		t.Fatalf("gemini DM SEATBELT_PROFILE = %q, want %q", got, "strict-open")
	}
}

func TestParseGeminiStreamJSON(t *testing.T) {
	stdout := strings.Join([]string{
		`{"type":"init","session_id":"test-session-id"}`,
		`{"type":"content","value":"First line"}`,
		`{"type":"content","value":{"text":"Second line"}}`,
		`{"type":"result","value":{"usageMetadata":{"totalTokenCount":42}}}`,
	}, "\n")

	output, sessionID, ok := parseGeminiStreamJSON(stdout)
	if !ok {
		t.Fatal("parseGeminiStreamJSON() = false, want true")
	}
	if sessionID != "test-session-id" {
		t.Fatalf("parseGeminiStreamJSON() sessionID = %q, want %q", sessionID, "test-session-id")
	}
	if output != "First line\nSecond line" {
		t.Fatalf("parseGeminiStreamJSON() output = %q", output)
	}
}

func TestParseGeminiStreamChunk(t *testing.T) {
	got := parseGeminiStreamChunk(`{"type":"content","value":{"text":"hello"}}`)
	if got != "hello" {
		t.Fatalf("parseGeminiStreamChunk() = %q, want %q", got, "hello")
	}
}
