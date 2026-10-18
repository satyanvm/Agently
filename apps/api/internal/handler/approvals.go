package handler

// approvals.go exposes the Inbox (services/approval_service.go).

import (
	"net/http"

	"github.com/agently/api/internal/domain"
	"github.com/agently/api/internal/domain/validate"
	"github.com/agently/api/internal/services"
	"github.com/go-chi/chi/v5"
)

func listApprovals(p *services.Platform) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handle(w, func() (any, int, error) {
			q, err := validate.ParseListApprovalsQuery(r.URL.Query())
			if err != nil {
				return nil, 0, err
			}
			return p.Approvals.List(q), http.StatusOK, nil
		})
	}
}

func getApproval(p *services.Platform) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handle(w, func() (any, int, error) {
			d, err := p.Approvals.Get(domain.ApprovalId(chi.URLParam(r, "id")))
			return d, http.StatusOK, err
		})
	}
}

func decideApproval(p *services.Platform) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handle(w, func() (any, int, error) {
			body, _ := decodeBody(r)
			in, err := validate.ParseDecisionInput(body)
			if err != nil {
				return nil, 0, err
			}
			d, err := p.Approvals.Decide(domain.ApprovalId(chi.URLParam(r, "id")), in)
			return d, http.StatusOK, err
		})
	}
}

func approvalHistory(p *services.Platform) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handle(w, func() (any, int, error) {
			h, err := p.Approvals.History(domain.ApprovalId(chi.URLParam(r, "id")))
			return h, http.StatusOK, err
		})
	}
}
