package slackbot

import "strings"

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

func statusMessageText(quiet bool) string {
	if quiet {
		return "_收到，處理中..._"
	}
	return "_執行中..._"
}
