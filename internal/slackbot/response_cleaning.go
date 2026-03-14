package slackbot

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

func cleanFinalResponse(text string) string {
	lines := strings.Split(strings.ReplaceAll(text, "\r", ""), "\n")
	lines = rebuildCharacterFragments(lines)
	lines = rebuildWrappedParagraphs(lines)
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

func rebuildCharacterFragments(lines []string) []string {
	out := make([]string, 0, len(lines))
	var fragment strings.Builder
	flush := func() {
		if fragment.Len() == 0 {
			return
		}
		text := strings.TrimSpace(fragment.String())
		if text != "" {
			out = append(out, text)
		}
		fragment.Reset()
	}

	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			flush()
			out = append(out, "")
			continue
		}
		if isSingleCharacterFragment(line) {
			fragment.WriteString(line)
			continue
		}
		flush()
		out = append(out, line)
	}
	flush()
	return out
}

func isSingleCharacterFragment(line string) bool {
	runes := []rune(line)
	if len(runes) != 1 {
		return false
	}
	r := runes[0]
	if r >= 'A' && r <= 'Z' {
		return true
	}
	if r >= 'a' && r <= 'z' {
		return true
	}
	if r >= '0' && r <= '9' {
		return true
	}
	switch r {
	case '.', ',', ':', ';', '!', '?', '\'', '"', '-', '_', '/', '\\', '(', ')':
		return true
	default:
		return false
	}
}

func rebuildWrappedParagraphs(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, raw := range lines {
		line := strings.TrimRight(raw, " \t")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			out = append(out, "")
			continue
		}
		if len(out) == 0 {
			out = append(out, trimmed)
			continue
		}
		out = appendRebuiltParagraphLine(out, trimmed)
	}
	return out
}

func appendRebuiltParagraphLine(lines []string, line string) []string {
	prev := strings.TrimSpace(lines[len(lines)-1])
	if prev == "" || !shouldJoinWrappedLines(prev, line) {
		return append(lines, line)
	}
	lines[len(lines)-1] = joinWrappedLines(prev, line)
	return lines
}

func shouldJoinWrappedLines(prev, curr string) bool {
	if isStructuredSlackLine(prev) || isStructuredSlackLine(curr) {
		return false
	}
	if hasTerminalPunctuation(prev) {
		return false
	}
	return true
}

func isStructuredSlackLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return true
	}
	switch {
	case hasAnyPrefix(trimmed, structuredLinePrefixes...),
		orderedListPattern.MatchString(trimmed),
		markdownHeadingPattern.MatchString(trimmed):
		return true
	default:
		return false
	}
}

func hasTerminalPunctuation(text string) bool {
	if text == "" {
		return false
	}
	r, _ := utf8.DecodeLastRuneInString(text)
	switch r {
	case '.', '!', '?', ':', ';', ',', '。', '！', '？', '：', '；':
		return true
	default:
		return false
	}
}

func joinWrappedLines(prev, curr string) string {
	if prev == "" {
		return curr
	}
	if curr == "" {
		return prev
	}
	lastPrev, _ := utf8.DecodeLastRuneInString(prev)
	firstCurr, _ := utf8.DecodeRuneInString(curr)
	if shouldJoinWithoutSpace(lastPrev, firstCurr) {
		return prev + curr
	}
	return prev + " " + curr
}

func shouldJoinWithoutSpace(lastPrev, firstCurr rune) bool {
	if isCJKRune(lastPrev) || isCJKRune(firstCurr) {
		return true
	}
	switch firstCurr {
	case '.', ',', ':', ';', '!', '?', ')', ']', '}', '。', '，', '：', '；', '！', '？':
		return true
	}
	switch lastPrev {
	case '(', '[', '{', '`':
		return true
	}
	return false
}

func isCJKRune(r rune) bool {
	return unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul)
}
