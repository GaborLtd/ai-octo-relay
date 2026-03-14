package slackbot

import "strings"

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
