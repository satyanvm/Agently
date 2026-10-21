package services

// intel_service.go backs the competitors / snapshots / signals API. The
// reasoner's intel.* nodes write snapshots and signals; this service lets the
// web app register competitors and read what was found.
//
//	GET    /api/competitors[?includeArchived=true]
//	POST   /api/competitors
//	GET    /api/competitors/{id}
//	PUT    /api/competitors/{id}            (archived:false restores)
//	DELETE /api/competitors/{id}            → archive (history is kept)
//	GET    /api/competitors/{id}/snapshots  → versions, without page text
//	GET    /api/snapshots/{id}              → one version, with page text
//	GET    /api/signals[?source&competitorId&runId&since&limit]
//	GET    /api/signals/{id}

import (
	"strings"
	"time"

	"github.com/agently/api/internal/domain"
	"github.com/agently/api/internal/domain/validate"
	"github.com/agently/api/internal/platform"
)

type IntelService struct {
	deps Deps
}

func NewIntelService(deps Deps) *IntelService { return &IntelService{deps: deps} }

func (s *IntelService) ws() domain.WorkspaceId { return s.deps.Repos.Workspace.Get().ID }

func (s *IntelService) now() domain.Timestamp {
	return domain.Timestamp(time.UnixMilli(s.deps.Clock.Now()).UTC().Format("2006-01-02T15:04:05.000Z"))
}

func (s *IntelService) ListCompetitors(includeArchived bool) []domain.Competitor {
	return s.deps.Repos.Competitors.ListByWorkspace(s.ws(), includeArchived)
}

// GetCompetitor returns a competitor of THIS workspace (ids from another
// workspace are not found, not forbidden, so they can't be probed).
func (s *IntelService) GetCompetitor(id domain.CompetitorId) (domain.Competitor, error) {
	c, ok := s.deps.Repos.Competitors.GetByID(id)
	if !ok || c.WorkspaceID != s.ws() {
		return domain.Competitor{}, domain.NotFound("Competitor")
	}
	return c, nil
}

func (s *IntelService) nameTaken(name string, except domain.CompetitorId) bool {
	for _, c := range s.deps.Repos.Competitors.ListByWorkspace(s.ws(), true) {
		if c.ID != except && strings.EqualFold(strings.TrimSpace(c.Name), strings.TrimSpace(name)) {
			return true
		}
	}
	return false
}

func (s *IntelService) CreateCompetitor(in validate.CompetitorInput) (domain.Competitor, error) {
	if s.nameTaken(*in.Name, "") {
		return domain.Competitor{}, domain.Conflict("a competitor named \"" + *in.Name + "\" already exists")
	}
	now := s.now()
	c := domain.Competitor{
		ID: domain.NewCompetitorId(), WorkspaceID: s.ws(), Name: *in.Name,
		Pages: []domain.CompetitorPage{}, CreatedAt: now, UpdatedAt: now,
	}
	if in.Website != nil {
		c.Website = *in.Website
	}
	if in.Pages != nil {
		c.Pages = *in.Pages
	}
	if in.Notes != nil {
		c.Notes = *in.Notes
	}
	return s.deps.Repos.Competitors.Insert(c), nil
}

func (s *IntelService) UpdateCompetitor(id domain.CompetitorId, in validate.CompetitorInput) (domain.Competitor, error) {
	if _, err := s.GetCompetitor(id); err != nil {
		return domain.Competitor{}, err
	}
	if in.Name != nil && s.nameTaken(*in.Name, id) {
		return domain.Competitor{}, domain.Conflict("a competitor named \"" + *in.Name + "\" already exists")
	}
	now := s.now()
	patch := platform.CompetitorPatch{Name: in.Name, Website: in.Website, Pages: in.Pages, Notes: in.Notes, UpdatedAt: &now}
	if in.Archived != nil {
		var at *domain.Timestamp
		if *in.Archived {
			at = &now
		}
		patch.ArchivedAt = &at
	}
	return s.deps.Repos.Competitors.Update(id, patch)
}

// ArchiveCompetitor stops watching a competitor. Its snapshots and signals
// stay, so drafts that cite them remain traceable.
func (s *IntelService) ArchiveCompetitor(id domain.CompetitorId) error {
	archived := true
	_, err := s.UpdateCompetitor(id, validate.CompetitorInput{Archived: &archived})
	return err
}

// SnapshotSummary is a page version without its (large) text.
type SnapshotSummary struct {
	ID           domain.PageSnapshotId `json:"id"`
	CompetitorID *domain.CompetitorId  `json:"competitorId"`
	PageKind     string                `json:"pageKind"`
	URL          string                `json:"url"`
	RunID        *domain.RunId         `json:"runId"`
	FetchedVia   string                `json:"fetchedVia"`
	StatusCode   *int                  `json:"statusCode"`
	Title        string                `json:"title"`
	ContentHash  string                `json:"contentHash"`
	Chars        int                   `json:"chars"`
	FetchedAt    domain.Timestamp      `json:"fetchedAt"`
	LastSeenAt   domain.Timestamp      `json:"lastSeenAt"`
}

func (s *IntelService) ListSnapshots(competitorID domain.CompetitorId, limit int) ([]SnapshotSummary, error) {
	if _, err := s.GetCompetitor(competitorID); err != nil {
		return nil, err
	}
	rows := s.deps.Repos.Snapshots.ListByCompetitor(competitorID, limit)
	out := make([]SnapshotSummary, 0, len(rows))
	for _, sn := range rows {
		out = append(out, SnapshotSummary{
			ID: sn.ID, CompetitorID: sn.CompetitorID, PageKind: sn.PageKind, URL: sn.URL, RunID: sn.RunID,
			FetchedVia: sn.FetchedVia, StatusCode: sn.StatusCode, Title: sn.Title, ContentHash: sn.ContentHash,
			Chars: len([]rune(sn.Content)), FetchedAt: sn.FetchedAt, LastSeenAt: sn.LastSeenAt,
		})
	}
	return out, nil
}

func (s *IntelService) GetSnapshot(id domain.PageSnapshotId) (domain.PageSnapshot, error) {
	sn, ok := s.deps.Repos.Snapshots.GetByID(id)
	if !ok || sn.WorkspaceID != s.ws() {
		return domain.PageSnapshot{}, domain.NotFound("Snapshot")
	}
	return sn, nil
}

func (s *IntelService) ListSignals(q validate.ListSignalsQuery) []domain.Signal {
	query := platform.SignalQuery{Source: q.Source, Limit: q.Limit}
	if q.CompetitorID != "" {
		id := domain.CompetitorId(q.CompetitorID)
		query.CompetitorID = &id
	}
	if q.RunID != "" {
		id := domain.RunId(q.RunID)
		query.RunID = &id
	}
	if q.Since != "" {
		since := domain.Timestamp(q.Since)
		query.Since = &since
	}
	return s.deps.Repos.Signals.List(s.ws(), query)
}

func (s *IntelService) GetSignal(id domain.SignalId) (domain.Signal, error) {
	sg, ok := s.deps.Repos.Signals.GetByID(id)
	if !ok || sg.WorkspaceID != s.ws() {
		return domain.Signal{}, domain.NotFound("Signal")
	}
	return sg, nil
}

// GetBrand returns the workspace's brand context; an empty one (never saved)
// when there is none yet.
//
//	GET /api/brand
//	PUT /api/brand   {voice, productFacts, positioning, avoidClaims, updatedBy}
func (s *IntelService) GetBrand() domain.BrandContext {
	b, ok := s.deps.Repos.Brand.Get(s.ws())
	if !ok {
		return domain.BrandContext{WorkspaceID: s.ws()}
	}
	return b
}

func (s *IntelService) UpdateBrand(in validate.BrandInput) domain.BrandContext {
	return s.deps.Repos.Brand.Upsert(domain.BrandContext{
		WorkspaceID: s.ws(), Voice: in.Voice, ProductFacts: in.ProductFacts, Positioning: in.Positioning,
		AvoidClaims: in.AvoidClaims, UpdatedAt: s.now(), UpdatedBy: in.UpdatedBy,
	})
}
