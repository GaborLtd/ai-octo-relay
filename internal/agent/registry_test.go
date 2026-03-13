package agent

import (
	"testing"

	"github.com/match/ai-octo-relay/internal/config"
)

func TestGeminiAdapterDisablesMaxSessionTurns(t *testing.T) {
	spec := geminiAdapter{}.Build(RunRequest{AgentName: "gemini"}, config.AgentConfig{
		Model:           "gemini-2.5-flash",
		MaxSessionTurns: 8,
	})
	if spec.MaxSessionTurns != 0 {
		t.Fatalf("gemini MaxSessionTurns = %d, want 0", spec.MaxSessionTurns)
	}
}
