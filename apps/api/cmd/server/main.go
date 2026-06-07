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
)

func main() {
	pretty := os.Getenv("ENV") != "production"
	logger := platform.NewLogger(pretty, "svc", "api")

	plat := services.NewPlatform(services.Options{
		Clock:  platform.SystemClock,
		Logger: logger,
	})

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
