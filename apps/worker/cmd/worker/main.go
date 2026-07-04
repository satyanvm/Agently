// Command worker is the Agently execution engine (data plane). It claims runs
// from the Postgres queue, holds a lease via heartbeats while it works, and marks
// them terminal. It shares NO code with the API — only the database. Kill it at
// any time: the reaper requeues whatever it was holding, and another worker (or
// the same one restarted) picks the run back up.
//
//	DATABASE_URL   required — postgres connection string
//	WORKER_ID      optional — defaults to a hostname-ish generated id
//	WORKER_REAPER  optional — "1" (default) to also run the reaper in this process
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/agently/worker/internal/browser"
	"github.com/agently/worker/internal/llm"
	"github.com/agently/worker/internal/notifier"
	"github.com/agently/worker/internal/queue"
	"github.com/agently/worker/internal/runner"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// Load .env from the repo root (best-effort) so DATABASE_URL, the LLM key, SMTP
	// creds, and BROWSERBASE_API_KEY are present without manual exporting. Real env
	// vars always win — godotenv does not overwrite what's already set.
	_ = godotenv.Load(".env", "../.env", "../../.env")

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Error("DATABASE_URL is required")
		os.Exit(1)
	}

	workerID := os.Getenv("WORKER_ID")
	if workerID == "" {
		workerID = generateWorkerID()
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	connCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	pool, err := pgxpool.New(connCtx, dbURL)
	cancel()
	if err != nil {
		log.Error("could not open pool", "error", err.Error())
		os.Exit(1)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		log.Error("could not reach postgres", "error", err.Error())
		os.Exit(1)
	}
	log.Info("connected to postgres", "workerId", workerID)

	q := queue.New(pool)

	// LLM provider: real Anthropic if ANTHROPIC_API_KEY is set, else a mock so the
	// system runs with no key. The runtime is provider-agnostic either way.
	provider := llm.New()
	log.Info("llm provider selected", "provider", provider.Name())

	// Browser provider: Browserbase if BROWSERBASE_API_KEY is set, else simulated
	// (drives the real browser_* tables, zero cost, fully demoable).
	browserProvider := browser.New()
	log.Info("browser provider selected", "provider", browserProvider.Name())
	if browser.BrowserbaseMisconfigured() {
		log.Warn("BROWSERBASE_API_KEY is set but BROWSERBASE_PROJECT_ID is missing — " +
			"using the simulated browser. Set BROWSERBASE_PROJECT_ID to enable real Browserbase sessions.")
	}

	// Notifier: external "ping me when done" channels (webhook/email), selected by
	// env. The in-app notification row is always written regardless.
	notif := notifier.New(log)

	appBaseURL := os.Getenv("APP_BASE_URL")
	if appBaseURL == "" {
		appBaseURL = "http://localhost:3000"
	}

	cfg := runner.Config{
		WorkerID:          workerID,
		PollInterval:      1 * time.Second,
		HeartbeatInterval: 3 * time.Second, // lease (reaper max_silence) is 15s → ~5x margin
		AppBaseURL:        appBaseURL,
	}
	r := runner.New(q, cfg, log, provider, browserProvider, notif)

	// Optionally run the reaper in-process. The lease window (15s) is the SQL
	// default in reap_stalled_runs(); we poll it every 5s.
	if os.Getenv("WORKER_REAPER") != "0" {
		rp := runner.NewReaper(q, 5*time.Second, log)
		go rp.Run(ctx)
	}

	r.Run(ctx) // blocks until signal
	log.Info("worker exited cleanly", "workerId", workerID)
}

// generateWorkerID builds a human-legible, unique-enough id: hostname + pid.
func generateWorkerID() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "worker"
	}
	return fmt.Sprintf("%s-%d", host, os.Getpid())
}
