package platform

import (
	"errors"
	"fmt"
	"strings"

	"github.com/agently/api/internal/domain"
	"github.com/jackc/pgx/v5"
)

// Postgres repositories for the signal → approval loop
// (packages/db/migrations/0015_marketing_loop.sql). Same semantics as
// memory_marketing.go.

func strPtrOrNil(s *string) *string {
	if s == nil {
		return nil
	}
	v := *s
	return &v
}

func typedPtr[T ~string](s *string) *T {
	if s == nil {
		return nil
	}
	v := T(*s)
	return &v
}

/* --------------------------- competitors --------------------------- */

type pgCompetitorRepo struct{ s *pgStore }

const competitorCols = `id, workspace_id, name, website, pages, notes, created_at, updated_at, archived_at`

func scanCompetitor(row pgx.Row) (domain.Competitor, error) {
	var c domain.Competitor
	var id, ws string
	var pages []byte
	var created, updated, archived any
	if err := row.Scan(&id, &ws, &c.Name, &c.Website, &pages, &c.Notes, &created, &updated, &archived); err != nil {
		return c, err
	}
	c.ID, c.WorkspaceID = domain.CompetitorId(id), domain.WorkspaceId(ws)
	c.Pages = []domain.CompetitorPage{}
	jsonInto(pages, &c.Pages)
	c.CreatedAt, c.UpdatedAt, c.ArchivedAt = anyTs(created), anyTs(updated), anyTsPtr(archived)
	return c, nil
}

func (r *pgCompetitorRepo) ListByWorkspace(workspaceID domain.WorkspaceId, includeArchived bool) []domain.Competitor {
	q := `select ` + competitorCols + ` from competitors where workspace_id=$1`
	if !includeArchived {
		q += ` and archived_at is null`
	}
	rows, err := r.s.pool.Query(bg(), q+` order by name`, string(workspaceID))
	if err != nil {
		r.s.fail("competitor.List", err)
		return nil
	}
	defer rows.Close()
	out := []domain.Competitor{}
	for rows.Next() {
		c, err := scanCompetitor(rows)
		if err != nil {
			r.s.fail("competitor.List scan", err)
			return out
		}
		out = append(out, c)
	}
	return out
}

func (r *pgCompetitorRepo) GetByID(id domain.CompetitorId) (domain.Competitor, bool) {
	c, err := scanCompetitor(r.s.pool.QueryRow(bg(),
		`select `+competitorCols+` from competitors where id=$1`, string(id)))
	if err != nil {
		return domain.Competitor{}, false
	}
	return c, true
}

func (r *pgCompetitorRepo) Insert(c domain.Competitor) domain.Competitor {
	if c.Pages == nil {
		c.Pages = []domain.CompetitorPage{}
	}
	_, err := r.s.pool.Exec(bg(),
		`insert into competitors (`+competitorCols+`) values ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		string(c.ID), string(c.WorkspaceID), c.Name, c.Website, jsonArg(c.Pages), c.Notes,
		tsArg(c.CreatedAt), tsArg(c.UpdatedAt), tsPtrArg(c.ArchivedAt))
	r.s.fail("competitor.Insert", err)
	return c
}

func (r *pgCompetitorRepo) Update(id domain.CompetitorId, patch CompetitorPatch) (domain.Competitor, error) {
	b := newSetBuilder()
	if patch.Name != nil {
		b.add("name", *patch.Name)
	}
	if patch.Website != nil {
		b.add("website", *patch.Website)
	}
	if patch.Pages != nil {
		b.add("pages", jsonArg(*patch.Pages))
	}
	if patch.Notes != nil {
		b.add("notes", *patch.Notes)
	}
	if patch.UpdatedAt != nil {
		b.add("updated_at", tsArg(*patch.UpdatedAt))
	}
	if patch.ArchivedAt != nil {
		b.add("archived_at", tsPtrArg(*patch.ArchivedAt))
	}
	if err := b.exec(r.s, "competitors", string(id)); err != nil {
		return domain.Competitor{}, err
	}
	c, ok := r.GetByID(id)
	if !ok {
		return domain.Competitor{}, domain.NotFound("Competitor")
	}
	return c, nil
}

/* ---------------------------- snapshots ---------------------------- */

type pgSnapshotRepo struct{ s *pgStore }

const snapshotCols = `id, workspace_id, competitor_id, page_kind, url, run_id, fetched_via,
  status_code, title, content_hash, content, fetched_at, last_seen_at`

func scanSnapshot(row pgx.Row) (domain.PageSnapshot, error) {
	var sn domain.PageSnapshot
	var id, ws string
	var competitorID, pageKind, runID *string
	var fetched, lastSeen any
	if err := row.Scan(&id, &ws, &competitorID, &pageKind, &sn.URL, &runID, &sn.FetchedVia,
		&sn.StatusCode, &sn.Title, &sn.ContentHash, &sn.Content, &fetched, &lastSeen); err != nil {
		return sn, err
	}
	sn.ID, sn.WorkspaceID = domain.PageSnapshotId(id), domain.WorkspaceId(ws)
	sn.CompetitorID = typedPtr[domain.CompetitorId](competitorID)
	sn.RunID = typedPtr[domain.RunId](runID)
	if pageKind != nil {
		sn.PageKind = *pageKind
	}
	sn.FetchedAt, sn.LastSeenAt = anyTs(fetched), anyTs(lastSeen)
	return sn, nil
}

func (r *pgSnapshotRepo) list(op, where string, limit int, args ...any) []domain.PageSnapshot {
	q := fmt.Sprintf(`select %s from page_snapshots where %s order by fetched_at desc limit %d`,
		snapshotCols, where, limitOr(limit, 50))
	rows, err := r.s.pool.Query(bg(), q, args...)
	if err != nil {
		r.s.fail(op, err)
		return nil
	}
	defer rows.Close()
	out := []domain.PageSnapshot{}
	for rows.Next() {
		sn, err := scanSnapshot(rows)
		if err != nil {
			r.s.fail(op+" scan", err)
			return out
		}
		out = append(out, sn)
	}
	return out
}

func (r *pgSnapshotRepo) ListByCompetitor(id domain.CompetitorId, limit int) []domain.PageSnapshot {
	return r.list("snapshot.ListByCompetitor", "competitor_id=$1", limit, string(id))
}

func (r *pgSnapshotRepo) ListByURL(workspaceID domain.WorkspaceId, url string, limit int) []domain.PageSnapshot {
	return r.list("snapshot.ListByURL", "workspace_id=$1 and url=$2", limit, string(workspaceID), url)
}

func (r *pgSnapshotRepo) GetByID(id domain.PageSnapshotId) (domain.PageSnapshot, bool) {
	sn, err := scanSnapshot(r.s.pool.QueryRow(bg(),
		`select `+snapshotCols+` from page_snapshots where id=$1`, string(id)))
	if err != nil {
		return domain.PageSnapshot{}, false
	}
	return sn, true
}

func (r *pgSnapshotRepo) Insert(sn domain.PageSnapshot) domain.PageSnapshot {
	_, err := r.s.pool.Exec(bg(),
		`insert into page_snapshots (`+snapshotCols+`) values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		string(sn.ID), string(sn.WorkspaceID), idPtrArg(sn.CompetitorID), nilIfEmptyStr(sn.PageKind), sn.URL,
		idPtrArg(sn.RunID), sn.FetchedVia, sn.StatusCode, sn.Title, sn.ContentHash, sn.Content,
		tsArg(sn.FetchedAt), tsArg(sn.LastSeenAt))
	r.s.fail("snapshot.Insert", err)
	return sn
}

/* ----------------------------- signals ----------------------------- */

type pgSignalRepo struct{ s *pgStore }

const signalCols = `id, workspace_id, source, competitor_id, title, summary, url, detail,
  snapshot_id, previous_snapshot_id, run_id, dedupe_key, detected_at`

func scanSignal(row pgx.Row) (domain.Signal, error) {
	var sg domain.Signal
	var id, ws string
	var competitorID, snapshotID, prevID, runID, dedupe *string
	var detail []byte
	var detected any
	if err := row.Scan(&id, &ws, &sg.Source, &competitorID, &sg.Title, &sg.Summary, &sg.URL, &detail,
		&snapshotID, &prevID, &runID, &dedupe, &detected); err != nil {
		return sg, err
	}
	sg.ID, sg.WorkspaceID = domain.SignalId(id), domain.WorkspaceId(ws)
	sg.CompetitorID = typedPtr[domain.CompetitorId](competitorID)
	sg.SnapshotID = typedPtr[domain.PageSnapshotId](snapshotID)
	sg.PreviousSnapshotID = typedPtr[domain.PageSnapshotId](prevID)
	sg.RunID = typedPtr[domain.RunId](runID)
	sg.DedupeKey = strPtrOrNil(dedupe)
	sg.Detail = map[string]any{}
	jsonInto(detail, &sg.Detail)
	sg.DetectedAt = anyTs(detected)
	return sg, nil
}

func (r *pgSignalRepo) List(workspaceID domain.WorkspaceId, q SignalQuery) []domain.Signal {
	where := []string{"workspace_id=$1"}
	args := []any{string(workspaceID)}
	add := func(cond string, v any) {
		args = append(args, v)
		where = append(where, fmt.Sprintf(cond, len(args)))
	}
	if q.CompetitorID != nil {
		add("competitor_id=$%d", string(*q.CompetitorID))
	}
	if q.RunID != nil {
		add("run_id=$%d", string(*q.RunID))
	}
	if q.Source != "" {
		add("source=$%d", q.Source)
	}
	if q.Since != nil {
		add("detected_at >= $%d", string(*q.Since))
	}
	rows, err := r.s.pool.Query(bg(), fmt.Sprintf(
		`select %s from signals where %s order by detected_at desc limit %d`,
		signalCols, strings.Join(where, " and "), limitOr(q.Limit, 100)), args...)
	if err != nil {
		r.s.fail("signal.List", err)
		return nil
	}
	defer rows.Close()
	out := []domain.Signal{}
	for rows.Next() {
		sg, err := scanSignal(rows)
		if err != nil {
			r.s.fail("signal.List scan", err)
			return out
		}
		out = append(out, sg)
	}
	return out
}

func (r *pgSignalRepo) GetByID(id domain.SignalId) (domain.Signal, bool) {
	sg, err := scanSignal(r.s.pool.QueryRow(bg(),
		`select `+signalCols+` from signals where id=$1`, string(id)))
	if err != nil {
		return domain.Signal{}, false
	}
	return sg, true
}

func (r *pgSignalRepo) Insert(sg domain.Signal) (domain.Signal, bool) {
	stored, err := scanSignal(r.s.pool.QueryRow(bg(),
		`insert into signals (`+signalCols+`) values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		 on conflict (workspace_id, dedupe_key) where dedupe_key is not null do nothing
		 returning `+signalCols,
		string(sg.ID), string(sg.WorkspaceID), sg.Source, idPtrArg(sg.CompetitorID), sg.Title, sg.Summary,
		sg.URL, jsonArg(orEmptyMap(sg.Detail)), idPtrArg(sg.SnapshotID), idPtrArg(sg.PreviousSnapshotID),
		idPtrArg(sg.RunID), strArg(sg.DedupeKey), tsArg(sg.DetectedAt)))
	if err == nil {
		return stored, true
	}
	if !errors.Is(err, pgx.ErrNoRows) || sg.DedupeKey == nil {
		r.s.fail("signal.Insert", err)
		return sg, false
	}
	// Dedupe hit: hand back the signal already recorded for this key.
	existing, err := scanSignal(r.s.pool.QueryRow(bg(),
		`select `+signalCols+` from signals where workspace_id=$1 and dedupe_key=$2`,
		string(sg.WorkspaceID), *sg.DedupeKey))
	if err != nil {
		r.s.fail("signal.Insert dedupe lookup", err)
		return sg, false
	}
	return existing, false
}

/* -------------------------- brand context -------------------------- */

type pgBrandRepo struct{ s *pgStore }

func (r *pgBrandRepo) Get(workspaceID domain.WorkspaceId) (domain.BrandContext, bool) {
	var b domain.BrandContext
	var updated any
	err := r.s.pool.QueryRow(bg(),
		`select voice, product_facts, positioning, avoid_claims, updated_at, updated_by
		   from brand_context where workspace_id=$1`, string(workspaceID)).
		Scan(&b.Voice, &b.ProductFacts, &b.Positioning, &b.AvoidClaims, &updated, &b.UpdatedBy)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			r.s.fail("brand.Get", err)
		}
		return domain.BrandContext{}, false
	}
	b.WorkspaceID, b.UpdatedAt = workspaceID, anyTs(updated)
	return b, true
}

func (r *pgBrandRepo) Upsert(b domain.BrandContext) domain.BrandContext {
	_, err := r.s.pool.Exec(bg(),
		`insert into brand_context (workspace_id, voice, product_facts, positioning, avoid_claims, updated_at, updated_by)
		 values ($1,$2,$3,$4,$5,$6,$7)
		 on conflict (workspace_id) do update set
		   voice=excluded.voice, product_facts=excluded.product_facts, positioning=excluded.positioning,
		   avoid_claims=excluded.avoid_claims, updated_at=excluded.updated_at, updated_by=excluded.updated_by`,
		string(b.WorkspaceID), b.Voice, b.ProductFacts, b.Positioning, b.AvoidClaims, tsArg(b.UpdatedAt), b.UpdatedBy)
	r.s.fail("brand.Upsert", err)
	return b
}

/* ---------------------------- approvals ---------------------------- */

type pgApprovalRepo struct{ s *pgStore }

const approvalCols = `id, workspace_id, run_id, node_key, iteration, title, channel, draft, final_content,
  status, assignee, notify, signal_ids, decided_by, decision_note, decided_at, signaled_at, expires_at, created_at`

func scanApproval(row pgx.Row) (domain.Approval, error) {
	var a domain.Approval
	var id, ws, runID, status string
	var notify []byte
	var signalIDs []string
	var decided, signaled, expires, created any
	if err := row.Scan(&id, &ws, &runID, &a.NodeKey, &a.Iteration, &a.Title, &a.Channel, &a.Draft,
		&a.FinalContent, &status, &a.Assignee, &notify, &signalIDs, &a.DecidedBy, &a.DecisionNote,
		&decided, &signaled, &expires, &created); err != nil {
		return a, err
	}
	a.ID, a.WorkspaceID, a.RunID = domain.ApprovalId(id), domain.WorkspaceId(ws), domain.RunId(runID)
	a.Status = domain.ApprovalStatus(status)
	a.Notify = map[string]any{}
	jsonInto(notify, &a.Notify)
	a.SignalIDs = make([]domain.SignalId, len(signalIDs))
	for i, s := range signalIDs {
		a.SignalIDs[i] = domain.SignalId(s)
	}
	a.DecidedAt, a.SignaledAt, a.ExpiresAt = anyTsPtr(decided), anyTsPtr(signaled), anyTsPtr(expires)
	a.CreatedAt = anyTs(created)
	return a, nil
}

func (r *pgApprovalRepo) List(workspaceID domain.WorkspaceId, q ApprovalQuery) []domain.Approval {
	where := []string{"workspace_id=$1"}
	args := []any{string(workspaceID)}
	if q.Status != nil {
		args = append(args, string(*q.Status))
		where = append(where, fmt.Sprintf("status=$%d", len(args)))
	}
	if q.RunID != nil {
		args = append(args, string(*q.RunID))
		where = append(where, fmt.Sprintf("run_id=$%d", len(args)))
	}
	rows, err := r.s.pool.Query(bg(), fmt.Sprintf(
		`select %s from approvals where %s order by created_at desc limit %d`,
		approvalCols, strings.Join(where, " and "), limitOr(q.Limit, 100)), args...)
	if err != nil {
		r.s.fail("approval.List", err)
		return nil
	}
	defer rows.Close()
	out := []domain.Approval{}
	for rows.Next() {
		a, err := scanApproval(rows)
		if err != nil {
			r.s.fail("approval.List scan", err)
			return out
		}
		out = append(out, a)
	}
	return out
}

func (r *pgApprovalRepo) GetByID(id domain.ApprovalId) (domain.Approval, bool) {
	a, err := scanApproval(r.s.pool.QueryRow(bg(),
		`select `+approvalCols+` from approvals where id=$1`, string(id)))
	if err != nil {
		return domain.Approval{}, false
	}
	return a, true
}

func (r *pgApprovalRepo) Insert(a domain.Approval) domain.Approval {
	if a.Status == "" {
		a.Status = domain.ApprovalPending
	}
	signalIDs := make([]string, len(a.SignalIDs))
	for i, s := range a.SignalIDs {
		signalIDs[i] = string(s)
	}
	// On a retried insert the row already exists: do nothing, then read it back.
	_, err := r.s.pool.Exec(bg(),
		`insert into approvals (`+approvalCols+`) values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)
		 on conflict (run_id, node_key, iteration) do nothing`,
		string(a.ID), string(a.WorkspaceID), string(a.RunID), a.NodeKey, a.Iteration, a.Title, a.Channel,
		a.Draft, strArg(a.FinalContent), string(a.Status), a.Assignee, jsonArg(orEmptyMap(a.Notify)), signalIDs,
		strArg(a.DecidedBy), strArg(a.DecisionNote), tsPtrArg(a.DecidedAt), tsPtrArg(a.SignaledAt),
		tsPtrArg(a.ExpiresAt), tsArg(a.CreatedAt))
	if err != nil {
		r.s.fail("approval.Insert", err)
		return a
	}
	stored, err := scanApproval(r.s.pool.QueryRow(bg(),
		`select `+approvalCols+` from approvals where run_id=$1 and node_key=$2 and iteration=$3`,
		string(a.RunID), a.NodeKey, a.Iteration))
	if err != nil {
		r.s.fail("approval.Insert read-back", err)
		return a
	}
	return stored
}

func (r *pgApprovalRepo) Decide(id domain.ApprovalId, d domain.ApprovalDecision) (domain.Approval, error) {
	if err := validateDecision(d); err != nil {
		return domain.Approval{}, err
	}
	current, ok := r.GetByID(id)
	if !ok {
		return domain.Approval{}, domain.NotFound("Approval")
	}
	// The status guard in WHERE makes the decision atomic: of two concurrent
	// deciders exactly one updates the row.
	a, err := scanApproval(r.s.pool.QueryRow(bg(),
		`update approvals set status=$2, final_content=$3, decided_by=$4, decision_note=$5, decided_at=$6
		  where id=$1 and status='pending'
		  returning `+approvalCols,
		string(id), string(d.Status), strArg(decidedContent(current, d)), d.DecidedBy, strArg(d.Note),
		string(d.DecidedAt)))
	if errors.Is(err, pgx.ErrNoRows) {
		latest, _ := r.GetByID(id)
		return domain.Approval{}, domain.Conflict("approval is already " + string(latest.Status))
	}
	if err != nil {
		r.s.fail("approval.Decide", err)
		return domain.Approval{}, err
	}
	return a, nil
}

const outcomeCols = `id, approval_id, run_id, node_key, node_type, status, summary, url, output, occurred_at`

func (r *pgApprovalRepo) ListOutcomes(id domain.ApprovalId) []domain.ApprovalOutcome {
	rows, err := r.s.pool.Query(bg(),
		`select `+outcomeCols+` from approval_outcomes where approval_id=$1 order by occurred_at`, string(id))
	if err != nil {
		r.s.fail("approval.ListOutcomes", err)
		return nil
	}
	defer rows.Close()
	out := []domain.ApprovalOutcome{}
	for rows.Next() {
		var o domain.ApprovalOutcome
		var oid, aid, runID string
		var output []byte
		var occurred any
		if err := rows.Scan(&oid, &aid, &runID, &o.NodeKey, &o.NodeType, &o.Status, &o.Summary, &o.URL,
			&output, &occurred); err != nil {
			r.s.fail("approval.ListOutcomes scan", err)
			return out
		}
		o.ID, o.ApprovalID, o.RunID = domain.ApprovalOutcomeId(oid), domain.ApprovalId(aid), domain.RunId(runID)
		jsonInto(output, &o.Output)
		o.OccurredAt = anyTs(occurred)
		out = append(out, o)
	}
	return out
}

func (r *pgApprovalRepo) InsertOutcome(o domain.ApprovalOutcome) domain.ApprovalOutcome {
	_, err := r.s.pool.Exec(bg(),
		`insert into approval_outcomes (`+outcomeCols+`) values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		string(o.ID), string(o.ApprovalID), string(o.RunID), o.NodeKey, o.NodeType, o.Status, o.Summary,
		o.URL, nullableJSON(o.Output), tsArg(o.OccurredAt))
	r.s.fail("approval.InsertOutcome", err)
	return o
}
