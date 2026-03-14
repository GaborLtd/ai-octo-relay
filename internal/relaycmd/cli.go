package relaycmd

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/match/ai-octo-relay/internal/agent"
	"github.com/match/ai-octo-relay/internal/app"
	"github.com/match/ai-octo-relay/internal/config"
	"github.com/match/ai-octo-relay/internal/logx"
	"github.com/match/ai-octo-relay/internal/project"
	"github.com/match/ai-octo-relay/internal/slackbot"
	"github.com/match/ai-octo-relay/internal/store"
)

type cliOptions struct {
	configPath      string
	daemonStateFile string
}

type daemonFiles struct {
	statePath string
	logPath   string
}

type daemonState struct {
	PID        int    `json:"pid"`
	ConfigPath string `json:"config_path"`
	LogPath    string `json:"log_path"`
	StartedAt  string `json:"started_at"`
}

func Run(args []string, stdout, stderr io.Writer) int {
	command, options, err := parseCLI(args, stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}

	resolvedConfigPath, err := config.ResolvePath(options.configPath)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}

	switch command {
	case "start":
		if err := startDaemon(resolvedConfigPath, stdout); err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 1
		}
		return 0
	case "stop":
		if err := stopDaemon(resolvedConfigPath, stdout); err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 1
		}
		return 0
	case "restart":
		if err := restartDaemon(resolvedConfigPath, stdout); err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 1
		}
		return 0
	case "serve":
		if err := runServe(resolvedConfigPath, options.daemonStateFile); err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 1
		}
		return 0
	default:
		panic(fmt.Sprintf("unexpected command: %s", command))
	}
}

func parseCLI(args []string, stderr io.Writer) (string, cliOptions, error) {
	if len(args) == 0 {
		printCLIUsage(stderr)
		return "", cliOptions{}, flag.ErrHelp
	}

	command := ""
	flagArgs := args
	switch args[0] {
	case "serve", "start", "stop", "restart":
		command = args[0]
		flagArgs = args[1:]
	case "help", "-h", "--help":
		printCLIUsage(stderr)
		return "", cliOptions{}, flag.ErrHelp
	default:
		printCLIUsage(stderr)
		return "", cliOptions{}, fmt.Errorf("unknown command: %s", args[0])
	}

	fs := flag.NewFlagSet("ai-octo-relay", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		printCLIUsage(stderr)
	}

	var options cliOptions
	fs.StringVar(&options.configPath, "config", "", "path to config file")
	fs.StringVar(&options.configPath, "c", "", "path to config file")
	fs.StringVar(&options.daemonStateFile, "daemon-state-file", "", "internal use only")

	if err := fs.Parse(flagArgs); err != nil {
		return "", cliOptions{}, err
	}
	if fs.NArg() > 0 {
		printCLIUsage(stderr)
		return "", cliOptions{}, fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	return command, options, nil
}

func printCLIUsage(w io.Writer) {
	fmt.Fprint(w, `Usage:
  ai-octo-relay [serve|start|stop|restart] [options]

Commands:
  serve    前景執行 Slack bot
  start    背景啟動 daemon
  stop     停止背景 daemon
  restart  重新啟動背景 daemon
  help     顯示這份說明

Options:
  -c, -config <path>          指定 config.json 路徑
  -daemon-state-file <path>   內部使用，通常不需手動指定
  -h, --help                  顯示說明

Examples:
  ai-octo-relay serve
  ai-octo-relay start
  ai-octo-relay restart -c ./config.json
  ai-octo-relay stop -c ./config.json

Compatibility:
  go run ./cmd/ai-octo-relay serve
  go run ./cmd/bot serve

Config search order:
  ./config.json
  ~/.config/ai-octo-relay/config.json
  ~/.ai-octo-relay/config.json
`)
}

func runServe(configPath string, daemonStateFile string) error {
	if daemonStateFile != "" {
		defer cleanupDaemonStateFile(daemonStateFile, os.Getpid())
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	logger := logx.New(cfg.LogLevel)
	logger.Infof("starting ai-octo-relay with config=%s log_level=%s", configPath, cfg.LogLevel)

	stateStore, err := store.OpenStateStore(cfg.StateStore)
	if err != nil {
		return fmt.Errorf("create state store: %w", err)
	}

	eventStore, err := store.OpenEventStore(cfg.EventStore)
	if err != nil {
		return fmt.Errorf("create event store: %w", err)
	}

	registry, err := project.NewRegistry(cfg.Projects)
	if err != nil {
		return fmt.Errorf("create project registry: %w", err)
	}

	runners, err := agent.NewRegistry(cfg.Agents, logger)
	if err != nil {
		return fmt.Errorf("create agent registry: %w", err)
	}
	if err := runners.ValidateCommands(); err != nil {
		return fmt.Errorf("validate agent commands: %w", err)
	}

	service := app.NewService(cfg, registry, runners, stateStore, eventStore)
	bot, err := slackbot.New(cfg, service, logger)
	if err != nil {
		return fmt.Errorf("create slack bot: %w", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := bot.Run(ctx); err != nil && ctx.Err() == nil {
		return fmt.Errorf("run slack bot: %w", err)
	}
	return nil
}

func startDaemon(configPath string, stdout io.Writer) error {
	files := daemonFilePaths(configPath)
	state, err := readDaemonState(files.statePath)
	if err == nil {
		running, runErr := isProcessRunning(state.PID)
		if runErr != nil {
			return runErr
		}
		if running {
			return fmt.Errorf("daemon already running: pid=%d state=%s", state.PID, files.statePath)
		}
		if removeErr := os.Remove(files.statePath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return fmt.Errorf("remove stale state file: %w", removeErr)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(files.statePath), 0o755); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	logFile, err := os.OpenFile(files.logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}
	defer logFile.Close()

	devNull, err := os.OpenFile(os.DevNull, os.O_RDONLY, 0)
	if err != nil {
		return fmt.Errorf("open devnull: %w", err)
	}
	defer devNull.Close()

	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}

	cmd := exec.Command(executable, "serve", "-config", configPath, "-daemon-state-file", files.statePath)
	cmd.Stdin = devNull
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start daemon: %w", err)
	}

	state = daemonState{
		PID:        cmd.Process.Pid,
		ConfigPath: configPath,
		LogPath:    files.logPath,
		StartedAt:  time.Now().Format(time.RFC3339),
	}
	if err := writeDaemonState(files.statePath, state); err != nil {
		_ = cmd.Process.Kill()
		return err
	}
	_ = cmd.Process.Release()

	fmt.Fprintf(stdout, "started ai-octo-relay\npid: %d\nconfig: %s\nstate: %s\nlog: %s\n", state.PID, state.ConfigPath, files.statePath, files.logPath)
	return nil
}

func stopDaemon(configPath string, stdout io.Writer) error {
	files := daemonFilePaths(configPath)
	state, err := readDaemonState(files.statePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("daemon not running: state file %s not found", files.statePath)
		}
		return err
	}

	running, err := isProcessRunning(state.PID)
	if err != nil {
		return err
	}
	if !running {
		if removeErr := os.Remove(files.statePath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return fmt.Errorf("remove stale state file: %w", removeErr)
		}
		fmt.Fprintf(stdout, "removed stale daemon state\nstate: %s\n", files.statePath)
		return nil
	}

	if err := syscall.Kill(state.PID, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("stop daemon pid=%d: %w", state.PID, err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		stillRunning, runErr := isProcessRunning(state.PID)
		if runErr != nil {
			return runErr
		}
		if !stillRunning {
			break
		}
		time.Sleep(150 * time.Millisecond)
	}
	stillRunning, err := isProcessRunning(state.PID)
	if err != nil {
		return err
	}
	if stillRunning {
		return fmt.Errorf("daemon pid=%d did not stop within timeout; check log: %s", state.PID, state.LogPath)
	}

	if removeErr := os.Remove(files.statePath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
		return fmt.Errorf("remove state file: %w", removeErr)
	}

	fmt.Fprintf(stdout, "stopped ai-octo-relay\npid: %d\nstate: %s\n", state.PID, files.statePath)
	return nil
}

func restartDaemon(configPath string, stdout io.Writer) error {
	if err := stopDaemon(configPath, io.Discard); err != nil && !errors.Is(err, os.ErrNotExist) && !isDaemonNotRunningError(err) {
		return err
	}
	return startDaemon(configPath, stdout)
}

func daemonFilePaths(configPath string) daemonFiles {
	baseDir := filepath.Dir(configPath)
	return daemonFiles{
		statePath: filepath.Join(baseDir, "ai-octo-relay.pid"),
		logPath:   filepath.Join(baseDir, "ai-octo-relay.log"),
	}
}

func readDaemonState(path string) (daemonState, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return daemonState{}, err
	}
	var state daemonState
	if err := json.Unmarshal(content, &state); err != nil {
		return daemonState{}, fmt.Errorf("parse daemon state %s: %w", path, err)
	}
	return state, nil
}

func writeDaemonState(path string, state daemonState) error {
	content, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal daemon state: %w", err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return fmt.Errorf("write daemon state %s: %w", path, err)
	}
	return nil
}

func cleanupDaemonStateFile(path string, pid int) {
	state, err := readDaemonState(path)
	if err != nil {
		return
	}
	if state.PID != pid {
		return
	}
	_ = os.Remove(path)
}

func isProcessRunning(pid int) (bool, error) {
	if pid <= 0 {
		return false, nil
	}
	err := syscall.Kill(pid, 0)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, syscall.ESRCH) {
		return false, nil
	}
	if errors.Is(err, syscall.EPERM) {
		return true, nil
	}
	return false, fmt.Errorf("check pid %d: %w", pid, err)
}

func isDaemonNotRunningError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "daemon not running:")
}
