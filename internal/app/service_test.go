package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/match/ai-octo-relay/internal/agent"
	"github.com/match/ai-octo-relay/internal/config"
	"github.com/match/ai-octo-relay/internal/logx"
	"github.com/match/ai-octo-relay/internal/project"
	"github.com/match/ai-octo-relay/internal/store"
)

func TestResolveScopeUsesProjectDefaultAgentForChannelThreads(t *testing.T) {
	svc := newTestService(t)

	if _, err := svc.UseAgent("C123", "", "gemini"); err != nil {
		t.Fatalf("UseAgent() error = %v", err)
	}

	scope, err := svc.ResolveScope("C123", "1730000000.000100")
	if err != nil {
		t.Fatalf("ResolveScope() error = %v", err)
	}
	if scope.AgentName != "codex" {
		t.Fatalf("ResolveScope() agent = %q, want %q", scope.AgentName, "codex")
	}
}

func TestResolveScopeKeepsChannelAgentForDM(t *testing.T) {
	svc := newTestService(t)

	if _, err := svc.UseAgent("D123", "", "gemini"); err != nil {
		t.Fatalf("UseAgent() error = %v", err)
	}

	scope, err := svc.ResolveScope("D123", "")
	if err != nil {
		t.Fatalf("ResolveScope() error = %v", err)
	}
	if scope.AgentName != "gemini" {
		t.Fatalf("ResolveScope() agent = %q, want %q", scope.AgentName, "gemini")
	}
}

func TestSummarizeAgentFailureForGeminiCapacity(t *testing.T) {
	message := summarizeAgentFailure("gemini", `RESOURCE_EXHAUSTED MODEL_CAPACITY_EXHAUSTED "code": 429`, context.DeadlineExceeded)
	if !strings.Contains(message, "Gemini 目前不可用") {
		t.Fatalf("summarizeAgentFailure() = %q", message)
	}
}

func TestAgentModelListTextUsesFallbackModels(t *testing.T) {
	svc := newTestService(t)

	text, err := svc.AgentModelListText(context.Background(), "gemini")
	if err != nil {
		t.Fatalf("AgentModelListText() error = %v", err)
	}
	if !strings.Contains(text, "configured_model: (not set)") {
		t.Fatalf("AgentModelListText() missing configured model: %q", text)
	}
	if !strings.Contains(text, "model_source: fallback") {
		t.Fatalf("AgentModelListText() missing fallback source: %q", text)
	}
	if !strings.Contains(text, "gemini-2.5-flash") {
		t.Fatalf("AgentModelListText() missing fallback model: %q", text)
	}
}

func TestUsesNativeSessionForGeminiIsDisabled(t *testing.T) {
	if usesNativeSession("gemini") {
		t.Fatal("usesNativeSession(gemini) = true, want false")
	}
	if !usesNativeSession("codex") {
		t.Fatal("usesNativeSession(codex) = false, want true")
	}
}

func TestSummarizeAgentFailureForGeminiModelNotFound(t *testing.T) {
	message := summarizeAgentFailure("gemini", `ModelNotFoundError: Requested entity was not found. "code": 404`, context.DeadlineExceeded)
	if !strings.Contains(message, "ModelNotFound") {
		t.Fatalf("summarizeAgentFailure() = %q", message)
	}
}

func TestPromptSuffixForContextUsesChannelWritePromptOutsideDM(t *testing.T) {
	svc := newTestService(t)
	svc.cfg.DMReadOnly = true
	svc.cfg.ChannelWritePrompt = "channel-write"
	svc.cfg.DMReadOnlyPrompt = "dm-readonly"
	svc.cfg.Prompts.Default.ChannelWrite = "channel-write"
	svc.cfg.Prompts.Default.DMReadOnly = "dm-readonly"

	if got := svc.promptSuffixForContext("codex", false); got != "channel-write" {
		t.Fatalf("promptSuffixForContext(false) = %q, want %q", got, "channel-write")
	}
	if got := svc.promptSuffixForContext("codex", true); got != "dm-readonly" {
		t.Fatalf("promptSuffixForContext(true) = %q, want %q", got, "dm-readonly")
	}
}

func TestPromptSuffixForContextAppendsTraditionalChineseInstruction(t *testing.T) {
	svc := newTestService(t)
	svc.cfg.Language = "zh-TW"
	svc.cfg.ChannelWritePrompt = "channel-write"
	svc.cfg.Prompts.Default.ChannelWrite = "channel-write"

	got := svc.promptSuffixForContext("codex", false)
	if !strings.Contains(got, "channel-write") {
		t.Fatalf("promptSuffixForContext(false) missing base prompt: %q", got)
	}
	if !strings.Contains(got, "請一律使用繁體中文回覆。") {
		t.Fatalf("promptSuffixForContext(false) missing language prompt: %q", got)
	}
}

func TestPromptSuffixForLanguageEnglishAddsNothing(t *testing.T) {
	if got := promptSuffixForLanguage("en"); got != "" {
		t.Fatalf("promptSuffixForLanguage(en) = %q, want empty", got)
	}
}

func TestPromptSuffixForContextUsesAgentSpecificPrompt(t *testing.T) {
	svc := newTestService(t)
	svc.cfg.Prompts.Default.ChannelWrite = "default-channel"
	svc.cfg.Prompts.Default.DMReadOnly = "default-dm"
	svc.cfg.Prompts.Agents["gemini"] = config.PromptModeConfig{
		ChannelWrite: "gemini-channel",
		DMReadOnly:   "gemini-dm",
	}

	if got := svc.promptSuffixForContext("gemini", false); got != "gemini-channel" {
		t.Fatalf("promptSuffixForContext(gemini, false) = %q", got)
	}
	if got := svc.promptSuffixForContext("gemini", true); got != "gemini-dm" {
		t.Fatalf("promptSuffixForContext(gemini, true) = %q", got)
	}
	if got := svc.promptSuffixForContext("codex", false); got != "default-channel" {
		t.Fatalf("promptSuffixForContext(codex, false) = %q", got)
	}
}

func TestBuildGeminiSummaryKeepsGoalAndFiles(t *testing.T) {
	previous := "Goal: 建立 CONFIG.md 說明設定方式\nLatest request: 幫我整理 config\nStatus: Agent replied in chat; file changes were not clearly confirmed.\nFiles: config.example.json, internal/config/config.go"
	output := "我已更新 docs/architecture-notes.md，並建議建立 CONFIG.md，請手動貼上。"

	got := buildGeminiSummary(previous, "可以直接幫我寫入 CONFIG.md 嗎？", output)

	if !strings.Contains(got, "Goal: 建立 CONFIG.md 說明設定方式") {
		t.Fatalf("buildGeminiSummary() missing goal: %q", got)
	}
	if !strings.Contains(got, "Latest request: 可以直接幫我寫入 CONFIG.md 嗎？") {
		t.Fatalf("buildGeminiSummary() missing latest request: %q", got)
	}
	if !strings.Contains(got, "Files:") || !strings.Contains(got, "internal/config/config.go") {
		t.Fatalf("buildGeminiSummary() missing files: %q", got)
	}
}

func TestApplyAgentContextPromptForGeminiIncludesSummary(t *testing.T) {
	svc := newTestService(t)
	scope := Scope{AgentName: "gemini"}
	session := store.NativeSessionState{Summary: "Goal: 建立 CONFIG.md"}

	got := svc.applyAgentContextPrompt(scope, session, "請直接寫入")

	if !strings.Contains(got, "Gemini thread summary:") {
		t.Fatalf("applyAgentContextPrompt() missing summary header: %q", got)
	}
	if !strings.Contains(got, "Latest user request:\n請直接寫入") {
		t.Fatalf("applyAgentContextPrompt() missing latest request: %q", got)
	}
}

func TestFormatGitBranchOutput(t *testing.T) {
	input := "  codex-slack-agent-routing        1ba60b3 Add agent-bound Slack sessions and DM read-only mode\n* main                              562c21f [ahead 3] Update docs for session concurrency model\n  remotes/origin/main               50b0c04 Restrict DM to question-only interactions"

	got := formatGitBranchOutput(input)

	if !strings.Contains(got, "* `main` 562c21f [ahead 3] Update docs for session concurrency model") {
		t.Fatalf("formatGitBranchOutput() missing main branch: %q", got)
	}
	if !strings.Contains(got, "- `origin/main` 50b0c04 Restrict DM to question-only interactions") {
		t.Fatalf("formatGitBranchOutput() missing remote branch: %q", got)
	}
}

func newTestService(t *testing.T) *Service {
	t.Helper()

	cfg := &config.Config{
		CommandPrefix:  "!",
		DefaultAgent:   "codex",
		QuietByDefault: true,
		Prompts: config.PromptConfig{
			Agents: map[string]config.PromptModeConfig{},
		},
		Projects: []config.ProjectConfig{
			{
				Name:         "relay",
				Path:         t.TempDir(),
				DefaultAgent: "codex",
				ChannelIDs:   []string{"C123"},
			},
		},
		Agents: map[string]config.AgentConfig{
			"codex":  {Adapter: "codex", Command: "codex"},
			"gemini": {Adapter: "gemini", Command: "gemini"},
		},
	}

	projects, err := project.NewRegistry(cfg.Projects)
	if err != nil {
		t.Fatalf("project.NewRegistry() error = %v", err)
	}
	agents, err := agent.NewRegistry(cfg.Agents, logx.New("error"))
	if err != nil {
		t.Fatalf("agent.NewRegistry() error = %v", err)
	}
	stateStore, err := store.NewJSONStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("store.NewJSONStore() error = %v", err)
	}

	return NewService(cfg, projects, agents, stateStore, &store.NoopEventStore{})
}
