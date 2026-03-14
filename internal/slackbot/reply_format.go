package slackbot

import (
	"regexp"
	"strings"
)

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

var markdownLinkPattern = regexp.MustCompile(`\[(.+?)\]\((.+?)\)`)
var markdownBoldPattern = regexp.MustCompile(`\*\*(.+?)\*\*`)
var markdownHeadingPattern = regexp.MustCompile(`^(#{1,6})\s+(.+)$`)
var orderedListPattern = regexp.MustCompile(`^\d+\.\s+`)
var structuredLinePrefixes = []string{"```", "- ", "* ", "> ", "|"}

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

		out = append(out, normalizeSlackMarkdownLine(trimmedRight))
		lastBlank = false
	}

	formatted := strings.TrimSpace(strings.Join(out, "\n"))
	if strings.Count(formatted, "```")%2 != 0 {
		formatted += "\n```"
	}
	return formatted
}

func normalizeSlackMarkdownLine(line string) string {
	line = normalizeSlackMarkdownLinks(line)
	line = markdownBoldPattern.ReplaceAllString(line, `*$1*`)
	if heading := normalizeSlackHeading(line); heading != "" {
		return heading
	}
	return line
}

func normalizeSlackMarkdownLinks(line string) string {
	return markdownLinkPattern.ReplaceAllStringFunc(line, func(match string) string {
		parts := markdownLinkPattern.FindStringSubmatch(match)
		if len(parts) != 3 {
			return match
		}
		label := strings.TrimSpace(parts[1])
		target := strings.TrimSpace(parts[2])
		if label == "" {
			return match
		}
		if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
			return "<" + target + "|" + label + ">"
		}
		return "`" + label + "`"
	})
}

func normalizeSlackHeading(line string) string {
	if parts := markdownHeadingPattern.FindStringSubmatch(strings.TrimSpace(line)); len(parts) == 3 {
		return "*" + strings.TrimSpace(parts[2]) + "*"
	}
	return ""
}

func hasAnyPrefix(text string, prefixes ...string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(text, prefix) {
			return true
		}
	}
	return false
}
