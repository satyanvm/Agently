package handler

import (
	"net/http"

	"github.com/agently/api/internal/domain"
	"github.com/agently/api/internal/domain/validate"
	"github.com/agently/api/internal/services"
	"github.com/go-chi/chi/v5"
)

func listWorkflows(p *services.Platform) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handle(w, func() (any, int, error) {
			query, err := validate.ParsePageQuery(r.URL.Query())
			if err != nil {
				return nil, 0, err
			}
			return p.Workflows.List(query), http.StatusOK, nil
		})
	}
}

func getWorkflow(p *services.Platform) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handle(w, func() (any, int, error) {
			slug := chi.URLParam(r, "slug")
			summary, err := p.Workflows.GetBySlug(slug)
			return summary, http.StatusOK, err
		})
	}
}

func createWorkflow(p *services.Platform) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handle(w, func() (any, int, error) {
			body, _ := decodeBody(r)
			input, err := validate.ParseCreateWorkflowInput(body)
			if err != nil {
				return nil, 0, err
			}
			summary, err := p.Workflows.Create(input)
			if err != nil {
				return nil, 0, err
			}
			return summary, http.StatusCreated, nil
		})
	}
}

func listWorkflowRuns(p *services.Platform) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handle(w, func() (any, int, error) {
			slug := chi.URLParam(r, "slug")
			wf, err := p.Workflows.GetBySlug(slug)
			if err != nil {
				return nil, 0, err
			}
			query, err := validate.ParseListRunsQuery(r.URL.Query())
			if err != nil {
				return nil, 0, err
			}
			query.WorkflowID = string(wf.ID)
			return p.Runs.List(query), http.StatusOK, nil
		})
	}
}

func launchRun(p *services.Platform) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handle(w, func() (any, int, error) {
			slug := chi.URLParam(r, "slug")
			body, _ := decodeBody(r)
			input, err := validate.ParseLaunchRunInput(body)
			if err != nil {
				return nil, 0, err
			}
			detail, err := p.Runs.Launch(slug, input)
			if err != nil {
				return nil, 0, err
			}
			return detail, http.StatusCreated, nil
		})
	}
}

func createAgent(p *services.Platform) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handle(w, func() (any, int, error) {
			body, _ := decodeBody(r)
			input, err := validate.ParseCreateAgentInput(body)
			if err != nil {
				return nil, 0, err
			}
			agent, err := p.Agents.Create(input)
			if err != nil {
				return nil, 0, err
			}
			return agent, http.StatusCreated, nil
		})
	}
}

func listAgents(p *services.Platform) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handle(w, func() (any, int, error) {
			query, err := validate.ParsePageQuery(r.URL.Query())
			if err != nil {
				return nil, 0, err
			}
			return p.Agents.List(query), http.StatusOK, nil
		})
	}
}

func activityFeed(p *services.Platform) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handle(w, func() (any, int, error) {
			query, err := validate.ParsePageQuery(r.URL.Query())
			if err != nil {
				return nil, 0, err
			}
			return p.Activity.List(query), http.StatusOK, nil
		})
	}
}

// asRunID casts a path string into a RunId at the trust boundary.
func asRunID(s string) domain.RunId { return domain.RunId(s) }
