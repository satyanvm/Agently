package services

// llm.go is a deliberately tiny model client for the CONTROL PLANE (the API). The
// data plane (the Temporal reasoner) has its own richer LLM layer for executing
// runs; the API needs an LLM only for ONE thing: turning a natural-language prompt
// into a structured workflow plan at create time. So this is a minimal,
// single-purpose JSON-completion call with a hard requirement: it must NEVER be on
// the critical path — every caller falls back to deterministic planning if it
// errors, returns junk, or no key is set. Keep it small.

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

// llmMsg is one conversation turn for multi-message calls (the reduce phase's
// validate→repair loop replays the model's own graph plus the validator errors).
type llmMsg struct {
	Role    string // "user" | "assistant"
	Content string
}

// planLLM asks a model to return ONLY a JSON object answering `system`+`user`. It
// returns the raw JSON text. Gemini if GEMINI_API_KEY is set, else OpenAI if
// OPENAI_API_KEY is set, else an error (caller falls back to deterministic). A short
// timeout keeps workflow creation snappy even when the model is slow/unreachable.
func planLLM(ctx context.Context, system, user string) (string, error) {
	return planLLMWith(ctx, "", 1024, 25*time.Second, system, []llmMsg{{Role: "user", Content: user}})
}

// planLLMWith is the general form: explicit model (empty = env default), token
// budget, timeout, and a message history. Same provider chain and same hard rule
// as planLLM: never on the critical path — every caller has a deterministic floor.
// Gemini is preferred (the run-time reasoner also runs on Gemini), then OpenAI.
func planLLMWith(ctx context.Context, model string, maxTokens int, timeout time.Duration, system string, msgs []llmMsg) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	client := &http.Client{Timeout: timeout}

	if key := envOr("GEMINI_API_KEY", os.Getenv("GOOGLE_API_KEY")); key != "" {
		// A caller-pinned model may name an Anthropic tier; on Gemini we use the
		// env-configured model instead so the request is always valid.
		return geminiJSON(ctx, client, key, envOr("GEMINI_MODEL", "gemini-2.5-flash"), maxTokens, system, msgs)
	}
	if key := os.Getenv("OPENAI_API_KEY"); key != "" {
		return openaiJSON(ctx, client, key, envOr("OPENAI_MODEL", "gpt-4o"), maxTokens, system, msgs)
	}
	return "", fmt.Errorf("no LLM key set")
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// geminiJSON calls Google's Generative Language API directly (no proxy). Gemini uses
// "model" for the assistant role and carries the system prompt in system_instruction;
// responseMimeType pins JSON output the same way the OpenAI path uses response_format.
func geminiJSON(ctx context.Context, c *http.Client, key, model string, maxTokens int, system string, msgs []llmMsg) (string, error) {
	contents := make([]map[string]any, 0, len(msgs))
	for _, m := range msgs {
		role := "user"
		if m.Role == "assistant" {
			role = "model"
		}
		contents = append(contents, map[string]any{
			"role":  role,
			"parts": []map[string]string{{"text": m.Content}},
		})
	}
	body, _ := json.Marshal(map[string]any{
		"system_instruction": map[string]any{
			"parts": []map[string]string{{"text": system + "\nRespond with a single JSON object and nothing else."}},
		},
		"contents": contents,
		"generationConfig": map[string]any{
			"maxOutputTokens":  maxTokens,
			"responseMimeType": "application/json",
			"temperature":      0.2,
		},
	})
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent", model)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-goog-api-key", key)
	resp, err := c.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("gemini status %d", resp.StatusCode)
	}
	var parsed struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", err
	}
	if len(parsed.Candidates) == 0 {
		return "", fmt.Errorf("gemini: empty candidates")
	}
	var b strings.Builder
	for _, p := range parsed.Candidates[0].Content.Parts {
		b.WriteString(p.Text)
	}
	return b.String(), nil
}

func openaiJSON(ctx context.Context, c *http.Client, key, model string, maxTokens int, system string, msgs []llmMsg) (string, error) {
	messages := make([]map[string]string, 0, len(msgs)+1)
	messages = append(messages, map[string]string{"role": "system", "content": system + "\nRespond with a single JSON object and nothing else."})
	for _, m := range msgs {
		messages = append(messages, map[string]string{"role": m.Role, "content": m.Content})
	}
	body, _ := json.Marshal(map[string]any{
		"model":           model,
		"max_tokens":      maxTokens,
		"response_format": map[string]string{"type": "json_object"},
		"messages":        messages,
	})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.openai.com/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("content-type", "application/json")
	req.Header.Set("authorization", "Bearer "+key)
	resp, err := c.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("openai status %d", resp.StatusCode)
	}
	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", err
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("openai: empty choices")
	}
	return parsed.Choices[0].Message.Content, nil
}
