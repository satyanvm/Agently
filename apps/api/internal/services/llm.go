package services

// llm.go is the control-plane model client used by the prompt compiler. There is
// deliberately one provider and one credential: ANTHROPIC_API_KEY.
//
// It used to carry a hard rule that it must NEVER be on the critical path — every
// caller fell back to deterministic planning when it errored. That rule is gone. A
// prompt the model could not compile is a FAILURE, reported with its reason, not a
// fixed trigger→research→report graph wearing the user's prompt as a label. Every
// error returned from here is expected to reach the user verbatim.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// llmMsg is one conversation turn for multi-message calls (the reduce phase's
// validate→repair loop replays the model's own graph plus the validator errors).
type llmMsg struct {
	Role    string // "user" | "assistant"
	Content string
}

const jsonOnly = "\nRespond with a single JSON object and nothing else."

// Compilation model tiers. Map wants cheap+fast over a large index; reduce wants
// the strongest available, because graph authorship is the hard part. Both
// env-overridable — but note the tiers are not interchangeable: see
// supportsAdaptiveThinking below.
func mapModel() string    { return envOr("PLANNER_MAP_MODEL", "claude-haiku-4-5") }
func reduceModel() string { return envOr("PLANNER_MODEL", "claude-opus-4-8") }

var (
	anthropicOnce   sync.Once
	anthropicClient anthropic.Client
)

// RequireAnthropicKey is the boot preflight and the guard tests use to tell a
// configuration error apart from a provider failure.
func RequireAnthropicKey() error {
	if strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY")) == "" {
		return errors.New("ANTHROPIC_API_KEY is required — the planner compiles every prompt " +
			"into a graph with Claude (route, map, and reduce). Set it in .env")
	}
	return nil
}

func claude() *anthropic.Client {
	anthropicOnce.Do(func() {
		anthropicClient = anthropic.NewClient(option.WithAPIKey(os.Getenv("ANTHROPIC_API_KEY")))
	})
	return &anthropicClient
}

// supportsAdaptiveThinking reports whether a model accepts
// `thinking: {type: "adaptive"}`. Opus 4.x and Sonnet 4.6 do; Haiku 4.5 rejects it
// with a 400, so the route/map calls must not send it. Derived from the model id
// rather than passed in, so PLANNER_MODEL / PLANNER_MAP_MODEL overrides still
// produce valid requests whichever tier they name.
func supportsAdaptiveThinking(model string) bool {
	return strings.HasPrefix(model, "claude-opus-4-") ||
		strings.HasPrefix(model, "claude-sonnet-4-6") ||
		strings.HasPrefix(model, "claude-fable-")
}

// planLLMWith sends a JSON-only completion using the requested model. The model
// argument is intentionally honoured: map uses the small model, reduce the strong
// one. (Its predecessor accepted a model and then silently ignored it, running
// whatever provider the environment happened to hold — which defeated the whole
// point of having tiers.)
//
// Note there is no temperature: Opus 4.7+ rejects temperature/top_p/top_k with a 400.
func planLLMWith(ctx context.Context, model string, maxTokens int, timeout time.Duration, system string, msgs []llmMsg) (string, error) {
	if err := RequireAnthropicKey(); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	messages := make([]anthropic.MessageParam, 0, len(msgs))
	for _, m := range msgs {
		block := anthropic.NewTextBlock(m.Content)
		if m.Role == "assistant" {
			messages = append(messages, anthropic.NewAssistantMessage(block))
			continue
		}
		messages = append(messages, anthropic.NewUserMessage(block))
	}

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(model),
		MaxTokens: int64(maxTokens),
		System:    []anthropic.TextBlockParam{{Text: system + jsonOnly}},
		Messages:  messages,
	}
	if supportsAdaptiveThinking(model) {
		// Graph authorship is the hard part; let the model decide how much to
		// think. Thinking tokens draw from MaxTokens, so callers budget for both.
		params.Thinking = anthropic.ThinkingConfigParamUnion{
			OfAdaptive: &anthropic.ThinkingConfigAdaptiveParam{},
		}
	}

	msg, err := claude().Messages.New(ctx, params)
	if err != nil {
		return "", describeAnthropicError(model, err)
	}

	// Skip thinking blocks — only text blocks carry the JSON we asked for.
	var b strings.Builder
	for _, block := range msg.Content {
		if text, ok := block.AsAny().(anthropic.TextBlock); ok {
			b.WriteString(text.Text)
		}
	}
	if msg.StopReason == anthropic.StopReasonMaxTokens {
		// The JSON is truncated. Say so plainly rather than letting the caller
		// report a confusing parse error several frames down.
		return "", fmt.Errorf("%s hit the %d-token output cap before finishing its JSON", model, maxTokens)
	}
	out := b.String()
	if strings.TrimSpace(out) == "" {
		return "", fmt.Errorf("%s returned no text (stop reason %q)", model, msg.StopReason)
	}
	return out, nil
}

// describeAnthropicError turns an SDK error into a message worth showing a user:
// which model, what the API said, and the request id to quote in a support thread.
func describeAnthropicError(model string, err error) error {
	var apiErr *anthropic.Error
	if errors.As(err, &apiErr) {
		if apiErr.RequestID != "" {
			return fmt.Errorf("%s: anthropic %d (request %s): %s",
				model, apiErr.StatusCode, apiErr.RequestID, apiErr.Error())
		}
		return fmt.Errorf("%s: anthropic %d: %s", model, apiErr.StatusCode, apiErr.Error())
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%s: timed out", model)
	}
	return fmt.Errorf("%s: %w", model, err)
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
