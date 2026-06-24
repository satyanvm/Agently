package services

// planner.go is the heart of "a prompt becomes a running agent crew." It compiles a
// natural-language prompt (e.g. "every morning pull the latest AI research from arXiv
// and Reddit and email me a digest at me@gmail.com") into:
//
//   - a workflow GRAPH (fetcher agents in parallel → an Editor that synthesizes+emails)
//   - a DEFAULT RUN INPUT (topic, arxivQuery, subreddits, urls, email) the worker reads
//   - an optional schedule string (captured, not yet executed)
//
// It is HYBRID and fail-safe: a deterministic keyword parser always produces a working
// plan; if an LLM key is present, we overlay the model's richer understanding on top.
// The deterministic layer is the floor — the feature works with no key and no network.
//
// Why the node NAMES matter: the worker dispatches a fetcher to its source by matching
// the agent name ("arxiv", "hn", "reddit", "news", "web") — see worker
// internal/agent/dag.go fetchForAgent. So the names here are a contract with the engine.

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/agently/api/internal/domain"
)

// Plan is the compiled result of a prompt: everything Create needs to persist a
// runnable workflow.
type Plan struct {
	Name         string            `json:"name"`
	Description  string            `json:"description"`
	Tags         []string          `json:"tags"`
	Nodes        []domain.GraphNode `json:"nodes"`
	DefaultInput map[string]any    `json:"defaultInput"`
	Schedule     *string           `json:"schedule"`
	Sources      []string          `json:"sources"` // for UI preview ("arxiv","reddit",...)
}

// sourceSpec maps a detected source key to the fetcher node the worker understands.
// Name MUST contain the substring the worker dispatches on (fetchForAgent).
type sourceSpec struct {
	key   string
	name  string
	role  domain.AgentRole
	model string
}

var knownSources = map[string]sourceSpec{
	"arxiv":  {"arxiv", "arXiv Fetcher", domain.RoleResearcher, "claude-sonnet-4-6"},
	"hn":     {"hn", "HN Fetcher", domain.RoleResearcher, "claude-sonnet-4-6"},
	"reddit": {"reddit", "Reddit Fetcher", domain.RoleResearcher, "claude-sonnet-4-6"},
	"news":   {"news", "News Fetcher", domain.RoleResearcher, "claude-sonnet-4-6"},
	"web":    {"web", "Web Fetcher", domain.RoleBrowser, "claude-sonnet-4-6"},
}

var (
	reEmail     = regexp.MustCompile(`[\w.+-]+@[\w-]+\.[\w.-]+`)
	reURL       = regexp.MustCompile(`https?://[^\s,)"']+`)
	reSubreddit = regexp.MustCompile(`(?i)r/([A-Za-z0-9_]+)`)
)

// CompilePrompt turns a prompt into a Plan. name/schedule, when non-empty, override
// what the planner infers. Never returns an error — it always falls back to a sane
// deterministic plan.
func CompilePrompt(ctx context.Context, prompt, name, email, schedule string) Plan {
	p := deterministicPlan(prompt, email)
	overlayLLM(ctx, prompt, &p) // best-effort; leaves p intact on any failure

	if strings.TrimSpace(name) != "" {
		p.Name = name
	}
	if p.Name == "" {
		p.Name = "Untitled Workflow"
	}
	if strings.TrimSpace(schedule) != "" {
		s := schedule
		p.Schedule = &s
	}
	if email := firstEmail(email); email != "" {
		p.DefaultInput["email"] = email
	}
	// If somehow no sources were detected, default to the AI Digest shape so the
	// workflow is always runnable rather than an empty graph.
	if len(p.Sources) == 0 {
		p.Sources = []string{"arxiv", "hn"}
	}
	p.Nodes = buildGraph(p.Sources)
	p.DefaultInput["sources"] = toAnySlice(p.Sources)
	return p
}

// deterministicPlan is the floor: pure keyword/regex parsing, no network.
func deterministicPlan(prompt, email string) Plan {
	lower := strings.ToLower(prompt)
	input := map[string]any{}
	var sources []string
	seen := map[string]bool{}
	add := func(k string) {
		if !seen[k] {
			seen[k] = true
			sources = append(sources, k)
		}
	}

	if strings.Contains(lower, "arxiv") || strings.Contains(lower, "papers") || strings.Contains(lower, "research") {
		add("arxiv")
	}
	if strings.Contains(lower, "hacker news") || strings.Contains(lower, "hackernews") ||
		strings.Contains(lower, " hn") || strings.Contains(lower, "ycombinator") ||
		strings.Contains(lower, " yc ") || strings.Contains(lower, "y combinator") || strings.Contains(lower, "startup") {
		add("hn")
	}
	if strings.Contains(lower, "reddit") || reSubreddit.MatchString(prompt) {
		add("reddit")
		subs := []any{}
		for _, m := range reSubreddit.FindAllStringSubmatch(prompt, -1) {
			subs = append(subs, m[1])
		}
		if len(subs) > 0 {
			input["subreddits"] = subs
		}
	}
	if strings.Contains(lower, "google news") || strings.Contains(lower, "news.google") ||
		strings.Contains(lower, "news") {
		add("news")
	}

	// Explicit URLs and X/Twitter → the browser-driven Web Fetcher (visits arbitrary
	// sites; X is auth-gated so it degrades gracefully to whatever it can extract).
	urls := []any{}
	for _, u := range reURL.FindAllString(prompt, -1) {
		urls = append(urls, strings.TrimRight(u, ".,);"))
	}
	if strings.Contains(lower, "twitter") || strings.Contains(lower, "x.com") ||
		regexp.MustCompile(`(?i)(^|\s)x(\s|,|$)`).MatchString(lower) {
		urls = append(urls, "https://x.com/search?q="+strings.ReplaceAll(topicFrom(lower), " ", "%20")+"&f=live")
	}
	if len(urls) > 0 || strings.Contains(lower, "website") || strings.Contains(lower, "visit") || strings.Contains(lower, "browse") {
		add("web")
		if len(urls) > 0 {
			input["urls"] = urls
		}
	}

	topic := topicFrom(lower)
	input["topic"] = topic
	input["arxivQuery"] = "all:" + topic
	if e := firstEmail(email + " " + prompt); e != "" {
		input["email"] = e
	}

	return Plan{
		Name:         titleFrom(topic),
		Description:  "Auto-generated from a prompt: " + truncateStr(strings.TrimSpace(prompt), 240),
		Tags:         []string{"prompt", "digest"},
		DefaultInput: input,
		Schedule:     scheduleFrom(lower),
		Sources:      sources,
	}
}

// overlayLLM asks the model for a richer structured plan and overlays valid fields
// onto p. Any failure (no key, timeout, bad JSON) leaves p exactly as it was.
func overlayLLM(ctx context.Context, prompt string, p *Plan) {
	const system = `You convert a user's request into a workflow plan for an AI digest agent.
Return JSON with keys:
  name (short title), description (one sentence), topic (search subject),
  sources (subset of ["arxiv","hn","reddit","news","web"]),
  arxivQuery (an arXiv search query), subreddits (array of subreddit names without r/),
  urls (array of full URLs the browser should visit), email (recipient or ""),
  schedule (e.g. "daily 08:00" or "" if none).
Pick sources that match the request. Use "news" for Google News, "web" for arbitrary
sites or X/Twitter. Only include keys you are confident about.`

	raw, err := planLLM(ctx, system, prompt)
	if err != nil {
		return
	}
	var out struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Topic       string   `json:"topic"`
		Sources     []string `json:"sources"`
		ArxivQuery  string   `json:"arxivQuery"`
		Subreddits  []string `json:"subreddits"`
		URLs        []string `json:"urls"`
		Email       string   `json:"email"`
		Schedule    string   `json:"schedule"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &out); err != nil {
		return
	}

	if out.Name != "" {
		p.Name = out.Name
	}
	if out.Description != "" {
		p.Description = out.Description
	}
	if out.Topic != "" {
		p.DefaultInput["topic"] = out.Topic
	}
	if out.ArxivQuery != "" {
		p.DefaultInput["arxivQuery"] = out.ArxivQuery
	}
	if len(out.Subreddits) > 0 {
		p.DefaultInput["subreddits"] = toAnySlice(out.Subreddits)
	}
	if len(out.URLs) > 0 {
		p.DefaultInput["urls"] = toAnySlice(out.URLs)
	}
	if out.Email != "" && firstEmail(out.Email) != "" {
		p.DefaultInput["email"] = firstEmail(out.Email)
	}
	if out.Schedule != "" {
		s := out.Schedule
		p.Schedule = &s
	}
	if valid := filterKnownSources(out.Sources); len(valid) > 0 {
		p.Sources = valid
	}
}

// buildGraph lays out N fetcher nodes (col 0) → one Editor (col 1) depending on all.
func buildGraph(sources []string) []domain.GraphNode {
	nodes := make([]domain.GraphNode, 0, len(sources)+1)
	deps := make([]string, 0, len(sources))
	for i, key := range sources {
		spec, ok := knownSources[key]
		if !ok {
			continue
		}
		nodes = append(nodes, domain.GraphNode{
			Key: spec.key, AgentDefinitionID: nil, Name: spec.name, Role: spec.role,
			Model: spec.model, Col: 0, Row: i, DependsOn: nil,
		})
		deps = append(deps, spec.key)
	}
	nodes = append(nodes, domain.GraphNode{
		Key: "editor", AgentDefinitionID: nil, Name: "Editor", Role: domain.RoleWriter,
		Model: "claude-opus-4-8", Col: 1, Row: len(sources) / 2, DependsOn: deps,
	})
	return nodes
}

/* ------------------------------- small helpers ------------------------------ */

func filterKnownSources(in []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, s := range in {
		s = strings.ToLower(strings.TrimSpace(s))
		if _, ok := knownSources[s]; ok && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// topicFrom pulls a rough subject out of the prompt: text after "about"/"on", else
// "AI" (the overwhelming common case for this product).
func topicFrom(lower string) string {
	for _, kw := range []string{" about ", " on the topic of ", " regarding "} {
		if i := strings.Index(lower, kw); i >= 0 {
			rest := lower[i+len(kw):]
			rest = strings.TrimSpace(strings.SplitN(rest, " and ", 2)[0])
			if cut := strings.IndexAny(rest, ".,;\n"); cut > 0 {
				rest = rest[:cut]
			}
			if rest != "" {
				return truncateStr(rest, 60)
			}
		}
	}
	if strings.Contains(lower, "ai") || strings.Contains(lower, "artificial intelligence") || strings.Contains(lower, "machine learning") {
		return "AI"
	}
	return "AI"
}

func titleFrom(topic string) string {
	t := strings.TrimSpace(topic)
	if t == "" || strings.EqualFold(t, "ai") {
		return "AI Digest"
	}
	return strings.Title(t) + " Digest" //nolint:staticcheck // Title is fine for a short label
}

// reTimeOfDay matches an explicit clock time anywhere in the prompt. Each branch
// captures the FULL time (hour, optional minutes, optional meridiem) so a lead-in like
// "at" can't truncate "at 9:30pm" to "9". A time must carry at least one signal — a
// lead-in word, a ":MM", or an am/pm — so stray numbers ("top 5 posts") never match.
//   branch 1: "at 9:30pm" / "by 8" / "around 17:00"  (lead-in word + time)
//   branch 2: "9:30pm" / "17:00"                      (HH:MM, optional meridiem)
//   branch 3: "9 am" / "9am"                          (hour + required meridiem)
var reTimeOfDay = regexp.MustCompile(`(?i)\b(?:at|by|around)\s+(\d{1,2})(?::(\d{2}))?\s*(am|pm)?\b|\b(\d{1,2}):(\d{2})\s*(am|pm)?\b|\b(\d{1,2})\s*(am|pm)\b`)

// parseTimeOfDay pulls a "HH:MM" 24-hour clock time out of the prompt, if one is
// stated. ok is false when no time is mentioned (caller falls back to a default).
func parseTimeOfDay(lower string) (hh, mm int, ok bool) {
	m := reTimeOfDay.FindStringSubmatch(lower)
	if m == nil {
		return 0, 0, false
	}
	var meridiem string
	switch {
	case m[1] != "": // lead-in: at/by/around H[:MM] [am/pm]
		hh = atoiSafe(m[1])
		if m[2] != "" {
			mm = atoiSafe(m[2])
		}
		meridiem = m[3]
	case m[4] != "": // HH:MM (+ optional am/pm)
		hh = atoiSafe(m[4])
		mm = atoiSafe(m[5])
		meridiem = m[6]
	default: // H am/pm
		hh = atoiSafe(m[7])
		meridiem = m[8]
	}
	switch strings.ToLower(meridiem) {
	case "pm":
		if hh < 12 {
			hh += 12
		}
	case "am":
		if hh == 12 {
			hh = 0
		}
	}
	if hh < 0 || hh > 23 || mm < 0 || mm > 59 {
		return 0, 0, false
	}
	return hh, mm, true
}

// scheduleFrom infers a schedule string from the prompt. Daily cadence carries the
// stated time of day ("every day at 9 am" → "daily 09:00"); a bare "morning" with no
// time defaults to 09:00. The scheduler (services/scheduler.go) parses these strings.
func scheduleFrom(lower string) *string {
	hh, mm, hasTime := parseTimeOfDay(lower)
	daily := strings.Contains(lower, "every morning") || strings.Contains(lower, "each morning") ||
		strings.Contains(lower, "morning") || strings.Contains(lower, "daily") ||
		strings.Contains(lower, "every day") || strings.Contains(lower, "each day") ||
		strings.Contains(lower, "every evening") || strings.Contains(lower, "every night")
	switch {
	case strings.Contains(lower, "every hour") || strings.Contains(lower, "hourly"):
		s := "hourly"
		return &s
	case daily || hasTime:
		if !hasTime {
			hh, mm = 9, 0 // "morning" with no explicit time
		}
		s := "daily " + pad2(hh) + ":" + pad2(mm)
		return &s
	}
	return nil
}

func pad2(n int) string {
	if n < 10 {
		return "0" + itoa(n)
	}
	return itoa(n)
}

func atoiSafe(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return -1
		}
		n = n*10 + int(r-'0')
	}
	return n
}

func firstEmail(s string) string { return reEmail.FindString(s) }

func toAnySlice(in []string) []any {
	out := make([]any, len(in))
	for i, s := range in {
		out[i] = s
	}
	return out
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
