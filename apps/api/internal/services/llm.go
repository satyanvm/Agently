package services

// llm.go is the control-plane model client used by the prompt compiler. Any of
// three providers can serve either tier (ollama/<model> runs locally, see
// planLLMOllama); the original split, tiered by difficulty, was:
//
//   ROUTE + MAP  — Gemini (GEMINI_API_KEY). Cheap classification over the cluster
//                  directory and per-cluster node indexes. The same key also
//                  serves the embedding prefilter (piecesembed.go).
//   REDUCE       — Kimi (MOONSHOT_API_KEY, OpenAI-compatible endpoint). Graph
//                  authorship is the hard part, so it gets the strongest model.
//
// The provider is inferred from the model name: kimi-*/moonshot-* goes to
// Moonshot, everything else to Gemini — so either tier can be re-pointed with
// PLANNER_MAP_MODEL / PLANNER_MODEL and the matching key, nothing else.
//
// It used to carry a hard rule that it must NEVER be on the critical path — every
// caller fell back to deterministic planning when it errored. That rule is gone. A
// prompt the model could not compile is a FAILURE, reported with its reason, not a
// fixed trigger→research→report graph wearing the user's prompt as a label. Every
// error returned from here is expected to reach the user verbatim.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
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
// env-overridable.
func mapModel() string    { return envOr("PLANNER_MAP_MODEL", "gemini-3.1-flash-lite") }
func reduceModel() string { return envOr("PLANNER_MODEL", "kimi-k3") }

// isMoonshotModel routes a model id to its provider: Kimi models run on the
// Moonshot API (OpenAI-compatible), everything else on Gemini.
func isMoonshotModel(model string) bool {
	l := strings.ToLower(model)
	return strings.HasPrefix(l, "kimi-") || strings.HasPrefix(l, "moonshot-")
}

func moonshotKey() string {
	if k := strings.TrimSpace(os.Getenv("MOONSHOT_API_KEY")); k != "" {
		return k
	}
	return strings.TrimSpace(os.Getenv("KIMI_API_KEY"))
}

func moonshotBaseURL() string { return envOr("MOONSHOT_BASE_URL", "https://api.moonshot.ai/v1") }

// RequireGeminiKey is the boot preflight and the guard tests use to tell a
// configuration error apart from a provider failure.
func RequireGeminiKey() error {
	if strings.TrimSpace(os.Getenv("GEMINI_API_KEY")) == "" {
		return errors.New("GEMINI_API_KEY is required — the planner's route and map " +
			"phases run on Gemini, and the piece router prefilters ~700 clusters " +
			"through Gemini embeddings. Set it in .env")
	}
	return nil
}

// RequireMoonshotKey is the same guard for the reduce phase's Kimi model.
func RequireMoonshotKey() error {
	if moonshotKey() == "" {
		return errors.New("MOONSHOT_API_KEY is required — the planner's reduce phase " +
			"authors every graph with Kimi (PLANNER_MODEL, default kimi-k3). Set it in .env")
	}
	return nil
}

// RequirePlannerKeys is the boot preflight for every credential the compiler can
// touch: Gemini always (route, map, embeddings), Moonshot when either tier is
// pointed at a Kimi model.
func RequirePlannerKeys() error {
	for _, model := range []string{mapModel(), reduceModel()} {
		switch llmProvider(model) {
		case "gemini":
			if err := RequireGeminiKey(); err != nil {
				return err
			}
		case "moonshot":
			if err := RequireMoonshotKey(); err != nil {
				return err
			}
		}
	}
	return RequireEmbedKey()
}

// llmProvider routes a model id to its provider: ollama/<model> runs on a local
// Ollama server (no key, no quota), kimi-*/moonshot-* on Moonshot, everything
// else on Gemini.
func llmProvider(model string) string {
	switch {
	case strings.HasPrefix(model, "ollama/"):
		return "ollama"
	case isMoonshotModel(model):
		return "moonshot"
	}
	return "gemini"
}

// planLLMWith sends a JSON-only completion using the requested model. The model
// argument is intentionally honoured: map uses the small model, reduce the strong
// one, and the provider follows the model name (isMoonshotModel). (Its
// predecessor accepted a model and then silently ignored it, running whatever
// provider the environment happened to hold — which defeated the whole point of
// having tiers.)
func planLLMWith(ctx context.Context, model string, maxTokens int, timeout time.Duration, system string, msgs []llmMsg) (string, error) {
	switch llmProvider(model) {
	case "ollama":
		return planLLMOllama(ctx, model, maxTokens, timeout, system, msgs)
	case "moonshot":
		return planLLMMoonshot(ctx, model, maxTokens, timeout, system, msgs)
	}
	return planLLMGemini(ctx, model, maxTokens, timeout, system, msgs)
}

// ollamaNumCtx is the context window requested per call. Ollama's default is a
// few thousand tokens and it truncates longer prompts SILENTLY — the router's
// cluster directory alone is ~9.5k tokens — so it is always set explicitly.
// 16k covers the largest prompt (router; reduce is ~6.5k plus its output) while
// keeping an 8B model's KV cache small enough for a 16 GB laptop.
func ollamaNumCtx() int {
	if n, err := strconv.Atoi(os.Getenv("OLLAMA_NUM_CTX")); err == nil && n > 0 {
		return n
	}
	return 16384
}

// planLLMOllama is the local path: Ollama's native /api/chat, which (unlike its
// OpenAI-compatible endpoint) takes num_ctx and a think switch. format=json
// constrains decoding to valid JSON. Thinking is off: on a laptop-sized model it
// multiplies latency for little gain on selection tasks.
//
// Local inference has no rate limit, only compute — so the per-phase timeouts,
// sized for a remote API, are stretched: map calls queue behind each other on
// one GPU and a large reduce can take minutes.
func planLLMOllama(ctx context.Context, model string, maxTokens int, timeout time.Duration, system string, msgs []llmMsg) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, max(timeout, 10*time.Minute))
	defer cancel()

	chat := make([]map[string]string, 0, len(msgs)+1)
	chat = append(chat, map[string]string{"role": "system", "content": system + jsonOnly})
	for _, m := range msgs {
		role := "user"
		if m.Role == "assistant" {
			role = "assistant"
		}
		chat = append(chat, map[string]string{"role": role, "content": m.Content})
	}
	body, _ := json.Marshal(map[string]any{
		"model":    strings.TrimPrefix(model, "ollama/"),
		"messages": chat,
		"stream":   false,
		"format":   "json",
		"think":    false,
		"options": map[string]any{
			"num_ctx":     ollamaNumCtx(),
			"num_predict": maxTokens,
		},
	})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, ollamaHost()+"/api/chat", bytes.NewReader(body))
	req.Header.Set("content-type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("%s: timed out: %w", model, errTimedOut)
		}
		return "", fmt.Errorf("%s (is `ollama serve` running at %s?): %w", model, ollamaHost(), err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s: ollama %d: %s", model, resp.StatusCode, truncateStr(string(raw), 300))
	}
	var parsed struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		DoneReason string `json:"done_reason"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", fmt.Errorf("%s: unparseable response: %w", model, err)
	}
	if parsed.DoneReason == "length" {
		return "", fmt.Errorf("%s hit the %d-token output cap before finishing its JSON: %w", model, maxTokens, errOutputCapped)
	}
	if strings.TrimSpace(parsed.Message.Content) == "" {
		return "", fmt.Errorf("%s returned no text (done reason %q)", model, parsed.DoneReason)
	}
	return parsed.Message.Content, nil
}

// planLLMGemini is the Gemini generateContent path. Gemini carries the system
// prompt in system_instruction, maps the assistant role to "model", and pins JSON
// output with responseMimeType — the same contract the OpenAI-era response_format
// provided.
func planLLMGemini(ctx context.Context, model string, maxTokens int, timeout time.Duration, system string, msgs []llmMsg) (string, error) {
	if err := RequireGeminiKey(); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	budget := geminiThinkingBudget(model, maxTokens)
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
	// Thinking tokens draw from maxOutputTokens, so the answer's budget is added
	// on top of the thinking allowance rather than shared with it. Only the 2.5
	// family takes an explicit token budget (3.x rejects budget 0 and Gemma
	// rejects thinkingConfig entirely); other models think as they see fit, so
	// they get a fixed ceiling of headroom instead.
	thinking := 8192
	is25 := strings.HasPrefix(model, "gemini-2.5-")
	if is25 {
		thinking = budget
	}
	genCfg := map[string]any{
		"maxOutputTokens":  maxTokens + thinking,
		"responseMimeType": "application/json",
	}
	if is25 {
		genCfg["thinkingConfig"] = map[string]any{"thinkingBudget": budget}
	}
	body, _ := json.Marshal(map[string]any{
		"system_instruction": map[string]any{
			"parts": []map[string]string{{"text": system + jsonOnly}},
		},
		"contents":         contents,
		"generationConfig": genCfg,
	})
	raw, err := geminiPost(ctx, model, body)
	if err != nil {
		return "", err
	}
	var parsed struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
			FinishReason string `json:"finishReason"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", fmt.Errorf("%s: unparseable response: %w", model, err)
	}
	if len(parsed.Candidates) == 0 {
		return "", fmt.Errorf("%s: gemini returned no candidates: %s", model, truncateStr(string(raw), 300))
	}
	cand := parsed.Candidates[0]
	if cand.FinishReason == "MAX_TOKENS" {
		// The JSON is truncated. Say so plainly rather than letting the caller
		// report a confusing parse error several frames down.
		return "", fmt.Errorf("%s hit the %d-token output cap before finishing its JSON: %w", model, maxTokens, errOutputCapped)
	}
	var b strings.Builder
	for _, p := range cand.Content.Parts {
		b.WriteString(p.Text)
	}
	out := b.String()
	if strings.TrimSpace(out) == "" {
		return "", fmt.Errorf("%s returned no text (finish reason %q)", model, cand.FinishReason)
	}
	return out, nil
}

// geminiPost sends one generateContent request, riding out per-minute rate
// limits: a 429 is retried after the delay Gemini suggests, for as long as the
// caller's deadline allows. Free-tier keys allow as few as 5 requests/minute per
// model, and the map phase alone fans out a dozen calls — without this a single
// prompt trips the limit. A limit that outlasts the deadline is still an error.
func geminiPost(ctx context.Context, model string, body []byte) ([]byte, error) {
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent", model)
	transient := 0
	for attempt := 0; ; attempt++ {
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		req.Header.Set("content-type", "application/json")
		req.Header.Set("x-goog-api-key", os.Getenv("GEMINI_API_KEY"))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
				return nil, fmt.Errorf("%s: timed out: %w", model, errTimedOut)
			}
			// A dropped connection (reset, EOF, TLS hiccup) on a long call is
			// transient: retry a few times before giving up.
			if transient < 3 && sleepCtx(ctx, time.Duration(transient+1)*2*time.Second) {
				transient++
				continue
			}
			return nil, fmt.Errorf("%s: %w", model, err)
		}
		raw, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK && readErr == nil {
			return raw, nil
		}
		if readErr != nil || resp.StatusCode == http.StatusInternalServerError ||
			resp.StatusCode == http.StatusBadGateway || resp.StatusCode == http.StatusGatewayTimeout {
			if transient < 3 && sleepCtx(ctx, time.Duration(transient+1)*2*time.Second) {
				transient++
				continue
			}
			if readErr != nil {
				return nil, fmt.Errorf("%s: reading response: %w", model, readErr)
			}
		}
		if resp.StatusCode == http.StatusTooManyRequests && geminiDailyQuota(raw) {
			// A per-day cap does not lift in seconds: waiting out the
			// suggested delay only burns the caller's deadline.
			return nil, fmt.Errorf("%s: gemini daily request quota used up for this API key (resets at midnight Pacific; raise it by enabling billing, or set PLANNER_FALLBACK_MODEL): %w", model, errQuotaExhausted)
		}
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable {
			wait := geminiRetryDelay(raw, attempt)
			if dl, ok := ctx.Deadline(); !ok || time.Until(dl) > wait {
				select {
				case <-time.After(wait):
					continue
				case <-ctx.Done():
				}
			}
		}
		return nil, fmt.Errorf("%s: gemini %d: %s", model, resp.StatusCode, truncateStr(string(raw), 300))
	}
}

// sleepCtx waits d unless ctx ends first or would end before d elapses;
// it reports whether the full wait happened.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	if dl, ok := ctx.Deadline(); ok && time.Until(dl) <= d {
		return false
	}
	select {
	case <-time.After(d):
		return true
	case <-ctx.Done():
		return false
	}
}

// errQuotaExhausted marks a provider quota that will not reset within a call.
var errQuotaExhausted = errors.New("quota exhausted")

// geminiDailyQuota reports whether a 429 body names a per-day quota.
func geminiDailyQuota(raw []byte) bool {
	var e struct {
		Error struct {
			Details []struct {
				Violations []struct {
					QuotaID string `json:"quotaId"`
				} `json:"violations"`
			} `json:"details"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &e) != nil {
		return false
	}
	for _, d := range e.Error.Details {
		for _, v := range d.Violations {
			if strings.Contains(v.QuotaID, "PerDay") {
				return true
			}
		}
	}
	return false
}

// geminiRetryDelay reads the RetryInfo delay ("17s") Gemini attaches to a 429,
// falling back to exponential backoff when it is absent.
func geminiRetryDelay(raw []byte, attempt int) time.Duration {
	var e struct {
		Error struct {
			Details []struct {
				RetryDelay string `json:"retryDelay"`
			} `json:"details"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &e) == nil {
		for _, d := range e.Error.Details {
			if dur, err := time.ParseDuration(d.RetryDelay); err == nil && d.RetryDelay != "" {
				return max(dur, time.Second) + 500*time.Millisecond
			}
		}
	}
	return min(time.Second<<attempt, 20*time.Second)
}

// geminiThinkingBudget sizes Gemini 2.5's thinking separately from the answer.
// Route and map (small answers) are classification over a list: thinking adds
// latency and nothing else, so it is off — except on pro, which cannot disable
// it and takes its 128-token minimum. Reduce (graph authorship, big answers) is
// the hard part and gets real room to think.
func geminiThinkingBudget(model string, maxTokens int) int {
	if maxTokens < 4096 {
		if strings.Contains(model, "-pro") {
			return 128
		}
		return 0
	}
	return 8192
}

// planLLMMoonshot is the Kimi path: OpenAI-compatible chat completions against
// api.moonshot.ai. The contract mirrors the Gemini one — the system message
// carries the instruction plus the JSON-only demand, response_format pins object
// output, and a truncated answer is reported as the token cap it is, not a parse
// error several frames down.
func planLLMMoonshot(ctx context.Context, model string, maxTokens int, timeout time.Duration, system string, msgs []llmMsg) (string, error) {
	if err := RequireMoonshotKey(); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	oaiMsgs := make([]map[string]string, 0, len(msgs)+1)
	oaiMsgs = append(oaiMsgs, map[string]string{"role": "system", "content": system + jsonOnly})
	for _, m := range msgs {
		role := "user"
		if m.Role == "assistant" {
			role = "assistant"
		}
		oaiMsgs = append(oaiMsgs, map[string]string{"role": role, "content": m.Content})
	}
	body, _ := json.Marshal(map[string]any{
		"model":           model,
		"messages":        oaiMsgs,
		"max_tokens":      maxTokens,
		"response_format": map[string]string{"type": "json_object"},
	})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, moonshotBaseURL()+"/chat/completions", bytes.NewReader(body))
	req.Header.Set("content-type", "application/json")
	req.Header.Set("authorization", "Bearer "+moonshotKey())
	resp, err := (&http.Client{Timeout: timeout}).Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return "", fmt.Errorf("%s: timed out: %w", model, errTimedOut)
		}
		return "", fmt.Errorf("%s: %w", model, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s: moonshot %d: %s", model, resp.StatusCode, truncateStr(string(raw), 300))
	}
	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", fmt.Errorf("%s: unparseable response: %w", model, err)
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("%s: moonshot returned no choices: %s", model, truncateStr(string(raw), 300))
	}
	choice := parsed.Choices[0]
	if choice.FinishReason == "length" {
		return "", fmt.Errorf("%s hit the %d-token output cap before finishing its JSON: %w", model, maxTokens, errOutputCapped)
	}
	out := choice.Message.Content
	if strings.TrimSpace(out) == "" {
		return "", fmt.Errorf("%s returned no text (finish reason %q)", model, choice.FinishReason)
	}
	return out, nil
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
