// Command server runs the Agently HTTP API: a single self-contained Go binary
// that serves the same contract the TypeScript backend served. It assembles the
// in-memory seeded platform and listens on API_ADDR (default :8080).
package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/agently/api/internal/handler"
	"github.com/agently/api/internal/platform"
	"github.com/agently/api/internal/services"
	"github.com/joho/godotenv"
)

func main() {
	// Load .env from the repo root (best-effort) so DATABASE_URL, GEMINI_API_KEY
	// and SMTP creds are present without manual exporting. Real env
	// vars always win — godotenv does not overwrite what's already set.
	_ = godotenv.Load(".env", "../.env", "../../.env")

	pretty := os.Getenv("ENV") != "production"
	logger := platform.NewLogger(pretty, "svc", "api")

	// Production must not expose the API (credentials, workflow launches) without
	// the shared token the web proxy attaches (handler/auth.go).
	if !pretty && os.Getenv("AGENTLY_API_TOKEN") == "" {
		logger.Error("AGENTLY_API_TOKEN is required", "reason", "ENV=production refuses to serve an unauthenticated API")
		os.Exit(1)
	}

	// Stored third-party credentials are sealed with AGENTLY_CREDENTIALS_KEY
	// (platform/secretbox.go). Production refuses to keep them in plaintext.
	if hasKey, err := platform.CredentialsKeyConfigured(); err != nil {
		logger.Error("invalid AGENTLY_CREDENTIALS_KEY", "error", err.Error())
		os.Exit(1)
	} else if !hasKey && !pretty {
		logger.Error("AGENTLY_CREDENTIALS_KEY is required", "reason", "ENV=production refuses to store credentials in plaintext")
		os.Exit(1)
	} else if !hasKey {
		logger.Warn("AGENTLY_CREDENTIALS_KEY is not set; credentials are stored in plaintext")
	}

	// Preflight. The compiler has no deterministic floor any more, so a missing
	// credential or a stale embedding sidecar means every POST /workflows/plan
	// will fail. Refuse to start instead, and name the thing that is wrong —
	// finding out at boot is cheap, finding out from a user's failed workflow
	// is not.
	for _, check := range []func() error{
		services.RequirePlannerKeys,
		services.RequireNodeCatalog,
		services.RequirePiecesIndex,
		services.RequirePieceEmbeddings,
	} {
		if err := check(); err != nil {
			logger.Error("preflight failed", "error", err.Error())
			os.Exit(1)
		}
	}

	// The server always uses Postgres as the durable source of truth. Tests
	// construct the platform directly with in-memory repositories.
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		logger.Error("DATABASE_URL is required", "reason", "the API must use durable Postgres storage")
		os.Exit(1)
	}
	opts := services.Options{
		Clock:  platform.SystemClock,
		Logger: logger,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	pool, err := platform.Connect(ctx, dbURL)
	cancel()
	if err != nil {
		logger.Error("could not connect to Postgres", "error", err.Error())
		os.Exit(1)
	}
	defer pool.Close()
	logger.Info("connected to Postgres", "store", "postgres")
	schemaCtx, schemaCancel := context.WithTimeout(context.Background(), 30*time.Second)
	err = platform.EnsureSchema(schemaCtx, pool)
	schemaCancel()
	if err != nil {
		logger.Error("could not upgrade the database schema", "error", err.Error())
		os.Exit(1)
	}
	sealCtx, sealCancel := context.WithTimeout(context.Background(), 30*time.Second)
	sealed, err := platform.EncryptPlaintextCredentials(sealCtx, pool)
	sealCancel()
	if err != nil {
		logger.Error("could not encrypt stored credentials", "error", err.Error())
		os.Exit(1)
	}
	if sealed > 0 {
		logger.Info("encrypted plaintext credentials", "count", sealed)
	}
	opts.Pool = pool

	plat := services.NewPlatform(opts)

	// Control-plane scheduler: fires trigger='schedule' workflows when they come due
	// (the "every day at 9am" path). Idempotent + restart-safe (see services/scheduler.go).
	// SCHEDULER_TZ (IANA name) sets the zone "daily HH:MM" is interpreted in; default
	// server-local. SCHEDULER=0 disables it (e.g. when running multiple API nodes).
	schedCtx, schedCancel := context.WithCancel(context.Background())
	defer schedCancel()
	if os.Getenv("SCHEDULER") != "0" {
		go plat.NewScheduler(os.Getenv("SCHEDULER_TZ")).Start(schedCtx)
	}
	// Piece polling triggers: ticks every PIECES_POLL_INTERVAL (default 5m) and
	// launches runs for new events (services/piecespoller.go). PIECES_POLLER=0 disables.
	if os.Getenv("PIECES_POLLER") != "0" {
		go plat.NewPiecesPoller().Start(schedCtx)
	}

	// Failed runs: start the workflow's error workflow and send its alerts
	// (services/failures.go). Runs alongside the scheduler, same off switch.
	if os.Getenv("SCHEDULER") != "0" {
		go plat.NewFailureWatcher().Start(schedCtx)
	}

	addr := os.Getenv("API_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	srv := &http.Server{
		Addr:    addr,
		Handler: handler.NewRouter(plat),
	}

	// Graceful shutdown on SIGINT/SIGTERM.
	go func() {
		stop := make(chan os.Signal, 1)
		signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
		<-stop
		logger.Info("shutting down")
		schedCancel()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	logger.Info("starting API server", "addr", addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("server failed", "error", err)
		os.Exit(1)
	}
}
