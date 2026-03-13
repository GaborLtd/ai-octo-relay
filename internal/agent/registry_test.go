package agent

import (
	"slices"
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

func TestCodexAdapterUsesResumeWithCustomArgs(t *testing.T) {
	spec := codexAdapter{}.Build(RunRequest{
		AgentName:       "codex",
		NativeSessionID: "abc123",
	}, config.AgentConfig{
		Command: "codex",
		Args: []string{
			"exec",
			"--skip-git-repo-check",
			"--color", "never",
			"--output-last-message", "{{last_message_path}}",
			"-C", "{{project_path}}",
			"-",
		},
	})
	wantPrefix := []string{"exec", "resume", "abc123"}
	if len(spec.Args) < len(wantPrefix) || !slices.Equal(spec.Args[:len(wantPrefix)], wantPrefix) {
		t.Fatalf("codex resume args prefix = %v, want prefix %v", spec.Args, wantPrefix)
	}
}

func TestBuildCodexResumeArgsKeepsExistingResumeShape(t *testing.T) {
	got := buildCodexResumeArgs([]string{"exec", "resume", "old", "--json"})
	want := []string{"exec", "resume", "{{native_session_id}}", "--json"}
	if !slices.Equal(got, want) {
		t.Fatalf("buildCodexResumeArgs() = %v, want %v", got, want)
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
