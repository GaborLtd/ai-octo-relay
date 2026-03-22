package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateRejectsAgentAliasThatMatchesOwnKey(t *testing.T) {
	cfg := validConfig()
	cfg.Agents["codex"] = AgentConfig{
		Command: "codex",
		Aliases: []string{"codex"},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), `agent "codex" alias "codex" duplicates its own selector`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateRejectsChannelIDAssignedToMultipleProjects(t *testing.T) {
	cfg := validConfig()
	cfg.Projects = []ProjectConfig{
		{Name: "project-a", Path: "/tmp/a", ChannelIDs: []string{"C123"}},
		{Name: "project-b", Path: "/tmp/b", ChannelIDs: []string{"C123"}},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), `channel_id "C123" is assigned to multiple projects: project-a, project-b`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateRejectsDuplicateChannelIDInsideProject(t *testing.T) {
	cfg := validConfig()
	cfg.Projects = []ProjectConfig{
		{Name: "project-a", Path: "/tmp/a", ChannelIDs: []string{"C123", "C123"}},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), `project "project-a" has duplicate channel_id "C123"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateRejectsDuplicateProjectCommandName(t *testing.T) {
	cfg := validConfig()
	cfg.Projects = []ProjectConfig{
		{
			Name: "project-a",
			Path: "/tmp/a",
			Commands: []ProjectCommandConfig{
				{Name: "test", Command: "make", Args: []string{"test"}},
				{Name: "test", Command: "go", Args: []string{"test", "./..."}},
			},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), `project "project-a" has duplicate command "test"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateRejectsDuplicateTerminalServerName(t *testing.T) {
	cfg := validConfig()
	cfg.Terminal.Servers = []TerminalServerConfig{
		{Name: "web", Command: "npm", Args: []string{"run", "dev"}},
		{Name: "web", Command: "go", Args: []string{"test", "./..."}},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), `terminal has duplicate server "web"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCandidatePathsWithExplicitPath(t *testing.T) {
	got, err := CandidatePaths("./custom.json")
	if err != nil {
		t.Fatalf("CandidatePaths() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(CandidatePaths()) = %d, want 1", len(got))
	}
	want, err := filepath.Abs("custom.json")
	if err != nil {
		t.Fatalf("Abs() error = %v", err)
	}
	if got[0] != want {
		t.Fatalf("CandidatePaths()[0] = %q, want %q", got[0], want)
	}
}

func TestResolvePathFindsCurrentDirectoryConfigFirst(t *testing.T) {
	tmpDir := t.TempDir()
	previousWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	t.Cleanup(func() {
		if chdirErr := os.Chdir(previousWD); chdirErr != nil {
			t.Fatalf("restore cwd: %v", chdirErr)
		}
	})
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Chdir() error = %v", err)
	}

	configPath := filepath.Join(tmpDir, "config.json")
	if err := os.WriteFile(configPath, []byte(`{}`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	got, err := ResolvePath("")
	if err != nil {
		t.Fatalf("ResolvePath() error = %v", err)
	}
	gotInfo, err := os.Stat(got)
	if err != nil {
		t.Fatalf("Stat(got) error = %v", err)
	}
	wantInfo, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("Stat(configPath) error = %v", err)
	}
	if !os.SameFile(gotInfo, wantInfo) {
		t.Fatalf("ResolvePath() = %q, want same file as %q", got, configPath)
	}
}

func TestResolvePathReportsCandidatesWhenMissing(t *testing.T) {
	tmpDir := t.TempDir()
	previousWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	t.Cleanup(func() {
		if chdirErr := os.Chdir(previousWD); chdirErr != nil {
			t.Fatalf("restore cwd: %v", chdirErr)
		}
	})
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Chdir() error = %v", err)
	}

	_, err = ResolvePath("")
	if err == nil {
		t.Fatal("ResolvePath() error = nil, want missing config error")
	}
	if !strings.Contains(err.Error(), "config file not found; tried:") {
		t.Fatalf("ResolvePath() error = %v", err)
	}
}

func validConfig() Config {
	return Config{
		LogLevel: "info",
		StateStore: StorageConfig{
			Type: "json",
			Path: "/tmp/state.json",
		},
		EventStore: StorageConfig{
			Type: "none",
		},
		Slack: SlackConfig{
			AppToken: "app-token",
			BotToken: "bot-token",
		},
		Projects: []ProjectConfig{
			{Name: "project-a", Path: "/tmp/a", ChannelIDs: []string{"C111"}},
		},
		Agents: map[string]AgentConfig{
			"codex": {
				Command: "codex",
				Aliases: []string{"cx"},
			},
		},
	}
}
