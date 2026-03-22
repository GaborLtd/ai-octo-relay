package terminal

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/slack-go/slack"

	"github.com/match/ai-octo-relay/internal/config"
)

const (
	maxLogLines           = 400
	defaultFollowTail     = 40
	defaultSnapshotTail   = 30
	defaultScannerBufSize = 1024 * 1024
)

var tryCloudflareURLPattern = regexp.MustCompile(`https://[A-Za-z0-9.-]+\.trycloudflare\.com`)

type SlackClient interface {
	PostMessage(channelID string, options ...slack.MsgOption) (string, string, error)
	UpdateMessage(channelID, timestamp string, options ...slack.MsgOption) (string, string, string, error)
}

type Event struct {
	UserID      string
	ChannelID   string
	ChannelType string
	ThreadTS    string
	Text        string
	ProjectDir  string
}

type Executor struct {
	cfg          config.TerminalConfig
	serversByID  map[string]*serverProcess
	serverByName map[string]config.TerminalServerConfig
	follows      map[string]*logFollower
	mu           sync.Mutex
}

type serverProcess struct {
	id          string
	name        string
	projectDir  string
	cfg         config.TerminalServerConfig
	cmd         *exec.Cmd
	tunnelCmd   *exec.Cmd
	startedAt   time.Time
	readyAt     time.Time
	exitedAt    time.Time
	exitCode    *int
	forward     bool
	forwardURL  string
	startMsgTS  string
	channelID   string
	threadTS    string
	readySent   bool
	done        chan struct{}
	timeoutKill *time.Timer

	logMu sync.Mutex
	logs  []string
}

type logFollower struct {
	key       string
	serverID  string
	channelID string
	threadTS  string
	messageTS string
	stop      chan struct{}
	done      chan struct{}
	stopOnce  sync.Once
}

func New(cfg config.TerminalConfig) *Executor {
	index := make(map[string]config.TerminalServerConfig, len(cfg.Servers))
	for _, server := range cfg.Servers {
		index[server.Name] = server
	}
	return &Executor{
		cfg:          cfg,
		serversByID:  map[string]*serverProcess{},
		serverByName: index,
		follows:      map[string]*logFollower{},
	}
}

func (e *Executor) StartJanitor() {
	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			e.cleanupExited()
		}
	}()
}

func (e *Executor) Handle(ev Event, client SlackClient) error {
	fields := strings.Fields(strings.TrimSpace(ev.Text))
	if len(fields) < 2 || fields[0] != "!server" {
		return nil
	}

	switch fields[1] {
	case "start":
		return e.handleStart(ev, client, fields[2:])
	case "list":
		return e.handleList(ev, client)
	case "stop":
		return e.handleStop(ev, client, fields[2:])
	case "logs":
		return e.handleLogs(ev, client, fields[2:])
	default:
		return e.reply(client, ev.ChannelID, ev.ThreadTS, "usage: !server start <name> [--forward] | !server list | !server stop <id> | !server logs <id> [--follow|--stop]")
	}
}

func (e *Executor) handleStart(ev Event, client SlackClient, args []string) error {
	if len(args) == 0 {
		return e.reply(client, ev.ChannelID, ev.ThreadTS, "usage: !server start <name> [--forward]")
	}
	name := strings.TrimSpace(args[0])
	cfg, ok := e.serverByName[name]
	if !ok {
		return e.reply(client, ev.ChannelID, ev.ThreadTS, fmt.Sprintf("server %q not found", name))
	}

	forward := false
	for _, arg := range args[1:] {
		switch arg {
		case "--forward":
			forward = true
		default:
			return e.reply(client, ev.ChannelID, ev.ThreadTS, fmt.Sprintf("unknown option: %s", arg))
		}
	}

	e.mu.Lock()
	for _, existing := range e.serversByID {
		if existing.name == name && existing.projectDir == ev.ProjectDir && !existing.isDone() {
			e.mu.Unlock()
			return e.reply(client, ev.ChannelID, ev.ThreadTS, fmt.Sprintf("server `%s` is already running as `%s`", name, existing.id))
		}
	}
	e.mu.Unlock()

	cmd := exec.Command(cfg.Command, cfg.Args...)
	cmd.Dir = ev.ProjectDir
	cmd.Env = mergeEnv(cfg.Env)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return e.reply(client, ev.ChannelID, ev.ThreadTS, fmt.Sprintf("stdout pipe failed: %v", err))
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return e.reply(client, ev.ChannelID, ev.ThreadTS, fmt.Sprintf("stderr pipe failed: %v", err))
	}
	if err := cmd.Start(); err != nil {
		return e.reply(client, ev.ChannelID, ev.ThreadTS, fmt.Sprintf("start failed: %v", err))
	}

	serverID := newServerID(name)
	_, startMsgTS, err := client.PostMessage(
		ev.ChannelID,
		slack.MsgOptionText(fmt.Sprintf("_啟動中…_ `%s` · `%s`", serverID, name), false),
		slack.MsgOptionTS(ev.ThreadTS),
	)
	if err != nil {
		_ = cmd.Process.Kill()
		return fmt.Errorf("post server start message: %w", err)
	}

	srv := &serverProcess{
		id:         serverID,
		name:       name,
		projectDir: ev.ProjectDir,
		cfg:        cfg,
		cmd:        cmd,
		startedAt:  time.Now(),
		forward:    forward,
		startMsgTS: startMsgTS,
		channelID:  ev.ChannelID,
		threadTS:   ev.ThreadTS,
		done:       make(chan struct{}),
	}

	if _, _, _, err := client.UpdateMessage(
		ev.ChannelID,
		startMsgTS,
		slack.MsgOptionText(fmt.Sprintf("_啟動中…_ `%s` · `%s`", srv.id, name), false),
	); err != nil {
		_ = cmd.Process.Kill()
		return fmt.Errorf("update server start message: %w", err)
	}

	e.mu.Lock()
	e.serversByID[srv.id] = srv
	e.mu.Unlock()

	if cfg.Oneshot && cfg.TimeoutSec > 0 {
		srv.timeoutKill = time.AfterFunc(time.Duration(cfg.TimeoutSec)*time.Second, func() {
			if srv.cmd.Process != nil {
				_ = srv.cmd.Process.Kill()
			}
		})
	}

	go e.scanOutput(srv, stdout, false, client)
	go e.scanOutput(srv, stderr, true, client)
	if forward && cfg.Port > 0 {
		go e.startTunnel(srv, client)
	}
	go e.waitServer(srv, client)
	return nil
}

func (e *Executor) handleList(ev Event, client SlackClient) error {
	e.mu.Lock()
	servers := make([]*serverProcess, 0, len(e.serversByID))
	for _, srv := range e.serversByID {
		if srv.isDone() {
			continue
		}
		servers = append(servers, srv)
	}
	e.mu.Unlock()

	if len(servers) == 0 {
		return e.reply(client, ev.ChannelID, ev.ThreadTS, "no active servers")
	}

	slices.SortFunc(servers, func(a, b *serverProcess) int {
		return strings.Compare(a.id, b.id)
	})

	lines := make([]string, 0, len(servers))
	for _, srv := range servers {
		line := fmt.Sprintf(":large_green_circle: %s · %s · %s", srv.id, srv.name, formatDuration(time.Since(srv.startedAt)))
		if srv.forwardURL != "" {
			line += fmt.Sprintf(" · :link: %s", slackLink(srv.forwardURL))
		}
		lines = append(lines, line)
	}
	return e.reply(client, ev.ChannelID, ev.ThreadTS, strings.Join(lines, "\n"))
}

func (e *Executor) handleStop(ev Event, client SlackClient, args []string) error {
	if len(args) == 0 {
		return e.reply(client, ev.ChannelID, ev.ThreadTS, "usage: !server stop <id>")
	}
	id := strings.TrimSpace(args[0])

	srv := e.getServer(id)
	if srv == nil {
		return e.reply(client, ev.ChannelID, ev.ThreadTS, fmt.Sprintf("server %q not found", id))
	}
	if srv.isDone() {
		return e.reply(client, ev.ChannelID, ev.ThreadTS, fmt.Sprintf("server `%s` is already stopped", id))
	}
	if srv.cmd.Process == nil {
		return e.reply(client, ev.ChannelID, ev.ThreadTS, fmt.Sprintf("server `%s` has no active process", id))
	}
	if err := srv.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return e.reply(client, ev.ChannelID, ev.ThreadTS, fmt.Sprintf("stop failed: %v", err))
	}
	return e.reply(client, ev.ChannelID, ev.ThreadTS, fmt.Sprintf("stopping `%s`", id))
}

func (e *Executor) handleLogs(ev Event, client SlackClient, args []string) error {
	if len(args) == 0 {
		return e.reply(client, ev.ChannelID, ev.ThreadTS, "usage: !server logs <id> [--follow|--stop]")
	}
	id := strings.TrimSpace(args[0])
	srv := e.getServer(id)
	if srv == nil {
		return e.reply(client, ev.ChannelID, ev.ThreadTS, fmt.Sprintf("server %q not found", id))
	}

	follow := false
	stop := false
	for _, arg := range args[1:] {
		switch arg {
		case "--follow":
			follow = true
		case "--stop":
			stop = true
		default:
			return e.reply(client, ev.ChannelID, ev.ThreadTS, fmt.Sprintf("unknown option: %s", arg))
		}
	}
	if follow && stop {
		return e.reply(client, ev.ChannelID, ev.ThreadTS, "choose either --follow or --stop")
	}

	if stop {
		count := e.stopFollowersForScope(id, ev.ChannelID, ev.ThreadTS, "log follow stopped")
		if count == 0 {
			return e.reply(client, ev.ChannelID, ev.ThreadTS, fmt.Sprintf("no active log follow for `%s`", id))
		}
		return e.reply(client, ev.ChannelID, ev.ThreadTS, fmt.Sprintf("stopped log follow for `%s`", id))
	}

	if !follow {
		return e.reply(client, ev.ChannelID, ev.ThreadTS, e.renderLogSnapshot(srv, defaultSnapshotTail))
	}

	key := followKey(id, ev.ChannelID, ev.ThreadTS)
	e.mu.Lock()
	if _, exists := e.follows[key]; exists {
		e.mu.Unlock()
		return e.reply(client, ev.ChannelID, ev.ThreadTS, fmt.Sprintf("log follow is already active for `%s`", id))
	}
	e.mu.Unlock()

	_, messageTS, err := client.PostMessage(
		ev.ChannelID,
		slack.MsgOptionText(e.renderLogFollow(srv), false),
		slack.MsgOptionTS(ev.ThreadTS),
	)
	if err != nil {
		return fmt.Errorf("post log follow message: %w", err)
	}

	follower := &logFollower{
		key:       key,
		serverID:  id,
		channelID: ev.ChannelID,
		threadTS:  ev.ThreadTS,
		messageTS: messageTS,
		stop:      make(chan struct{}),
		done:      make(chan struct{}),
	}

	e.mu.Lock()
	e.follows[key] = follower
	e.mu.Unlock()

	go e.runLogFollow(srv, follower, client)
	return nil
}

func (e *Executor) startTunnel(srv *serverProcess, client SlackClient) {
	tunnel := exec.Command(e.cfg.CloudflaredCommand, "tunnel", "--url", fmt.Sprintf("http://localhost:%d", srv.cfg.Port))
	tunnel.Dir = srv.projectDir
	tunnel.Env = os.Environ()
	stderr, err := tunnel.StderrPipe()
	if err != nil {
		srv.appendLog("[tunnel] stderr pipe failed: " + err.Error())
		return
	}
	if err := tunnel.Start(); err != nil {
		srv.appendLog("[tunnel] start failed: " + err.Error())
		return
	}

	e.mu.Lock()
	srv.tunnelCmd = tunnel
	e.mu.Unlock()

	go func() {
		scanner := newScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			srv.appendLog("[tunnel] " + line)
			if srv.forwardURL == "" {
				if url := tryCloudflareURLPattern.FindString(line); url != "" {
					e.mu.Lock()
					srv.forwardURL = url
					e.mu.Unlock()
					e.updateStatusMessage(srv, client)
				}
			}
		}
	}()
}

func (e *Executor) scanOutput(srv *serverProcess, reader interface{ Read([]byte) (int, error) }, isStderr bool, client SlackClient) {
	scanner := newScanner(reader)
	for scanner.Scan() {
		line := scanner.Text()
		if isStderr {
			srv.appendLog("[stderr] " + line)
		} else {
			srv.appendLog(line)
			if srv.cfg.ReadyPattern != "" && !srv.readySent {
				matched, err := regexp.MatchString(srv.cfg.ReadyPattern, line)
				if err == nil && matched {
					e.mu.Lock()
					srv.readySent = true
					srv.readyAt = time.Now()
					e.mu.Unlock()
					e.updateStatusMessage(srv, client)
				}
			}
		}
	}
}

func (e *Executor) waitServer(srv *serverProcess, client SlackClient) {
	err := srv.cmd.Wait()
	if srv.timeoutKill != nil {
		srv.timeoutKill.Stop()
	}

	code := 0
	if err != nil {
		code = exitCode(err)
	}

	e.mu.Lock()
	now := time.Now()
	srv.exitedAt = now
	srv.exitCode = &code
	e.mu.Unlock()

	close(srv.done)
	e.killTunnel(srv)
	e.updateExitMessage(srv, client)
}

func (e *Executor) updateStatusMessage(srv *serverProcess, client SlackClient) {
	text := fmt.Sprintf("已啟動 · `%s`", srv.id)
	if srv.cfg.Port > 0 {
		text += fmt.Sprintf(" · port %d", srv.cfg.Port)
	}
	if srv.forwardURL != "" {
		text += " · " + slackLink(srv.forwardURL)
	}
	_, _, _, _ = client.UpdateMessage(srv.channelID, srv.startMsgTS, slack.MsgOptionText(text, false))
}

func (e *Executor) updateExitMessage(srv *serverProcess, client SlackClient) {
	text := fmt.Sprintf("已結束 · `%s` · exit %d", srv.id, valueOrZero(srv.exitCode))
	if srv.forwardURL != "" {
		text += " · " + slackLink(srv.forwardURL)
	}
	_, _, _, _ = client.UpdateMessage(srv.channelID, srv.startMsgTS, slack.MsgOptionText(text, false))
}

func (e *Executor) runLogFollow(srv *serverProcess, follower *logFollower, client SlackClient) {
	defer close(follower.done)
	ticker := time.NewTicker(time.Duration(e.cfg.FollowIntervalSec) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-follower.stop:
			e.deleteFollower(follower.key)
			return
		case <-srv.done:
			_, _, _, _ = client.UpdateMessage(follower.channelID, follower.messageTS, slack.MsgOptionText(e.renderLogSnapshot(srv, defaultFollowTail), false))
			e.deleteFollower(follower.key)
			return
		case <-ticker.C:
			_, _, _, _ = client.UpdateMessage(follower.channelID, follower.messageTS, slack.MsgOptionText(e.renderLogFollow(srv), false))
		}
	}
}

func (e *Executor) cleanupExited() {
	e.mu.Lock()
	defer e.mu.Unlock()
	cutoff := time.Now().Add(-time.Duration(e.cfg.CleanupAfterSec) * time.Second)
	for id, srv := range e.serversByID {
		if srv.exitedAt.IsZero() || srv.exitedAt.After(cutoff) {
			continue
		}
		delete(e.serversByID, id)
	}
}

func (e *Executor) killTunnel(srv *serverProcess) {
	e.mu.Lock()
	tunnel := srv.tunnelCmd
	e.mu.Unlock()
	if tunnel != nil && tunnel.Process != nil {
		_ = tunnel.Process.Kill()
		_, _ = tunnel.Process.Wait()
	}
}

func (e *Executor) getServer(id string) *serverProcess {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.serversByID[id]
}

func (e *Executor) stopFollowersForScope(serverID, channelID, threadTS, _ string) int {
	e.mu.Lock()
	var followers []*logFollower
	for _, follower := range e.follows {
		if follower.serverID == serverID && follower.channelID == channelID && follower.threadTS == threadTS {
			followers = append(followers, follower)
		}
	}
	e.mu.Unlock()

	for _, follower := range followers {
		follower.requestStop()
	}
	return len(followers)
}

func (e *Executor) deleteFollower(key string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.follows, key)
}

func (e *Executor) renderLogFollow(srv *serverProcess) string {
	return fmt.Sprintf("logs · `%s`\n```%s```", srv.id, truncateLogBlock(srv.tail(defaultFollowTail)))
}

func (e *Executor) renderLogSnapshot(srv *serverProcess, tail int) string {
	status := "running"
	if srv.isDone() {
		status = fmt.Sprintf("exit %d", valueOrZero(srv.exitCode))
	}
	return fmt.Sprintf("logs · `%s` · %s\n```%s```", srv.id, status, truncateLogBlock(srv.tail(tail)))
}

func (e *Executor) reply(client SlackClient, channelID, threadTS, text string) error {
	_, _, err := client.PostMessage(
		channelID,
		slack.MsgOptionText(text, false),
		slack.MsgOptionTS(threadTS),
	)
	return err
}

func (s *serverProcess) appendLog(line string) {
	s.logMu.Lock()
	defer s.logMu.Unlock()
	s.logs = append(s.logs, line)
	if len(s.logs) > maxLogLines {
		s.logs = append([]string(nil), s.logs[len(s.logs)-maxLogLines:]...)
	}
}

func (s *serverProcess) tail(n int) []string {
	s.logMu.Lock()
	defer s.logMu.Unlock()
	if len(s.logs) == 0 {
		return []string{"(no logs yet)"}
	}
	if n > len(s.logs) {
		n = len(s.logs)
	}
	return append([]string(nil), s.logs[len(s.logs)-n:]...)
}

func (s *serverProcess) isDone() bool {
	select {
	case <-s.done:
		return true
	default:
		return false
	}
}

func (f *logFollower) requestStop() {
	f.stopOnce.Do(func() {
		close(f.stop)
	})
}

func newScanner(reader interface{ Read([]byte) (int, error) }) *bufio.Scanner {
	scanner := bufio.NewScanner(reader)
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, defaultScannerBufSize)
	return scanner
}

func mergeEnv(extra map[string]string) []string {
	env := append([]string(nil), os.Environ()...)
	for key, value := range extra {
		env = append(env, key+"="+value)
	}
	return env
}

func newServerID(name string) string {
	return fmt.Sprintf("%s-%04d", name, time.Now().UnixNano()%10000)
}

func followKey(serverID, channelID, threadTS string) string {
	return strings.Join([]string{serverID, channelID, threadTS}, ":")
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	minutes := int(d / time.Minute)
	seconds := int((d % time.Minute) / time.Second)
	return fmt.Sprintf("%dm%02ds", minutes, seconds)
}

func truncateLogBlock(lines []string) string {
	text := strings.Join(lines, "\n")
	if len(text) <= 2800 {
		return text
	}
	return text[len(text)-2800:]
}

func slackLink(url string) string {
	return fmt.Sprintf("<%s|%s>", url, url)
}

func exitCode(err error) int {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if waitStatus, ok := exitErr.Sys().(syscall.WaitStatus); ok {
			return waitStatus.ExitStatus()
		}
	}
	return 1
}

func valueOrZero(v *int) int {
	if v == nil {
		return 0
	}
	return *v
}
