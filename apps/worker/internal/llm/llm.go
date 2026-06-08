// Package llm is the worker's model-calling layer, behind a provider interface so
// the agent runtime doesn't care which model it talks to. Anthropic when
// ANTHROPIC_API_KEY is set; a deterministic mock otherwise — so the whole system
// runs and is testable with no key and no network.
//
// This is the same "interface + swappable implementation" seam as the storage
// layer. The agent loop depends on Provider; we can add OpenAI, a local model,
// etc., without touching the loop.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Message is one turn in a chat. Role is "user" or "assistant".
type Message struct {
	Role    string
	Content string
}

// Result is what a completion returns: the text plus token accounting (used for
// per-run cost, which matters for the product's budget story).
type Result struct {
	Text      string
	TokensIn  int
	TokensOut int
	Model     string
}

// Provider is the seam. One method: turn a system prompt + messages into a Result.
type Provider interface {
	Complete(ctx context.Context, system string, msgs []Message) (Result, error)
	Name() string
}

// New picks a provider from the environment: real Anthropic if a key is present,
// otherwise the mock. Returning the interface keeps callers provider-agnostic.
func New() Provider {
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		model := os.Getenv("ANTHROPIC_MODEL")
		if model == "" {
			model = "claude-sonnet-4-6"
		}
		return &anthropic{key: key, model: model, http: &http.Client{Timeout: 120 * time.Second}}
	}
	return &mock{}
}

/* ------------------------------- Anthropic ------------------------------- */

type anthropic struct {
	key   string
	model string
	http  *http.Client
}

func (a *anthropic) Name() string { return "anthropic:" + a.model }

// Complete calls the Anthropic Messages API. Deliberately minimal — no tool use
// yet (that arrives with the browser layer); just a single text completion.
func (a *anthropic) Complete(ctx context.Context, system string, msgs []Message) (Result, error) {
	type apiMsg struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	body := map[string]any{
		"model":      a.model,
		"max_tokens": 1024,
		"system":     system,
		"messages": func() []apiMsg {
			out := make([]apiMsg, len(msgs))
			for i, m := range msgs {
				out[i] = apiMsg{Role: m.Role, Content: m.Content}
			}
			return out
		}(),
	}
	raw, _ := json.Marshal(body)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.anthropic.com/v1/messages", bytes.NewReader(raw))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-api-key", a.key)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := a.http.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("anthropic request: %w", err)
	}
	defer resp.Body.Close()
	payload, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return Result{}, fmt.Errorf("anthropic status %d: %s", resp.StatusCode, truncate(string(payload), 300))
	}

	var parsed struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(payload, &parsed); err != nil {
		return Result{}, fmt.Errorf("anthropic decode: %w", err)
	}
	var text strings.Builder
	for _, c := range parsed.Content {
		text.WriteString(c.Text)
	}
	return Result{
		Text:      text.String(),
		TokensIn:  parsed.Usage.InputTokens,
		TokensOut: parsed.Usage.OutputTokens,
		Model:     a.model,
	}, nil
}

/* --------------------------------- Mock ---------------------------------- */

// mock returns deterministic, plausible output with fake-but-sane token counts,
// so a run produces real-looking logs/artifacts and cost with no API key. It even
// pauses briefly to mimic latency, so live streaming is visible in the UI.
type mock struct{}

func (m *mock) Name() string { return "mock" }

func (m *mock) Complete(ctx context.Context, system string, msgs []Message) (Result, error) {
	select {
	case <-ctx.Done():
		return Result{}, ctx.Err()
	case <-time.After(900 * time.Millisecond):
	}
	last := ""
	if len(msgs) > 0 {
		last = msgs[len(msgs)-1].Content
	}
	// If the prompt carries real fetched content (the fetcher/editor agents append
	// it under these markers), echo the actual items back so the mock digest
	// reflects REAL data — not just boilerplate. This proves content flows through
	// even without an LLM key; a real model would synthesize rather than echo.
	var text string
	if bullets := extractBullets(last); bullets != "" {
		text = "Here is a structured digest of the fetched items:\n\n" + bullets +
			"\nThese are the most notable items; full synthesis requires a live LLM key."
	} else {
		text = fmt.Sprintf(
			"Based on the objective, here is my analysis.\n\nTask: %s\n\n"+
				"I considered the available information and reached a structured conclusion.\n"+
				"Recommendation: proceed with the plan as outlined.",
			truncate(strings.TrimSpace(last), 160))
	}
	in := (len(system) + len(last)) / 4
	out := len(text) / 4
	return Result{Text: text, TokensIn: in, TokensOut: out, Model: "mock"}, nil
}

/* -------------------------------- helpers -------------------------------- */

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// extractBullets pulls the "- ..." lines (fetched items / upstream summaries) out
// of a prompt, returning up to ~12 of them. Lets the mock reflect real content.
func extractBullets(prompt string) string {
	var out []string
	for _, line := range strings.Split(prompt, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "- ") || strings.HasPrefix(t, "• ") {
			out = append(out, t)
			if len(out) >= 12 {
				break
			}
		}
	}
	return strings.Join(out, "\n")
}

// EstimateCostUSD is a rough blended price so the product's cost number is
// non-zero and directional. Real per-model pricing can replace this later.
func EstimateCostUSD(tokensIn, tokensOut int) float64 {
	const inPerM = 3.0   // $/M input tokens (blended placeholder)
	const outPerM = 15.0 // $/M output tokens
	return (float64(tokensIn)*inPerM + float64(tokensOut)*outPerM) / 1_000_000.0
}
