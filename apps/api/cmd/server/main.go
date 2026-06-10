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
	// Load .env from the repo root (best-effort) so DATABASE_URL, the LLM key the
	// prompt-planner uses, and SMTP creds are present without manual exporting. Real
	// env vars always win — godotenv does not overwrite what's already set.
	_ = godotenv.Load(".env", "../.env", "../../.env")

	pretty := os.Getenv("ENV") != "production"
	logger := platform.NewLogger(pretty, "svc", "api")

	// Storage: if DATABASE_URL is set, use Postgres (durable, survives restarts);
	// otherwise fall back to the in-memory seeded store (zero-setup dev/test).
	opts := services.Options{
		Clock:  platform.SystemClock,
		Logger: logger,
	}
	if dbURL := os.Getenv("DATABASE_URL"); dbURL != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		pool, err := platform.Connect(ctx, dbURL)
		cancel()
		if err != nil {
			logger.Error("could not connect to Postgres", "error", err.Error())
			os.Exit(1)
		}
		defer pool.Close()
		logger.Info("connected to Postgres", "store", "postgres")
		opts.Pool = pool
	} else {
		logger.Info("no DATABASE_URL set", "store", "in-memory")
	}

	plat := services.NewPlatform(opts)

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
