package slackbot

import "github.com/slack-go/slack"

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
