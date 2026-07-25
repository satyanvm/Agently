package handler

// credentials.go exposes the DB-backed credential store
// (docs/credentials-contract.md §4). Secret values are WRITE-ONLY: every
// response is a summary (id/name/type/setKeys/timestamps) — stored values never
// leave the API.
//
//	GET    /api/credentials        → list summaries
//	POST   /api/credentials        → create (201 + summary)
//	PUT    /api/credentials/{id}   → rename / merge values per-key (200 + summary)
//	DELETE /api/credentials/{id}   → 204

import (
	"net/http"

	"github.com/agently/api/internal/domain"
	"github.com/agently/api/internal/domain/validate"
	"github.com/agently/api/internal/services"
	"github.com/go-chi/chi/v5"
)

func listCredentials(p *services.Platform) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handle(w, func() (any, int, error) {
			return p.Credentials.List(), http.StatusOK, nil
		})
	}
}

func createCredential(p *services.Platform) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handle(w, func() (any, int, error) {
			body, _ := decodeBody(r)
			input, err := validate.ParseCreateCredentialInput(body)
			if err != nil {
				return nil, 0, err
			}
			summary, err := p.Credentials.Create(input.Name, input.Type, input.Values)
			if err != nil {
				return nil, 0, err
			}
			return summary, http.StatusCreated, nil
		})
	}
}

func updateCredential(p *services.Platform) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handle(w, func() (any, int, error) {
			body, _ := decodeBody(r)
			input, err := validate.ParseUpdateCredentialInput(body)
			if err != nil {
				return nil, 0, err
			}
			id := domain.CredentialId(chi.URLParam(r, "id"))
			summary, err := p.Credentials.Update(id, input.Name, input.Values)
			if err != nil {
				return nil, 0, err
			}
			return summary, http.StatusOK, nil
		})
	}
}

func deleteCredential(p *services.Platform) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := domain.CredentialId(chi.URLParam(r, "id"))
		if err := p.Credentials.Delete(id); err != nil {
			handle(w, func() (any, int, error) { return nil, 0, err })
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
