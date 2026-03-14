package slackbot

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

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

	runMu          sync.Mutex
	runningSession map[string]struct{}
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
		cfg:            cfg,
		service:        service,
		logger:         logger,
		client:         client,
		socket:         socket,
		botUserID:      auth.UserID,
		runningSession: map[string]struct{}{},
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
			go func(evt socketmode.Event) {
				if err := b.handleEvent(ctx, evt); err != nil {
					b.logger.Errorf("handle slack event: %v", err)
				}
			}(evt)
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
	return b.processMessage(ctx, ev.Channel, ev.User, ev.Text, ev.ThreadTimeStamp, ev.TimeStamp, true, false)
}

func (b *Bot) handleMessage(ctx context.Context, ev slackevents.MessageEvent) error {
	if ev.BotID != "" || ev.SubType != "" {
		b.logger.Infof("ignored message event: channel=%s user=%s bot_id=%s subtype=%s thread_ts=%s ts=%s", ev.Channel, ev.User, ev.BotID, ev.SubType, ev.ThreadTimeStamp, ev.TimeStamp)
		return nil
	}
	if ev.ChannelType == "im" {
		b.logger.Infof("slack dm: channel=%s user=%s thread_ts=%s ts=%s", ev.Channel, ev.User, ev.ThreadTimeStamp, ev.TimeStamp)
		return b.processMessage(ctx, ev.Channel, ev.User, ev.Text, ev.ThreadTimeStamp, ev.TimeStamp, false, true)
	}
	if b.isBotMention(ev.Text) {
		b.logger.Infof("slack channel mention via message event: channel=%s user=%s thread_ts=%s ts=%s", ev.Channel, ev.User, ev.ThreadTimeStamp, ev.TimeStamp)
		return b.processMessage(ctx, ev.Channel, ev.User, ev.Text, ev.ThreadTimeStamp, ev.TimeStamp, true, false)
	}
	if ev.ThreadTimeStamp != "" && b.service.HasThreadSession(ev.Channel, ev.ThreadTimeStamp) {
		b.logger.Infof("slack thread continuation: channel=%s user=%s thread_ts=%s ts=%s", ev.Channel, ev.User, ev.ThreadTimeStamp, ev.TimeStamp)
		return b.processMessage(ctx, ev.Channel, ev.User, ev.Text, ev.ThreadTimeStamp, ev.TimeStamp, false, false)
	}
	b.logger.Infof("ignored message event: channel=%s channel_type=%s user=%s thread_ts=%s ts=%s reason=no_mention_or_active_thread", ev.Channel, ev.ChannelType, ev.User, ev.ThreadTimeStamp, ev.TimeStamp)
	return nil
}

func (b *Bot) processMessage(ctx context.Context, channelID, userID, rawText, threadTS, messageTS string, preferThread bool, isDM bool) error {
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
		return b.handleCommandMessage(ctx, channelID, userID, threadTS, text, isDM)
	}

	request, err := b.preparePromptRequest(channelID, userID, text, threadTS, messageTS, preferThread)
	if err != nil {
		if errors.Is(err, errPromptRequestHandled) {
			return nil
		}
		return err
	}

	statusTS, err := b.postStatusMessage(channelID, request.ThreadTS, request.Scope.Quiet)
	if err != nil {
		return err
	}

	onChunk := b.newStatusUpdater(channelID, statusTS, request.Scope.Quiet)
	return b.runPromptAndPublish(ctx, channelID, userID, statusTS, request, isDM, onChunk)
}

func (b *Bot) tryBeginSessionRun(sessionKey string) bool {
	b.runMu.Lock()
	defer b.runMu.Unlock()
	if _, exists := b.runningSession[sessionKey]; exists {
		return false
	}
	b.runningSession[sessionKey] = struct{}{}
	return true
}

func (b *Bot) endSessionRun(sessionKey string) {
	b.runMu.Lock()
	defer b.runMu.Unlock()
	delete(b.runningSession, sessionKey)
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

func truncateForLog(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	return text[:limit] + "...[truncated]"
}
