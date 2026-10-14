package handler

import (
	"crypto/subtle"
	"net/http"
	"os"
	"strings"

	"github.com/agently/api/internal/domain"
)

// authMiddleware gates every /api route behind a shared bearer token
// (AGENTLY_API_TOKEN). The web app's server-side proxy attaches it, so browsers
// never hold it. An empty token disables the gate — main refuses to start that
// way when ENV=production.
//
// Exempt: the health probe, and webhook ingress (/api/hooks/*), which third-party
// providers call directly and which only ever launches the trigger's own workflow.
func authMiddleware(token string) func(http.Handler) http.Handler {
	want := []byte("Bearer " + token)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if token == "" || r.Method == http.MethodOptions || authExempt(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}
			got := []byte(r.Header.Get("Authorization"))
			if subtle.ConstantTimeCompare(got, want) != 1 {
				fail(w, domain.CodeUnauthorized, "Missing or invalid API token", nil)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func authExempt(path string) bool {
	return path == "/api/health" || strings.HasPrefix(path, "/api/hooks/")
}

// corsOrigins reads CORS_ORIGINS (comma-separated); localhost when unset.
func corsOrigins() []string {
	var out []string
	for _, o := range strings.Split(os.Getenv("CORS_ORIGINS"), ",") {
		if o = strings.TrimSpace(o); o != "" {
			out = append(out, o)
		}
	}
	if len(out) == 0 {
		return []string{"http://localhost:*", "https://localhost:*"}
	}
	return out
}
