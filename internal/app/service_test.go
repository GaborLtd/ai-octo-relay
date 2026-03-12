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
