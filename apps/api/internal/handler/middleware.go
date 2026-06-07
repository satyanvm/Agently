package handler

import (
	"net/http"

	"github.com/go-chi/cors"
)

// corsMiddleware allows the Next.js dev origin and common headers. Not strictly
// needed when the frontend proxies /api/* (same-origin), but useful for direct
// curl and for running the frontend against the API cross-origin.
func corsMiddleware() func(http.Handler) http.Handler {
	return cors.Handler(cors.Options{
		AllowedOrigins:   []string{"http://localhost:*", "https://localhost:*"},
		AllowedMethods:   []string{"GET", "POST", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Content-Type", "Authorization", "Last-Event-ID"},
		ExposedHeaders:   []string{"Content-Type"},
		AllowCredentials: false,
		MaxAge:           300,
	})
}
