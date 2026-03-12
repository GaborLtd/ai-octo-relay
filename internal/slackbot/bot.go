package slackbot

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"

	"github.com/match/ai-octo-relay/internal/app"
	"github.com/match/ai-octo-relay/internal/config"
	"github.com/match/ai-octo-relay/internal/logx"
)

type Bot struct {
	cfg       *config.Config
	service   *app.Service
	logger    *logx.Logger
	client    *slack.Client
	socket    *socketmode.Client
	botUserID string
}

func New(cfg *config.Config, service *app.Service, logger *logx.Logger) (*Bot, error) {
	client := slack.New(
		cfg.Slack.BotToken,
		slack.OptionAppLevelToken(cfg.Slack.AppToken),
	)
	socket := socketmode.New(client)

	auth, err := client.AuthTest()
	if err != nil {
		return nil, fmt.Errorf("slack auth test: %w", err)
	}

	return &Bot{
		cfg:       cfg,
		service:   service,
		logger:    logger,
		client:    client,
		socket:    socket,
		botUserID: auth.UserID,
	}, nil
}

func (b *Bot) Run(ctx context.Context) error {
	go func() {
		if err := b.socket.RunContext(ctx); err != nil && ctx.Err() == nil {
			b.logger.Errorf("slack socket stopped: %v", err)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case evt, ok := <-b.socket.Events:
			if !ok {
				return nil
			}
			if err := b.handleEvent(ctx, evt); err != nil {
				b.logger.Errorf("handle slack event: %v", err)
			}
		}
	}
}

func (b *Bot) handleEvent(ctx context.Context, evt socketmode.Event) error {
	switch evt.Type {
	case socketmode.EventTypeConnecting:
		b.logger.Infof("slack socket connecting")
	case socketmode.EventTypeConnectionError:
		b.logger.Warnf("slack socket connection error: %v", evt.Data)
	case socketmode.EventTypeConnected:
		b.logger.Infof("slack socket connected")
	case socketmode.EventTypeEventsAPI:
		apiEvent, ok := evt.Data.(slackevents.EventsAPIEvent)
		if !ok {
			return nil
		}
		b.logger.Infof("events api received: type=%s", apiEvent.Type)
		b.socket.Ack(*evt.Request)
		return b.handleEventsAPI(ctx, apiEvent)
	}
	return nil
}

func (b *Bot) handleEventsAPI(ctx context.Context, apiEvent slackevents.EventsAPIEvent) error {
	b.logger.Infof("events api inner event: type=%s concrete=%T", apiEvent.InnerEvent.Type, apiEvent.InnerEvent.Data)
	switch ev := apiEvent.InnerEvent.Data.(type) {
	case *slackevents.MessageEvent:
		return b.handleMessage(ctx, *ev)
	case slackevents.MessageEvent:
		return b.handleMessage(ctx, ev)
	case *slackevents.AppMentionEvent:
		return b.handleMention(ctx, *ev)
	case slackevents.AppMentionEvent:
		return b.handleMention(ctx, ev)
	default:
		b.logger.Warnf("unhandled slack inner event: type=%s concrete=%T", apiEvent.InnerEvent.Type, apiEvent.InnerEvent.Data)
		return nil
	}
}

func (b *Bot) handleMention(ctx context.Context, ev slackevents.AppMentionEvent) error {
	b.logger.Infof("slack mention: channel=%s user=%s thread_ts=%s ts=%s", ev.Channel, ev.User, ev.ThreadTimeStamp, ev.TimeStamp)
	return b.processMessage(ctx, ev.Channel, ev.User, ev.Text, ev.ThreadTimeStamp, ev.TimeStamp, true)
}

func (b *Bot) handleMessage(ctx context.Context, ev slackevents.MessageEvent) error {
	if ev.BotID != "" || ev.SubType != "" {
		return nil
	}
	if ev.ChannelType == "im" {
		b.logger.Infof("slack dm: channel=%s user=%s thread_ts=%s ts=%s", ev.Channel, ev.User, ev.ThreadTimeStamp, ev.TimeStamp)
		return b.processMessage(ctx, ev.Channel, ev.User, ev.Text, ev.ThreadTimeStamp, ev.TimeStamp, false)
	}
	if b.isBotMention(ev.Text) {
		b.logger.Infof("slack channel mention via message event: channel=%s user=%s thread_ts=%s ts=%s", ev.Channel, ev.User, ev.ThreadTimeStamp, ev.TimeStamp)
		return b.processMessage(ctx, ev.Channel, ev.User, ev.Text, ev.ThreadTimeStamp, ev.TimeStamp, true)
	}
	b.logger.Debugf("ignored non-dm message event: channel=%s channel_type=%s user=%s ts=%s", ev.Channel, ev.ChannelType, ev.User, ev.TimeStamp)
	return nil
}

func (b *Bot) processMessage(ctx context.Context, channelID, userID, rawText, threadTS, messageTS string, preferThread bool) error {
	if !b.isAllowedChannel(channelID) {
		return b.reply(channelID, threadTS, "這個 channel 不在允許清單。")
	}

	text := strings.TrimSpace(b.stripBotMention(rawText))
	if text == "" {
		return nil
	}

	b.logger.Debugf(
		"process message: channel=%s user=%s message_ts=%s thread_ts=%s prefer_thread=%t text=%q",
		channelID,
		userID,
		messageTS,
		threadTS,
		preferThread,
		truncateForLog(text, 200),
	)
	if strings.HasPrefix(text, b.cfg.CommandPrefix) {
		commandThreadTS := threadTS
		b.logger.Infof("command received: channel=%s user=%s command=%q", channelID, userID, truncateForLog(text, 200))
		response, err := b.handleCommand(ctx, channelID, userID, commandThreadTS, text)
		if err != nil {
			b.logger.Warnf("command failed: channel=%s user=%s error=%v", channelID, userID, err)
			return b.reply(channelID, commandThreadTS, "command error: "+err.Error())
		}
		b.logger.Infof("command completed: channel=%s user=%s", channelID, userID)
		return b.reply(channelID, commandThreadTS, response)
	}

	normalizedThreadTS := normalizeThreadTS(threadTS, messageTS, preferThread)
	b.logger.Infof("thread decision: channel=%s message_ts=%s incoming_thread_ts=%s prefer_thread=%t resolved_thread_ts=%s", channelID, messageTS, threadTS, preferThread, normalizedThreadTS)

	scope, err := b.service.ResolveScope(channelID, normalizedThreadTS)
	if err != nil {
		return fmt.Errorf("resolve scope: %w", err)
	}
	b.logger.Infof("resolved scope: channel=%s agent=%s project=%s quiet=%t", channelID, scope.AgentName, scope.ProjectName, scope.Quiet)
	b.logger.Infof("resolved session: channel=%s session_key=%s is_thread=%t thread_key=%s", channelID, scope.SessionKey, scope.IsThread, scope.ThreadKey)

	_, statusTS, err := b.client.PostMessage(
		channelID,
		slack.MsgOptionText(statusMessageText(scope.Quiet), false),
		slack.MsgOptionTS(normalizedThreadTS),
	)
	if err != nil {
		return fmt.Errorf("post status message: %w", err)
	}
	b.logger.Infof("status message posted: channel=%s status_ts=%s thread_ts=%s", channelID, statusTS, normalizedThreadTS)

	var (
		mu         sync.Mutex
		chunks     []string
		lastUpdate time.Time
	)
	onChunk := func(line string) {
		if scope.Quiet {
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

	b.logger.Infof("agent run started: channel=%s user=%s status_ts=%s", channelID, userID, statusTS)
	output, runErr := b.service.RunPrompt(ctx, channelID, normalizedThreadTS, userID, text, onChunk)
	if runErr != nil {
		b.logger.Warnf("agent run failed: channel=%s user=%s status_ts=%s error=%v", channelID, userID, statusTS, runErr)
	} else {
		b.logger.Infof("agent run completed: channel=%s user=%s status_ts=%s output_len=%d", channelID, userID, statusTS, len(output))
	}
	finalText := output
	if finalText == "" && runErr != nil {
		finalText = runErr.Error()
	}
	if runErr != nil && output != "" {
		finalText = output + "\n\nerror: " + runErr.Error()
	}
	if finalText == "" {
		finalText = "(empty response)"
	}
	b.logger.Infof("output lengths: raw=%d", len(finalText))
	finalText = formatSlackReply(cleanFinalResponse(finalText))
	b.logger.Infof("output lengths: formatted=%d", len(finalText))

	if err := b.publishFinalResponse(channelID, normalizedThreadTS, statusTS, finalText); err != nil {
		b.logger.Errorf("final status publish failed: channel=%s status_ts=%s error=%v", channelID, statusTS, err)
		return fmt.Errorf("publish final response: %w", err)
	}
	b.logger.Infof("final status update completed: channel=%s status_ts=%s final_len=%d", channelID, statusTS, len(finalText))
	return nil
}

func (b *Bot) handleCommand(ctx context.Context, channelID, userID, threadTS, text string) (string, error) {
	fields := strings.Fields(strings.TrimPrefix(text, b.cfg.CommandPrefix))
	if len(fields) == 0 {
		return b.service.HelpText(), nil
	}

	switch fields[0] {
	case "help":
		return b.service.HelpText(), nil
	case "status":
		return b.service.StatusText(channelID, threadTS)
	case "session":
		return b.handleSessionCommand(ctx, channelID, userID, threadTS, fields[1:])
	case "quiet":
		return b.handleQuietCommand(channelID, threadTS, fields[1:])
	case "reset":
		return b.service.Reset(channelID, threadTS)
	case "project":
		return b.handleProjectCommand(channelID, threadTS, fields[1:])
	case "agent":
		return b.handleAgentCommand(channelID, threadTS, fields[1:])
	default:
		return "", fmt.Errorf("unknown command: %s", fields[0])
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

func (b *Bot) handleAgentCommand(channelID, threadTS string, args []string) (string, error) {
	if len(args) == 0 {
		return "", fmt.Errorf("missing agent subcommand")
	}
	switch args[0] {
	case "list":
		return b.service.AgentListText(), nil
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

func (b *Bot) reply(channelID, threadTS, text string) error {
	chunks := splitSlackMessage(formatSlackReply(text), 3000)
	for _, chunk := range chunks {
		_, _, err := b.client.PostMessage(
			channelID,
			slack.MsgOptionText(chunk, false),
			slack.MsgOptionTS(threadTS),
		)
		if err != nil {
			return err
		}
	}
	return nil
}

func (b *Bot) stripBotMention(text string) string {
	mention := "<@" + b.botUserID + ">"
	return strings.ReplaceAll(text, mention, "")
}

func (b *Bot) isBotMention(text string) bool {
	mention := "<@" + b.botUserID + ">"
	return strings.Contains(text, mention)
}

func (b *Bot) isAllowedChannel(channelID string) bool {
	if len(b.cfg.Slack.AllowedChannels) == 0 {
		return true
	}
	for _, allowed := range b.cfg.Slack.AllowedChannels {
		if allowed == channelID {
			return true
		}
	}
	return false
}

func normalizeThreadTS(threadTS, messageTS string, preferThread bool) string {
	if threadTS != "" {
		return threadTS
	}
	if preferThread {
		return messageTS
	}
	return ""
}

func formatExecutionUpdate(chunks []string) string {
	if len(chunks) == 0 {
		return "執行中..."
	}
	start := 0
	if len(chunks) > 20 {
		start = len(chunks) - 20
	}
	return "執行中...\n\n" + truncateForSlackChunk(strings.Join(chunks[start:], "\n"), 3000)
}

func truncateForLog(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	return text[:limit] + "...[truncated]"
}

func statusMessageText(quiet bool) string {
	if quiet {
		return "_收到，處理中..._"
	}
	return "_執行中..._"
}

var noiseLinePatterns = []*regexp.Regexp{
	regexp.MustCompile(`^OpenAI Codex v[0-9.]+`),
	regexp.MustCompile(`^-{4,}$`),
	regexp.MustCompile(`^(workdir|model|provider|approval|sandbox|reasoning effort|reasoning summaries|session id):`),
	regexp.MustCompile(`^(mcp:|mcp startup:)`),
	regexp.MustCompile(`^(user|assistant|codex)$`),
	regexp.MustCompile(`^exec$`),
	regexp.MustCompile(`^/bin/zsh -lc `),
	regexp.MustCompile(`^succeeded in [0-9]+ms:?$`),
	regexp.MustCompile(`^failed in [0-9]+ms:?$`),
}

func cleanFinalResponse(text string) string {
	lines := strings.Split(strings.ReplaceAll(text, "\r", ""), "\n")
	out := make([]string, 0, len(lines))
	seen := map[string]struct{}{}
	lastBlank := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if len(out) > 0 && !lastBlank {
				out = append(out, "")
				lastBlank = true
			}
			continue
		}
		if isNoiseLine(trimmed) {
			continue
		}
		if _, ok := seen[trimmed]; ok && len(trimmed) > 20 {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
		lastBlank = false
	}
	cleaned := strings.TrimSpace(strings.Join(out, "\n"))
	if cleaned == "" {
		return strings.TrimSpace(text)
	}
	return cleaned
}

func isNoiseLine(line string) bool {
	for _, pattern := range noiseLinePatterns {
		if pattern.MatchString(line) {
			return true
		}
	}
	return false
}

func formatSlackReply(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return "(empty response)"
	}

	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	inCodeBlock := false
	lastBlank := false

	for _, line := range lines {
		trimmedRight := strings.TrimRight(line, " \t")
		trimmed := strings.TrimSpace(trimmedRight)

		if strings.HasPrefix(trimmed, "```") {
			inCodeBlock = !inCodeBlock
			out = append(out, trimmed)
			lastBlank = false
			continue
		}

		if trimmed == "" {
			if inCodeBlock {
				out = append(out, "")
				continue
			}
			if len(out) > 0 && !lastBlank {
				out = append(out, "")
				lastBlank = true
			}
			continue
		}

		out = append(out, trimmedRight)
		lastBlank = false
	}

	formatted := strings.TrimSpace(strings.Join(out, "\n"))
	if strings.Count(formatted, "```")%2 != 0 {
		formatted += "\n```"
	}
	return formatted
}

func (b *Bot) publishFinalResponse(channelID, threadTS, statusTS, text string) error {
	chunks := splitSlackMessage(text, 3000)
	if len(chunks) == 0 {
		chunks = []string{"(empty response)"}
	}
	b.logger.Infof("publish final response: channel=%s thread_ts=%s status_ts=%s chunks=%d", channelID, threadTS, statusTS, len(chunks))
	for idx, chunk := range chunks {
		b.logger.Tracef("publish chunk: index=%d len=%d preview=%q", idx, len(chunk), truncateForLog(chunk, 160))
	}

	_, _, _, err := b.client.UpdateMessage(channelID, statusTS, slack.MsgOptionText(chunks[0], false))
	if err != nil {
		return err
	}

	for _, chunk := range chunks[1:] {
		_, _, err := b.client.PostMessage(
			channelID,
			slack.MsgOptionText(chunk, false),
			slack.MsgOptionTS(threadTS),
		)
		if err != nil {
			return err
		}
	}
	return nil
}

func splitSlackMessage(text string, limit int) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if len(text) <= limit {
		return []string{text}
	}

	paragraphs := strings.Split(text, "\n\n")
	chunks := make([]string, 0)
	var current strings.Builder

	flush := func() {
		if current.Len() == 0 {
			return
		}
		chunks = append(chunks, strings.TrimSpace(current.String()))
		current.Reset()
	}

	for _, paragraph := range paragraphs {
		paragraph = strings.TrimSpace(paragraph)
		if paragraph == "" {
			continue
		}

		candidate := paragraph
		if current.Len() > 0 {
			candidate = current.String() + "\n\n" + paragraph
		}
		if len(candidate) <= limit {
			if current.Len() > 0 {
				current.WriteString("\n\n")
			}
			current.WriteString(paragraph)
			continue
		}

		flush()
		if len(paragraph) <= limit {
			current.WriteString(paragraph)
			continue
		}

		lines := strings.Split(paragraph, "\n")
		for _, line := range lines {
			line = strings.TrimRight(line, " \t")
			candidateLine := line
			if current.Len() > 0 {
				candidateLine = current.String() + "\n" + line
			}
			if len(candidateLine) <= limit {
				if current.Len() > 0 {
					current.WriteString("\n")
				}
				current.WriteString(line)
				continue
			}
			flush()
			for _, part := range splitLongLine(line, limit) {
				if len(part) == 0 {
					continue
				}
				current.WriteString(part)
				flush()
			}
		}
	}
	flush()

	for i := range chunks {
		chunks[i] = balanceCodeFence(chunks[i], i < len(chunks)-1)
	}
	return chunks
}

func splitLongLine(line string, limit int) []string {
	if len(line) <= limit {
		return []string{line}
	}
	parts := make([]string, 0)
	remaining := line
	for len(remaining) > limit {
		cut := strings.LastIndex(remaining[:limit], " ")
		if cut < limit/3 {
			cut = limit
		}
		parts = append(parts, strings.TrimSpace(remaining[:cut]))
		remaining = strings.TrimSpace(remaining[cut:])
	}
	if remaining != "" {
		parts = append(parts, remaining)
	}
	return parts
}

func balanceCodeFence(text string, addContinuation bool) string {
	if strings.Count(text, "```")%2 == 0 {
		return text
	}
	if addContinuation {
		return text + "\n```"
	}
	return "```\n" + text
}

func truncateForSlackChunk(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	return text[:limit]
}
