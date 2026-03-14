package slackbot

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/slack-go/slack"

	"github.com/match/ai-octo-relay/internal/app"
)

type promptRequest struct {
	AgentOverride string
	Prompt        string
	ThreadTS      string
	Scope         app.Scope
}

var errPromptRequestHandled = errors.New("prompt request already handled")

func (b *Bot) handleCommandMessage(ctx context.Context, channelID, userID, threadTS, text string, isDM bool) error {
	b.logger.Infof("command received: channel=%s user=%s command=%q", channelID, userID, truncateForLog(text, 200))
	response, err := b.handleCommand(ctx, channelID, userID, threadTS, text, isDM)
	if err != nil {
		b.logger.Warnf("command failed: channel=%s user=%s error=%v", channelID, userID, err)
		return b.reply(channelID, threadTS, "command error: "+err.Error())
	}
	b.logger.Infof("command completed: channel=%s user=%s", channelID, userID)
	return b.reply(channelID, threadTS, response)
}

func (b *Bot) preparePromptRequest(channelID, userID, text, threadTS, messageTS string, preferThread bool) (promptRequest, error) {
	agentOverride, promptText, hasAgentOverride := b.service.ExtractAgentOverride(text)
	if hasAgentOverride {
		if promptText == "" {
			if err := b.reply(channelID, threadTS, "agent selector 後面缺少 prompt"); err != nil {
				return promptRequest{}, err
			}
			return promptRequest{}, errPromptRequestHandled
		}
		text = promptText
	}

	normalizedThreadTS := normalizeThreadTS(threadTS, messageTS, preferThread)
	if err := b.service.ValidateAgentLock(channelID, normalizedThreadTS, agentOverride); err != nil {
		if replyErr := b.reply(channelID, normalizedThreadTS, err.Error()); replyErr != nil {
			return promptRequest{}, replyErr
		}
		return promptRequest{}, errPromptRequestHandled
	}
	if hasAgentOverride {
		if _, err := b.service.UseAgent(channelID, normalizedThreadTS, agentOverride); err != nil {
			return promptRequest{}, fmt.Errorf("persist agent override: %w", err)
		}
		b.logger.Infof("agent override persisted: channel=%s user=%s thread_ts=%s agent=%s", channelID, userID, normalizedThreadTS, agentOverride)
	}
	if err := b.service.MarkThreadSessionActive(channelID, normalizedThreadTS); err != nil {
		return promptRequest{}, fmt.Errorf("mark thread session active: %w", err)
	}
	b.logger.Infof("thread decision: channel=%s message_ts=%s incoming_thread_ts=%s prefer_thread=%t resolved_thread_ts=%s", channelID, messageTS, threadTS, preferThread, normalizedThreadTS)

	scope, err := b.service.ResolvePromptScope(channelID, normalizedThreadTS, agentOverride)
	if err != nil {
		return promptRequest{}, fmt.Errorf("resolve scope: %w", err)
	}
	b.logger.Infof("resolved scope: channel=%s agent=%s project=%s quiet=%t", channelID, scope.AgentName, scope.ProjectName, scope.Quiet)
	b.logger.Infof("resolved session: channel=%s session_key=%s is_thread=%t thread_key=%s", channelID, scope.SessionKey, scope.IsThread, scope.ThreadKey)
	b.logger.Infof(
		"prompt dispatch: channel=%s user=%s agent=%s project=%s thread_ts=%s prompt=%q",
		channelID,
		userID,
		scope.AgentName,
		scope.ProjectName,
		normalizedThreadTS,
		truncateForLog(text, 100),
	)

	return promptRequest{
		AgentOverride: agentOverride,
		Prompt:        text,
		ThreadTS:      normalizedThreadTS,
		Scope:         scope,
	}, nil
}

func (b *Bot) postStatusMessage(channelID, threadTS string, quiet bool) (string, error) {
	_, statusTS, err := b.client.PostMessage(
		channelID,
		slack.MsgOptionText(statusMessageText(quiet), false),
		slack.MsgOptionTS(threadTS),
	)
	if err != nil {
		return "", fmt.Errorf("post status message: %w", err)
	}
	b.logger.Infof("status message posted: channel=%s status_ts=%s thread_ts=%s", channelID, statusTS, threadTS)
	return statusTS, nil
}

func (b *Bot) newStatusUpdater(channelID, statusTS string, quiet bool) func(string) {
	var (
		mu         sync.Mutex
		chunks     []string
		lastUpdate time.Time
	)
	return func(line string) {
		if quiet {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		chunks = append(chunks, line)
		if time.Since(lastUpdate) < 2*time.Second {
			return
		}
		lastUpdate = time.Now()
		if _, _, _, err := b.client.UpdateMessage(channelID, statusTS, slack.MsgOptionText(formatExecutionUpdate(chunks), false)); err != nil {
			b.logger.Warnf("status update failed: channel=%s status_ts=%s error=%v", channelID, statusTS, err)
		}
	}
}

func (b *Bot) runPromptAndPublish(ctx context.Context, channelID, userID, statusTS string, request promptRequest, isDM bool, onChunk func(string)) error {
	scope := request.Scope
	b.logger.Infof("agent run started: channel=%s user=%s agent=%s project=%s status_ts=%s", channelID, userID, scope.AgentName, scope.ProjectName, statusTS)
	if !b.tryBeginSessionRun(scope.SessionKey) {
		b.logger.Warnf("session already running: channel=%s user=%s session_key=%s", channelID, userID, scope.SessionKey)
		return b.publishFinalResponse(channelID, request.ThreadTS, statusTS, "這個 thread 目前還在處理上一個請求，請稍候再試，或開新的 thread。")
	}
	defer b.endSessionRun(scope.SessionKey)

	output, runErr := b.service.RunPrompt(ctx, channelID, request.ThreadTS, userID, request.Prompt, request.AgentOverride, isDM, onChunk)
	if runErr != nil {
		b.logger.Warnf("agent run failed: channel=%s user=%s status_ts=%s error=%v", channelID, userID, statusTS, runErr)
	} else {
		b.logger.Infof("agent run completed: channel=%s user=%s status_ts=%s output_len=%d", channelID, userID, statusTS, len(output))
	}

	finalText := finalizePromptOutput(output, runErr)
	b.logger.Infof("output lengths: raw=%d", len(finalText))
	finalText = formatSlackReply(cleanFinalResponse(finalText))
	b.logger.Infof("output lengths: formatted=%d", len(finalText))

	if err := b.publishFinalResponse(channelID, request.ThreadTS, statusTS, finalText); err != nil {
		b.logger.Errorf("final status publish failed: channel=%s status_ts=%s error=%v", channelID, statusTS, err)
		return fmt.Errorf("publish final response: %w", err)
	}
	b.logger.Infof("final status update completed: channel=%s status_ts=%s final_len=%d", channelID, statusTS, len(finalText))
	return nil
}

func finalizePromptOutput(output string, runErr error) string {
	switch {
	case output == "" && runErr != nil:
		return runErr.Error()
	case runErr != nil:
		return output + "\n\nerror: " + runErr.Error()
	case output == "":
		return "(empty response)"
	default:
		return output
	}
}
