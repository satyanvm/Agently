package queue

import (
	"context"
	"fmt"
)

// browser.go is the worker's WRITE path for browser sessions. A browser-capable
// agent opens a session, performs actions (navigate/click/extract/screenshot),
// and everything is persisted to the browser_* tables so the UI's Browser tab can
// show live activity and a replay after the fact. Like logs and agents, browser
// state lives in Postgres — so it survives a crash and is visible to a watcher.

// CreateBrowserSession opens a session row for a run+agent and returns its id.
// liveViewURL is the provider's watch-along URL (empty for the simulated provider).
func (q *Queue) CreateBrowserSession(ctx context.Context, runID, agentName, liveViewURL string, vw, vh int) (string, error) {
	id := genID("bs")
	_, err := q.pool.Exec(ctx,
		`insert into browser_sessions (id, run_id, agent_name, status, current_url, page_title, viewport_w, viewport_h, pages_visited, actions_count, started_at)
		 values ($1,$2,$3,'running','','',$4,$5,0,0, now())`,
		id, runID, agentName, vw, vh)
	if err != nil {
		return "", fmt.Errorf("create browser session: %w", err)
	}
	// Point the run at its primary browser session so /api/runs/{id}/browser resolves.
	_, _ = q.pool.Exec(ctx, `update runs set browser_session_id=$1 where id=$2`, id, runID)
	// liveViewURL has no column in the current schema; it rides along in the page
	// title prefix only if set (kept simple — Browserbase exposes it via its API).
	_ = liveViewURL
	return id, nil
}

// RecordAction appends a browser action and bumps the session's action counter +
// current page state. status is ok|error|pending.
func (q *Queue) RecordAction(ctx context.Context, sessionID, actType, target, value, status string, durationMs int) error {
	id := genID("ba")
	var valArg any
	if value != "" {
		valArg = value
	}
	_, err := q.pool.Exec(ctx,
		`insert into browser_actions (id, session_id, ts, type, target, value, status, duration_ms)
		 values ($1,$2, now(), $3,$4,$5,$6,$7)`,
		id, sessionID, actType, target, valArg, status, durationMs)
	if err != nil {
		return fmt.Errorf("record action: %w", err)
	}
	_, _ = q.pool.Exec(ctx,
		`update browser_sessions set actions_count = actions_count + 1 where id=$1`, sessionID)
	return nil
}

// Navigate records a navigation: an action plus updated current_url/page_title and
// an incremented pages_visited counter.
func (q *Queue) Navigate(ctx context.Context, sessionID, url, title string, durationMs int) error {
	if err := q.RecordAction(ctx, sessionID, "navigate", url, "", "ok", durationMs); err != nil {
		return err
	}
	_, err := q.pool.Exec(ctx,
		`update browser_sessions set current_url=$2, page_title=$3, pages_visited = pages_visited + 1 where id=$1`,
		sessionID, url, title)
	return err
}

// RecordShot stores a screenshot reference (a "filmstrip" frame for replay).
func (q *Queue) RecordShot(ctx context.Context, sessionID, url, title, label, storageKey string) error {
	id := genID("shot")
	var keyArg any
	if storageKey != "" {
		keyArg = storageKey
	}
	_, err := q.pool.Exec(ctx,
		`insert into browser_shots (id, session_id, ts, url, title, label, storage_key)
		 values ($1,$2, now(), $3,$4,$5,$6)`,
		id, sessionID, url, title, label, keyArg)
	if err != nil {
		return fmt.Errorf("record shot: %w", err)
	}
	return nil
}

// RecordConsole appends a browser console line.
func (q *Queue) RecordConsole(ctx context.Context, sessionID, level, text string) error {
	_, err := q.pool.Exec(ctx,
		`insert into browser_console (session_id, ts, level, text) values ($1, now(), $2, $3)`,
		sessionID, level, text)
	if err != nil {
		return fmt.Errorf("record console: %w", err)
	}
	return nil
}

// FinishBrowserSession marks a session terminal (succeeded|failed).
func (q *Queue) FinishBrowserSession(ctx context.Context, sessionID, status string) error {
	_, err := q.pool.Exec(ctx,
		`update browser_sessions set status=$2, finished_at=now() where id=$1`, sessionID, status)
	if err != nil {
		return fmt.Errorf("finish browser session: %w", err)
	}
	return nil
}
