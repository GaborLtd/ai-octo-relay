package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	CommandPrefix    string                 `json:"command_prefix"`
	DefaultAgent     string                 `json:"default_agent"`
	StorePath        string                 `json:"store_path"`
	StateStore       StorageConfig          `json:"state_store"`
	EventStore       StorageConfig          `json:"event_store"`
	LogLevel         string                 `json:"log_level"`
	QuietByDefault   bool                   `json:"quiet_by_default"`
	DMReadOnly       bool                   `json:"dm_read_only"`
	DMReadOnlyPrompt string                 `json:"dm_read_only_prompt"`
	Slack            SlackConfig            `json:"slack"`
	Projects         []ProjectConfig        `json:"projects"`
	Agents           map[string]AgentConfig `json:"agents"`
}

type StorageConfig struct {
	Type string `json:"type"`
	Path string `json:"path"`
}

type SlackConfig struct {
	AppToken        string   `json:"app_token"`
	BotToken        string   `json:"bot_token"`
	AllowedChannels []string `json:"allowed_channels"`
}

type ProjectConfig struct {
	Name         string                 `json:"name"`
	Path         string                 `json:"path"`
	DefaultAgent string                 `json:"default_agent"`
	ChannelIDs   []string               `json:"channel_ids"`
	Commands     []ProjectCommandConfig `json:"commands"`
}

type ProjectCommandConfig struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Command     string   `json:"command"`
	Args        []string `json:"args"`
}

type AgentConfig struct {
	Adapter             string            `json:"adapter"`
	Aliases             []string          `json:"aliases"`
	Command             string            `json:"command"`
	Args                []string          `json:"args"`
	InteractiveCommand  string            `json:"interactive_command"`
	InteractiveArgs     []string          `json:"interactive_args"`
	Env                 map[string]string `json:"env"`
	Mode                string            `json:"mode"`
	Transport           string            `json:"transport"`
	TimeoutSeconds      int               `json:"timeout_seconds"`
	ResponseIdleMS      int               `json:"response_idle_ms"`
	FirstChunkTimeoutMS int               `json:"first_chunk_timeout_ms"`
	SessionIdleMS       int               `json:"session_idle_ms"`
	StartupWaitMS       int               `json:"startup_wait_ms"`
	PromptSuffix        string            `json:"prompt_suffix"`
	MaxSessionTurns     int               `json:"max_session_turns"`
}

func Load(path string) (*Config, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(content, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	applyDefaults(&cfg)
	applyEnvOverrides(&cfg)

	baseDir := filepath.Dir(path)
	if err := normalizePaths(&cfg, baseDir); err != nil {
		return nil, err
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func applyDefaults(cfg *Config) {
	if cfg.CommandPrefix == "" {
		cfg.CommandPrefix = "!"
	}
	if cfg.StorePath == "" {
		cfg.StorePath = "data/state.json"
	}
	if cfg.StateStore.Type == "" {
		cfg.StateStore.Type = "json"
	}
	if cfg.StateStore.Path == "" {
		cfg.StateStore.Path = cfg.StorePath
	}
	if cfg.EventStore.Type == "" {
		cfg.EventStore.Type = "none"
	}
	if cfg.EventStore.Type == "jsonl" && cfg.EventStore.Path == "" {
		cfg.EventStore.Path = "data/events.jsonl"
	}
	if cfg.EventStore.Type == "sqlite" && cfg.EventStore.Path == "" {
		cfg.EventStore.Path = "data/events.db"
	}
	if cfg.LogLevel == "" {
		cfg.LogLevel = "info"
	}
	if cfg.DMReadOnlyPrompt == "" {
		cfg.DMReadOnlyPrompt = defaultDMReadOnlyPrompt
	}
	for name, agent := range cfg.Agents {
		if agent.TimeoutSeconds <= 0 {
			agent.TimeoutSeconds = 1800
		}
		if agent.ResponseIdleMS <= 0 {
			agent.ResponseIdleMS = 1800
		}
		if agent.FirstChunkTimeoutMS <= 0 {
			agent.FirstChunkTimeoutMS = 30000
		}
		if agent.SessionIdleMS <= 0 {
			agent.SessionIdleMS = 900000
		}
		if agent.StartupWaitMS <= 0 {
			agent.StartupWaitMS = 1200
		}
		if agent.Env == nil {
			agent.Env = map[string]string{}
		}
		if agent.Aliases == nil {
			agent.Aliases = []string{}
		}
		cfg.Agents[name] = agent
	}
}

const defaultDMReadOnlyPrompt = `

DM mode is read-only.

Rules:
- Answer questions, explain code, review code, and suggest patches in text only.
- Do not modify files.
- Do not run commands that write files, change git state, install packages, or alter the system.
- Do not propose that you already changed the project.
- If the user asks for a change, provide analysis or a proposed diff in text instead.
`

func applyEnvOverrides(cfg *Config) {
	if token := os.Getenv("SLACK_APP_TOKEN"); token != "" {
		cfg.Slack.AppToken = token
	}
	if token := os.Getenv("SLACK_BOT_TOKEN"); token != "" {
		cfg.Slack.BotToken = token
	}
}

func normalizePaths(cfg *Config, baseDir string) error {
	if !filepath.IsAbs(cfg.StorePath) {
		cfg.StorePath = filepath.Clean(filepath.Join(baseDir, cfg.StorePath))
	}
	if cfg.StateStore.Path != "" && !filepath.IsAbs(cfg.StateStore.Path) {
		cfg.StateStore.Path = filepath.Clean(filepath.Join(baseDir, cfg.StateStore.Path))
	}
	if cfg.EventStore.Path != "" && !filepath.IsAbs(cfg.EventStore.Path) {
		cfg.EventStore.Path = filepath.Clean(filepath.Join(baseDir, cfg.EventStore.Path))
	}
	for i := range cfg.Projects {
		projectPath := os.ExpandEnv(cfg.Projects[i].Path)
		if !filepath.IsAbs(projectPath) {
			projectPath = filepath.Join(baseDir, projectPath)
		}
		cfg.Projects[i].Path = filepath.Clean(projectPath)
	}
	return nil
}

func (c *Config) Validate() error {
	if c.Slack.AppToken == "" {
		return errors.New("slack.app_token is required or set SLACK_APP_TOKEN")
	}
	if c.Slack.BotToken == "" {
		return errors.New("slack.bot_token is required or set SLACK_BOT_TOKEN")
	}
	if len(c.Projects) == 0 {
		return errors.New("at least one project is required")
	}
	if len(c.Agents) == 0 {
		return errors.New("at least one agent is required")
	}
	switch c.LogLevel {
	case "trace", "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("log_level must be one of trace, debug, info, warn, error")
	}
	switch c.StateStore.Type {
	case "json", "sqlite":
	default:
		return fmt.Errorf("state_store.type must be one of json, sqlite")
	}
	if strings.TrimSpace(c.StateStore.Path) == "" {
		return errors.New("state_store.path is required")
	}
	switch c.EventStore.Type {
	case "none", "jsonl", "sqlite":
	default:
		return fmt.Errorf("event_store.type must be one of none, jsonl, sqlite")
	}
	if c.EventStore.Type != "none" && strings.TrimSpace(c.EventStore.Path) == "" {
		return errors.New("event_store.path is required unless event_store.type is none")
	}
	if c.DefaultAgent != "" {
		if _, ok := c.Agents[c.DefaultAgent]; !ok {
			return fmt.Errorf("default_agent %q not found in agents", c.DefaultAgent)
		}
	}
	seenChannels := make(map[string]string)
	seenAgentSelectors := make(map[string]string)
	for _, p := range c.Projects {
		if strings.TrimSpace(p.Name) == "" {
			return errors.New("project name is required")
		}
		if strings.TrimSpace(p.Path) == "" {
			return fmt.Errorf("project %q path is required", p.Name)
		}
		if p.DefaultAgent != "" {
			if _, ok := c.Agents[p.DefaultAgent]; !ok {
				return fmt.Errorf("project %q default_agent %q not found", p.Name, p.DefaultAgent)
			}
		}
		for _, channelID := range p.ChannelIDs {
			channelID = strings.TrimSpace(channelID)
			if channelID == "" {
				continue
			}
			if existing, ok := seenChannels[channelID]; ok {
				if existing == p.Name {
					return fmt.Errorf("project %q has duplicate channel_id %q", p.Name, channelID)
				}
				return fmt.Errorf("channel_id %q is assigned to multiple projects: %s, %s", channelID, existing, p.Name)
			}
			seenChannels[channelID] = p.Name
		}
		seenCommands := make(map[string]struct{})
		for _, command := range p.Commands {
			commandName := strings.TrimSpace(command.Name)
			if commandName == "" {
				return fmt.Errorf("project %q command name is required", p.Name)
			}
			if _, ok := seenCommands[commandName]; ok {
				return fmt.Errorf("project %q has duplicate command %q", p.Name, commandName)
			}
			seenCommands[commandName] = struct{}{}
			if strings.TrimSpace(command.Command) == "" {
				return fmt.Errorf("project %q command %q requires command", p.Name, commandName)
			}
		}
	}
	for name, agent := range c.Agents {
		if strings.TrimSpace(name) == "" {
			return errors.New("agent name is required")
		}
		if agent.Mode != "" && agent.Mode != "oneshot" && agent.Mode != "persistent" {
			return fmt.Errorf("agent %q mode must be oneshot or persistent", name)
		}
		if agent.Transport != "" && agent.Transport != "stdio" && agent.Transport != "pty" {
			return fmt.Errorf("agent %q transport must be stdio or pty", name)
		}
		if strings.TrimSpace(agent.Command) == "" && strings.TrimSpace(agent.Adapter) == "" {
			switch name {
			case "codex", "gemini", "claude":
			default:
				return fmt.Errorf("agent %q requires command or adapter", name)
			}
		}
		normalizedName := normalizeAgentSelector(name)
		if normalizedName == "" {
			return fmt.Errorf("agent %q has an empty normalized selector", name)
		}
		if existing, ok := seenAgentSelectors[normalizedName]; ok && existing != name {
			return fmt.Errorf("agent selector %q is assigned to multiple agents: %s, %s", normalizedName, existing, name)
		}
		seenAgentSelectors[normalizedName] = name
		for _, alias := range agent.Aliases {
			normalizedAlias := normalizeAgentSelector(alias)
			if normalizedAlias == "" {
				return fmt.Errorf("agent %q has an empty alias", name)
			}
			if existing, ok := seenAgentSelectors[normalizedAlias]; ok {
				if existing == name {
					return fmt.Errorf("agent %q alias %q duplicates its own selector", name, alias)
				}
				return fmt.Errorf("agent alias %q conflicts with agent %s", alias, existing)
			}
			seenAgentSelectors[normalizedAlias] = name
		}
	}
	return nil
}

func normalizeAgentSelector(value string) string {
	normalized := strings.TrimSpace(strings.ToLower(value))
	normalized = strings.TrimLeft(normalized, "@")
	normalized = strings.TrimLeft(normalized, "#")
	normalized = strings.TrimSuffix(normalized, ":")
	normalized = strings.TrimSpace(normalized)
	return normalized
}
