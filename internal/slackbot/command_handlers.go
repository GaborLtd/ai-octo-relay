package slackbot

import (
	"context"
	"fmt"
	"strings"
)

func (b *Bot) handleCommand(ctx context.Context, channelID, userID, threadTS, text string, isDM bool) (string, error) {
	fields := strings.Fields(strings.TrimPrefix(text, b.cfg.CommandPrefix))
	if len(fields) == 0 {
		return b.service.HelpText(), nil
	}
	if isDM {
		if err := b.service.ValidateDMCommandAccess(fields[0], fields[1:]); err != nil {
			return "", err
		}
	}

	switch fields[0] {
	case "help":
		return b.service.HelpText(), nil
	case "status":
		return b.service.StatusText(channelID, threadTS)
	case "cmd":
		return b.handleProjectCmd(ctx, channelID, threadTS, fields[1:])
	case "git":
		return b.service.RunGitCommand(ctx, channelID, threadTS, fields[1:])
	case "session":
		return b.handleSessionCommand(ctx, channelID, userID, threadTS, fields[1:])
	case "quiet":
		return b.handleQuietCommand(channelID, threadTS, fields[1:])
	case "reset":
		return b.service.Reset(channelID, threadTS)
	case "project":
		return b.handleProjectCommand(channelID, threadTS, fields[1:])
	case "agent":
		return b.handleAgentCommand(ctx, channelID, threadTS, fields[1:])
	default:
		return "", fmt.Errorf("unknown command: %s", fields[0])
	}
}

func (b *Bot) handleProjectCmd(ctx context.Context, channelID, threadTS string, args []string) (string, error) {
	if len(args) == 0 {
		return "", fmt.Errorf("missing cmd subcommand")
	}
	switch args[0] {
	case "list":
		return b.service.ProjectCommandListText(channelID, threadTS)
	case "run":
		if len(args) < 2 {
			return "", fmt.Errorf("missing command name")
		}
		return b.service.RunProjectCommand(ctx, channelID, threadTS, args[1])
	default:
		return "", fmt.Errorf("unknown cmd subcommand: %s", args[0])
	}
}

func (b *Bot) handleQuietCommand(channelID, threadTS string, args []string) (string, error) {
	if len(args) == 0 {
		return b.service.QuietStatusText(channelID, threadTS)
	}
	switch args[0] {
	case "on":
		return b.service.SetQuiet(channelID, threadTS, true)
	case "off":
		return b.service.SetQuiet(channelID, threadTS, false)
	default:
		return "", fmt.Errorf("unknown quiet subcommand: %s", args[0])
	}
}

func (b *Bot) handleSessionCommand(ctx context.Context, channelID, userID, threadTS string, args []string) (string, error) {
	if len(args) == 0 {
		return b.service.SessionStatusText(channelID, threadTS)
	}
	switch args[0] {
	case "status":
		return b.service.SessionStatusText(channelID, threadTS)
	case "restart":
		return b.service.RestartSession(ctx, channelID, threadTS, userID)
	case "close":
		return b.service.CloseSession(channelID, threadTS)
	default:
		return "", fmt.Errorf("unknown session subcommand: %s", args[0])
	}
}

func (b *Bot) handleProjectCommand(channelID, threadTS string, args []string) (string, error) {
	if len(args) == 0 {
		return "", fmt.Errorf("missing project subcommand")
	}
	switch args[0] {
	case "list":
		return b.service.ProjectListText(), nil
	case "current":
		return b.service.ProjectCurrentText(channelID, threadTS)
	case "use":
		if len(args) < 2 {
			return "", fmt.Errorf("missing project name")
		}
		return b.service.UseProject(channelID, threadTS, args[1])
	case "clear":
		return b.service.ClearProject(channelID, threadTS)
	default:
		return "", fmt.Errorf("unknown project subcommand: %s", args[0])
	}
}

func (b *Bot) handleAgentCommand(ctx context.Context, channelID, threadTS string, args []string) (string, error) {
	if len(args) == 0 {
		return "", fmt.Errorf("missing agent subcommand")
	}
	switch args[0] {
	case "list":
		return b.service.AgentListText(), nil
	case "model":
		if len(args) < 3 || args[1] != "list" {
			return "", fmt.Errorf("usage: !agent model list <name>")
		}
		return b.service.AgentModelListText(ctx, args[2])
	case "use":
		if len(args) < 2 {
			return "", fmt.Errorf("missing agent name")
		}
		return b.service.UseAgent(channelID, threadTS, args[1])
	case "clear":
		return b.service.ClearAgent(channelID, threadTS)
	default:
		return "", fmt.Errorf("unknown agent subcommand: %s", args[0])
	}
}
