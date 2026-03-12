package config

import (
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

func validConfig() Config {
	return Config{
		LogLevel: "info",
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
