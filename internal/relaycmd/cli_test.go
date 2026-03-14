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

func TestParseCLINoCommandPrintsUsage(t *testing.T) {
	var stderr bytes.Buffer

	_, _, err := parseCLI([]string{}, &stderr)
	if err == nil {
		t.Fatal("parseCLI() error = nil, want error")
	}

	got := stderr.String()
	if !strings.Contains(got, "Usage:") {
		t.Fatalf("parseCLI() missing usage: %q", got)
	}
}

func TestParseCLIUnknownCommandPrintsUsage(t *testing.T) {
	var stderr bytes.Buffer

	_, _, err := parseCLI([]string{"invalid"}, &stderr)
	if err == nil {
		t.Fatal("parseCLI() error = nil, want error")
	}

	got := stderr.String()
	if !strings.Contains(got, "Usage:") {
		t.Fatalf("parseCLI() missing usage: %q", got)
	}
	if !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("parseCLI() error = %q", err)
	}
}

func TestParseCLIValidCommands(t *testing.T) {
	tests := []string{"serve", "start", "stop", "restart"}
	for _, cmd := range tests {
		t.Run(cmd, func(t *testing.T) {
			var stderr bytes.Buffer
			command, _, err := parseCLI([]string{cmd}, &stderr)
			if err != nil {
				t.Fatalf("parseCLI(%s) error = %v, want nil", cmd, err)
			}
			if command != cmd {
				t.Fatalf("parseCLI(%s) command = %q, want %q", cmd, command, cmd)
			}
		})
	}
}
