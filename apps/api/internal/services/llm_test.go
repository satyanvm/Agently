package services

import (
	"strings"
	"testing"
)

// The provider follows the model name, so the boot preflight must demand exactly
// the keys the configured tiers will actually touch — no more, no less.

func TestIsMoonshotModel(t *testing.T) {
	for model, want := range map[string]bool{
		"kimi-k3":          true,
		"kimi-k2.6":        true,
		"moonshot-v1-8k":   true,
		"gemini-2.5-flash": false,
		"gemini-2.5-pro":   false,
	} {
		if got := isMoonshotModel(model); got != want {
			t.Errorf("isMoonshotModel(%q) = %v, want %v", model, got, want)
		}
	}
}

func TestRequirePlannerKeys_NamesMoonshotWhenReduceIsKimi(t *testing.T) {
	// Gemini is satisfied, but the default reduce model (kimi-k3) has no key —
	// the error must name MOONSHOT_API_KEY, not "compilation failed".
	t.Setenv("GEMINI_API_KEY", "test-dummy")
	t.Setenv("MOONSHOT_API_KEY", "")
	t.Setenv("KIMI_API_KEY", "")
	t.Setenv("PLANNER_MODEL", "") // default kimi-k3
	if err := RequirePlannerKeys(); err == nil {
		t.Fatal("MOONSHOT_API_KEY unset with a Kimi reduce model: expected an error")
	} else if !strings.Contains(err.Error(), "MOONSHOT_API_KEY") {
		t.Fatalf("error must name MOONSHOT_API_KEY, got %v", err)
	}
}

func TestRequirePlannerKeys_GeminiOnlyTiersNeedNoMoonshot(t *testing.T) {
	// Point both tiers back at Gemini and the Moonshot key is not demanded.
	t.Setenv("GEMINI_API_KEY", "test-dummy")
	t.Setenv("MOONSHOT_API_KEY", "")
	t.Setenv("KIMI_API_KEY", "")
	t.Setenv("PLANNER_MODEL", "gemini-2.5-pro")
	t.Setenv("PLANNER_MAP_MODEL", "gemini-2.5-flash")
	if err := RequirePlannerKeys(); err != nil {
		t.Fatalf("all-Gemini tiers should need only GEMINI_API_KEY, got %v", err)
	}
}

func TestGeminiThinkingBudget(t *testing.T) {
	// Selection calls must not let thinking eat the answer's token budget —
	// that is what made the 1024-token router call fail on gemini-2.5-flash.
	for _, tc := range []struct {
		model     string
		maxTokens int
		want      int
	}{
		{"gemini-2.5-flash", 1024, 0},
		{"gemini-2.5-flash", 512, 0},
		{"gemini-2.5-pro", 1024, 128},
		{"gemini-2.5-flash", 16000, 8192},
		{"gemini-2.5-pro", 16000, 8192},
	} {
		if got := geminiThinkingBudget(tc.model, tc.maxTokens); got != tc.want {
			t.Errorf("geminiThinkingBudget(%q, %d) = %d, want %d", tc.model, tc.maxTokens, got, tc.want)
		}
	}
}

func TestRequirePlannerKeys_AllOllamaNeedsNoKeys(t *testing.T) {
	// Fully local: no Gemini, Moonshot or OpenAI key is demanded.
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("MOONSHOT_API_KEY", "")
	t.Setenv("KIMI_API_KEY", "")
	t.Setenv("PLANNER_MAP_MODEL", "ollama/qwen3:8b")
	t.Setenv("PLANNER_MODEL", "ollama/qwen3:8b")
	t.Setenv("PLANNER_EMBED_MODEL", "ollama/nomic-embed-text")
	if err := RequirePlannerKeys(); err != nil {
		t.Fatalf("all-Ollama tiers should need no keys, got %v", err)
	}
}

func TestLLMProvider(t *testing.T) {
	for model, want := range map[string]string{
		"ollama/qwen3:8b":       "ollama",
		"kimi-k3":               "moonshot",
		"gemini-3.5-flash":      "gemini",
		"gemini-3.1-flash-lite": "gemini",
	} {
		if got := llmProvider(model); got != want {
			t.Errorf("llmProvider(%q) = %q, want %q", model, got, want)
		}
	}
}
