package handler

// intel.go exposes competitors, page snapshots and signals
// (services/intel_service.go has the route list).

import (
	"net/http"
	"strconv"

	"github.com/agently/api/internal/domain"
	"github.com/agently/api/internal/domain/validate"
	"github.com/agently/api/internal/services"
	"github.com/go-chi/chi/v5"
)

func listCompetitors(p *services.Platform) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handle(w, func() (any, int, error) {
			return p.Intel.ListCompetitors(r.URL.Query().Get("includeArchived") == "true"), http.StatusOK, nil
		})
	}
}

func createCompetitor(p *services.Platform) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handle(w, func() (any, int, error) {
			body, _ := decodeBody(r)
			input, err := validate.ParseCreateCompetitorInput(body)
			if err != nil {
				return nil, 0, err
			}
			c, err := p.Intel.CreateCompetitor(input)
			if err != nil {
				return nil, 0, err
			}
			return c, http.StatusCreated, nil
		})
	}
}

func getCompetitor(p *services.Platform) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handle(w, func() (any, int, error) {
			c, err := p.Intel.GetCompetitor(domain.CompetitorId(chi.URLParam(r, "id")))
			return c, http.StatusOK, err
		})
	}
}

func updateCompetitor(p *services.Platform) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handle(w, func() (any, int, error) {
			body, _ := decodeBody(r)
			input, err := validate.ParseUpdateCompetitorInput(body)
			if err != nil {
				return nil, 0, err
			}
			c, err := p.Intel.UpdateCompetitor(domain.CompetitorId(chi.URLParam(r, "id")), input)
			return c, http.StatusOK, err
		})
	}
}

func archiveCompetitor(p *services.Platform) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := p.Intel.ArchiveCompetitor(domain.CompetitorId(chi.URLParam(r, "id"))); err != nil {
			handle(w, func() (any, int, error) { return nil, 0, err })
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func listCompetitorSnapshots(p *services.Platform) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handle(w, func() (any, int, error) {
			limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
			if limit <= 0 || limit > 200 {
				limit = 50
			}
			out, err := p.Intel.ListSnapshots(domain.CompetitorId(chi.URLParam(r, "id")), limit)
			return out, http.StatusOK, err
		})
	}
}

func getSnapshot(p *services.Platform) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handle(w, func() (any, int, error) {
			sn, err := p.Intel.GetSnapshot(domain.PageSnapshotId(chi.URLParam(r, "id")))
			return sn, http.StatusOK, err
		})
	}
}

func listSignals(p *services.Platform) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handle(w, func() (any, int, error) {
			q, err := validate.ParseListSignalsQuery(r.URL.Query())
			if err != nil {
				return nil, 0, err
			}
			return p.Intel.ListSignals(q), http.StatusOK, nil
		})
	}
}

func getSignal(p *services.Platform) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handle(w, func() (any, int, error) {
			sg, err := p.Intel.GetSignal(domain.SignalId(chi.URLParam(r, "id")))
			return sg, http.StatusOK, err
		})
	}
}

func getBrand(p *services.Platform) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handle(w, func() (any, int, error) { return p.Intel.GetBrand(), http.StatusOK, nil })
	}
}

func updateBrand(p *services.Platform) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handle(w, func() (any, int, error) {
			body, _ := decodeBody(r)
			in, err := validate.ParseBrandInput(body)
			if err != nil {
				return nil, 0, err
			}
			return p.Intel.UpdateBrand(in), http.StatusOK, nil
		})
	}
}
