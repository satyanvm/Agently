package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/agently/worker/internal/browser"
	"github.com/agently/worker/internal/llm"
	"github.com/agently/worker/internal/queue"
	"github.com/agently/worker/internal/sources"
)

// dag.go executes a run's AGENT GRAPH (the run_agents DAG) instead of a fixed
// linear plan. This is the multi-agent engine: it walks the graph in dependency
// order, runs each ready agent against the LLM, threads upstream outputs into
// downstream prompts, and records hand-offs as messages.
//
// Chunk 8: the ready frontier runs CONCURRENTLY (bounded pool). The flagship's 3
// scouts now execute in parallel; a fan-in node still waits for ALL its deps.
// Concurrency means shared state (the log seq counter + done/failed/outputs maps)
// is touched by multiple goroutines, so it lives behind a mutex in dagState.

// maxConcurrentAgents caps how many agents run at once per run. Small and fixed
// for the demo; a real system would tie this to model rate limits / cost budgets.
const maxConcurrentAgents = 4

// dagState is the mutex-guarded shared state for one DAG execution. Centralizing
// it means every goroutine allocates log seqs and reads/writes completion through
// one lock — no races, no duplicate seqs (which the unique(run_id,seq) index would
// otherwise reject).
type dagState struct {
	mu      sync.Mutex
	seq     int
	done    map[string]bool
	failed  map[string]bool
	outputs map[string]string // agentID -> summary, fed to downstream agents
}

// nextSeq atomically returns the current seq and advances it.
func (s *dagState) nextSeq() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := s.seq
	s.seq++
	return n
}

func (s *dagState) markDone(id, summary string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.done[id] = true
	s.outputs[id] = summary
}

func (s *dagState) markFailed(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failed[id] = true
}

// snapshot returns copies of done/failed/outputs for computing the frontier
// without holding the lock during the (slower) agent runs.
func (s *dagState) snapshot() (done, failed map[string]bool, outputs map[string]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	done = make(map[string]bool, len(s.done))
	failed = make(map[string]bool, len(s.failed))
	outputs = make(map[string]string, len(s.outputs))
	for k, v := range s.done {
		done[k] = v
	}
	for k, v := range s.failed {
		failed[k] = v
	}
	for k, v := range s.outputs {
		outputs[k] = v
	}
	return
}

// RunDAG executes the agent graph for a run, running each ready frontier
// concurrently. Returns true iff every agent succeeded. Resume-safe: agents
// already 'succeeded' from a prior attempt are skipped and their summaries remain
// available to downstream agents.
func (rt *Runtime) RunDAG(ctx context.Context, runID string, agents []queue.GraphAgent, input map[string]any) bool {
	start := time.Now()
	firstSeq, err := rt.q.NextLogSeq(ctx, runID)
	if err != nil {
		rt.log.Error("dag: log seq", "runId", runID, "error", err.Error())
		return false
	}

	st := &dagState{
		seq:     firstSeq,
		done:    map[string]bool{},
		failed:  map[string]bool{},
		outputs: map[string]string{},
	}
	byID := make(map[string]queue.GraphAgent, len(agents))
	total := len(agents)
	completedAtStart := 0
	for _, a := range agents {
		byID[a.ID] = a
		if a.Status == "succeeded" {
			st.done[a.ID] = true
			st.outputs[a.ID] = a.Name + " (completed earlier)"
			completedAtStart++
		}
	}

	if completedAtStart > 0 {
		rt.emitSeq(ctx, runID, st, start, "info", "system", "orchestrator",
			fmt.Sprintf("Resuming agent graph: %d of %d agents already done", completedAtStart, total), false, nil)
	} else {
		rt.emitSeq(ctx, runID, st, start, "info", "system", "orchestrator",
			fmt.Sprintf("Executing agent graph — %d agents, up to %d in parallel", total, maxConcurrentAgents), false, nil)
	}

	processed := completedAtStart
	for processed < total {
		select {
		case <-ctx.Done():
			return false
		default:
		}
		done, failed, _ := st.snapshot()
		ready := readyAgents(agents, done, failed)
		if len(ready) == 0 {
			rt.blockRemaining(ctx, runID, rt.workerID, agents, done, failed)
			rt.log.Error("dag: no ready agents but graph incomplete", "runId", runID, "done", processed, "total", total)
			return false
		}
		if len(ready) > 1 {
			rt.emitSeq(ctx, runID, st, start, "info", "system", "orchestrator",
				fmt.Sprintf("%d agents ready — running in parallel", len(ready)), false, nil)
		}

		// Run the whole ready frontier concurrently, bounded by a semaphore.
		var wg sync.WaitGroup
		sem := make(chan struct{}, maxConcurrentAgents)
		for _, ag := range ready {
			wg.Add(1)
			go func(ag queue.GraphAgent) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				ok, summary := rt.runAgent(ctx, runID, ag, byID, st, start, input)
				if ok {
					st.markDone(ag.ID, summary)
				} else if ctx.Err() == nil {
					st.markFailed(ag.ID)
				}
			}(ag)
		}
		wg.Wait()

		if ctx.Err() != nil {
			return false
		}
		// Recount from authoritative state.
		done, failed, _ = st.snapshot()
		processed = len(done) + len(failed)
		if len(failed) > 0 {
			// A frontier agent failed; block whatever can no longer run, then stop.
			rt.blockRemaining(ctx, runID, rt.workerID, agents, done, failed)
			return false
		}
	}

	rt.emitSeq(ctx, runID, st, start, "success", "system", "orchestrator",
		"Agent graph complete — all agents succeeded", false, nil)
	_, _, outputs := st.snapshot()
	rt.q.AddArtifact(ctx, runID, "result.md", "report", "agent-graph", sinkSummary(agents, byID, outputs))
	return true
}

// runAgent executes a single agent node. Returns (ok, summary). All log emission
// goes through st.nextSeq so concurrent agents never collide on seq.
func (rt *Runtime) runAgent(ctx context.Context, runID string, ag queue.GraphAgent, byID map[string]queue.GraphAgent, st *dagState, start time.Time, input map[string]any) (bool, string) {
	agentStart := time.Now()

	if err := rt.q.SetAgentStatus(ctx, runID, rt.workerID, ag.ID, "running"); err != nil {
		if err == queue.ErrLeaseLost {
			return false, ""
		}
	}
	rt.emitSeq(ctx, runID, st, start, "info", "agent", ag.Name,
		fmt.Sprintf("%s (%s) started", ag.Name, ag.Role), false, nil)

	// Hand-off messages: each upstream dependency hands off to this agent.
	for _, depID := range ag.DependsOn {
		if dep, ok := byID[depID]; ok {
			_ = rt.q.AddMessage(ctx, runID, depID, ag.ID, fmt.Sprintf("%s → %s", dep.Name, ag.Name))
		}
	}

	// Fetcher agents pull REAL content from public APIs (arXiv/HN/Reddit/web) and
	// feed it into the prompt, so the LLM summarizes real data, not its imagination.
	var fetched string
	if items := rt.fetchForAgent(ctx, runID, ag, st, start, input); items != "" {
		fetched = items
	}

	// Browser-role agents may also drive a browser session (for JS-heavy/auth sites).
	// The pages it visits come from the run input's "urls" (the planner extracts the
	// exact sites a prompt names — X, a blog, etc.), so the browser is a universal
	// source rather than a fixed crawl.
	var browserNotes string
	if ag.Role == "browser" && rt.browser != nil && rt.browser.Name() != "simulated" {
		browserNotes = rt.runBrowser(ctx, runID, ag, st, start, input)
	}

	// Read upstream outputs (snapshot) to build a context-aware prompt.
	_, _, outputs := st.snapshot()
	prompt := buildAgentPrompt(ag, byID, outputs)
	if fetched != "" {
		prompt += "\n\nReal fetched content (summarize the most important items):\n" + fetched
	}
	if browserNotes != "" {
		prompt += "\n\nBrowser observations:\n" + browserNotes
	}
	res, err := rt.llm.Complete(ctx, agentSystemPrompt(ag), []llm.Message{{Role: "user", Content: prompt}})
	if err != nil {
		if ctx.Err() != nil {
			return false, ""
		}
		rt.emitSeq(ctx, runID, st, start, "error", "model", ag.Name,
			fmt.Sprintf("%s failed: %v", ag.Name, err), false, nil)
		_ = rt.q.SetAgentStatus(ctx, runID, rt.workerID, ag.ID, "failed")
		return false, ""
	}

	detail := res.Text
	rt.emitSeq(ctx, runID, st, start, "success", "model", ag.Name, summarize(res.Text), true, &detail)
	cost := llm.EstimateCostUSD(res.TokensIn, res.TokensOut)
	_ = rt.q.AddUsage(ctx, runID, rt.workerID, res.TokensIn, res.TokensOut, cost)

	runtimeMs := int(time.Since(agentStart).Milliseconds())
	_ = rt.q.SetAgentResult(ctx, ag.ID, summarize(res.Text), res.TokensIn+res.TokensOut, cost, 1.0, 1, runtimeMs)
	if err := rt.q.SetAgentStatus(ctx, runID, rt.workerID, ag.ID, "succeeded"); err != nil {
		if err == queue.ErrLeaseLost {
			return false, ""
		}
	}
	rt.emitSeq(ctx, runID, st, start, "success", "agent", ag.Name,
		fmt.Sprintf("%s succeeded", ag.Name), false, nil)
	// Return the FULL text (not the truncated summary): it feeds downstream prompts
	// and becomes the digest artifact / email body. The per-agent DB summary stays short.
	return true, res.Text
}

// emitSeq appends one log line, allocating its seq atomically from st. Used by all
// concurrent DAG code (the single-agent runtime still uses the simpler emit()).
func (rt *Runtime) emitSeq(ctx context.Context, runID string, st *dagState, start time.Time, level, channel, source, message string, reasoning bool, detail *string) {
	seq := st.nextSeq()
	offset := int(time.Since(start).Milliseconds())
	if err := rt.q.AppendLog(ctx, runID, seq, offset, level, channel, source, message, reasoning, detail); err != nil {
		rt.log.Error("dag log append failed", "runId", runID, "seq", seq, "error", err.Error())
	}
}

// runBrowser opens a browser session for a browser-role agent, performs a short
// navigate→extract sequence (all persisted to the browser_* tables + visible in
// the UI Browser tab), and returns a notes string for the agent's prompt. Errors
// are non-fatal: a browser hiccup logs and the agent proceeds with what it got.
func (rt *Runtime) runBrowser(ctx context.Context, runID string, ag queue.GraphAgent, st *dagState, start time.Time, input map[string]any) string {
	sess, err := rt.browser.Open(ctx, runID, ag.Name, rt.q)
	if err != nil {
		rt.emitSeq(ctx, runID, st, start, "warn", "browser", ag.Name,
			fmt.Sprintf("could not open browser: %v", err), false, nil)
		return ""
	}
	rt.emitSeq(ctx, runID, st, start, "info", "browser", ag.Name,
		fmt.Sprintf("browser session opened (%s)", rt.browser.Name()), false, nil)

	// Targets come from the prompt (run input "urls"). Fall back to a representative
	// page only if the prompt named no sites, so the session is never empty.
	targets := inputStrings(input, "urls", nil)
	if len(targets) == 0 {
		targets = []string{"https://news.ycombinator.com"}
	}
	var notes strings.Builder
	ok := true
	for _, url := range targets {
		if ctx.Err() != nil {
			ok = false
			break
		}
		r, err := sess.Do(ctx, browser.Action{Type: "navigate", Target: url})
		if err == nil {
			rt.emitSeq(ctx, runID, st, start, "info", "browser", ag.Name, "navigated → "+url, false, nil)
			// Extract with an EMPTY target = whole-page body text. Passing r.Title
			// here treated the page title ("Hacker News") as a CSS selector, which
			// chromedp.ByQuery rejected instantly (0ms) — the extract must receive a
			// selector, not the title.
			ex, exErr := sess.Do(ctx, browser.Action{Type: "extract", Target: ""})
			if exErr != nil && ctx.Err() == nil {
				detail := exErr.Error()
				rt.emitSeq(ctx, runID, st, start, "warn", "browser", ag.Name, "extract failed: "+url, false, &detail)
			}
			fmt.Fprintf(&notes, "- %s (%s): %s\n", url, r.Title, ex.Output)
		} else if ctx.Err() == nil {
			// Surface the underlying error (chromedp/CDP) as the log detail — a bare
			// "navigation failed" is undebuggable. Visible in the run's Logs tab.
			detail := err.Error()
			rt.emitSeq(ctx, runID, st, start, "warn", "browser", ag.Name, "navigation failed: "+url, false, &detail)
		}
	}
	_ = sess.Close(ctx, ok)
	rt.emitSeq(ctx, runID, st, start, "success", "browser", ag.Name, "browser session closed", false, nil)
	return notes.String()
}

// fetchForAgent pulls real content for a fetcher agent based on its name and the
// run's input. Returns a formatted text block of items (empty if this agent isn't
// a fetcher or the fetch failed — non-fatal, the agent proceeds with what it has).
func (rt *Runtime) fetchForAgent(ctx context.Context, runID string, ag queue.GraphAgent, st *dagState, start time.Time, input map[string]any) string {
	topic := inputString(input, "topic", "AI")
	name := strings.ToLower(ag.Name)

	var items []sources.Item
	var err error
	switch {
	case strings.Contains(name, "arxiv"):
		items, err = sources.ArXiv(ctx, inputString(input, "arxivQuery", "cat:cs.AI OR cat:cs.LG OR cat:cs.CL"), 8)
	case strings.Contains(name, "hn") || strings.Contains(name, "hacker"):
		items, err = sources.HackerNews(ctx, topic, 10)
	case strings.Contains(name, "reddit"):
		items, err = fetchReddit(ctx, input)
	case strings.Contains(name, "news"):
		items, err = fetchNews(ctx, topic)
	case strings.Contains(name, "web"):
		items, err = fetchWebList(ctx, input)
	default:
		return "" // not a fetcher agent
	}

	if err != nil {
		rt.emitSeq(ctx, runID, st, start, "warn", "tool", ag.Name,
			fmt.Sprintf("fetch failed: %v", err), false, nil)
		return ""
	}
	if len(items) == 0 {
		rt.emitSeq(ctx, runID, st, start, "info", "tool", ag.Name, "no items fetched", false, nil)
		return ""
	}
	rt.emitSeq(ctx, runID, st, start, "success", "tool", ag.Name,
		fmt.Sprintf("fetched %d items from %s", len(items), items[0].Source), false, nil)

	var b strings.Builder
	for _, it := range items {
		fmt.Fprintf(&b, "- [%s] %s (%s)\n  %s\n", it.Source, it.Title, it.URL, it.Summary)
	}
	return b.String()
}

func fetchReddit(ctx context.Context, input map[string]any) ([]sources.Item, error) {
	subs := inputStrings(input, "subreddits", []string{"MachineLearning", "artificial"})
	var all []sources.Item
	for _, s := range subs {
		items, err := sources.Reddit(ctx, s, 6)
		if err != nil {
			continue // skip a failing subreddit, keep the rest
		}
		all = append(all, items...)
	}
	return all, nil
}

func fetchNews(ctx context.Context, topic string) ([]sources.Item, error) {
	return sources.GoogleNews(ctx, topic, 10)
}

func fetchWebList(ctx context.Context, input map[string]any) ([]sources.Item, error) {
	urls := inputStrings(input, "urls", nil)
	var all []sources.Item
	for _, u := range urls {
		it, err := sources.Web(ctx, u)
		if err != nil {
			continue
		}
		all = append(all, it)
	}
	return all, nil
}

// inputString reads a string from the run input, with a default.
func inputString(input map[string]any, key, def string) string {
	if input != nil {
		if v, ok := input[key].(string); ok && strings.TrimSpace(v) != "" {
			return v
		}
	}
	return def
}

// inputStrings reads a []string from the run input (JSON array), with a default.
func inputStrings(input map[string]any, key string, def []string) []string {
	if input == nil {
		return def
	}
	raw, ok := input[key].([]any)
	if !ok {
		return def
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return def
	}
	return out
}

// readyAgents returns not-done/not-failed agents whose every dependency is done.
func readyAgents(agents []queue.GraphAgent, done, failed map[string]bool) []queue.GraphAgent {
	var ready []queue.GraphAgent
	for _, a := range agents {
		if done[a.ID] || failed[a.ID] {
			continue
		}
		ok := true
		for _, dep := range a.DependsOn {
			if !done[dep] {
				ok = false
				break
			}
		}
		if ok {
			ready = append(ready, a)
		}
	}
	return ready
}

func (rt *Runtime) blockRemaining(ctx context.Context, runID, workerID string, agents []queue.GraphAgent, done, failed map[string]bool) {
	for _, a := range agents {
		if !done[a.ID] && !failed[a.ID] {
			_ = rt.q.SetAgentStatus(ctx, runID, workerID, a.ID, "blocked")
		}
	}
}

/* ----------------------------- prompt helpers ---------------------------- */

func agentSystemPrompt(ag queue.GraphAgent) string {
	return fmt.Sprintf(
		"You are %q, a %s agent in a multi-agent workflow on the Agently platform. "+
			"Do your part concisely and hand a clear, structured result to downstream agents.",
		ag.Name, ag.Role)
}

func buildAgentPrompt(ag queue.GraphAgent, byID map[string]queue.GraphAgent, outputs map[string]string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Your role: %s. Your task as %q.\n", ag.Role, ag.Name)
	if len(ag.DependsOn) > 0 {
		b.WriteString("\nUpstream results handed to you:\n")
		for _, depID := range ag.DependsOn {
			name := depID
			if d, ok := byID[depID]; ok {
				name = d.Name
			}
			fmt.Fprintf(&b, "  - %s: %s\n", name, firstNonEmpty(outputs[depID], "(no summary)"))
		}
		b.WriteString("\nUsing those, produce your contribution.")
	} else {
		b.WriteString("\nYou are an entry agent. Plan the work and produce your contribution.")
	}
	return b.String()
}

func sinkSummary(agents []queue.GraphAgent, byID map[string]queue.GraphAgent, outputs map[string]string) string {
	depended := make(map[string]bool)
	for _, a := range agents {
		for _, d := range a.DependsOn {
			depended[d] = true
		}
	}
	var parts []string
	for _, a := range agents {
		if !depended[a.ID] {
			parts = append(parts, fmt.Sprintf("%s: %s", a.Name, firstNonEmpty(outputs[a.ID], "done")))
		}
	}
	if len(parts) == 0 {
		return "Agent graph complete."
	}
	return strings.Join(parts, "\n")
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}
