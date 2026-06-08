package browser

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
)

// browserbase.go is the managed-browser provider. It creates a real hosted
// Chromium session via the Browserbase API and exposes the live-view URL for
// watch-along; actions are driven over the session's CDP/Playwright WebSocket and
// recorded to the same browser_* tables as the simulated provider.
//
// It activates only when BROWSERBASE_API_KEY is set. Without a key the system uses
// the simulated provider, so nothing here runs (or costs money) until a real
// account is wired. The CDP action driver is intentionally minimal — the session
// lifecycle (create → live view → close) is the part that proves the integration;
// rich page manipulation (Playwright over the WS endpoint) is a follow-up.

const browserbaseAPI = "https://api.browserbase.com/v1"

type browserbaseProvider struct {
	apiKey    string
	projectID string
}

func (p *browserbaseProvider) Name() string { return "browserbase" }

// Open creates a Browserbase session, records it (with its live-view URL), and
// returns a session that records actions as it drives the remote browser.
func (p *browserbaseProvider) Open(ctx context.Context, runID, agentName string, persist Persister) (Session, error) {
	bbSession, err := p.createSession(ctx)
	if err != nil {
		return nil, fmt.Errorf("browserbase create session: %w", err)
	}
	liveView := p.liveViewURL(ctx, bbSession.ID)

	dbID, err := persist.CreateBrowserSession(ctx, runID, agentName, liveView, 1440, 900)
	if err != nil {
		return nil, err
	}
	_ = persist.RecordConsole(ctx, dbID, "info",
		fmt.Sprintf("browserbase session %s started (live view available)", bbSession.ID))

	// Connect chromedp to the remote Chromium over Browserbase's CDP WebSocket.
	// allocCtx/browserCtx are torn down in Close(). If the connection can't be
	// established we still return a session (actions degrade to logging).
	sess := &browserbaseSession{
		dbID:     dbID,
		bbID:     bbSession.ID,
		liveView: liveView,
		persist:  persist,
		provider: p,
	}
	if bbSession.ConnectURL != "" {
		allocCtx, cancelAlloc := chromedp.NewRemoteAllocator(context.Background(), bbSession.ConnectURL)
		browserCtx, cancelBrowser := chromedp.NewContext(allocCtx)
		sess.cdp = browserCtx
		sess.cancel = func() { cancelBrowser(); cancelAlloc() }
	}
	return sess, nil
}

type bbSession struct {
	ID         string `json:"id"`
	ConnectURL string `json:"connectUrl"`
}

func (p *browserbaseProvider) createSession(ctx context.Context) (bbSession, error) {
	body, _ := json.Marshal(map[string]any{"projectId": p.projectID})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, browserbaseAPI+"/sessions", bytes.NewReader(body))
	if err != nil {
		return bbSession{}, err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-bb-api-key", p.apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return bbSession{}, err
	}
	defer resp.Body.Close()
	payload, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return bbSession{}, fmt.Errorf("status %d: %s", resp.StatusCode, string(payload))
	}
	var s bbSession
	if err := json.Unmarshal(payload, &s); err != nil {
		return bbSession{}, err
	}
	return s, nil
}

// liveViewURL fetches the session's debugger/live-view URL (best-effort).
func (p *browserbaseProvider) liveViewURL(ctx context.Context, sessionID string) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("%s/sessions/%s/debug", browserbaseAPI, sessionID), nil)
	if err != nil {
		return ""
	}
	req.Header.Set("x-bb-api-key", p.apiKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	var dbg struct {
		DebuggerFullscreenURL string `json:"debuggerFullscreenUrl"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&dbg)
	return dbg.DebuggerFullscreenURL
}

type browserbaseSession struct {
	dbID     string
	bbID     string
	liveView string
	persist  Persister
	provider *browserbaseProvider
	cdp      context.Context // chromedp browser context (nil if connect failed)
	cancel   func()          // tears down the chromedp contexts
}

func (s *browserbaseSession) ID() string         { return s.dbID }
func (s *browserbaseSession) LiveViewURL() string { return s.liveView }

// Do drives the REAL remote browser over CDP. navigate loads the page and reads
// its title; extract pulls visible text from the body (or a selector). Everything
// is recorded to the DB. If CDP isn't connected, actions degrade to logging.
func (s *browserbaseSession) Do(ctx context.Context, a Action) (Result, error) {
	start := time.Now()
	switch a.Type {
	case "navigate":
		if s.cdp == nil {
			title := titleFor(a.Target)
			_ = s.persist.Navigate(ctx, s.dbID, a.Target, title, 0)
			return Result{OK: true, URL: a.Target, Title: title}, nil
		}
		var title string
		actCtx, cancel := context.WithTimeout(s.cdp, 30*time.Second)
		defer cancel()
		err := chromedp.Run(actCtx,
			chromedp.Navigate(a.Target),
			chromedp.Title(&title),
		)
		dur := int(time.Since(start).Milliseconds())
		if err != nil {
			_ = s.persist.RecordAction(ctx, s.dbID, "navigate", a.Target, "", "error", dur)
			return Result{}, fmt.Errorf("navigate: %w", err)
		}
		_ = s.persist.Navigate(ctx, s.dbID, a.Target, title, dur)
		_ = s.persist.RecordShot(ctx, s.dbID, a.Target, title, "Loaded "+title, "")
		return Result{OK: true, URL: a.Target, Title: title, Duration: dur}, nil

	case "extract":
		if s.cdp == nil {
			_ = s.persist.RecordAction(ctx, s.dbID, "extract", a.Target, "", "ok", 0)
			return Result{OK: true}, nil
		}
		sel := a.Target
		if strings.TrimSpace(sel) == "" {
			sel = "body"
		}
		var text string
		actCtx, cancel := context.WithTimeout(s.cdp, 20*time.Second)
		defer cancel()
		err := chromedp.Run(actCtx, chromedp.Text(sel, &text, chromedp.NodeVisible, chromedp.ByQuery))
		dur := int(time.Since(start).Milliseconds())
		if err != nil {
			_ = s.persist.RecordAction(ctx, s.dbID, "extract", sel, "", "error", dur)
			return Result{}, fmt.Errorf("extract: %w", err)
		}
		if len(text) > 1200 {
			text = text[:1200] + "…"
		}
		_ = s.persist.RecordAction(ctx, s.dbID, "extract", sel, "", "ok", dur)
		return Result{OK: true, Output: text, Duration: dur}, nil

	default:
		_ = s.persist.RecordAction(ctx, s.dbID, a.Type, a.Target, a.Value, "ok", 0)
		return Result{OK: true}, nil
	}
}

func (s *browserbaseSession) Close(ctx context.Context, ok bool) error {
	if s.cancel != nil {
		s.cancel() // close the chromedp connection to the remote browser
	}
	_ = s.provider.releaseSession(ctx, s.bbID)
	status := "succeeded"
	if !ok {
		status = "failed"
	}
	return s.persist.FinishBrowserSession(ctx, s.dbID, status)
}

func (p *browserbaseProvider) releaseSession(ctx context.Context, sessionID string) error {
	body, _ := json.Marshal(map[string]any{"projectId": p.projectID, "status": "REQUEST_RELEASE"})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf("%s/sessions/%s", browserbaseAPI, sessionID), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-bb-api-key", p.apiKey)
	ctx2, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req = req.WithContext(ctx2)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}
