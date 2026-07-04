// Package browser is the worker's browser-automation layer, behind a Provider
// interface so the agent runtime doesn't care WHERE the browser runs. Two
// implementations, selected by env — the same seam as the LLM layer:
//
//   - Browserbase (managed): real hosted Chromium with live-view + replay, used
//     when BROWSERBASE_API_KEY is set. Code-complete; activates with a key.
//   - Simulated: drives the real browser_* DB tables (navigate/extract/screenshot)
//     with no external service, so the UI Browser tab + replay are fully demoable
//     at zero cost and with no account. Default.
//
// Per the design doc (§6), starting on a provider interface is the whole point:
// build on the managed service now, swap to self-hosted Chromium later without
// touching the agent runtime. Here the "swap" also covers the no-key dev case.
package browser

import (
	"context"
	"os"
)

// Action is one browser operation the agent asks for.
type Action struct {
	Type   string // navigate | click | type | extract | screenshot | wait
	Target string // url for navigate; selector/description otherwise
	Value  string // text to type; extracted text returned in Result.Output
}

// Result is the outcome of an action.
type Result struct {
	OK       bool
	URL      string // resulting page url (after navigate)
	Title    string // resulting page title
	Output   string // extracted text / observation
	Duration int    // ms
}

// Session is one browser session bound to an agent-run. The agent drives it with
// Do(); Close() ends it. LiveViewURL is the watch-along url (Browserbase only).
type Session interface {
	ID() string
	LiveViewURL() string
	Do(ctx context.Context, a Action) (Result, error)
	Close(ctx context.Context, ok bool) error
}

// Provider opens browser sessions. The agent runtime depends on this, not on a
// concrete browser backend.
type Provider interface {
	Name() string
	// Open starts a session for a run+agent. persist is the worker's DB writer so
	// the provider records actions/shots/console to Postgres as they happen.
	Open(ctx context.Context, runID, agentName string, persist Persister) (Session, error)
}

// Persister is the slice of the queue the browser layer writes through. Defining
// it here (not importing queue) keeps the dependency one-way and the seam clean.
type Persister interface {
	CreateBrowserSession(ctx context.Context, runID, agentName, liveViewURL string, vw, vh int) (string, error)
	Navigate(ctx context.Context, sessionID, url, title string, durationMs int) error
	RecordAction(ctx context.Context, sessionID, actType, target, value, status string, durationMs int) error
	RecordShot(ctx context.Context, sessionID, url, title, label, storageKey string) error
	RecordConsole(ctx context.Context, sessionID, level, text string) error
	FinishBrowserSession(ctx context.Context, sessionID, status string) error
}

// New selects a provider from the environment. Browserbase requires BOTH
// BROWSERBASE_API_KEY and BROWSERBASE_PROJECT_ID — a session create fails with
// `400 Invalid uuid at "projectId"` without the project id, so a half-configured
// setup must fall back to the simulated provider rather than 400 on every browse.
// (This mirrors the Python reasoner, whose browserbase_enabled gates on both.)
// Otherwise the simulated provider (zero-cost, no account, fully demoable).
func New() Provider {
	if key := os.Getenv("BROWSERBASE_API_KEY"); key != "" {
		if projectID := os.Getenv("BROWSERBASE_PROJECT_ID"); projectID != "" {
			return &browserbaseProvider{apiKey: key, projectID: projectID}
		}
		// Key present but project id missing → not usable; degrade to simulated.
	}
	return &simProvider{}
}

// BrowserbaseMisconfigured reports the "key set but project id missing" case, so
// the entrypoint can warn that it degraded to the simulated browser (a silent
// fallback here is exactly what made a missing project id hard to diagnose).
func BrowserbaseMisconfigured() bool {
	return os.Getenv("BROWSERBASE_API_KEY") != "" && os.Getenv("BROWSERBASE_PROJECT_ID") == ""
}
