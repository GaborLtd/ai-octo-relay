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
