package handler

import (
	"net/http"
	"strings"

	"github.com/agently/api/internal/domain"
	"github.com/agently/api/internal/domain/validate"
	"github.com/agently/api/internal/services"
	"github.com/go-chi/chi/v5"
)

func listRuns(p *services.Platform) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handle(w, func() (any, int, error) {
			query, err := validate.ParseListRunsQuery(r.URL.Query())
			if err != nil {
				return nil, 0, err
			}
			return p.Runs.List(query), http.StatusOK, nil
		})
	}
}

func getRun(p *services.Platform) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handle(w, func() (any, int, error) {
			detail, err := p.Runs.Get(asRunID(chi.URLParam(r, "id")))
			return detail, http.StatusOK, err
		})
	}
}

func cancelRun(p *services.Platform) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handle(w, func() (any, int, error) {
			run, err := p.Runs.Cancel(asRunID(chi.URLParam(r, "id")))
			return run, http.StatusOK, err
		})
	}
}

// downloadArtifact serves one artifact's content as a file download (not JSON), so
// the UI's download button can save the digest. Content-Disposition makes the browser
// save it with the artifact's own filename.
func downloadArtifact(p *services.Platform) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		runID := asRunID(chi.URLParam(r, "id"))
		artifactID := domain.ArtifactId(chi.URLParam(r, "artifactId"))
		name, content, err := p.Runs.ArtifactContent(runID, artifactID)
		if err != nil {
			handle(w, func() (any, int, error) { return nil, 0, err })
			return
		}
		ct := "text/plain; charset=utf-8"
		if strings.HasSuffix(name, ".md") {
			ct = "text/markdown; charset=utf-8"
		} else if strings.HasSuffix(name, ".json") {
			ct = "application/json; charset=utf-8"
		}
		w.Header().Set("Content-Type", ct)
		w.Header().Set("Content-Disposition", `attachment; filename="`+sanitizeFilename(name)+`"`)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(content))
	}
}

// sanitizeFilename strips characters that would break the Content-Disposition header.
func sanitizeFilename(name string) string {
	name = strings.ReplaceAll(name, `"`, "")
	name = strings.ReplaceAll(name, "\n", "")
	name = strings.ReplaceAll(name, "\r", "")
	if name == "" {
		return "artifact.txt"
	}
	return name
}

func getBrowserSession(p *services.Platform) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handle(w, func() (any, int, error) {
			runID := asRunID(chi.URLParam(r, "id"))
			session, ok := p.Browser.GetByRun(runID)
			if !ok {
				return nil, 0, domain.NotFound("browser session")
			}
			return session, http.StatusOK, nil
		})
	}
}

func listLogs(p *services.Platform) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handle(w, func() (any, int, error) {
			query, err := validate.ParseListLogsQuery(r.URL.Query())
			if err != nil {
				return nil, 0, err
			}
			return p.Logs.List(asRunID(chi.URLParam(r, "id")), query), http.StatusOK, nil
		})
	}
}

func listNotifications(p *services.Platform) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handle(w, func() (any, int, error) {
			query, err := validate.ParseListNotificationsQuery(r.URL.Query())
			if err != nil {
				return nil, 0, err
			}
			return p.Notifications.List(query), http.StatusOK, nil
		})
	}
}

func markNotificationRead(p *services.Platform) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handle(w, func() (any, int, error) {
			n, err := p.Notifications.MarkRead(domain.NotificationId(chi.URLParam(r, "id")))
			return n, http.StatusOK, err
		})
	}
}

func markAllNotificationsRead(p *services.Platform) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handle(w, func() (any, int, error) {
			return map[string]any{"updated": p.Notifications.MarkAllRead()}, http.StatusOK, nil
		})
	}
}
