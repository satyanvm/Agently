package handler

import (
	"net/http"
	"testing"

	"github.com/agently/api/internal/domain"
)

func TestApprovalsAPI(t *testing.T) {
	p, h := intelServer(t)
	ws := p.WorkspaceID
	run := p.Repos.Runs.Insert(domain.Run{
		ID: domain.NewRunId(), WorkspaceID: ws, WorkflowID: "wf_watch", WorkflowName: "Competitor pricing",
		WorkflowSlug: "competitor-pricing", Number: 1, Status: domain.RunPaused, CostUsd: 0.0123,
		QueuedAt: "2026-10-18T09:00:00.000Z",
	})

	sig, _ := p.Repos.Signals.Insert(domain.Signal{
		ID: domain.NewSignalId(), WorkspaceID: ws, Source: "page_change", Title: "Acme pricing page changed",
		URL: "https://acme.test/pricing", RunID: &run.ID, DetectedAt: "2026-10-18T09:00:00.000Z",
	})
	open := func(node string, at domain.Timestamp) domain.Approval {
		return p.Repos.Approvals.Insert(domain.Approval{
			ID: domain.NewApprovalId(), WorkspaceID: ws, RunID: run.ID, NodeKey: node, Title: "LinkedIn post",
			Channel: "LinkedIn", Draft: "We ship faster than Acme.", Assignee: "Rahul",
			SignalIDs: []domain.SignalId{sig.ID}, CreatedAt: at,
		})
	}
	a := open("approve_post", "2026-10-18T09:01:00.000Z")
	b := open("approve_email", "2026-10-18T09:02:00.000Z")

	_, body := call(t, h, http.MethodGet, "/api/approvals?status=pending", nil)
	list := listOf(body)
	if len(list) != 2 {
		t.Fatalf("pending = %#v", body)
	}
	first := dataOf(list[0])
	signal := dataOf(first["signal"])
	if first["id"] != string(b.ID) || signal["title"] != "Acme pricing page changed" || first["workflowName"] != run.WorkflowName {
		t.Fatalf("list item = %#v", first)
	}

	rec, body := call(t, h, http.MethodPost, "/api/approvals/"+string(a.ID)+"/decision", map[string]any{
		"decision": "approved", "content": "We ship 2x faster than Acme.", "decidedBy": "Rahul",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("approve = %d %s", rec.Code, rec.Body.String())
	}
	d := dataOf(body)
	if d["status"] != "approved" || d["finalContent"] != "We ship 2x faster than Acme." || d["decidedBy"] != "Rahul" ||
		d["decidedAt"] == nil || d["signaledAt"] != nil {
		t.Fatalf("decision = %#v", d)
	}
	runInfo := dataOf(d["run"])
	if runInfo["id"] != string(run.ID) || len(listOf(d["signals"])) != 1 {
		t.Fatalf("detail should carry the run and its signals: %#v", d)
	}

	// The second click (another tab, another person) loses.
	if rec, _ := call(t, h, http.MethodPost, "/api/approvals/"+string(a.ID)+"/decision",
		map[string]any{"decision": "rejected", "decidedBy": "Ana"}); rec.Code != http.StatusConflict {
		t.Fatalf("second decision = %d, want 409", rec.Code)
	}

	for i, bad := range []map[string]any{
		{"decision": "approved"},                                            // no name
		{"decision": "maybe", "decidedBy": "Ana"},                           // bad decision
		{"decision": "timed_out", "decidedBy": "Ana"},                       // people can't time out
		{"decision": "approved", "decidedBy": "Ana", "content": "   "},      // empty edit
		{"decision": "approved", "decidedBy": "Ana", "content": float64(3)}, // wrong type
	} {
		if rec, _ := call(t, h, http.MethodPost, "/api/approvals/"+string(b.ID)+"/decision", bad); rec.Code == http.StatusOK {
			t.Fatalf("bad decision %d accepted", i)
		}
	}

	rec, body = call(t, h, http.MethodPost, "/api/approvals/"+string(b.ID)+"/decision",
		map[string]any{"decision": "rejected", "decidedBy": "Ana", "note": "off-brand"})
	if rec.Code != http.StatusOK || dataOf(body)["finalContent"] != nil || dataOf(body)["decisionNote"] != "off-brand" {
		t.Fatalf("reject = %d %s", rec.Code, rec.Body.String())
	}

	if rec, _ := call(t, h, http.MethodGet, "/api/approvals/apr_missing", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("missing = %d", rec.Code)
	}
	if rec, _ := call(t, h, http.MethodGet, "/api/approvals?status=waiting", nil); rec.Code == http.StatusOK {
		t.Fatal("unknown status filter should be rejected")
	}
	_, body = call(t, h, http.MethodGet, "/api/approvals?status=pending", nil)
	if len(listOf(body)) != 0 {
		t.Fatalf("nothing should be pending now: %#v", body)
	}
}
