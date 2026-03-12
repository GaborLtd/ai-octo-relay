package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/match/ai-octo-relay/internal/agent"
	"github.com/match/ai-octo-relay/internal/config"
	"github.com/match/ai-octo-relay/internal/project"
	"github.com/match/ai-octo-relay/internal/store"
)

type Service struct {
	cfg      *config.Config
	projects *project.Registry
	agents   *agent.Registry
	store    *store.JSONStore
}

type Scope struct {
	ProjectName string
	AgentName   string
	IsThread    bool
	ThreadKey   string
	SessionKey  string
	Quiet       bool
}

func NewService(cfg *config.Config, projects *project.Registry, agents *agent.Registry, stateStore *store.JSONStore) *Service {
	return &Service{
		cfg:      cfg,
		projects: projects,
		agents:   agents,
		store:    stateStore,
	}
}

func (s *Service) HelpText() string {
	return strings.Join([]string{
		"Commands:",
		s.cfg.CommandPrefix + "help",
		s.cfg.CommandPrefix + "status",
		s.cfg.CommandPrefix + "project list",
		s.cfg.CommandPrefix + "project current",
		s.cfg.CommandPrefix + "project use <name>  (thread override only)",
		s.cfg.CommandPrefix + "project clear      (thread override only)",
		s.cfg.CommandPrefix + "agent list",
		s.cfg.CommandPrefix + "agent use <name>",
		s.cfg.CommandPrefix + "agent clear",
		s.cfg.CommandPrefix + "session status",
		s.cfg.CommandPrefix + "session restart",
		s.cfg.CommandPrefix + "session close",
		s.cfg.CommandPrefix + "quiet",
		s.cfg.CommandPrefix + "quiet on",
		s.cfg.CommandPrefix + "quiet off",
		s.cfg.CommandPrefix + "reset",
		"",
		"非指令訊息會送到目前選擇的 project + agent 執行。",
	}, "\n")
}

func (s *Service) ResolveScope(channelID, threadTS string) (Scope, error) {
	channelState := s.store.GetChannel(channelID)
	scope := Scope{
		ProjectName: channelState.Project,
		AgentName:   channelState.Agent,
		IsThread:    threadTS != "",
		Quiet:       s.cfg.QuietByDefault,
	}
	if channelState.Quiet != nil {
		scope.Quiet = *channelState.Quiet
	}

	if threadTS != "" {
		scope.ThreadKey = channelID + ":" + threadTS
		threadState := s.store.GetThread(scope.ThreadKey)
		if threadState.Project != "" {
			scope.ProjectName = threadState.Project
		}
		if threadState.Agent != "" {
			scope.AgentName = threadState.Agent
		}
		if threadState.Quiet != nil {
			scope.Quiet = *threadState.Quiet
		}
	}

	if scope.ProjectName == "" {
		if channelProject, ok := s.projects.ProjectForChannel(channelID); ok {
			scope.ProjectName = channelProject.Name
		} else {
			projects := s.projects.List()
			if len(projects) == 0 {
				return Scope{}, errors.New("no project configured")
			}
			scope.ProjectName = projects[0].Name
		}
	}

	projectCfg, ok := s.projects.Get(scope.ProjectName)
	if !ok {
		return Scope{}, fmt.Errorf("unknown project in state: %s", scope.ProjectName)
	}

	if scope.AgentName == "" {
		switch {
		case projectCfg.DefaultAgent != "":
			scope.AgentName = projectCfg.DefaultAgent
		case s.cfg.DefaultAgent != "":
			scope.AgentName = s.cfg.DefaultAgent
		default:
			names := s.agents.Names()
			if len(names) == 0 {
				return Scope{}, errors.New("no agent configured")
			}
			scope.AgentName = names[0]
		}
	}

	if !s.agents.Has(scope.AgentName) {
		return Scope{}, fmt.Errorf("unknown agent in state: %s", scope.AgentName)
	}

	threadPart := "channel"
	if threadTS != "" {
		threadPart = threadTS
	}
	scope.SessionKey = fmt.Sprintf("%s:%s:%s:%s", channelID, threadPart, scope.ProjectName, scope.AgentName)

	return scope, nil
}

func (s *Service) StatusText(channelID, threadTS string) (string, error) {
	scope, err := s.ResolveScope(channelID, threadTS)
	if err != nil {
		return "", err
	}
	projectCfg, _ := s.projects.Get(scope.ProjectName)
	scopeLabel := "channel"
	if scope.IsThread {
		scopeLabel = "thread"
	}
	return fmt.Sprintf(
		"scope: %s\nproject: %s\npath: %s\nagent: %s\nagent_mode: %s",
		scopeLabel,
		scope.ProjectName,
		projectCfg.Path,
		scope.AgentName,
		s.agents.Mode(scope.AgentName),
	), nil
}

func (s *Service) ProjectListText() string {
	var lines []string
	for _, projectCfg := range s.projects.List() {
		line := fmt.Sprintf("- %s => %s", projectCfg.Name, projectCfg.Path)
		if projectCfg.DefaultAgent != "" {
			line += fmt.Sprintf(" (default agent: %s)", projectCfg.DefaultAgent)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func (s *Service) ProjectCurrentText(channelID, threadTS string) (string, error) {
	scope, err := s.ResolveScope(channelID, threadTS)
	if err != nil {
		return "", err
	}
	projectCfg, _ := s.projects.Get(scope.ProjectName)
	source := "channel mapping"
	if threadTS != "" {
		threadState := s.store.GetThread(scope.ThreadKey)
		if threadState.Project != "" {
			source = "thread override"
		}
	}
	return fmt.Sprintf("project: %s\npath: %s\nsource: %s", scope.ProjectName, projectCfg.Path, source), nil
}

func (s *Service) AgentListText() string {
	var lines []string
	for _, name := range s.agents.Names() {
		mode := s.agents.Mode(name)
		if mode == "" {
			mode = agent.ModeOneShot
		}
		lines = append(lines, fmt.Sprintf("- %s (%s)", name, mode))
	}
	return strings.Join(lines, "\n")
}

func (s *Service) QuietStatusText(channelID, threadTS string) (string, error) {
	scope, err := s.ResolveScope(channelID, threadTS)
	if err != nil {
		return "", err
	}
	mode := "off"
	if scope.Quiet {
		mode = "on"
	}
	scopeLabel := "channel"
	if scope.IsThread {
		scopeLabel = "thread"
	}
	return fmt.Sprintf("quiet: %s\nscope: %s", mode, scopeLabel), nil
}

func (s *Service) SetQuiet(channelID, threadTS string, quiet bool) (string, error) {
	if threadTS != "" {
		scope, err := s.ResolveScope(channelID, threadTS)
		if err != nil {
			return "", err
		}
		threadState := s.store.GetThread(scope.ThreadKey)
		threadState.Quiet = &quiet
		if err := s.store.SetThread(scope.ThreadKey, threadState); err != nil {
			return "", err
		}
		return fmt.Sprintf("thread quiet set to %t", quiet), nil
	}

	state := s.store.GetChannel(channelID)
	state.Quiet = &quiet
	if err := s.store.SetChannel(channelID, state); err != nil {
		return "", err
	}
	return fmt.Sprintf("channel quiet set to %t", quiet), nil
}

func (s *Service) UseProject(channelID, threadTS, name string) (string, error) {
	if _, ok := s.projects.Get(name); !ok {
		return "", fmt.Errorf("project %q not found", name)
	}
	if threadTS == "" {
		return "", fmt.Errorf("channel project comes from config mapping; use this inside a thread only")
	}
	scope, err := s.ResolveScope(channelID, threadTS)
	if err != nil {
		return "", err
	}
	threadState := s.store.GetThread(scope.ThreadKey)
	threadState.Project = name
	if err := s.store.SetThread(scope.ThreadKey, threadState); err != nil {
		return "", err
	}
	return "thread project override set to " + name, nil
}

func (s *Service) ClearProject(channelID, threadTS string) (string, error) {
	if threadTS == "" {
		return "", fmt.Errorf("channel project comes from config mapping; nothing to clear at channel level")
	}
	scope, err := s.ResolveScope(channelID, threadTS)
	if err != nil {
		return "", err
	}
	threadState := s.store.GetThread(scope.ThreadKey)
	threadState.Project = ""
	if threadState.Agent == "" && threadState.Quiet == nil {
		if err := s.store.ClearThread(scope.ThreadKey); err != nil {
			return "", err
		}
	} else if err := s.store.SetThread(scope.ThreadKey, threadState); err != nil {
		return "", err
	}
	return "thread project override cleared", nil
}

func (s *Service) UseAgent(channelID, threadTS, name string) (string, error) {
	if !s.agents.Has(name) {
		return "", fmt.Errorf("agent %q not found", name)
	}
	if threadTS != "" {
		scope, err := s.ResolveScope(channelID, threadTS)
		if err != nil {
			return "", err
		}
		scope.AgentName = name
		if err := s.store.SetThread(scope.ThreadKey, store.ScopeState{Project: scope.ProjectName, Agent: scope.AgentName}); err != nil {
			return "", err
		}
		return "thread agent set to " + name, nil
	}

	state := s.store.GetChannel(channelID)
	state.Agent = name
	if err := s.store.SetChannel(channelID, state); err != nil {
		return "", err
	}
	return "channel agent set to " + name, nil
}

func (s *Service) ClearAgent(channelID, threadTS string) (string, error) {
	if threadTS != "" {
		scope, err := s.ResolveScope(channelID, threadTS)
		if err != nil {
			return "", err
		}
		threadState := s.store.GetThread(scope.ThreadKey)
		threadState.Agent = ""
		if threadState.Project == "" {
			if err := s.store.ClearThread(scope.ThreadKey); err != nil {
				return "", err
			}
		} else if err := s.store.SetThread(scope.ThreadKey, threadState); err != nil {
			return "", err
		}
		return "thread agent override cleared", nil
	}

	state := s.store.GetChannel(channelID)
	state.Agent = ""
	if state.Project == "" {
		if err := s.store.ClearChannel(channelID); err != nil {
			return "", err
		}
	} else if err := s.store.SetChannel(channelID, state); err != nil {
		return "", err
	}
	return "channel agent override cleared", nil
}

func (s *Service) Reset(channelID, threadTS string) (string, error) {
	if threadTS != "" {
		scope, err := s.ResolveScope(channelID, threadTS)
		if err != nil {
			return "", err
		}
		if err := s.store.ClearThread(scope.ThreadKey); err != nil {
			return "", err
		}
		return "thread overrides cleared", nil
	}
	if err := s.store.ClearChannel(channelID); err != nil {
		return "", err
	}
	return "channel overrides cleared", nil
}

func (s *Service) RunPrompt(ctx context.Context, channelID, threadTS, slackUserID, prompt string, onChunk func(string)) (string, error) {
	scope, err := s.ResolveScope(channelID, threadTS)
	if err != nil {
		return "", err
	}

	projectCfg, ok := s.projects.Get(scope.ProjectName)
	if !ok {
		return "", fmt.Errorf("project %q not found", scope.ProjectName)
	}
	if !s.agents.Has(scope.AgentName) {
		return "", fmt.Errorf("agent %q not found", scope.AgentName)
	}

	req := agent.RunRequest{
		AgentName:   scope.AgentName,
		ProjectName: scope.ProjectName,
		ProjectPath: projectCfg.Path,
		Prompt:      prompt,
		SessionKey:  scope.SessionKey,
		ThreadTS:    threadTS,
		ChannelID:   channelID,
		SlackUserID: slackUserID,
		ExtraEnvVars: map[string]string{
			"AI_OCTO_PROJECT":    projectCfg.Name,
			"AI_OCTO_CHANNEL_ID": channelID,
			"AI_OCTO_THREAD_TS":  threadTS,
			"AI_OCTO_SLACK_USER": slackUserID,
		},
	}

	return s.agents.Run(ctx, req, onChunk)
}

func (s *Service) SessionStatusText(channelID, threadTS string) (string, error) {
	scope, err := s.ResolveScope(channelID, threadTS)
	if err != nil {
		return "", err
	}
	info, ok := s.agents.SessionStatus(scope.SessionKey)
	if !ok {
		return fmt.Sprintf("session: inactive\nkey: %s\nagent: %s", scope.SessionKey, scope.AgentName), nil
	}
	return fmt.Sprintf(
		"session: active\nkey: %s\nagent: %s\nstarted_at: %s\nlast_used_at: %s\ninteractive: %t\nresponse_idle: %s",
		info.Key,
		info.AgentName,
		info.StartedAt.Format("2006-01-02 15:04:05"),
		info.LastUsedAt.Format("2006-01-02 15:04:05"),
		info.Interactive,
		info.ResponseIdle,
	), nil
}

func (s *Service) RestartSession(ctx context.Context, channelID, threadTS, slackUserID string) (string, error) {
	scope, err := s.ResolveScope(channelID, threadTS)
	if err != nil {
		return "", err
	}
	projectCfg, ok := s.projects.Get(scope.ProjectName)
	if !ok {
		return "", fmt.Errorf("project %q not found", scope.ProjectName)
	}
	info, err := s.agents.RestartSession(ctx, agent.RunRequest{
		AgentName:   scope.AgentName,
		ProjectName: scope.ProjectName,
		ProjectPath: projectCfg.Path,
		SessionKey:  scope.SessionKey,
		ThreadTS:    threadTS,
		ChannelID:   channelID,
		SlackUserID: slackUserID,
		ExtraEnvVars: map[string]string{
			"AI_OCTO_PROJECT":    projectCfg.Name,
			"AI_OCTO_CHANNEL_ID": channelID,
			"AI_OCTO_THREAD_TS":  threadTS,
			"AI_OCTO_SLACK_USER": slackUserID,
		},
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("session restarted\nkey: %s\nagent: %s", info.Key, info.AgentName), nil
}

func (s *Service) CloseSession(channelID, threadTS string) (string, error) {
	scope, err := s.ResolveScope(channelID, threadTS)
	if err != nil {
		return "", err
	}
	if err := s.agents.CloseSession(scope.SessionKey); err != nil {
		return "", err
	}
	return "session closed", nil
}
