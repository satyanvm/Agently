package handler

import (
	"github.com/agently/api/internal/services"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// NewRouter mounts every API route under /api. The path table mirrors the TS
// contract in packages/contracts/src/api.ts exactly.
func NewRouter(p *services.Platform) chi.Router {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Logger)
	r.Use(corsMiddleware())
	r.Use(middleware.Recoverer)

	r.Route("/api", func(r chi.Router) {
		r.Get("/health", health(p))
		r.Get("/dashboard", dashboard(p))

		r.Get("/workflows", listWorkflows(p))
		r.Post("/workflows", createWorkflow(p))
		r.Get("/workflows/{slug}", getWorkflow(p))
		r.Post("/workflows/{slug}/runs", launchRun(p))
		r.Get("/workflows/{slug}/runs", listWorkflowRuns(p))

		r.Get("/runs", listRuns(p))
		r.Get("/runs/{id}", getRun(p))
		r.Post("/runs/{id}/cancel", cancelRun(p))
		r.Get("/runs/{id}/logs", listLogs(p))
		r.Get("/runs/{id}/logs/stream", streamLogs(p))
		r.Get("/runs/{id}/browser", getBrowserSession(p))

		r.Get("/agents", listAgents(p))
		r.Post("/agents", createAgent(p))

		r.Get("/notifications", listNotifications(p))
		r.Post("/notifications/read-all", markAllNotificationsRead(p))
		r.Post("/notifications/{id}/read", markNotificationRead(p))

		r.Get("/activity", activityFeed(p))

		r.Get("/events/stream", eventsStream(p))
	})

	return r
}
