package services

// llm.go is a deliberately tiny model client for the CONTROL PLANE (the API). The
// worker has its own richer llm package (apps/worker/internal/llm) for executing
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

// planLLM asks a model to return ONLY a JSON object answering `system`+`user`. It
// returns the raw JSON text. Anthropic if ANTHROPIC_API_KEY is set, else OpenAI if
// OPENAI_API_KEY is set, else an error (caller falls back to deterministic). A short
// timeout keeps workflow creation snappy even when the model is slow/unreachable.
func planLLM(ctx context.Context, system, user string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	client := &http.Client{Timeout: 25 * time.Second}

	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		model := envOr("ANTHROPIC_MODEL", "claude-sonnet-4-6")
		return anthropicJSON(ctx, client, key, model, system, user)
	}
	if key := os.Getenv("OPENAI_API_KEY"); key != "" {
		model := envOr("OPENAI_MODEL", "gpt-4o")
		return openaiJSON(ctx, client, key, model, system, user)
	}
	return "", fmt.Errorf("no LLM key set")
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func anthropicJSON(ctx context.Context, c *http.Client, key, model, system, user string) (string, error) {
	body, _ := json.Marshal(map[string]any{
		"model":      model,
		"max_tokens": 1024,
		"system":     system + "\nRespond with a single JSON object and nothing else.",
		"messages":   []map[string]string{{"role": "user", "content": user}},
	})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.anthropic.com/v1/messages", bytes.NewReader(body))
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-api-key", key)
	req.Header.Set("anthropic-version", "2023-06-01")
	resp, err := c.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("anthropic status %d", resp.StatusCode)
	}
	var parsed struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", err
	}
	var b strings.Builder
	for _, c := range parsed.Content {
		b.WriteString(c.Text)
	}
	return b.String(), nil
}

func openaiJSON(ctx context.Context, c *http.Client, key, model, system, user string) (string, error) {
	body, _ := json.Marshal(map[string]any{
		"model":           model,
		"max_tokens":      1024,
		"response_format": map[string]string{"type": "json_object"},
		"messages": []map[string]string{
			{"role": "system", "content": system + "\nRespond with a single JSON object and nothing else."},
			{"role": "user", "content": user},
		},
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
