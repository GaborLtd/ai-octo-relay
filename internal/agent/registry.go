package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"

	"github.com/match/ai-octo-relay/internal/config"
	"github.com/match/ai-octo-relay/internal/logx"
)

type RunRequest struct {
	AgentName       string
	ProjectName     string
	ProjectPath     string
	Prompt          string
	SessionKey      string
	NativeSessionID string
	LastMessagePath string
	ThreadTS        string
	ChannelID       string
	SlackUserID     string
	ExtraEnvVars    map[string]string
}

type RunResult struct {
	Output          string
	NativeSessionID string
}

type SessionInfo struct {
	Key          string
	AgentName    string
	Mode         string
	StartedAt    time.Time
	LastUsedAt   time.Time
	Active       bool
	ProjectPath  string
	Interactive  bool
	ResponseIdle time.Duration
}

type Registry struct {
	defs     map[string]definition
	names    []string
	sessions *SessionManager
	logger   *logx.Logger
}

type definition struct {
	name    string
	config  config.AgentConfig
	adapter Adapter
}

type ExecSpec struct {
	Command            string
	Args               []string
	Env                map[string]string
	Mode               string
	Transport          string
	PromptInArgs       bool
	CaptureLastMessage bool
	PromptSuffix       string
	Timeout            time.Duration
	ResponseIdle       time.Duration
	FirstChunkTimeout  time.Duration
	SessionIdleTimeout time.Duration
	StartupWait        time.Duration
	OutputParser       string
}

type Adapter interface {
	Build(req RunRequest, cfg config.AgentConfig) ExecSpec
}

func NewRegistry(configs map[string]config.AgentConfig, logger *logx.Logger) (*Registry, error) {
	defs := make(map[string]definition, len(configs))
	names := make([]string, 0, len(configs))
	for name, cfg := range configs {
		defs[name] = definition{
			name:    name,
			config:  cfg,
			adapter: selectAdapter(name, cfg.Adapter),
		}
		names = append(names, name)
	}
	slices.Sort(names)
	reg := &Registry{
		defs:   defs,
		names:  names,
		logger: logger,
	}
	reg.sessions = NewSessionManager(reg)
	return reg, nil
}

func (r *Registry) Names() []string {
	return slices.Clone(r.names)
}

func (r *Registry) ValidateCommands() error {
	for _, name := range r.names {
		def := r.defs[name]
		req := RunRequest{AgentName: name}
		spec := def.adapter.Build(req, def.config)
		if _, err := exec.LookPath(spec.Command); err != nil {
			return fmt.Errorf("agent %q command %q not found in PATH: %w", name, spec.Command, err)
		}
	}
	return nil
}

func (r *Registry) Has(name string) bool {
	_, ok := r.defs[name]
	return ok
}

func (r *Registry) Mode(name string) string {
	def, ok := r.defs[name]
	if !ok {
		return ""
	}
	return def.config.Mode
}

func (r *Registry) Run(ctx context.Context, req RunRequest, onChunk func(string)) (RunResult, error) {
	def, ok := r.defs[req.AgentName]
	if !ok {
		return RunResult{}, fmt.Errorf("agent %q not found", req.AgentName)
	}
	spec := def.adapter.Build(req, def.config)
	if spec.OutputParser != "" {
		onChunk = nil
	}
	r.logger.Debugf("agent run spec: agent=%s mode=%s transport=%s command=%q args=%q project=%s session_key=%s native_session_id=%s capture_last_message=%t parser=%s", req.AgentName, spec.Mode, spec.Transport, spec.Command, strings.Join(spec.Args, " "), req.ProjectPath, req.SessionKey, req.NativeSessionID, spec.CaptureLastMessage, spec.OutputParser)
	if spec.Mode == ModePersistent {
		output, err := r.sessions.Send(ctx, req, spec, onChunk)
		return RunResult{Output: output}, err
	}
	return runOneShot(ctx, req, spec, onChunk)
}

func (r *Registry) SessionStatus(sessionKey string) (SessionInfo, bool) {
	return r.sessions.Status(sessionKey)
}

func (r *Registry) RestartSession(ctx context.Context, req RunRequest) (SessionInfo, error) {
	def, ok := r.defs[req.AgentName]
	if !ok {
		return SessionInfo{}, fmt.Errorf("agent %q not found", req.AgentName)
	}
	spec := def.adapter.Build(req, def.config)
	if spec.Mode != ModePersistent {
		return SessionInfo{}, fmt.Errorf("agent %q is not configured for persistent sessions", req.AgentName)
	}
	return r.sessions.Restart(ctx, req, spec)
}

func (r *Registry) CloseSession(sessionKey string) error {
	return r.sessions.Close(sessionKey)
}

const (
	ModeOneShot    = "oneshot"
	ModePersistent = "persistent"

	TransportStdio = "stdio"
	TransportPTY   = "pty"
)

type genericAdapter struct{}
type codexAdapter struct{}
type geminiAdapter struct{}
type claudeAdapter struct{}

func selectAdapter(agentName, explicit string) Adapter {
	switch strings.ToLower(strings.TrimSpace(explicit)) {
	case "codex":
		return codexAdapter{}
	case "gemini":
		return geminiAdapter{}
	case "claude":
		return claudeAdapter{}
	case "generic":
		return genericAdapter{}
	}

	switch strings.ToLower(agentName) {
	case "codex":
		return codexAdapter{}
	case "gemini":
		return geminiAdapter{}
	case "claude":
		return claudeAdapter{}
	default:
		return genericAdapter{}
	}
}

func (a genericAdapter) Build(req RunRequest, cfg config.AgentConfig) ExecSpec {
	return finalizeSpec(req, cfg, cfg.Command, cfg.Args, cfg.Command, cfg.InteractiveArgs)
}

func (a codexAdapter) Build(req RunRequest, cfg config.AgentConfig) ExecSpec {
	command := firstNonEmpty(cfg.Command, "codex")
	oneshotArgs := defaultArgs(cfg.Args, "exec", "--skip-git-repo-check", "--color", "never", "--json", "--output-last-message", "{{last_message_path}}", "-C", "{{project_path}}", "-")
	if req.NativeSessionID != "" && len(cfg.Args) == 0 {
		oneshotArgs = []string{"exec", "resume", "{{native_session_id}}", "--skip-git-repo-check", "--json", "--output-last-message", "{{last_message_path}}", "-"}
	}
	return finalizeSpec(
		req,
		cfg,
		command,
		oneshotArgs,
		command,
		defaultArgs(cfg.InteractiveArgs, "-C", "{{project_path}}"),
	)
}

func (a geminiAdapter) Build(req RunRequest, cfg config.AgentConfig) ExecSpec {
	command := firstNonEmpty(cfg.Command, "gemini")
	oneshotArgs := defaultArgs(cfg.Args, "--output-format", "json", "-p", "{{prompt}}")
	if req.NativeSessionID != "" && len(cfg.Args) == 0 {
		oneshotArgs = []string{"--resume", "{{native_session_id}}", "--output-format", "json", "-p", "{{prompt}}"}
	}
	return finalizeSpec(
		req,
		cfg,
		command,
		oneshotArgs,
		command,
		cfg.InteractiveArgs,
	)
}

func (a claudeAdapter) Build(req RunRequest, cfg config.AgentConfig) ExecSpec {
	command := firstNonEmpty(cfg.Command, "claude")
	oneshotArgs := defaultArgs(cfg.Args, "-p", "--output-format", "text", "--session-id", "{{native_session_id}}", "{{prompt}}")
	return finalizeSpec(
		req,
		cfg,
		command,
		oneshotArgs,
		command,
		cfg.InteractiveArgs,
	)
}

func finalizeSpec(req RunRequest, cfg config.AgentConfig, oneshotCommand string, oneshotArgs []string, interactiveCommand string, interactiveArgs []string) ExecSpec {
	mode := cfg.Mode
	if mode == "" {
		mode = ModeOneShot
	}
	transport := cfg.Transport
	if transport == "" {
		transport = TransportStdio
		if mode == ModePersistent {
			transport = TransportPTY
		}
	}
	command := oneshotCommand
	args := oneshotArgs
	if mode == ModePersistent {
		command = firstNonEmpty(cfg.InteractiveCommand, interactiveCommand)
		args = interactiveArgs
	}
	return ExecSpec{
		Command:            expandTemplate(command, req),
		Args:               expandSlice(args, req),
		Env:                expandMap(cfg.Env, req),
		Mode:               mode,
		Transport:          transport,
		PromptInArgs:       containsPromptTemplate(args),
		CaptureLastMessage: strings.Contains(command, "{{last_message_path}}") || containsLastMessageTemplate(args),
		PromptSuffix:       expandTemplate(firstNonEmpty(cfg.PromptSuffix, "\n"), req),
		Timeout:            durationWithDefault(cfg.TimeoutSeconds, 1800*time.Second),
		ResponseIdle:       durationMillisWithDefault(cfg.ResponseIdleMS, 1800*time.Millisecond),
		FirstChunkTimeout:  durationMillisWithDefault(cfg.FirstChunkTimeoutMS, 30000*time.Millisecond),
		SessionIdleTimeout: durationMillisWithDefault(cfg.SessionIdleMS, 900000*time.Millisecond),
		StartupWait:        durationMillisWithDefault(cfg.StartupWaitMS, 1200*time.Millisecond),
		OutputParser:       inferOutputParser(req.AgentName, mode),
	}
}

func runOneShot(ctx context.Context, req RunRequest, spec ExecSpec, onChunk func(string)) (RunResult, error) {
	if spec.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, spec.Timeout)
		defer cancel()
	}

	if spec.CaptureLastMessage && req.LastMessagePath == "" {
		tempFile, err := os.CreateTemp("", "ai-octo-relay-last-message-*.txt")
		if err != nil {
			return RunResult{}, fmt.Errorf("create last message temp file: %w", err)
		}
		req.LastMessagePath = tempFile.Name()
		_ = tempFile.Close()
		defer os.Remove(req.LastMessagePath)
		spec.Args = injectLastMessagePath(spec.Args, req.LastMessagePath)
	}
	// one-shot execution path

	cmd := exec.CommandContext(ctx, spec.Command, spec.Args...)
	cmd.Dir = req.ProjectPath
	cmd.Env = mergeEnv(os.Environ(), spec.Env, req.ExtraEnvVars)

	stdinUsed := !spec.PromptInArgs
	var stdin io.WriteCloser
	var err error
	if stdinUsed {
		stdin, err = cmd.StdinPipe()
		if err != nil {
			return RunResult{}, fmt.Errorf("create stdin pipe: %w", err)
		}
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return RunResult{}, fmt.Errorf("create stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return RunResult{}, fmt.Errorf("create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return RunResult{}, fmt.Errorf("start command: %w", err)
	}

	var (
		wg        sync.WaitGroup
		stdoutBuf bytes.Buffer
		stderrBuf bytes.Buffer
		writeMu   sync.Mutex
	)

	consume := func(reader io.Reader, target *bytes.Buffer, streamChunks bool) {
		defer wg.Done()
		scanner := bufio.NewScanner(reader)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			rawLine := scanner.Text()
			line := sanitizeOutput(rawLine)
			if strings.TrimSpace(line) == "" && strings.TrimSpace(rawLine) == "" {
				continue
			}
			writeMu.Lock()
			target.WriteString(rawLine)
			target.WriteByte('\n')
			writeMu.Unlock()
			if streamChunks && onChunk != nil && line != "" {
				onChunk(line)
			}
		}
	}

	wg.Add(2)
	go consume(stdout, &stdoutBuf, true)
	go consume(stderr, &stderrBuf, false)

	if stdinUsed {
		if _, err := io.WriteString(stdin, req.Prompt); err != nil {
			_ = stdin.Close()
			return RunResult{}, fmt.Errorf("write prompt: %w", err)
		}
		if !strings.HasSuffix(req.Prompt, "\n") {
			if _, err := io.WriteString(stdin, "\n"); err != nil {
				_ = stdin.Close()
				return RunResult{}, fmt.Errorf("terminate prompt: %w", err)
			}
		}
		_ = stdin.Close()
	}

	waitErr := cmd.Wait()
	wg.Wait()
	stdoutText := strings.TrimSpace(stdoutBuf.String())
	stderrText := strings.TrimSpace(stderrBuf.String())
	output := stdoutText
	nativeSessionID := req.NativeSessionID
	if parsedOutput, parsedSessionID, ok := parseStructuredOutput(spec.OutputParser, stdoutText); ok {
		if parsedOutput != "" {
			output = parsedOutput
		}
		if parsedSessionID != "" {
			nativeSessionID = parsedSessionID
		}
	}
	if nativeSessionID == req.NativeSessionID && stderrText != "" {
		if _, parsedSessionID, ok := parseStructuredOutput(spec.OutputParser, stderrText); ok && parsedSessionID != "" {
			nativeSessionID = parsedSessionID
		}
	}
	if spec.CaptureLastMessage && req.LastMessagePath != "" {
		if content, err := os.ReadFile(req.LastMessagePath); err == nil {
			lastMessage := strings.TrimSpace(string(content))
			if lastMessage != "" {
				output = lastMessage
			}
		}
	}
	if waitErr != nil {
		if output == "" {
			if stderrText != "" {
				output = stderrText
			}
			return RunResult{Output: output, NativeSessionID: nativeSessionID}, fmt.Errorf("command failed: %w", waitErr)
		}
		return RunResult{Output: output, NativeSessionID: nativeSessionID}, fmt.Errorf("command failed: %w", waitErr)
	}
	if output == "" {
		output = stderrText
	}
	return RunResult{Output: output, NativeSessionID: nativeSessionID}, nil
}

func containsPromptTemplate(args []string) bool {
	for _, arg := range args {
		if strings.Contains(arg, "{{prompt}}") {
			return true
		}
	}
	return false
}

func containsLastMessageTemplate(args []string) bool {
	for _, arg := range args {
		if strings.Contains(arg, "{{last_message_path}}") {
			return true
		}
	}
	return false
}

func injectLastMessagePath(args []string, path string) []string {
	out := make([]string, len(args))
	copy(out, args)
	for i := 0; i < len(out); i++ {
		if out[i] == "--output-last-message" && i+1 < len(out) && strings.TrimSpace(out[i+1]) == "" {
			out[i+1] = path
		}
	}
	return out
}

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

func sanitizeOutput(text string) string {
	text = ansiPattern.ReplaceAllString(text, "")
	text = strings.ReplaceAll(text, "\r", "")
	return strings.TrimSpace(text)
}

func expandSlice(values []string, req RunRequest) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, expandTemplate(value, req))
	}
	return out
}

func expandMap(values map[string]string, req RunRequest) map[string]string {
	if len(values) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = expandTemplate(value, req)
	}
	return out
}

func expandTemplate(input string, req RunRequest) string {
	replacer := strings.NewReplacer(
		"{{prompt}}", req.Prompt,
		"{{project_name}}", req.ProjectName,
		"{{project_path}}", req.ProjectPath,
		"{{channel_id}}", req.ChannelID,
		"{{thread_ts}}", req.ThreadTS,
		"{{slack_user_id}}", req.SlackUserID,
		"{{session_key}}", req.SessionKey,
		"{{native_session_id}}", req.NativeSessionID,
		"{{last_message_path}}", req.LastMessagePath,
	)
	return replacer.Replace(input)
}

func inferOutputParser(agentName, mode string) string {
	if mode != ModeOneShot {
		return ""
	}
	switch strings.ToLower(agentName) {
	case "codex":
		return "codex-jsonl"
	case "gemini":
		return "gemini-json"
	default:
		return ""
	}
}

func parseStructuredOutput(parser, stdoutText string) (string, string, bool) {
	switch parser {
	case "codex-jsonl":
		return parseCodexJSONL(stdoutText)
	case "gemini-json":
		return parseGeminiJSON(stdoutText)
	default:
		return "", "", false
	}
}

func parseCodexJSONL(stdoutText string) (string, string, bool) {
	var sessionID string
	for _, line := range strings.Split(stdoutText, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, "{") {
			continue
		}
		var payload any
		if err := json.Unmarshal([]byte(line), &payload); err != nil {
			continue
		}
		if sessionID == "" {
			sessionID = findStringValue(payload, "session_id", "sessionId", "session", "conversation_id")
		}
	}
	return "", sessionID, sessionID != ""
}

func parseGeminiJSON(stdoutText string) (string, string, bool) {
	var payload struct {
		SessionID string `json:"session_id"`
		Response  string `json:"response"`
	}
	if err := json.Unmarshal([]byte(stdoutText), &payload); err != nil {
		return "", "", false
	}
	return strings.TrimSpace(payload.Response), strings.TrimSpace(payload.SessionID), true
}

func findStringValue(value any, keys ...string) string {
	switch typed := value.(type) {
	case map[string]any:
		for _, key := range keys {
			if v, ok := typed[key]; ok {
				if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
					return strings.TrimSpace(s)
				}
			}
		}
		for _, v := range typed {
			if result := findStringValue(v, keys...); result != "" {
				return result
			}
		}
	case []any:
		for _, item := range typed {
			if result := findStringValue(item, keys...); result != "" {
				return result
			}
		}
	}
	return ""
}

func mergeEnv(base []string, groups ...map[string]string) []string {
	envMap := map[string]string{}
	for _, item := range base {
		parts := strings.SplitN(item, "=", 2)
		if len(parts) == 2 {
			envMap[parts[0]] = parts[1]
		}
	}
	for _, group := range groups {
		for key, value := range group {
			envMap[key] = value
		}
	}
	out := make([]string, 0, len(envMap))
	for key, value := range envMap {
		out = append(out, key+"="+value)
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func defaultArgs(current []string, fallback ...string) []string {
	if len(current) > 0 {
		return current
	}
	return fallback
}

func durationWithDefault(seconds int, fallback time.Duration) time.Duration {
	if seconds <= 0 {
		return fallback
	}
	return time.Duration(seconds) * time.Second
}

func durationMillisWithDefault(ms int, fallback time.Duration) time.Duration {
	if ms <= 0 {
		return fallback
	}
	return time.Duration(ms) * time.Millisecond
}

type SessionManager struct {
	registry *Registry
	mu       sync.Mutex
	items    map[string]*PersistentSession
}

func NewSessionManager(registry *Registry) *SessionManager {
	return &SessionManager{
		registry: registry,
		items:    map[string]*PersistentSession{},
	}
}

func (m *SessionManager) Send(ctx context.Context, req RunRequest, spec ExecSpec, onChunk func(string)) (string, error) {
	session, err := m.getOrCreate(ctx, req, spec)
	if err != nil {
		return "", err
	}
	m.registry.logger.Infof("persistent session send: key=%s agent=%s active=%t", req.SessionKey, req.AgentName, session.Info().Active)
	return session.Send(ctx, req.Prompt, onChunk)
}

func (m *SessionManager) Restart(ctx context.Context, req RunRequest, spec ExecSpec) (SessionInfo, error) {
	_ = m.Close(req.SessionKey)
	session, err := m.getOrCreate(ctx, req, spec)
	if err != nil {
		return SessionInfo{}, err
	}
	return session.Info(), nil
}

func (m *SessionManager) Status(sessionKey string) (SessionInfo, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	session, ok := m.items[sessionKey]
	if !ok {
		return SessionInfo{}, false
	}
	if session.IsExpired() {
		go m.Close(sessionKey)
		return SessionInfo{}, false
	}
	return session.Info(), true
}

func (m *SessionManager) Close(sessionKey string) error {
	m.mu.Lock()
	session, ok := m.items[sessionKey]
	if ok {
		delete(m.items, sessionKey)
	}
	m.mu.Unlock()
	if !ok {
		return nil
	}
	m.registry.logger.Debugf("persistent session close: key=%s", sessionKey)
	return session.Close()
}

func (m *SessionManager) getOrCreate(ctx context.Context, req RunRequest, spec ExecSpec) (*PersistentSession, error) {
	m.mu.Lock()
	session, ok := m.items[req.SessionKey]
	if ok && session.IsExpired() {
		delete(m.items, req.SessionKey)
		go session.Close()
		ok = false
	}
	if ok {
		m.registry.logger.Infof("persistent session reuse: key=%s", req.SessionKey)
		m.mu.Unlock()
		return session, nil
	}

	session = NewPersistentSession(req, spec)
	m.items[req.SessionKey] = session
	m.mu.Unlock()

	if err := session.Start(ctx); err != nil {
		m.mu.Lock()
		delete(m.items, req.SessionKey)
		m.mu.Unlock()
		return nil, err
	}
	m.registry.logger.Infof("persistent session started: key=%s agent=%s project=%s transport=%s", req.SessionKey, req.AgentName, req.ProjectPath, spec.Transport)
	return session, nil
}

type PersistentSession struct {
	req        RunRequest
	spec       ExecSpec
	startedAt  time.Time
	lastUsedAt time.Time
	processMu  sync.Mutex
	active     *responseCollector
	cmd        *exec.Cmd
	ptyFile    *os.File
	stdin      io.WriteCloser
	stdout     io.ReadCloser
	stderr     io.ReadCloser
	done       chan error
	closed     bool
}

type responseCollector struct {
	lines chan string
	errs  chan error
}

func NewPersistentSession(req RunRequest, spec ExecSpec) *PersistentSession {
	now := time.Now()
	return &PersistentSession{
		req:        req,
		spec:       spec,
		startedAt:  now,
		lastUsedAt: now,
		done:       make(chan error, 1),
	}
}

func (s *PersistentSession) Start(ctx context.Context) error {
	s.processMu.Lock()
	defer s.processMu.Unlock()

	if s.cmd != nil {
		return nil
	}

	cmd := exec.CommandContext(context.Background(), s.spec.Command, s.spec.Args...)
	cmd.Dir = s.req.ProjectPath
	cmd.Env = mergeEnv(os.Environ(), s.spec.Env, s.req.ExtraEnvVars)

	if s.spec.Transport == TransportPTY {
		ptyFile, err := pty.Start(cmd)
		if err != nil {
			return fmt.Errorf("start pty command: %w", err)
		}
		s.cmd = cmd
		s.ptyFile = ptyFile
		s.stdin = ptyFile
		go s.readLoop(ptyFile)
	} else {
		stdin, err := cmd.StdinPipe()
		if err != nil {
			return fmt.Errorf("create stdin pipe: %w", err)
		}
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return fmt.Errorf("create stdout pipe: %w", err)
		}
		stderr, err := cmd.StderrPipe()
		if err != nil {
			return fmt.Errorf("create stderr pipe: %w", err)
		}
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("start command: %w", err)
		}
		s.cmd = cmd
		s.stdin = stdin
		s.stdout = stdout
		s.stderr = stderr
		go s.readLoop(stdout)
		go s.readLoop(stderr)
	}

	go func() {
		s.done <- s.cmd.Wait()
	}()

	if s.spec.StartupWait > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(s.spec.StartupWait):
		}
	}

	return nil
}

func (s *PersistentSession) readLoop(reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := sanitizeOutput(scanner.Text())
		if line == "" {
			continue
		}
		s.processMu.Lock()
		collector := s.active
		s.processMu.Unlock()
		if collector != nil {
			select {
			case collector.lines <- line:
			default:
			}
		}
	}
	if err := scanner.Err(); err != nil && !strings.Contains(err.Error(), "file already closed") {
		s.processMu.Lock()
		collector := s.active
		s.processMu.Unlock()
		if collector != nil {
			select {
			case collector.errs <- err:
			default:
			}
		}
	}
}

func (s *PersistentSession) Send(ctx context.Context, prompt string, onChunk func(string)) (string, error) {
	s.processMu.Lock()
	if s.closed || s.cmd == nil || s.stdin == nil {
		s.processMu.Unlock()
		return "", fmt.Errorf("session is not running")
	}
	if s.active != nil {
		s.processMu.Unlock()
		return "", fmt.Errorf("session is busy")
	}
	collector := &responseCollector{
		lines: make(chan string, 256),
		errs:  make(chan error, 8),
	}
	s.active = collector
	s.lastUsedAt = time.Now()
	s.processMu.Unlock()

	defer func() {
		s.processMu.Lock()
		if s.active == collector {
			s.active = nil
		}
		s.lastUsedAt = time.Now()
		s.processMu.Unlock()
	}()

	if _, err := io.WriteString(s.stdin, prompt+s.spec.PromptSuffix); err != nil {
		return "", fmt.Errorf("write prompt to session: %w", err)
	}

	var (
		builder        strings.Builder
		gotFirstChunk  bool
		firstChunkWait = time.NewTimer(s.spec.FirstChunkTimeout)
	)
	defer firstChunkWait.Stop()

	var idleTimer *time.Timer
	resetIdle := func() {
		if idleTimer == nil {
			idleTimer = time.NewTimer(s.spec.ResponseIdle)
			return
		}
		if !idleTimer.Stop() {
			select {
			case <-idleTimer.C:
			default:
			}
		}
		idleTimer.Reset(s.spec.ResponseIdle)
	}

	for {
		select {
		case <-ctx.Done():
			return strings.TrimSpace(builder.String()), ctx.Err()
		case err := <-collector.errs:
			if builder.Len() == 0 {
				return "", err
			}
			return strings.TrimSpace(builder.String()), err
		case err := <-s.done:
			if builder.Len() == 0 && err != nil {
				return "", fmt.Errorf("session exited: %w", err)
			}
			if err != nil {
				return strings.TrimSpace(builder.String()), fmt.Errorf("session exited: %w", err)
			}
			return strings.TrimSpace(builder.String()), nil
		case line := <-collector.lines:
			if !gotFirstChunk {
				gotFirstChunk = true
			}
			builder.WriteString(line)
			builder.WriteByte('\n')
			if onChunk != nil {
				onChunk(line)
			}
			resetIdle()
		case <-firstChunkWait.C:
			if !gotFirstChunk {
				return "", fmt.Errorf("session first response timeout after %s", s.spec.FirstChunkTimeout)
			}
		case <-idleChan(idleTimer):
			return strings.TrimSpace(builder.String()), nil
		}
	}
}

func idleChan(timer *time.Timer) <-chan time.Time {
	if timer == nil {
		return nil
	}
	return timer.C
}

func (s *PersistentSession) Info() SessionInfo {
	s.processMu.Lock()
	defer s.processMu.Unlock()
	return SessionInfo{
		Key:          s.req.SessionKey,
		AgentName:    s.req.AgentName,
		Mode:         s.spec.Mode,
		StartedAt:    s.startedAt,
		LastUsedAt:   s.lastUsedAt,
		Active:       s.active != nil,
		ProjectPath:  s.req.ProjectPath,
		Interactive:  s.spec.Transport == TransportPTY,
		ResponseIdle: s.spec.ResponseIdle,
	}
}

func (s *PersistentSession) IsExpired() bool {
	s.processMu.Lock()
	defer s.processMu.Unlock()
	if s.spec.SessionIdleTimeout <= 0 || s.active != nil {
		return false
	}
	return time.Since(s.lastUsedAt) > s.spec.SessionIdleTimeout
}

func (s *PersistentSession) Close() error {
	s.processMu.Lock()
	defer s.processMu.Unlock()
	s.closed = true
	var closeErr error
	if s.stdin != nil {
		_ = s.stdin.Close()
	}
	if s.ptyFile != nil {
		_ = s.ptyFile.Close()
	}
	if s.cmd != nil && s.cmd.Process != nil {
		if err := s.cmd.Process.Kill(); err != nil && !strings.Contains(err.Error(), "finished") {
			closeErr = err
		}
	}
	s.cmd = nil
	s.stdin = nil
	s.stdout = nil
	s.stderr = nil
	return closeErr
}
