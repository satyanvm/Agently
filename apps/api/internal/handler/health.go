package handler

import (
	"net/http"

	"github.com/agently/api/internal/services"
)

func health(p *services.Platform) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"ok":      true,
			"service": "agently-api",
			"time":    p.Clock.ISO(),
		}, http.StatusOK)
	}
}

func dashboard(p *services.Platform) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handle(w, func() (any, int, error) {
			return p.Stats.Workspace(), http.StatusOK, nil
		})
	}
}
