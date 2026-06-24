package handler

import (
	"net/http"

	"github.com/agently/api/internal/services"
)

// integrations.go exposes the OAuth2 "Connect Gmail" flow + connection status.
//
//	GET  /api/integrations                       → status of all providers
//	GET  /api/integrations/google/connect        → 302 to Google consent
//	GET  /api/integrations/google/callback       → handles Google's redirect
//	POST /api/integrations/google/disconnect     → remove the connection

// listIntegrations returns connection status for the providers the UI shows.
func listIntegrations(p *services.Platform) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handle(w, func() (any, int, error) {
			return map[string]any{
				"items": []services.IntegrationStatus{p.Integrations.Status("google")},
			}, http.StatusOK, nil
		})
	}
}

// connectGoogle redirects the browser to Google's consent screen.
func connectGoogle(p *services.Platform) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authURL, err := p.Integrations.AuthURL()
		if err != nil {
			// No JSON here — this is a browser navigation. Show a plain message.
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		http.Redirect(w, r, authURL, http.StatusFound)
	}
}

// googleCallback handles Google's redirect: exchange code → store token → bounce
// the browser back into the app (success or error reflected in the query string).
func googleCallback(p *services.Platform) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		state := r.URL.Query().Get("state")
		if err := p.Integrations.HandleCallback(r.Context(), code, state); err != nil {
			p.Logger.Error("google oauth callback failed", "error", err.Error())
			http.Redirect(w, r, p.Integrations.AppRedirect("error"), http.StatusFound)
			return
		}
		http.Redirect(w, r, p.Integrations.AppRedirect("connected"), http.StatusFound)
	}
}

// disconnectGoogle removes the stored Google connection.
func disconnectGoogle(p *services.Platform) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handle(w, func() (any, int, error) {
			return map[string]any{"disconnected": p.Integrations.Disconnect("google")}, http.StatusOK, nil
		})
	}
}
