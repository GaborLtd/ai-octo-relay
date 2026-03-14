package relaycmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunHelpReturnsZeroAndPrintsUsage(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run([]string{"--help"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run(--help) code = %d, want 0", code)
	}

	got := stderr.String()
	if !strings.Contains(got, "Usage:") {
		t.Fatalf("Run(--help) missing usage: %q", got)
	}
	if !strings.Contains(got, "ai-octo-relay restart -c ./config.json") {
		t.Fatalf("Run(--help) missing restart example: %q", got)
	}
}

func TestParseCLIUnexpectedArgumentsPrintsUsage(t *testing.T) {
	var stderr bytes.Buffer

	_, _, err := parseCLI([]string{"serve", "extra"}, &stderr)
	if err == nil {
		t.Fatal("parseCLI() error = nil, want error")
	}

	got := stderr.String()
	if !strings.Contains(got, "Usage:") {
		t.Fatalf("parseCLI() missing usage: %q", got)
	}
	if !strings.Contains(err.Error(), "unexpected arguments") {
		t.Fatalf("parseCLI() error = %q", err)
	}
}
