// Package agent is the native agent runtime: the minimal "prompt → LLM → record"
// loop that turns a claimed run into real model work with durable, streamed logs
// and a result artifact. It is deliberately tiny and stateless beyond the DB —
// every step is persisted, so a crash mid-run loses nothing (chunk 4 builds resume
// on top of this). Agent frameworks (LangGraph/CrewAI) will later plug in at this
// same boundary as alternative executors.
package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/agently/worker/internal/browser"
	"github.com/agently/worker/internal/llm"
	"github.com/agently/worker/internal/queue"
)

// Step is one unit of the run's plan. The native runtime executes them in order;
// each completed step is a checkpoint boundary (chunk 4). Keeping the plan
// explicit (rather than buried in a framework) is what makes resume tractable.
type Step struct {
	Name   string // short label shown as current_step
	Prompt string // what we ask the model at this step
}

// Logger is the small logging surface the agent needs (worker passes slog).
type Logger interface {
	Info(msg string, args ...any)
	Error(msg string, args ...any)
}

// Runtime executes a run's steps against an LLM, persisting everything. A browser
// provider is optional — present, browser-role agents get a real (or simulated)
// session; absent, they fall back to a plain LLM step.
type Runtime struct {
	q        *queue.Queue
	llm      llm.Provider
	browser  browser.Provider
	log      Logger
	workerID string
}

func New(q *queue.Queue, provider llm.Provider, browserProvider browser.Provider, workerID string, log Logger) *Runtime {
	return &Runtime{q: q, llm: provider, browser: browserProvider, log: log, workerID: workerID}
}

// Run executes steps[startStep:] for a run, returning the final step index reached
// and whether it succeeded. startStep > 0 means we're resuming (chunk 4): earlier
// steps already ran and are NOT repeated — that's the no-double-work guarantee.
//
// ctx cancellation (lease lost / shutdown) stops the loop cleanly between steps;
// the caller decides whether the run can be finished or must be abandoned.
func (rt *Runtime) Run(ctx context.Context, runID string, steps []Step, startStep int) (int, bool) {
	start := time.Now()
	seq, err := rt.q.NextLogSeq(ctx, runID)
	if err != nil {
		rt.log.Error("could not get log seq", "runId", runID, "error", err.Error())
		return startStep, false
	}

	// announce (only on a fresh start, not on resume, to avoid duplicate banners)
	if startStep == 0 {
		seq = rt.emit(ctx, runID, seq, start, "info", "system", "orchestrator",
			fmt.Sprintf("Run started — native runtime, model %s, %d steps", rt.llm.Name(), len(steps)), false, nil)
	} else {
		seq = rt.emit(ctx, runID, seq, start, "info", "system", "orchestrator",
			fmt.Sprintf("Resuming from step %d of %d after recovery", startStep+1, len(steps)), false, nil)
	}

	for i := startStep; i < len(steps); i++ {
		select {
		case <-ctx.Done():
			rt.log.Info("agent loop interrupted", "runId", runID, "atStep", i)
			return i, false
		default:
		}
		step := steps[i]

		// mark current step (visible on the run's progress bar)
		if err := rt.q.Progress(ctx, runID, rt.workerID, i, step.Name); err != nil {
			if err == queue.ErrLeaseLost {
				return i, false
			}
		}
		seq = rt.emit(ctx, runID, seq, start, "info", "agent", step.Name,
			fmt.Sprintf("Thinking: %s", step.Name), false, nil)

		// the actual model call
		res, err := rt.llm.Complete(ctx, systemPrompt, []llm.Message{{Role: "user", Content: step.Prompt}})
		if err != nil {
			if ctx.Err() != nil {
				return i, false // interrupted, not a real failure
			}
			rt.emit(ctx, runID, seq, start, "error", "model", step.Name,
				fmt.Sprintf("Model call failed: %v", err), false, nil)
			return i, false
		}

		// record the reasoning (this is the "reasoning trace" the product promises)
		detail := res.Text
		seq = rt.emit(ctx, runID, seq, start, "success", "model", rt.llm.Name(),
			summarize(res.Text), true, &detail)

		// accumulate token/cost usage
		cost := llm.EstimateCostUSD(res.TokensIn, res.TokensOut)
		if err := rt.q.AddUsage(ctx, runID, rt.workerID, res.TokensIn, res.TokensOut, cost); err != nil {
			rt.log.Error("usage update failed", "runId", runID, "error", err.Error())
		}

		// advance the checkpoint: this step is DONE (chunk 4 reads steps_done on resume)
		if err := rt.q.Progress(ctx, runID, rt.workerID, i+1, step.Name); err != nil {
			if err == queue.ErrLeaseLost {
				return i + 1, false
			}
		}
		rt.log.Info("step complete", "runId", runID, "step", i+1, "of", len(steps),
			"tokensIn", res.TokensIn, "tokensOut", res.TokensOut)

		// the final step's output becomes the run's result artifact
		if i == len(steps)-1 {
			if err := rt.q.AddArtifact(ctx, runID, "result.md", "report", "native-runtime", summarize(res.Text)); err != nil {
				rt.log.Error("artifact write failed", "runId", runID, "error", err.Error())
			}
			seq = rt.emit(ctx, runID, seq, start, "success", "system", "orchestrator",
				"Result artifact produced — run complete", false, nil)
		}
	}
	return len(steps), true
}

// emit appends one log line and returns the next seq, so callers thread it.
func (rt *Runtime) emit(ctx context.Context, runID string, seq int, start time.Time, level, channel, source, message string, reasoning bool, detail *string) int {
	offset := int(time.Since(start).Milliseconds())
	if err := rt.q.AppendLog(ctx, runID, seq, offset, level, channel, source, message, reasoning, detail); err != nil {
		rt.log.Error("log append failed", "runId", runID, "seq", seq, "error", err.Error())
		return seq // don't advance on failure; retry will reuse the seq
	}
	return seq + 1
}

// BuildPlan turns a run into an ordered list of steps. For the skeleton this is a
// fixed research-style plan; a real workflow definition would drive this. Kept
// here so the plan is explicit and checkpointable.
func BuildPlan(workflowName string, number int) []Step {
	objective := fmt.Sprintf("workflow %q (run #%d)", workflowName, number)
	return []Step{
		{Name: "Plan", Prompt: "Briefly outline how you would approach: " + objective},
		{Name: "Research", Prompt: "Identify the key factors and considerations for: " + objective},
		{Name: "Analyze", Prompt: "Weigh the factors and reason toward a conclusion for: " + objective},
		{Name: "Synthesize", Prompt: "Write a concise final recommendation report for: " + objective},
	}
}

const systemPrompt = "You are an autonomous research agent running on the Agently platform. " +
	"You work in discrete steps. Be concise, structured, and decisive. Produce useful, " +
	"grounded output even with limited information."

func summarize(s string) string {
	const n = 140
	s = firstLine(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func firstLine(s string) string {
	for i, r := range s {
		if r == '\n' {
			return s[:i]
		}
	}
	return s
}
