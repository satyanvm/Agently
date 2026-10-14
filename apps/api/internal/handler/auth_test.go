package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthMiddleware(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	cases := []struct {
		name, token, path, header string
		want                      int
	}{
		{"gate disabled", "", "/api/workflows", "", http.StatusNoContent},
		{"missing token", "s3cret", "/api/workflows", "", http.StatusUnauthorized},
		{"wrong token", "s3cret", "/api/workflows", "Bearer nope", http.StatusUnauthorized},
		{"valid token", "s3cret", "/api/workflows", "Bearer s3cret", http.StatusNoContent},
		{"health exempt", "s3cret", "/api/health", "", http.StatusNoContent},
		{"webhook exempt", "s3cret", "/api/hooks/digest/trigger", "", http.StatusNoContent},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, c.path, nil)
			if c.header != "" {
				req.Header.Set("Authorization", c.header)
			}
			rec := httptest.NewRecorder()
			authMiddleware(c.token)(ok).ServeHTTP(rec, req)
			if rec.Code != c.want {
				t.Fatalf("status = %d, want %d", rec.Code, c.want)
			}
		})
	}
}
