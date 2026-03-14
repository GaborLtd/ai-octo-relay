package slackbot

import "testing"

func TestSessionRunGuard(t *testing.T) {
	bot := &Bot{
		runningSession: map[string]struct{}{},
	}

	if ok := bot.tryBeginSessionRun("session-1"); !ok {
		t.Fatalf("first tryBeginSessionRun() = false, want true")
	}
	if ok := bot.tryBeginSessionRun("session-1"); ok {
		t.Fatalf("second tryBeginSessionRun() = true, want false")
	}

	bot.endSessionRun("session-1")

	if ok := bot.tryBeginSessionRun("session-1"); !ok {
		t.Fatalf("tryBeginSessionRun() after end = false, want true")
	}
}

func TestNormalizeSlackMarkdownLine(t *testing.T) {
	got := normalizeSlackMarkdownLine("變更在 [internal/config/config.go](/Users/match/git/ai-octo-relay/internal/config/config.go)")
	want := "變更在 `internal/config/config.go`"
	if got != want {
		t.Fatalf("normalizeSlackMarkdownLine() = %q, want %q", got, want)
	}
}

func TestNormalizeSlackMarkdownLineConvertsBoldAndHeading(t *testing.T) {
	if got := normalizeSlackMarkdownLine("**重點摘要**"); got != "*重點摘要*" {
		t.Fatalf("normalizeSlackMarkdownLine() bold = %q", got)
	}
	if got := normalizeSlackMarkdownLine("## 小節標題"); got != "*小節標題*" {
		t.Fatalf("normalizeSlackMarkdownLine() heading = %q", got)
	}
}

func TestCleanFinalResponseRebuildsWrappedParagraphs(t *testing.T) {
	input := "好的，我來讀一下這個\n專案。\n\n首先，我會查看 cmd, data, docs, 和 internal 目錄的內容，以了解專案的結構\n和目的。"
	got := cleanFinalResponse(input)
	want := "好的，我來讀一下這個專案。\n\n首先，我會查看 cmd, data, docs, 和 internal 目錄的內容，以了解專案的結構和目的。"
	if got != want {
		t.Fatalf("cleanFinalResponse() = %q, want %q", got, want)
	}
}

func TestNormalizeThreadTS(t *testing.T) {
	if got := normalizeThreadTS("111.222", "333.444", true); got != "111.222" {
		t.Fatalf("normalizeThreadTS() with existing thread = %q", got)
	}
	if got := normalizeThreadTS("", "333.444", true); got != "333.444" {
		t.Fatalf("normalizeThreadTS() preferThread = %q", got)
	}
	if got := normalizeThreadTS("", "333.444", false); got != "" {
		t.Fatalf("normalizeThreadTS() without thread = %q", got)
	}
}

func TestFinalizePromptOutput(t *testing.T) {
	if got := finalizePromptOutput("", nil); got != "(empty response)" {
		t.Fatalf("finalizePromptOutput() empty = %q", got)
	}
	if got := finalizePromptOutput("", errTestBoom); got != "boom" {
		t.Fatalf("finalizePromptOutput() err only = %q", got)
	}
	if got := finalizePromptOutput("ok", errTestBoom); got != "ok\n\nerror: boom" {
		t.Fatalf("finalizePromptOutput() output+err = %q", got)
	}
}
