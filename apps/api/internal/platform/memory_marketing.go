package platform

import (
	"sort"

	"github.com/agently/api/internal/domain"
)

// In-memory repositories for the signal → approval loop (0015). Same
// semantics as postgres_marketing.go; timestamps are ISO strings, so string
// comparison orders them.

func limitOr(n, def int) int {
	if n <= 0 {
		return def
	}
	return n
}

/* --------------------------- competitors --------------------------- */

type competitorRepo struct{ s *store }

func (r *competitorRepo) ListByWorkspace(workspaceID domain.WorkspaceId, includeArchived bool) []domain.Competitor {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	out := []domain.Competitor{}
	for _, c := range r.s.competitors {
		if c.WorkspaceID == workspaceID && (includeArchived || c.ArchivedAt == nil) {
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (r *competitorRepo) GetByID(id domain.CompetitorId) (domain.Competitor, bool) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	for _, c := range r.s.competitors {
		if c.ID == id {
			return c, true
		}
	}
	return domain.Competitor{}, false
}

func (r *competitorRepo) Insert(c domain.Competitor) domain.Competitor {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if c.Pages == nil {
		c.Pages = []domain.CompetitorPage{}
	}
	r.s.competitors = append(r.s.competitors, c)
	return c
}

func (r *competitorRepo) Update(id domain.CompetitorId, patch CompetitorPatch) (domain.Competitor, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	for i := range r.s.competitors {
		c := &r.s.competitors[i]
		if c.ID != id {
			continue
		}
		if patch.Name != nil {
			c.Name = *patch.Name
		}
		if patch.Website != nil {
			c.Website = *patch.Website
		}
		if patch.Pages != nil {
			c.Pages = *patch.Pages
		}
		if patch.Notes != nil {
			c.Notes = *patch.Notes
		}
		if patch.UpdatedAt != nil {
			c.UpdatedAt = *patch.UpdatedAt
		}
		if patch.ArchivedAt != nil {
			c.ArchivedAt = *patch.ArchivedAt
		}
		return *c, nil
	}
	return domain.Competitor{}, domain.NotFound("Competitor")
}

/* ---------------------------- snapshots ---------------------------- */

type snapshotRepo struct{ s *store }

// newestSnapshots returns matches newest first, at most limit.
func (r *snapshotRepo) newestSnapshots(match func(domain.PageSnapshot) bool, limit int) []domain.PageSnapshot {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	out := []domain.PageSnapshot{}
	for _, sn := range r.s.snapshots {
		if match(sn) {
			out = append(out, sn)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].FetchedAt > out[j].FetchedAt })
	if n := limitOr(limit, 50); len(out) > n {
		out = out[:n]
	}
	return out
}

func (r *snapshotRepo) ListByCompetitor(id domain.CompetitorId, limit int) []domain.PageSnapshot {
	return r.newestSnapshots(func(sn domain.PageSnapshot) bool {
		return sn.CompetitorID != nil && *sn.CompetitorID == id
	}, limit)
}

func (r *snapshotRepo) ListByURL(workspaceID domain.WorkspaceId, url string, limit int) []domain.PageSnapshot {
	return r.newestSnapshots(func(sn domain.PageSnapshot) bool {
		return sn.WorkspaceID == workspaceID && sn.URL == url
	}, limit)
}

func (r *snapshotRepo) GetByID(id domain.PageSnapshotId) (domain.PageSnapshot, bool) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	for _, sn := range r.s.snapshots {
		if sn.ID == id {
			return sn, true
		}
	}
	return domain.PageSnapshot{}, false
}

func (r *snapshotRepo) Insert(sn domain.PageSnapshot) domain.PageSnapshot {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	r.s.snapshots = append(r.s.snapshots, sn)
	return sn
}

/* ----------------------------- signals ----------------------------- */

type signalRepo struct{ s *store }

func (r *signalRepo) List(workspaceID domain.WorkspaceId, q SignalQuery) []domain.Signal {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	out := []domain.Signal{}
	for _, sg := range r.s.signals {
		if sg.WorkspaceID != workspaceID {
			continue
		}
		if q.CompetitorID != nil && (sg.CompetitorID == nil || *sg.CompetitorID != *q.CompetitorID) {
			continue
		}
		if q.RunID != nil && (sg.RunID == nil || *sg.RunID != *q.RunID) {
			continue
		}
		if q.Source != "" && sg.Source != q.Source {
			continue
		}
		if q.Since != nil && sg.DetectedAt < *q.Since {
			continue
		}
		out = append(out, sg)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].DetectedAt > out[j].DetectedAt })
	if n := limitOr(q.Limit, 100); len(out) > n {
		out = out[:n]
	}
	return out
}

func (r *signalRepo) GetByID(id domain.SignalId) (domain.Signal, bool) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	for _, sg := range r.s.signals {
		if sg.ID == id {
			return sg, true
		}
	}
	return domain.Signal{}, false
}

func (r *signalRepo) Insert(sg domain.Signal) (domain.Signal, bool) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if sg.DedupeKey != nil {
		for _, existing := range r.s.signals {
			if existing.WorkspaceID == sg.WorkspaceID && existing.DedupeKey != nil && *existing.DedupeKey == *sg.DedupeKey {
				return existing, false
			}
		}
	}
	if sg.Detail == nil {
		sg.Detail = map[string]any{}
	}
	r.s.signals = append(r.s.signals, sg)
	return sg, true
}

/* -------------------------- brand context -------------------------- */

type brandRepo struct{ s *store }

func (r *brandRepo) Get(workspaceID domain.WorkspaceId) (domain.BrandContext, bool) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	b, ok := r.s.brand[workspaceID]
	return b, ok
}

func (r *brandRepo) Upsert(b domain.BrandContext) domain.BrandContext {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if r.s.brand == nil {
		r.s.brand = map[domain.WorkspaceId]domain.BrandContext{}
	}
	r.s.brand[b.WorkspaceID] = b
	return b
}

/* ---------------------------- approvals ---------------------------- */

type approvalRepo struct{ s *store }

func (r *approvalRepo) List(workspaceID domain.WorkspaceId, q ApprovalQuery) []domain.Approval {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	out := []domain.Approval{}
	for _, a := range r.s.approvals {
		if a.WorkspaceID != workspaceID {
			continue
		}
		if q.Status != nil && a.Status != *q.Status {
			continue
		}
		if q.RunID != nil && a.RunID != *q.RunID {
			continue
		}
		out = append(out, a)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	if n := limitOr(q.Limit, 100); len(out) > n {
		out = out[:n]
	}
	return out
}

func (r *approvalRepo) GetByID(id domain.ApprovalId) (domain.Approval, bool) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	for _, a := range r.s.approvals {
		if a.ID == id {
			return a, true
		}
	}
	return domain.Approval{}, false
}

func (r *approvalRepo) Insert(a domain.Approval) domain.Approval {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	for _, existing := range r.s.approvals {
		if existing.RunID == a.RunID && existing.NodeKey == a.NodeKey && existing.Iteration == a.Iteration {
			return existing
		}
	}
	if a.Status == "" {
		a.Status = domain.ApprovalPending
	}
	if a.Notify == nil {
		a.Notify = map[string]any{}
	}
	if a.SignalIDs == nil {
		a.SignalIDs = []domain.SignalId{}
	}
	r.s.approvals = append(r.s.approvals, a)
	return a
}

func (r *approvalRepo) Decide(id domain.ApprovalId, d domain.ApprovalDecision) (domain.Approval, error) {
	if err := validateDecision(d); err != nil {
		return domain.Approval{}, err
	}
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	for i := range r.s.approvals {
		a := &r.s.approvals[i]
		if a.ID != id {
			continue
		}
		if a.Status != domain.ApprovalPending {
			return domain.Approval{}, domain.Conflict("approval is already " + string(a.Status))
		}
		a.Status = d.Status
		a.FinalContent = decidedContent(*a, d)
		by, at := d.DecidedBy, d.DecidedAt
		a.DecidedBy, a.DecidedAt, a.DecisionNote = &by, &at, d.Note
		return *a, nil
	}
	return domain.Approval{}, domain.NotFound("Approval")
}

func (r *approvalRepo) ListOutcomes(id domain.ApprovalId) []domain.ApprovalOutcome {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	out := []domain.ApprovalOutcome{}
	for _, o := range r.s.outcomes {
		if o.ApprovalID == id {
			out = append(out, o)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].OccurredAt < out[j].OccurredAt })
	return out
}

func (r *approvalRepo) InsertOutcome(o domain.ApprovalOutcome) domain.ApprovalOutcome {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	r.s.outcomes = append(r.s.outcomes, o)
	return o
}

/* ------------------------ shared decision rules ------------------------ */

// validateDecision: only approve/reject come from a person (timeouts and
// cancellations are written by the run), and a decision must say who made it.
func validateDecision(d domain.ApprovalDecision) error {
	if d.Status != domain.ApprovalApproved && d.Status != domain.ApprovalRejected {
		return domain.BadRequest("decision must be approved or rejected")
	}
	if d.DecidedBy == "" {
		return domain.BadRequest("decidedBy is required")
	}
	if d.DecidedAt == "" {
		return domain.BadRequest("decidedAt is required")
	}
	return nil
}

// decidedContent is what gets published: the edited content when given,
// otherwise the draft as written. A rejection publishes nothing.
func decidedContent(a domain.Approval, d domain.ApprovalDecision) *string {
	if d.Status != domain.ApprovalApproved {
		return nil
	}
	if d.FinalContent != nil {
		c := *d.FinalContent
		return &c
	}
	c := a.Draft
	return &c
}
