package handler

// hooks.go — trigger ingress + lifecycle for piece triggers
// (docs/pieces-runtime-contract.md, triggers section).
//
//	POST /api/hooks/{slug}/{nodeKey}                       webhook ingress
//	POST /api/workflows/{slug}/triggers/{nodeKey}/enable   register with provider
//	POST /api/workflows/{slug}/triggers/{nodeKey}/disable  deregister
//	POST /api/workflows/{slug}/triggers/{nodeKey}/poll     one polling tick (manual)
//
// A webhook delivery is transformed into events by the piece trigger's real
// run() on the pieces worker BEFORE any run exists; each event launches a
// temporal run carrying it as run.input.__trigger_event. The builtin
// trigger.webhook node skips transformation — the raw delivery is the event.

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/agently/api/internal/domain"
	"github.com/agently/api/internal/services"
	"github.com/go-chi/chi/v5"
)

func pieceHook(p *services.Platform) http.HandlerFunc {
	client := services.NewPieceTriggerClient()
	return func(w http.ResponseWriter, r *http.Request) {
		handle(w, func() (any, int, error) {
			slug := chi.URLParam(r, "slug")
			nodeKey := chi.URLParam(r, "nodeKey")
			wf, node, err := findTriggerNode(p, slug, nodeKey)
			if err != nil {
				return nil, 0, err
			}

			payload := webhookPayload(r)
			var event any
			if req, ok := services.BuildTriggerRequest(string(wf.ID), node); ok {
				req.Payload = payload
				req.WebhookURL = services.WebhookURLFor(slug, nodeKey)
				res, callErr := client.RunTrigger(req)
				switch {
				case callErr != nil:
					// Worker down: don't drop the delivery — pass the raw payload
					// through, honestly flagged, so the run still records it.
					event = map[string]any{"raw": true, "reason": "pieces-worker-unavailable", "payload": payload}
				case !res.OK:
					return map[string]any{"ok": false, "error": res.Error, "errorType": res.ErrorType}, http.StatusOK, nil
				case len(res.Events) == 0:
					return map[string]any{"ok": true, "accepted": true, "runs": 0}, http.StatusOK, nil
				default:
					event = res.Events[0] // v1: first event only (documented limitation)
				}
			} else {
				event = payload // builtin trigger.webhook
			}

			detail, err := p.Runs.Launch(slug, services.LaunchInputForTriggerEvent(event, domain.TriggerWebhook))
			if err != nil {
				return nil, 0, err
			}
			return map[string]any{"ok": true, "accepted": true, "runs": 1, "runId": detail.ID}, http.StatusCreated, nil
		})
	}
}

func triggerLifecycle(p *services.Platform, op string) http.HandlerFunc {
	client := services.NewPieceTriggerClient()
	return func(w http.ResponseWriter, r *http.Request) {
		handle(w, func() (any, int, error) {
			slug := chi.URLParam(r, "slug")
			nodeKey := chi.URLParam(r, "nodeKey")
			wf, node, err := findTriggerNode(p, slug, nodeKey)
			if err != nil {
				return nil, 0, err
			}
			req, ok := services.BuildTriggerRequest(string(wf.ID), node)
			if !ok {
				return nil, 0, domain.BadRequest("node " + nodeKey + " is not a pieces trigger")
			}
			req.Op = op
			req.WebhookURL = services.WebhookURLFor(slug, nodeKey)
			out, callErr := client.Lifecycle(req)
			if callErr != nil {
				return map[string]any{"ok": false, "error": "pieces worker unavailable", "errorType": "TriggerWorkerUnavailable"}, http.StatusOK, nil
			}
			return out, http.StatusOK, nil
		})
	}
}

func triggerPoll(p *services.Platform) http.HandlerFunc {
	client := services.NewPieceTriggerClient()
	return func(w http.ResponseWriter, r *http.Request) {
		handle(w, func() (any, int, error) {
			slug := chi.URLParam(r, "slug")
			nodeKey := chi.URLParam(r, "nodeKey")
			wf, node, err := findTriggerNode(p, slug, nodeKey)
			if err != nil {
				return nil, 0, err
			}
			req, ok := services.BuildTriggerRequest(string(wf.ID), node)
			if !ok {
				return nil, 0, domain.BadRequest("node " + nodeKey + " is not a pieces trigger")
			}
			res, callErr := client.RunTrigger(req)
			if callErr != nil {
				return map[string]any{"ok": false, "error": "pieces worker unavailable", "errorType": "TriggerWorkerUnavailable"}, http.StatusOK, nil
			}
			if !res.OK {
				return map[string]any{"ok": false, "error": res.Error, "errorType": res.ErrorType}, http.StatusOK, nil
			}
			runIds := []string{}
			for i, ev := range res.Events {
				if i >= 3 {
					break
				}
				detail, launchErr := p.Runs.Launch(slug, services.LaunchInputForTriggerEvent(ev, domain.TriggerSchedule))
				if launchErr != nil {
					return nil, 0, launchErr
				}
				runIds = append(runIds, string(detail.ID))
			}
			return map[string]any{"ok": true, "events": len(res.Events), "runs": len(runIds), "runIds": runIds}, http.StatusOK, nil
		})
	}
}

func findTriggerNode(p *services.Platform, slug, nodeKey string) (domain.WorkflowSummary, domain.GraphNode, error) {
	wf, err := p.Workflows.GetBySlug(slug)
	if err != nil {
		return domain.WorkflowSummary{}, domain.GraphNode{}, err
	}
	nodes, err := p.Workflows.GraphNodes(slug)
	if err != nil {
		return domain.WorkflowSummary{}, domain.GraphNode{}, err
	}
	for _, n := range nodes {
		if n.Key == nodeKey {
			return wf, n, nil
		}
	}
	return domain.WorkflowSummary{}, domain.GraphNode{}, domain.NotFound("trigger node " + nodeKey)
}

// webhookPayload captures a delivery as {body, headers, queryParams} — the
// shape the worker's trigger context exposes as ctx.payload.
func webhookPayload(r *http.Request) map[string]any {
	raw, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	var body any = map[string]any{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &body); err != nil {
			body = string(raw)
		}
	}
	headers := map[string]string{}
	for k := range r.Header {
		headers[strings.ToLower(k)] = r.Header.Get(k)
	}
	query := map[string]string{}
	for k := range r.URL.Query() {
		query[k] = r.URL.Query().Get(k)
	}
	return map[string]any{"body": body, "headers": headers, "queryParams": query}
}
