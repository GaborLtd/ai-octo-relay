package slackbot

import "github.com/match/ai-octo-relay/internal/terminal"

func appTerminalEvent(channelID, userID, threadTS, text string, isDM bool) terminal.Event {
	channelType := "channel"
	if isDM {
		channelType = "im"
	}
	return terminal.Event{
		UserID:      userID,
		ChannelID:   channelID,
		ChannelType: channelType,
		ThreadTS:    threadTS,
		Text:        text,
	}
}
