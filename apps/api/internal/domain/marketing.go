package domain

// Entities of the signal → draft → approval → publish loop
// (packages/db/migrations/0015_marketing_loop.sql, docs/phase-2-plan.md).

/* ========================= Competitors ========================= */

// Kinds of competitor page we watch. "other" covers anything else worth a
// snapshot (a docs page, a status page).
const (
	PageKindPricing   = "pricing"
	PageKindChangelog = "changelog"
	PageKindBlog      = "blog"
	PageKindCareers   = "careers"
	PageKindOther     = "other"
)

var ValidPageKinds = map[string]bool{
	PageKindPricing: true, PageKindChangelog: true, PageKindBlog: true,
	PageKindCareers: true, PageKindOther: true,
}

// How a page is fetched: plain HTTP first; "browser" (Browserbase) for pages
// that only render with JavaScript.
const (
	RenderHTTP    = "http"
	RenderBrowser = "browser"
)

type CompetitorPage struct {
	Kind   string `json:"kind"`
	URL    string `json:"url"`
	Render string `json:"render"`
}

type Competitor struct {
	ID          CompetitorId     `json:"id"`
	WorkspaceID WorkspaceId      `json:"workspaceId"`
	Name        string           `json:"name"`
	Website     string           `json:"website"`
	Pages       []CompetitorPage `json:"pages"`
	Notes       string           `json:"notes"`
	CreatedAt   Timestamp        `json:"createdAt"`
	UpdatedAt   Timestamp        `json:"updatedAt"`
	ArchivedAt  *Timestamp       `json:"archivedAt"`
}

// PageSnapshot is one distinct version of a watched page. Content is the
// normalized visible text the diff runs on.
type PageSnapshot struct {
	ID           PageSnapshotId `json:"id"`
	WorkspaceID  WorkspaceId    `json:"workspaceId"`
	CompetitorID *CompetitorId  `json:"competitorId"`
	PageKind     string         `json:"pageKind"`
	URL          string         `json:"url"`
	RunID        *RunId         `json:"runId"`
	FetchedVia   string         `json:"fetchedVia"`
	StatusCode   *int           `json:"statusCode"`
	Title        string         `json:"title"`
	ContentHash  string         `json:"contentHash"`
	Content      string         `json:"content"`
	FetchedAt    Timestamp      `json:"fetchedAt"`
	LastSeenAt   Timestamp      `json:"lastSeenAt"`
}

/* =========================== Signals =========================== */

const (
	SignalPageChange = "page_change"
	SignalNews       = "news"
	SignalRSS        = "rss"
	SignalReddit     = "reddit"
	SignalHackerNews = "hackernews"
	SignalReview     = "review"
	SignalOther      = "other"
)

var ValidSignalSources = map[string]bool{
	SignalPageChange: true, SignalNews: true, SignalRSS: true, SignalReddit: true,
	SignalHackerNews: true, SignalReview: true, SignalOther: true,
}

// Signal is a detected market event — the "why" every draft traces back to.
type Signal struct {
	ID                 SignalId        `json:"id"`
	WorkspaceID        WorkspaceId     `json:"workspaceId"`
	Source             string          `json:"source"`
	CompetitorID       *CompetitorId   `json:"competitorId"`
	Title              string          `json:"title"`
	Summary            string          `json:"summary"`
	URL                string          `json:"url"`
	Detail             map[string]any  `json:"detail"`
	SnapshotID         *PageSnapshotId `json:"snapshotId"`
	PreviousSnapshotID *PageSnapshotId `json:"previousSnapshotId"`
	RunID              *RunId          `json:"runId"`
	DedupeKey          *string         `json:"dedupeKey"`
	DetectedAt         Timestamp       `json:"detectedAt"`
}

/* ======================== Brand context ======================== */

// BrandContext is the per-workspace material injected into drafting nodes so
// drafts sound like the company and avoid claims it can't make.
type BrandContext struct {
	WorkspaceID  WorkspaceId `json:"workspaceId"`
	Voice        string      `json:"voice"`
	ProductFacts string      `json:"productFacts"`
	Positioning  string      `json:"positioning"`
	AvoidClaims  string      `json:"avoidClaims"`
	UpdatedAt    Timestamp   `json:"updatedAt"`
	UpdatedBy    string      `json:"updatedBy"`
}

/* ========================== Approvals ========================== */

type ApprovalStatus string

const (
	ApprovalPending  ApprovalStatus = "pending"
	ApprovalApproved ApprovalStatus = "approved"
	ApprovalRejected ApprovalStatus = "rejected"
	ApprovalTimedOut ApprovalStatus = "timed_out"
	ApprovalCanceled ApprovalStatus = "canceled"
)

var ValidApprovalStatuses = map[string]bool{
	"pending": true, "approved": true, "rejected": true, "timed_out": true, "canceled": true,
}

// Approval is a draft waiting for a person, or the record of their decision.
// The run that drafted it waits on a Temporal signal; the reasoner's relay
// delivers the decision and stamps SignaledAt.
type Approval struct {
	ID           ApprovalId     `json:"id"`
	WorkspaceID  WorkspaceId    `json:"workspaceId"`
	RunID        RunId          `json:"runId"`
	NodeKey      string         `json:"nodeKey"`
	Iteration    int            `json:"iteration"`
	Title        string         `json:"title"`
	Channel      string         `json:"channel"`
	Draft        string         `json:"draft"`
	FinalContent *string        `json:"finalContent"`
	Status       ApprovalStatus `json:"status"`
	Assignee     string         `json:"assignee"`
	Notify       map[string]any `json:"notify"`
	SignalIDs    []SignalId     `json:"signalIds"`
	DecidedBy    *string        `json:"decidedBy"`
	DecisionNote *string        `json:"decisionNote"`
	DecidedAt    *Timestamp     `json:"decidedAt"`
	SignaledAt   *Timestamp     `json:"signaledAt"`
	ExpiresAt    *Timestamp     `json:"expiresAt"`
	CreatedAt    Timestamp      `json:"createdAt"`
}

// ApprovalDecision is what a person submits from the inbox.
type ApprovalDecision struct {
	Status       ApprovalStatus // approved | rejected
	FinalContent *string        // the (possibly edited) content; approved only
	DecidedBy    string
	Note         *string
	DecidedAt    Timestamp
}

// ApprovalOutcome records a node that ran (or was skipped) after a decision —
// for a publish node, the published item.
type ApprovalOutcome struct {
	ID         ApprovalOutcomeId `json:"id"`
	ApprovalID ApprovalId        `json:"approvalId"`
	RunID      RunId             `json:"runId"`
	NodeKey    string            `json:"nodeKey"`
	NodeType   string            `json:"nodeType"`
	Status     string            `json:"status"`
	Summary    string            `json:"summary"`
	URL        string            `json:"url"`
	Output     map[string]any    `json:"output"`
	OccurredAt Timestamp         `json:"occurredAt"`
}
