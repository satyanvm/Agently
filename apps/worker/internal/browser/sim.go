package browser

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// sim.go is the simulated browser provider: it performs no real navigation but
// drives the real browser_* tables with plausible activity, so the UI's Browser
// tab (viewport, action list, filmstrip, console) is fully populated and the
// session replay works — at zero cost and with no Browserbase account. This is
// what makes the browser feature demoable everywhere; swap in a key for the real
// thing without changing any agent code.

type simProvider struct{}

func (p *simProvider) Name() string { return "simulated" }

func (p *simProvider) Open(ctx context.Context, runID, agentName string, persist Persister) (Session, error) {
	id, err := persist.CreateBrowserSession(ctx, runID, agentName, "", 1440, 900)
	if err != nil {
		return nil, err
	}
	_ = persist.RecordConsole(ctx, id, "info", "session started (simulated)")
	return &simSession{id: id, persist: persist}, nil
}

type simSession struct {
	id      string
	persist Persister
}

func (s *simSession) ID() string          { return s.id }
func (s *simSession) LiveViewURL() string { return "" } // no live view in sim mode

// Do performs a simulated action and records it. navigate updates page state +
// drops a filmstrip frame; extract returns plausible text; others just log.
func (s *simSession) Do(ctx context.Context, a Action) (Result, error) {
	// brief, cancellable pause to mimic latency so the UI shows progress
	select {
	case <-ctx.Done():
		return Result{}, ctx.Err()
	case <-time.After(450 * time.Millisecond):
	}

	switch a.Type {
	case "navigate":
		title := titleFor(a.Target)
		if err := s.persist.Navigate(ctx, s.id, a.Target, title, 450); err != nil {
			return Result{}, err
		}
		_ = s.persist.RecordShot(ctx, s.id, a.Target, title, "Loaded "+title, "")
		_ = s.persist.RecordConsole(ctx, s.id, "info", "navigated to "+a.Target)
		return Result{OK: true, URL: a.Target, Title: title, Duration: 450}, nil

	case "extract":
		out := fmt.Sprintf("Extracted from %q: a concise, structured summary of the page's key facts.", a.Target)
		_ = s.persist.RecordAction(ctx, s.id, "extract", a.Target, "", "ok", 450)
		return Result{OK: true, Output: out, Duration: 450}, nil

	case "screenshot":
		_ = s.persist.RecordShot(ctx, s.id, "", a.Target, firstNonEmpty(a.Target, "Snapshot"), "")
		_ = s.persist.RecordAction(ctx, s.id, "screenshot", a.Target, "", "ok", 450)
		return Result{OK: true, Duration: 450}, nil

	default: // click | type | wait | scroll
		_ = s.persist.RecordAction(ctx, s.id, a.Type, a.Target, a.Value, "ok", 450)
		return Result{OK: true, Duration: 450}, nil
	}
}

func (s *simSession) Close(ctx context.Context, ok bool) error {
	status := "succeeded"
	if !ok {
		status = "failed"
	}
	_ = s.persist.RecordConsole(ctx, s.id, "info", "session closed")
	return s.persist.FinishBrowserSession(ctx, s.id, status)
}

/* -------------------------------- helpers -------------------------------- */

func titleFor(url string) string {
	host := url
	host = strings.TrimPrefix(host, "https://")
	host = strings.TrimPrefix(host, "http://")
	host = strings.TrimPrefix(host, "www.")
	if i := strings.IndexByte(host, '/'); i >= 0 {
		host = host[:i]
	}
	if host == "" {
		return "Page"
	}
	return strings.Title(strings.SplitN(host, ".", 2)[0]) + " — homepage"
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}
