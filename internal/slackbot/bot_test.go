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
