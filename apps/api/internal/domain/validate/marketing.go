package validate

import (
	"net/url"
	"strings"
	"time"

	"github.com/agently/api/internal/domain"
)

// Inputs for the competitors / snapshots / signals API (step 2 of
// docs/phase-2-plan.md).

type CompetitorInput struct {
	Name    *string
	Website *string
	Pages   *[]domain.CompetitorPage
	Notes   *string
	// Archived is update-only: false restores an archived competitor.
	Archived *bool
}

func validHTTPURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

func (v *ValidationError) pages(body map[string]any) *[]domain.CompetitorPage {
	raw, ok := body["pages"].([]any)
	if !ok {
		if hasKey(body, "pages") {
			v.add("pages", "must be an array")
		}
		return nil
	}
	out := make([]domain.CompetitorPage, 0, len(raw))
	seen := map[string]bool{}
	for i, item := range raw {
		path := "pages." + itoa(i)
		obj, ok := item.(map[string]any)
		if !ok {
			v.add(path, "must be an object")
			continue
		}
		p := domain.CompetitorPage{
			Kind:   strings.TrimSpace(getString(obj, "kind")),
			URL:    strings.TrimSpace(getString(obj, "url")),
			Render: strings.TrimSpace(getString(obj, "render")),
		}
		if p.Render == "" {
			p.Render = domain.RenderHTTP
		}
		v.stringRequired(path+".kind", p.Kind)
		v.enumValue(path+".kind", p.Kind, domain.ValidPageKinds)
		if !validHTTPURL(p.URL) {
			v.add(path+".url", "must be an http(s) URL")
		} else if seen[p.URL] {
			v.add(path+".url", "is listed twice")
		}
		seen[p.URL] = true
		if p.Render != domain.RenderHTTP && p.Render != domain.RenderBrowser {
			v.add(path+".render", "must be http or browser")
		}
		out = append(out, p)
	}
	return &out
}

func parseCompetitor(body map[string]any, create bool) (CompetitorInput, error) {
	v := &ValidationError{}
	in := CompetitorInput{}
	if create || hasKey(body, "name") {
		name := strings.TrimSpace(getString(body, "name"))
		v.stringRequired("name", name)
		v.stringMaxLen("name", name, 120)
		in.Name = &name
	}
	if hasKey(body, "website") {
		site := strings.TrimSpace(getString(body, "website"))
		if site != "" && !validHTTPURL(site) {
			v.add("website", "must be an http(s) URL")
		}
		in.Website = &site
	}
	if hasKey(body, "notes") {
		notes := getString(body, "notes")
		v.stringMaxLen("notes", notes, 4000)
		in.Notes = &notes
	}
	in.Pages = v.pages(body)
	if !create && hasKey(body, "archived") {
		b, ok := body["archived"].(bool)
		if !ok {
			v.add("archived", "must be a boolean")
		}
		in.Archived = &b
	}
	if v.HasErrors() {
		return CompetitorInput{}, v
	}
	return in, nil
}

func ParseCreateCompetitorInput(body map[string]any) (CompetitorInput, error) {
	return parseCompetitor(body, true)
}

func ParseUpdateCompetitorInput(body map[string]any) (CompetitorInput, error) {
	return parseCompetitor(body, false)
}

// ListSignalsQuery mirrors GET /api/signals.
type ListSignalsQuery struct {
	Source       string
	CompetitorID string
	RunID        string
	Since        string // ISO timestamp
	Limit        int
}

func ParseListSignalsQuery(q url.Values) (ListSignalsQuery, error) {
	v := &ValidationError{}
	out := ListSignalsQuery{
		Source:       q.Get("source"),
		CompetitorID: q.Get("competitorId"),
		RunID:        q.Get("runId"),
		Since:        q.Get("since"),
		Limit:        v.parseLimit(q),
	}
	if out.Source != "" {
		v.enumValue("source", out.Source, domain.ValidSignalSources)
	}
	if out.Since != "" {
		if _, err := time.Parse(time.RFC3339, out.Since); err != nil {
			v.add("since", "must be an RFC 3339 timestamp")
		}
	}
	if v.HasErrors() {
		return ListSignalsQuery{}, v
	}
	return out, nil
}

// DecisionInput mirrors POST /api/approvals/{id}/decision.
type DecisionInput struct {
	Status    domain.ApprovalStatus
	Content   *string // the edited draft; omitted = approve as written
	DecidedBy string
	Note      *string
}

func ParseDecisionInput(body map[string]any) (DecisionInput, error) {
	v := &ValidationError{}
	out := DecisionInput{}
	switch d := getString(body, "decision"); d {
	case "approved", "rejected":
		out.Status = domain.ApprovalStatus(d)
	default:
		v.add("decision", "must be approved or rejected")
	}
	out.DecidedBy = strings.TrimSpace(getString(body, "decidedBy"))
	v.stringRequired("decidedBy", out.DecidedBy)
	v.stringMaxLen("decidedBy", out.DecidedBy, 120)
	if hasKey(body, "content") {
		c, ok := body["content"].(string)
		if !ok {
			v.add("content", "must be a string")
		} else if strings.TrimSpace(c) == "" {
			v.add("content", "must not be empty; reject the draft instead")
		} else {
			v.stringMaxLen("content", c, 100_000)
			out.Content = &c
		}
	}
	if hasKey(body, "note") {
		n := strings.TrimSpace(getString(body, "note"))
		v.stringMaxLen("note", n, 2000)
		if n != "" {
			out.Note = &n
		}
	}
	if v.HasErrors() {
		return DecisionInput{}, v
	}
	return out, nil
}

// ListApprovalsQuery mirrors GET /api/approvals.
type ListApprovalsQuery struct {
	Status string
	RunID  string
	Limit  int
}

func ParseListApprovalsQuery(q url.Values) (ListApprovalsQuery, error) {
	v := &ValidationError{}
	out := ListApprovalsQuery{Status: q.Get("status"), RunID: q.Get("runId"), Limit: v.parseLimit(q)}
	v.enumValue("status", out.Status, domain.ValidApprovalStatuses)
	if v.HasErrors() {
		return ListApprovalsQuery{}, v
	}
	return out, nil
}

// BrandInput mirrors PUT /api/brand. Every field is replaced as given.
type BrandInput struct {
	Voice        string
	ProductFacts string
	Positioning  string
	AvoidClaims  string
	UpdatedBy    string
}

func ParseBrandInput(body map[string]any) (BrandInput, error) {
	v := &ValidationError{}
	in := BrandInput{
		Voice:        strings.TrimSpace(getString(body, "voice")),
		ProductFacts: strings.TrimSpace(getString(body, "productFacts")),
		Positioning:  strings.TrimSpace(getString(body, "positioning")),
		AvoidClaims:  strings.TrimSpace(getString(body, "avoidClaims")),
		UpdatedBy:    strings.TrimSpace(getString(body, "updatedBy")),
	}
	for path, val := range map[string]string{"voice": in.Voice, "productFacts": in.ProductFacts,
		"positioning": in.Positioning, "avoidClaims": in.AvoidClaims} {
		v.stringMaxLen(path, val, 8000)
	}
	v.stringMaxLen("updatedBy", in.UpdatedBy, 120)
	if v.HasErrors() {
		return BrandInput{}, v
	}
	return in, nil
}
