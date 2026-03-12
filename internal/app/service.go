package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/match/ai-octo-relay/internal/agent"
	"github.com/match/ai-octo-relay/internal/config"
	"github.com/match/ai-octo-relay/internal/project"
	"github.com/match/ai-octo-relay/internal/store"
)

type Service struct {
	cfg      *config.Config
	projects *project.Registry
	agents   *agent.Registry
	store    store.StateStore
	events   store.EventStore
}

type Scope struct {
	ProjectName string
	AgentName   string
	IsThread    bool
	ThreadKey   string
	SessionKey  string
	Quiet       bool
}

func NewService(cfg *config.Config, projects *project.Registry, agents *agent.Registry, stateStore store.StateStore, eventStore store.EventStore) *Service {
	return &Service{
		cfg:      cfg,
		projects: projects,
		agents:   agents,
		store:    stateStore,
		events:   eventStore,
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
		s.cfg.CommandPrefix + "agent model list <name>",
		s.cfg.CommandPrefix + "agent use <name>",
		s.cfg.CommandPrefix + "agent clear",
		s.cfg.CommandPrefix + "cmd list",
		s.cfg.CommandPrefix + "cmd run <name>",
		s.cfg.CommandPrefix + "git status|diff|log|branch|show|fetch|pull",
		s.cfg.CommandPrefix + "session status",
		s.cfg.CommandPrefix + "session restart",
		s.cfg.CommandPrefix + "session close",
		s.cfg.CommandPrefix + "quiet",
		s.cfg.CommandPrefix + "quiet on",
		s.cfg.CommandPrefix + "quiet off",
		s.cfg.CommandPrefix + "reset",
		"",
		"非指令訊息會送到目前選擇的 project + agent 執行。",
		"同一個 thread 會優先沿用各 CLI 自己的 session/resume 能力。",
		"也可在訊息開頭指定 agent，例如：gemini: 幫我看這個錯誤。",
	}, "\n")
}

func (s *Service) ResolveScope(channelID, threadTS string) (Scope, error) {
	return s.resolveScope(channelID, threadTS, "")
}

func (s *Service) ResolvePromptScope(channelID, threadTS, agentOverride string) (Scope, error) {
	return s.resolveScope(channelID, threadTS, agentOverride)
}

func (s *Service) ValidateAgentLock(channelID, threadTS, agentOverride string) error {
	if agentOverride == "" {
		return nil
	}
	scope, err := s.ResolveScope(channelID, threadTS)
	if err != nil {
		return err
	}
	if scope.AgentName == "" {
		return nil
	}
	if scope.AgentName == agentOverride {
		return nil
	}

	if threadTS != "" {
		threadState := s.store.GetThread(scope.ThreadKey)
		if threadState.SessionActive {
			return fmt.Errorf("這個 thread 已綁定 agent %s；若要改用 %s，請開新 thread", scope.AgentName, agentOverride)
		}
		return nil
	}

	channelState := s.store.GetChannel(channelID)
	if channelState.SessionActive {
		return fmt.Errorf("這個 DM session 已綁定 agent %s；若要改用 %s，請先 %ssession restart 或開新對話", scope.AgentName, agentOverride, s.cfg.CommandPrefix)
	}
	return nil
}

func (s *Service) resolveScope(channelID, threadTS, agentOverride string) (Scope, error) {
	channelState := s.store.GetChannel(channelID)
	scope := Scope{
		IsThread:    threadTS != "",
		Quiet:       s.cfg.QuietByDefault,
	}
	if threadTS == "" {
		scope.ProjectName = channelState.Project
		scope.AgentName = channelState.Agent
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

	if agentOverride != "" {
		scope.AgentName = agentOverride
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

	canonicalAgentName, ok := s.agents.ResolveName(scope.AgentName)
	if !ok {
		return Scope{}, fmt.Errorf("unknown agent in state: %s", scope.AgentName)
	}
	scope.AgentName = canonicalAgentName

	threadPart := "channel"
	if threadTS != "" {
		threadPart = threadTS
	}
	scope.SessionKey = fmt.Sprintf("%s:%s:%s:%s", channelID, threadPart, scope.ProjectName, scope.AgentName)

	return scope, nil
}

func (s *Service) ExtractAgentOverride(text string) (string, string, bool) {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) == 0 {
		return "", "", false
	}
	selector := fields[0]
	if !isAgentSelectorToken(selector) {
		return "", text, false
	}
	agentName, ok := s.agents.ResolveName(selector)
	if !ok {
		return "", text, false
	}
	return agentName, strings.TrimSpace(strings.Join(fields[1:], " ")), true
}

func isAgentSelectorToken(token string) bool {
	if strings.HasPrefix(token, "#") {
		return true
	}
	return strings.HasSuffix(token, ":")
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

func (s *Service) ProjectCommandListText(channelID, threadTS string) (string, error) {
	scope, err := s.ResolveScope(channelID, threadTS)
	if err != nil {
		return "", err
	}
	projectCfg, ok := s.projects.Get(scope.ProjectName)
	if !ok {
		return "", fmt.Errorf("project %q not found", scope.ProjectName)
	}
	if len(projectCfg.CommandNames) == 0 {
		return "no project commands configured", nil
	}
	lines := make([]string, 0, len(projectCfg.CommandNames))
	for _, name := range projectCfg.CommandNames {
		command := projectCfg.Commands[name]
		line := fmt.Sprintf("- %s => %s %s", command.Name, command.Command, strings.Join(command.Args, " "))
		line = strings.TrimSpace(line)
		if strings.TrimSpace(command.Description) != "" {
			line += fmt.Sprintf(" | %s", command.Description)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n"), nil
}

func (s *Service) RunProjectCommand(ctx context.Context, channelID, threadTS, name string) (string, error) {
	scope, err := s.ResolveScope(channelID, threadTS)
	if err != nil {
		return "", err
	}
	projectCfg, ok := s.projects.Get(scope.ProjectName)
	if !ok {
		return "", fmt.Errorf("project %q not found", scope.ProjectName)
	}
	command, ok := projectCfg.Commands[name]
	if !ok {
		return "", fmt.Errorf("project command %q not found", name)
	}
	return s.runCommand(ctx, projectCfg.Path, command.Command, command.Args...)
}

func (s *Service) RunGitCommand(ctx context.Context, channelID, threadTS string, args []string) (string, error) {
	scope, err := s.ResolveScope(channelID, threadTS)
	if err != nil {
		return "", err
	}
	projectCfg, ok := s.projects.Get(scope.ProjectName)
	if !ok {
		return "", fmt.Errorf("project %q not found", scope.ProjectName)
	}
	if len(args) == 0 {
		return "", fmt.Errorf("missing git subcommand")
	}
	cmdArgs, err := validateGitArgs(args)
	if err != nil {
		return "", err
	}
	output, runErr := s.runCommand(ctx, projectCfg.Path, "git", cmdArgs...)
	if len(args) > 0 && args[0] == "branch" {
		output = formatGitBranchOutput(output)
	}
	return output, runErr
}

func formatGitBranchOutput(text string) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) == 0 {
		return text
	}
	out := make([]string, 0, len(lines))
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		current := strings.HasPrefix(line, "* ")
		line = strings.TrimSpace(strings.TrimPrefix(line, "*"))
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}

		name := fields[0]
		rest := strings.TrimSpace(strings.TrimPrefix(line, name))
		prefix := "-"
		if current {
			prefix = "*"
		}
		if strings.HasPrefix(name, "remotes/") {
			name = strings.TrimPrefix(name, "remotes/")
		}
		out = append(out, fmt.Sprintf("%s `%s`%s", prefix, name, formatBranchSuffix(rest)))
	}
	if len(out) == 0 {
		return text
	}
	return strings.Join(out, "\n")
}

func formatBranchSuffix(rest string) string {
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return ""
	}
	return " " + rest
}

func (s *Service) ValidateDMCommandAccess(command string, args []string) error {
	if !s.cfg.DMReadOnly {
		return nil
	}
	switch command {
	case "cmd":
		return fmt.Errorf("dm_read_only 已開啟；DM 中不可使用 !cmd，project command 只能在對應 channel/thread 中執行")
	case "git":
		return fmt.Errorf("dm_read_only 已開啟；DM 中不可使用 !git，git 操作只能在對應 channel/thread 中執行")
	}
	return nil
}

func (s *Service) runCommand(ctx context.Context, dir, command string, args ...string) (string, error) {
	runCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	cmd := exec.CommandContext(runCtx, command, args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(output))
	if text == "" && err == nil {
		text = "(no output)"
	}
	if runCtx.Err() == context.DeadlineExceeded {
		if text == "" {
			return "", fmt.Errorf("command timed out after 120s")
		}
		return text, fmt.Errorf("command timed out after 120s")
	}
	if err != nil {
		if text == "" {
			return "", fmt.Errorf("command failed: %w", err)
		}
		return text, fmt.Errorf("command failed: %w", err)
	}
	return text, nil
}

func validateGitArgs(args []string) ([]string, error) {
	switch args[0] {
	case "status":
		return []string{"status", "--short", "--branch"}, nil
	case "diff":
		out := []string{"diff"}
		for _, arg := range args[1:] {
			switch arg {
			case "--staged", "--stat", "--cached":
				out = append(out, arg)
			default:
				return nil, fmt.Errorf("unsupported git diff option: %s", arg)
			}
		}
		return out, nil
	case "log":
		out := []string{"log", "--oneline", "--decorate", "-n", "20"}
		if len(args) > 1 {
			n, err := strconv.Atoi(args[1])
			if err != nil || n <= 0 || n > 100 {
				return nil, fmt.Errorf("git log count must be 1-100")
			}
			out = []string{"log", "--oneline", "--decorate", "-n", strconv.Itoa(n)}
		}
		return out, nil
	case "branch":
		return []string{"branch", "--all", "--verbose"}, nil
	case "show":
		if len(args) < 2 {
			return nil, fmt.Errorf("git show requires a revision")
		}
		return []string{"show", "--stat", "--summary", args[1]}, nil
	case "fetch":
		return []string{"fetch", "--all", "--prune"}, nil
	case "pull":
		return []string{"pull", "--ff-only"}, nil
	default:
		return nil, fmt.Errorf("unsupported git subcommand: %s", args[0])
	}
}

func (s *Service) AgentListText() string {
	var lines []string
	for _, name := range s.agents.Names() {
		mode := s.agents.Mode(name)
		if mode == "" {
			mode = agent.ModeOneShot
		}
		line := fmt.Sprintf("- %s (%s)", name, mode)
		aliases := s.agents.Aliases(name)
		if len(aliases) > 0 {
			line += fmt.Sprintf(" aliases: %s", strings.Join(aliases, ", "))
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func (s *Service) AgentModelListText(name string) (string, error) {
	canonicalName, ok := s.agents.ResolveName(name)
	if !ok {
		return "", fmt.Errorf("agent %q not found", name)
	}

	cfg, ok := s.cfg.Agents[canonicalName]
	if !ok {
		return "", fmt.Errorf("agent %q config not found", canonicalName)
	}

	currentModel := strings.TrimSpace(cfg.Model)
	if currentModel == "" {
		currentModel = "(not set)"
	}

	switch canonicalName {
	case "codex":
		return strings.Join([]string{
			fmt.Sprintf("agent: %s", canonicalName),
			fmt.Sprintf("configured_model: %s", currentModel),
			"CLI status: codex 目前沒有穩定的 `list models` 子命令可直接列出你帳號可用 model。",
			"請改看 OpenAI models 文件，或直接在 config 設 `agents.codex.model`。",
		}, "\n"), nil
	case "claude":
		return strings.Join([]string{
			fmt.Sprintf("agent: %s", canonicalName),
			fmt.Sprintf("configured_model: %s", currentModel),
			"CLI status: claude 目前沒有穩定的 `list models` 子命令可直接列出你帳號可用 model。",
			"可用 model 仍以 Anthropic 帳號權限與官方 models 文件為準。",
		}, "\n"), nil
	case "gemini":
		return strings.Join([]string{
			fmt.Sprintf("agent: %s", canonicalName),
			fmt.Sprintf("configured_model: %s", currentModel),
			"CLI status: gemini 目前沒有穩定的 `list models` 子命令可直接列出你帳號可用 model。",
			"建議直接指定 `gemini-2.5-flash`；實際可用性仍受帳號權限與 server capacity 影響。",
		}, "\n"), nil
	default:
		return strings.Join([]string{
			fmt.Sprintf("agent: %s", canonicalName),
			fmt.Sprintf("configured_model: %s", currentModel),
			"這個 agent 目前沒有內建 model list 探測邏輯。",
		}, "\n"), nil
	}
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

func (s *Service) MarkThreadSessionActive(channelID, threadTS string) error {
	if threadTS == "" {
		state := s.store.GetChannel(channelID)
		state.SessionActive = true
		return s.store.SetChannel(channelID, state)
	}
	threadKey := channelID + ":" + threadTS
	state := s.store.GetThread(threadKey)
	state.SessionActive = true
	return s.store.SetThread(threadKey, state)
}

func (s *Service) HasThreadSession(channelID, threadTS string) bool {
	if threadTS == "" {
		state := s.store.GetChannel(channelID)
		return state.SessionActive
	}
	threadKey := channelID + ":" + threadTS
	state := s.store.GetThread(threadKey)
	return state.SessionActive
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
	if threadState.Agent == "" && threadState.Quiet == nil && !threadState.SessionActive {
		if err := s.store.ClearThread(scope.ThreadKey); err != nil {
			return "", err
		}
	} else if err := s.store.SetThread(scope.ThreadKey, threadState); err != nil {
		return "", err
	}
	return "thread project override cleared", nil
}

func (s *Service) UseAgent(channelID, threadTS, name string) (string, error) {
	canonicalName, ok := s.agents.ResolveName(name)
	if !ok {
		return "", fmt.Errorf("agent %q not found", name)
	}
	if threadTS != "" {
		scope, err := s.ResolveScope(channelID, threadTS)
		if err != nil {
			return "", err
		}
		threadState := s.store.GetThread(scope.ThreadKey)
		threadState.Project = scope.ProjectName
		threadState.Agent = canonicalName
		if err := s.store.SetThread(scope.ThreadKey, threadState); err != nil {
			return "", err
		}
		return "thread agent set to " + canonicalName, nil
	}

	state := s.store.GetChannel(channelID)
	state.Agent = canonicalName
	if err := s.store.SetChannel(channelID, state); err != nil {
		return "", err
	}
	return "channel agent set to " + canonicalName, nil
}

func (s *Service) ClearAgent(channelID, threadTS string) (string, error) {
	if threadTS != "" {
		scope, err := s.ResolveScope(channelID, threadTS)
		if err != nil {
			return "", err
		}
		threadState := s.store.GetThread(scope.ThreadKey)
		threadState.Agent = ""
		if threadState.Project == "" && threadState.Quiet == nil && !threadState.SessionActive {
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
		if err := s.store.ClearSession(scope.SessionKey); err != nil {
			return "", err
		}
		return "thread overrides cleared", nil
	}
	if err := s.store.ClearChannel(channelID); err != nil {
		return "", err
	}
	return "channel overrides cleared", nil
}

func (s *Service) RunPrompt(ctx context.Context, channelID, threadTS, slackUserID, prompt, agentOverride string, isDM bool, onChunk func(string)) (string, error) {
	scope, err := s.resolveScope(channelID, threadTS, agentOverride)
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

	nativeSession := s.store.GetSession(scope.SessionKey)
	nativeSessionID := nativeSession.NativeID
	if nativeSessionID == "" && scope.AgentName == "claude" {
		nativeSessionID, err = newSessionUUID()
		if err != nil {
			return "", fmt.Errorf("generate claude session id: %w", err)
		}
	}

	req := agent.RunRequest{
		AgentName:       scope.AgentName,
		ProjectName:     scope.ProjectName,
		ProjectPath:     projectCfg.Path,
		Prompt:          prompt,
		PromptSuffix:    s.promptSuffixForContext(isDM),
		DMReadOnly:      isDM && s.cfg.DMReadOnly,
		SessionKey:      scope.SessionKey,
		ThreadTS:        threadTS,
		ChannelID:       channelID,
		SlackUserID:     slackUserID,
		NativeSessionID: nativeSessionID,
		ExtraEnvVars: map[string]string{
			"AI_OCTO_PROJECT":    projectCfg.Name,
			"AI_OCTO_CHANNEL_ID": channelID,
			"AI_OCTO_THREAD_TS":  threadTS,
			"AI_OCTO_SLACK_USER": slackUserID,
		},
	}

	result, err := s.agents.Run(ctx, req, onChunk)
	if shouldRetryWithFreshNativeSession(scope.AgentName, nativeSessionID, err, result.Output) {
		if clearErr := s.store.ClearSession(scope.SessionKey); clearErr != nil {
			return result.Output, clearErr
		}
		s.appendEvent(scope, channelID, "session.native_cleared_for_retry", map[string]any{
			"previous_native_session_id": nativeSessionID,
		})
		req.NativeSessionID = ""
		result, err = s.agents.Run(ctx, req, onChunk)
	}
	if result.NativeSessionID != "" && result.NativeSessionID != nativeSession.NativeID {
		if saveErr := s.store.SetSession(scope.SessionKey, store.NativeSessionState{
			Agent:     scope.AgentName,
			NativeID:  result.NativeSessionID,
			UpdatedAt: time.Now().Format(time.RFC3339),
			Project:   scope.ProjectName,
			ThreadKey: scope.ThreadKey,
			ChannelID: channelID,
		}); saveErr != nil {
			return "", saveErr
		}
		s.appendEvent(scope, channelID, "session.native_saved", map[string]any{
			"native_session_id":          result.NativeSessionID,
			"previous_native_session_id": nativeSession.NativeID,
		})
	}
	if friendly := summarizeAgentFailure(scope.AgentName, result.Output, err); friendly != "" {
		return friendly, nil
	}
	return result.Output, err
}

func shouldRetryWithFreshNativeSession(agentName, nativeSessionID string, runErr error, output string) bool {
	if nativeSessionID == "" || runErr == nil {
		return false
	}
	text := strings.ToLower(output)
	if runErr != nil {
		text += "\n" + strings.ToLower(runErr.Error())
	}
	switch agentName {
	case "gemini":
		return strings.Contains(text, "fatalturnlimitederror") ||
			strings.Contains(text, "reached max session turns") ||
			strings.Contains(text, `"code": 53`)
	default:
		return false
	}
}

func summarizeAgentFailure(agentName, output string, runErr error) string {
	if runErr == nil {
		return ""
	}

	text := strings.ToLower(output)
	if runErr != nil {
		text += "\n" + strings.ToLower(runErr.Error())
	}

	switch agentName {
	case "gemini":
		if strings.Contains(text, "model_capacity_exhausted") ||
			strings.Contains(text, "resource_exhausted") ||
			strings.Contains(text, "no capacity available for model") ||
			strings.Contains(text, "status 429") ||
			strings.Contains(text, `"code": 429`) {
			return "Gemini 目前不可用（429 / model capacity exhausted）。請稍後再試，或改用 `codex:` / `claude:`。"
		}
		if strings.Contains(text, "fatalturnlimitederror") ||
			strings.Contains(text, "reached max session turns") ||
			strings.Contains(text, `"code": 53`) {
			return "Gemini session 已達最大 turns，系統已嘗試自動重置；若仍失敗，請執行 `!session restart`，或改開新 thread / 改用 `codex:`。"
		}
	}

	return ""
}

func (s *Service) promptSuffixForContext(isDM bool) string {
	if !isDM || !s.cfg.DMReadOnly {
		return ""
	}
	return s.cfg.DMReadOnlyPrompt
}

func (s *Service) SessionStatusText(channelID, threadTS string) (string, error) {
	scope, err := s.ResolveScope(channelID, threadTS)
	if err != nil {
		return "", err
	}
	if native := s.store.GetSession(scope.SessionKey); native.NativeID != "" {
		return fmt.Sprintf(
			"session: active\nkey: %s\nagent: %s\nmode: native-resume\nnative_session_id: %s\nupdated_at: %s",
			scope.SessionKey,
			scope.AgentName,
			native.NativeID,
			native.UpdatedAt,
		), nil
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
	if s.agents.Mode(scope.AgentName) != agent.ModePersistent {
		if err := s.store.ClearSession(scope.SessionKey); err != nil {
			return "", err
		}
		s.appendEvent(scope, channelID, "session.native_cleared", nil)
		return fmt.Sprintf("native session cleared\nkey: %s\nagent: %s", scope.SessionKey, scope.AgentName), nil
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
	s.appendEvent(scope, channelID, "session.restarted", map[string]any{
		"interactive": true,
	})
	return fmt.Sprintf("session restarted\nkey: %s\nagent: %s", info.Key, info.AgentName), nil
}

func (s *Service) CloseSession(channelID, threadTS string) (string, error) {
	scope, err := s.ResolveScope(channelID, threadTS)
	if err != nil {
		return "", err
	}
	if err := s.store.ClearSession(scope.SessionKey); err != nil {
		return "", err
	}
	s.appendEvent(scope, channelID, "session.closed", nil)
	if s.agents.Mode(scope.AgentName) != agent.ModePersistent {
		return "session closed", nil
	}
	if err := s.agents.CloseSession(scope.SessionKey); err != nil {
		return "", err
	}
	return "session closed", nil
}

func (s *Service) appendEvent(scope Scope, channelID, eventType string, payload map[string]any) {
	if s.events == nil {
		return
	}
	_ = s.events.Append(store.Event{
		Type:       eventType,
		Timestamp:  time.Now().Format(time.RFC3339),
		ChannelID:  channelID,
		ThreadKey:  scope.ThreadKey,
		SessionKey: scope.SessionKey,
		Project:    scope.ProjectName,
		Agent:      scope.AgentName,
		Payload:    payload,
	})
}

func newSessionUUID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	hexValue := hex.EncodeToString(raw[:])
	return fmt.Sprintf(
		"%s-%s-%s-%s-%s",
		hexValue[0:8],
		hexValue[8:12],
		hexValue[12:16],
		hexValue[16:20],
		hexValue[20:32],
	), nil
}
