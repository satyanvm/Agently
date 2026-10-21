package handler

// playbooks.go exposes the ready-made playbooks (services/playbooks.go).

import (
	"net/http"

	"github.com/agently/api/internal/services"
	"github.com/go-chi/chi/v5"
)

func listPlaybooks(p *services.Platform) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handle(w, func() (any, int, error) {
			out, err := p.Playbooks.List()
			return out, http.StatusOK, err
		})
	}
}

func getPlaybook(p *services.Platform) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handle(w, func() (any, int, error) {
			out, err := p.Playbooks.Get(chi.URLParam(r, "id"))
			return out, http.StatusOK, err
		})
	}
}

func installPlaybook(p *services.Platform) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handle(w, func() (any, int, error) {
			body, _ := decodeBody(r)
			name, _ := body["name"].(string)
			inputs, _ := body["inputs"].(map[string]any)
			if inputs == nil {
				inputs = map[string]any{}
			}
			out, err := p.Playbooks.Install(chi.URLParam(r, "id"), name, inputs)
			return out, http.StatusCreated, err
		})
	}
}
