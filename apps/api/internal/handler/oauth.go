package handler

// oauth.go exposes browser sign-in for OAuth2 credential types
// (services/oauth_service.go).
//
//	GET /api/oauth/apps            → checklist of popular providers' OAuth apps
//	GET /api/oauth/{type}          → OAuthAppStatus (supported/configured)
//	GET /api/oauth/{type}/start    → 302 to the provider consent screen
//	    ?name=&credentialId=&props=<json object of connection props>
//	GET /api/oauth/callback        → exchange code, store credential, 302 to
//	                                 the web app's /oauth/done popup page

import (
	"encoding/json"
	"net/http"

	"github.com/agently/api/internal/services"
	"github.com/go-chi/chi/v5"
)

func oauthStatus(p *services.Platform) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handle(w, func() (any, int, error) {
			return p.OAuth.Status(chi.URLParam(r, "type")), http.StatusOK, nil
		})
	}
}

// oauthStart is a popup navigation: failures land on /oauth/done with the
// reason so the opener can show it, never as a raw JSON page.
func oauthStart(p *services.Platform) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		var props map[string]any
		if raw := q.Get("props"); raw != "" {
			if err := json.Unmarshal([]byte(raw), &props); err != nil {
				http.Redirect(w, r, p.OAuth.DoneURL("error", "", "invalid connection fields"), http.StatusFound)
				return
			}
		}
		authURL, err := p.OAuth.StartURL(chi.URLParam(r, "type"), q.Get("name"), q.Get("credentialId"), props)
		if err != nil {
			http.Redirect(w, r, p.OAuth.DoneURL("error", "", err.Error()), http.StatusFound)
			return
		}
		http.Redirect(w, r, authURL, http.StatusFound)
	}
}

func oauthCallback(p *services.Platform) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		providerErr := q.Get("error_description")
		if providerErr == "" {
			providerErr = q.Get("error")
		}
		cred, err := p.OAuth.HandleCallback(r.Context(), q.Get("code"), q.Get("state"), providerErr)
		if err != nil {
			p.Logger.Error("oauth callback failed", "error", err.Error())
			http.Redirect(w, r, p.OAuth.DoneURL("error", "", err.Error()), http.StatusFound)
			return
		}
		http.Redirect(w, r, p.OAuth.DoneURL("connected", string(cred.ID), ""), http.StatusFound)
	}
}

func oauthApps(p *services.Platform) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handle(w, func() (any, int, error) {
			return p.OAuth.PopularApps(), http.StatusOK, nil
		})
	}
}
