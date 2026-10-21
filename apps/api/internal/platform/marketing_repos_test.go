package platform

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/agently/api/internal/domain"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The same contract runs against the in-memory store and, when
// AGENTLY_TEST_DATABASE_URL points at a migrated test database, Postgres.
// (RUNBOOK.md "Verification" shows how to create one. Never point it at a
// database whose data you care about: the test creates and deletes a
// throwaway workspace, but it does write.)

func ts(offset time.Duration) domain.Timestamp {
	return domain.Timestamp(time.Date(2026, 10, 18, 9, 0, 0, 0, time.UTC).Add(offset).Format("2006-01-02T15:04:05.000Z"))
}

func TestMarketingReposMemory(t *testing.T) {
	ws := domain.WorkspaceId("ws_test")
	repos := NewMemoryRepositories(MemoryData{Workspace: domain.Workspace{ID: ws}})
	runMarketingContract(t, repos, ws, domain.RunId("run_a"), domain.RunId("run_b"))
}

func TestMarketingReposPostgres(t *testing.T) {
	url := os.Getenv("AGENTLY_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("AGENTLY_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	// Cleanups run last-in-first-out: closing the pool is registered first so
	// the workspace delete below still has a connection.
	t.Cleanup(pool.Close)
	if err := EnsureSchema(ctx, pool); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	// Twice: every upgrade must be idempotent.
	if err := EnsureSchema(ctx, pool); err != nil {
		t.Fatalf("EnsureSchema (second run): %v", err)
	}

	ws := domain.NewWorkspaceId()
	wf := domain.NewWorkflowId()
	runA, runB := domain.NewRunId(), domain.NewRunId()
	mustExec := func(q string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	mustExec(`insert into workspaces (id, name, slug) values ($1, 'test', $1)`, string(ws))
	t.Cleanup(func() {
		if _, err := pool.Exec(ctx, `delete from workspaces where id=$1`, string(ws)); err != nil {
			t.Errorf("cleanup: %v", err)
		}
	})
	mustExec(`insert into workflows (id, workspace_id, slug, name) values ($1, $2, 'wf', 'wf')`, string(wf), string(ws))
	for i, r := range []domain.RunId{runA, runB} {
		mustExec(`insert into runs (id, workspace_id, workflow_id, number, triggered_by, region)
		          values ($1, $2, $3, $4, '{}', 'local')`, string(r), string(ws), string(wf), i+1)
	}
	repos := NewPostgresRepositories(pool, NewLogger(false))
	runMarketingContract(t, repos, ws, runA, runB)
}

func runMarketingContract(t *testing.T, repos *Repositories, ws domain.WorkspaceId, runA, runB domain.RunId) {
	t.Run("competitors", func(t *testing.T) {
		acme := repos.Competitors.Insert(domain.Competitor{
			ID: domain.NewCompetitorId(), WorkspaceID: ws, Name: "Acme", Website: "https://acme.test",
			Pages: []domain.CompetitorPage{
				{Kind: domain.PageKindPricing, URL: "https://acme.test/pricing", Render: domain.RenderHTTP},
			},
			CreatedAt: ts(0), UpdatedAt: ts(0),
		})
		repos.Competitors.Insert(domain.Competitor{
			ID: domain.NewCompetitorId(), WorkspaceID: ws, Name: "Beta", CreatedAt: ts(0), UpdatedAt: ts(0),
		})

		got, ok := repos.Competitors.GetByID(acme.ID)
		if !ok || got.Name != "Acme" || len(got.Pages) != 1 || got.Pages[0].URL != "https://acme.test/pricing" {
			t.Fatalf("GetByID = %+v, %v", got, ok)
		}

		pages := []domain.CompetitorPage{
			{Kind: domain.PageKindPricing, URL: "https://acme.test/pricing", Render: domain.RenderHTTP},
			{Kind: domain.PageKindChangelog, URL: "https://acme.test/changelog", Render: domain.RenderBrowser},
		}
		name, archived := "Acme Inc", ts(time.Hour)
		archivedPtr := &archived
		upd, err := repos.Competitors.Update(acme.ID, CompetitorPatch{Name: &name, Pages: &pages, ArchivedAt: &archivedPtr})
		if err != nil || upd.Name != "Acme Inc" || len(upd.Pages) != 2 || upd.ArchivedAt == nil {
			t.Fatalf("Update = %+v, %v", upd, err)
		}
		if active := repos.Competitors.ListByWorkspace(ws, false); len(active) != 1 || active[0].Name != "Beta" {
			t.Fatalf("active list = %+v", active)
		}
		if all := repos.Competitors.ListByWorkspace(ws, true); len(all) != 2 || all[0].Name != "Acme Inc" {
			t.Fatalf("full list = %+v", all)
		}
		var unarchive *domain.Timestamp
		if upd, _ := repos.Competitors.Update(acme.ID, CompetitorPatch{ArchivedAt: &unarchive}); upd.ArchivedAt != nil {
			t.Fatalf("unarchive left archivedAt = %v", *upd.ArchivedAt)
		}
		if _, err := repos.Competitors.Update(domain.NewCompetitorId(), CompetitorPatch{Name: &name}); err == nil {
			t.Fatal("updating a missing competitor should fail")
		}
	})

	t.Run("snapshots and signals", func(t *testing.T) {
		cmp := repos.Competitors.Insert(domain.Competitor{
			ID: domain.NewCompetitorId(), WorkspaceID: ws, Name: "Gamma", CreatedAt: ts(0), UpdatedAt: ts(0),
		})
		url := "https://gamma.test/pricing"
		code := 200
		var ids []domain.PageSnapshotId
		for i, content := range []string{"Starter $10", "Starter $12"} {
			sn := repos.Snapshots.Insert(domain.PageSnapshot{
				ID: domain.NewPageSnapshotId(), WorkspaceID: ws, CompetitorID: &cmp.ID,
				PageKind: domain.PageKindPricing, URL: url, RunID: &runA, FetchedVia: domain.RenderHTTP,
				StatusCode: &code, Title: "Pricing", ContentHash: content, Content: content,
				FetchedAt: ts(time.Duration(i) * time.Hour), LastSeenAt: ts(time.Duration(i) * time.Hour),
			})
			ids = append(ids, sn.ID)
		}
		byURL := repos.Snapshots.ListByURL(ws, url, 10)
		if len(byURL) != 2 || byURL[0].Content != "Starter $12" || byURL[1].ID != ids[0] {
			t.Fatalf("ListByURL (newest first) = %+v", byURL)
		}
		if one := repos.Snapshots.ListByCompetitor(cmp.ID, 1); len(one) != 1 || one[0].ID != ids[1] {
			t.Fatalf("ListByCompetitor limit 1 = %+v", one)
		}
		if got, ok := repos.Snapshots.GetByID(ids[0]); !ok || *got.StatusCode != 200 || got.RunID == nil || *got.RunID != runA {
			t.Fatalf("GetByID = %+v, %v", got, ok)
		}

		key := "page_change:" + url + ":" + "Starter $12"
		sig, created := repos.Signals.Insert(domain.Signal{
			ID: domain.NewSignalId(), WorkspaceID: ws, Source: domain.SignalPageChange, CompetitorID: &cmp.ID,
			Title: "Gamma pricing changed", URL: url, Detail: map[string]any{"added": []any{"Starter $12"}},
			SnapshotID: &ids[1], PreviousSnapshotID: &ids[0], RunID: &runA, DedupeKey: &key, DetectedAt: ts(time.Hour),
		})
		if !created {
			t.Fatal("first insert should create")
		}
		dup, created := repos.Signals.Insert(domain.Signal{
			ID: domain.NewSignalId(), WorkspaceID: ws, Source: domain.SignalPageChange, Title: "dup",
			RunID: &runB, DedupeKey: &key, DetectedAt: ts(2 * time.Hour),
		})
		if created || dup.ID != sig.ID || dup.Title != "Gamma pricing changed" {
			t.Fatalf("dedupe insert = %+v, created=%v", dup, created)
		}
		repos.Signals.Insert(domain.Signal{
			ID: domain.NewSignalId(), WorkspaceID: ws, Source: domain.SignalNews, Title: "Gamma raises",
			RunID: &runB, DetectedAt: ts(3 * time.Hour),
		})

		all := repos.Signals.List(ws, SignalQuery{})
		if len(all) != 2 || all[0].Title != "Gamma raises" {
			t.Fatalf("List newest first = %+v", all)
		}
		if got := repos.Signals.List(ws, SignalQuery{RunID: &runA}); len(got) != 1 || got[0].ID != sig.ID {
			t.Fatalf("List by run = %+v", got)
		}
		if got := repos.Signals.List(ws, SignalQuery{Source: domain.SignalNews}); len(got) != 1 {
			t.Fatalf("List by source = %+v", got)
		}
		since := ts(2 * time.Hour)
		if got := repos.Signals.List(ws, SignalQuery{Since: &since}); len(got) != 1 || got[0].Source != domain.SignalNews {
			t.Fatalf("List since = %+v", got)
		}
		if got := repos.Signals.List(ws, SignalQuery{CompetitorID: &cmp.ID}); len(got) != 1 {
			t.Fatalf("List by competitor = %+v", got)
		}
		got, ok := repos.Signals.GetByID(sig.ID)
		if !ok || got.SnapshotID == nil || *got.PreviousSnapshotID != ids[0] {
			t.Fatalf("GetByID = %+v", got)
		}
		if added, _ := got.Detail["added"].([]any); len(added) != 1 || added[0] != "Starter $12" {
			t.Fatalf("detail round trip = %#v", got.Detail)
		}
	})

	t.Run("brand context", func(t *testing.T) {
		if _, ok := repos.Brand.Get(ws); ok {
			t.Fatal("no brand context yet")
		}
		repos.Brand.Upsert(domain.BrandContext{WorkspaceID: ws, Voice: "plain", UpdatedAt: ts(0), UpdatedBy: "Sam"})
		repos.Brand.Upsert(domain.BrandContext{
			WorkspaceID: ws, Voice: "plain, direct", AvoidClaims: "never say 'best'", UpdatedAt: ts(time.Hour), UpdatedBy: "Ana",
		})
		b, ok := repos.Brand.Get(ws)
		if !ok || b.Voice != "plain, direct" || b.AvoidClaims != "never say 'best'" || b.UpdatedBy != "Ana" || b.UpdatedAt != ts(time.Hour) {
			t.Fatalf("Get after upsert = %+v, %v", b, ok)
		}
	})

	t.Run("approvals", func(t *testing.T) {
		sig, _ := repos.Signals.Insert(domain.Signal{
			ID: domain.NewSignalId(), WorkspaceID: ws, Source: domain.SignalOther, Title: "x", DetectedAt: ts(0),
		})
		open := func(runID domain.RunId, node string, at time.Duration) domain.Approval {
			return repos.Approvals.Insert(domain.Approval{
				ID: domain.NewApprovalId(), WorkspaceID: ws, RunID: runID, NodeKey: node,
				Title: "LinkedIn post", Channel: "LinkedIn", Draft: "We ship faster.", Assignee: "Rahul",
				Notify: map[string]any{"slack": "sent"}, SignalIDs: []domain.SignalId{sig.ID}, CreatedAt: ts(at),
			})
		}
		a := open(runB, "approve_post", 0)
		if a.Status != domain.ApprovalPending || len(a.SignalIDs) != 1 || a.Notify["slack"] != "sent" {
			t.Fatalf("Insert = %+v", a)
		}
		// A retried open-request returns the row already there.
		if again := open(runB, "approve_post", time.Minute); again.ID != a.ID {
			t.Fatalf("retried Insert made a new row: %s vs %s", again.ID, a.ID)
		}
		b := open(runB, "approve_email", time.Hour)

		pending := domain.ApprovalPending
		if got := repos.Approvals.List(ws, ApprovalQuery{Status: &pending}); len(got) != 2 || got[0].ID != b.ID {
			t.Fatalf("pending newest first = %+v", got)
		}

		edited := "We ship 2x faster."
		dec, err := repos.Approvals.Decide(a.ID, domain.ApprovalDecision{
			Status: domain.ApprovalApproved, FinalContent: &edited, DecidedBy: "Rahul", DecidedAt: ts(2 * time.Hour),
		})
		if err != nil || dec.Status != domain.ApprovalApproved || *dec.FinalContent != edited ||
			*dec.DecidedBy != "Rahul" || dec.DecidedAt == nil || dec.SignaledAt != nil {
			t.Fatalf("Decide approve = %+v, %v", dec, err)
		}
		var de *domain.DomainError
		_, err = repos.Approvals.Decide(a.ID, domain.ApprovalDecision{
			Status: domain.ApprovalRejected, DecidedBy: "Ana", DecidedAt: ts(3 * time.Hour),
		})
		if !errors.As(err, &de) || de.Code != domain.CodeConflict || !strings.Contains(de.Message, "approved") {
			t.Fatalf("second decision should conflict, got %v", err)
		}

		note := "off-brand"
		rej, err := repos.Approvals.Decide(b.ID, domain.ApprovalDecision{
			Status: domain.ApprovalRejected, FinalContent: &edited, DecidedBy: "Ana", Note: &note, DecidedAt: ts(3 * time.Hour),
		})
		if err != nil || rej.FinalContent != nil || *rej.DecisionNote != "off-brand" {
			t.Fatalf("reject = %+v, %v (a rejection must not carry content to publish)", rej, err)
		}
		if _, err := repos.Approvals.Decide(b.ID, domain.ApprovalDecision{Status: domain.ApprovalTimedOut, DecidedBy: "x", DecidedAt: ts(0)}); err == nil {
			t.Fatal("people can only approve or reject")
		}
		if _, err := repos.Approvals.Decide(domain.NewApprovalId(), domain.ApprovalDecision{Status: domain.ApprovalApproved, DecidedBy: "x", DecidedAt: ts(0)}); err == nil {
			t.Fatal("deciding a missing approval should fail")
		}

		// Approving without edits publishes the draft as written.
		c := open(runA, "approve_blog", 0)
		plain, _ := repos.Approvals.Decide(c.ID, domain.ApprovalDecision{Status: domain.ApprovalApproved, DecidedBy: "Sam", DecidedAt: ts(time.Hour)})
		if plain.FinalContent == nil || *plain.FinalContent != "We ship faster." {
			t.Fatalf("unedited approval content = %v", plain.FinalContent)
		}
		if got := repos.Approvals.List(ws, ApprovalQuery{RunID: &runA}); len(got) != 1 || got[0].ID != c.ID {
			t.Fatalf("List by run = %+v", got)
		}

		repos.Approvals.InsertOutcome(domain.ApprovalOutcome{
			ID: domain.NewApprovalOutcomeId(), ApprovalID: a.ID, RunID: runB, NodeKey: "post", NodeType: "pieces.linkedin.create_share_update",
			Status: "succeeded", Summary: "Posted", URL: "https://linkedin.test/p/1", Output: map[string]any{"id": "1"}, OccurredAt: ts(5 * time.Hour),
		})
		repos.Approvals.InsertOutcome(domain.ApprovalOutcome{
			ID: domain.NewApprovalOutcomeId(), ApprovalID: a.ID, RunID: runB, NodeKey: "notify", NodeType: "slack.sendMessage",
			Status: "succeeded", OccurredAt: ts(4 * time.Hour),
		})
		outs := repos.Approvals.ListOutcomes(a.ID)
		if len(outs) != 2 || outs[0].NodeKey != "notify" || outs[1].URL != "https://linkedin.test/p/1" || outs[1].Output["id"] != "1" {
			t.Fatalf("outcomes oldest first = %+v", outs)
		}
	})
}
