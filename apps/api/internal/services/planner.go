package services

// planner.go is the heart of "a prompt becomes a running agent crew." It compiles a
// natural-language prompt into a TYPED node graph over the shared integration
// catalog (packages/nodes) — any topology, any of the hundreds of catalog nodes —
// executed by the Temporal reasoner (apps/reasoner).
//
// The compiler is MAP-REDUCE over the catalog, because the node universe is far too
// large to hand a model whole:
//
//   MAP    — for every catalog cluster in parallel, a small fast model reads that
//            cluster's compact index ("id — label — description") plus the user's
//            request and returns the node ids that could plausibly serve it.
//   REDUCE — the big model receives ONLY the selected nodes' full schemas (config
//            keys, output fields, credential slots) plus the built-in core, and
//            authors the complete graph: keys, types, dependsOn, per-node config
//            with {{input.x}} / {{outputs.key.field}} templating.
//
// The reduce output goes through a VALIDATE → REPAIR loop (structural rules
// mirrored from reasoner/plan.py in nodecatalog.go); validator errors are fed back
// to the model verbatim for up to two repair rounds.
//
// It stays HYBRID and fail-safe like its predecessor: with no LLM key, on timeout,
// or if every repair fails, we fall back to a deterministic TYPED graph
// (trigger → research agent → report [→ email]) so creating a workflow never fails.

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/agently/api/internal/domain"
)

// Plan is the compiled result of a prompt: everything Create needs to persist a
// runnable workflow.
type Plan struct {
	Name         string             `json:"name"`
	Description  string             `json:"description"`
	Tags         []string           `json:"tags"`
	Nodes        []domain.GraphNode `json:"nodes"`
	DefaultInput map[string]any     `json:"defaultInput"`
	Schedule     *string            `json:"schedule"`
	Sources      []string           `json:"sources"` // distinct services used, for UI preview
}

var (
	reEmail     = regexp.MustCompile(`[\w.+-]+@[\w-]+\.[\w.-]+`)
	reSubreddit = regexp.MustCompile(`(?i)r/([A-Za-z0-9_]+)`)
)

// Compilation model tiers. Map wants cheap+fast; reduce wants the strongest
// available (graph authorship is the hard part). Both env-overridable.
func mapModel() string    { return envOr("PLANNER_MAP_MODEL", "claude-haiku-4-5") }
func reduceModel() string { return envOr("PLANNER_MODEL", "claude-opus-4-8") }

// planCache memoizes compiled plans by (prompt,name,email,schedule) so the create
// dialog's debounced live preview (POST /workflows/plan on every pause in typing)
// doesn't re-run the full map-reduce for the same text, and Create reuses the
// preview's work. Small and process-local by design.
var planCache = struct {
	sync.Mutex
	m map[string]Plan
}{m: map[string]Plan{}}

const planCacheMax = 64

// CompilePrompt turns a prompt into a Plan. name/schedule, when non-empty, override
// what the planner infers. Never returns an error — it always falls back to a sane
// deterministic plan.
func CompilePrompt(ctx context.Context, prompt, name, email, schedule string) Plan {
	cacheKey := prompt + "\x00" + name + "\x00" + email + "\x00" + schedule
	planCache.Lock()
	if p, ok := planCache.m[cacheKey]; ok {
		planCache.Unlock()
		return p
	}
	planCache.Unlock()

	cat := LoadCatalog()
	p := basePlan(prompt, email)

	if compiled, ok := compileGraphLLM(ctx, cat, prompt, &p); ok {
		p.Nodes = compiled
	} else {
		p.Nodes = fallbackGraph(p)
	}

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
	if e := firstEmail(email); e != "" {
		p.DefaultInput["email"] = e
	}
	p.Nodes = layoutGraph(p.Nodes)
	p.Sources = servicesOf(p.Nodes)

	planCache.Lock()
	if len(planCache.m) >= planCacheMax { // crude but sufficient: reset when full
		planCache.m = map[string]Plan{}
	}
	planCache.m[cacheKey] = p
	planCache.Unlock()
	return p
}

// basePlan is the deterministic floor: name/description/schedule/input extracted
// with regexes, no network. The graph itself comes from the LLM (or fallbackGraph).
func basePlan(prompt, email string) Plan {
	lower := strings.ToLower(prompt)
	input := map[string]any{}

	topic := topicFrom(lower)
	input["topic"] = topic
	if e := firstEmail(email + " " + prompt); e != "" {
		input["email"] = e
	}

	return Plan{
		Name:         titleFrom(topic),
		Description:  "Auto-generated from a prompt: " + truncateStr(strings.TrimSpace(prompt), 240),
		Tags:         []string{"prompt"},
		DefaultInput: input,
		Schedule:     scheduleFrom(lower),
	}
}

/* --------------------------------- map phase --------------------------------- */

// Pieces clusters (one per Activepieces piece, ~700 of them) would explode the
// map fan-out if each got its own LLM call. They are prescreened LEXICALLY: a
// cheap term-overlap score against the prompt keeps only the most plausible
// few, so the map phase stays ~a dozen-and-a-half calls no matter how large
// the installed piece surface grows. Hand-written clusters (a dozen) always go
// to the model, exactly as before.
const maxPieceClusterCalls = 12

// mapConcurrency bounds parallel map-phase LLM calls (rate-limit hygiene).
const mapConcurrency = 8

// promptTokens lowercases and tokenizes the user's request for the prescreen.
func promptTokens(prompt string) map[string]bool {
	toks := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(prompt), func(r rune) bool {
		return !('a' <= r && r <= 'z') && !('0' <= r && r <= '9')
	}) {
		if len(w) > 2 {
			toks[w] = true
		}
	}
	return toks
}

// topPieceClusters scores each pieces.<slug> cluster by term overlap with the
// prompt (slug words weigh extra) and returns the best few, alphabetical among
// ties so the selection — and therefore the compiled prompt — is reproducible.
func topPieceClusters(cat *Catalog, prompt string, keys []string, max int) []string {
	toks := promptTokens(prompt)
	if len(toks) == 0 {
		return nil
	}
	type scored struct {
		key   string
		score int
	}
	var ranked []scored
	for _, key := range keys {
		f := cat.Clusters[key]
		score := 0
		for _, w := range strings.Split(strings.TrimPrefix(key, "pieces."), "-") {
			if toks[w] {
				score += 3 // naming the service is the strongest signal
			}
		}
		seen := map[string]bool{}
		for _, n := range f.Nodes {
			for _, w := range strings.Fields(strings.ToLower(n.Search + " " + n.Label)) {
				if toks[w] && !seen[w] {
					seen[w] = true
					score++
				}
			}
		}
		if score > 0 {
			ranked = append(ranked, scored{key, score})
		}
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		return ranked[i].key < ranked[j].key
	})
	if len(ranked) > max {
		ranked = ranked[:max]
	}
	out := make([]string, len(ranked))
	for i, r := range ranked {
		out[i] = r.key
	}
	return out
}

// mapPhase fans one small-model call out per integration cluster (builtin is
// always in the reduce context and never routed). Each call sees only that
// cluster's compact index. Failures and junk are dropped silently — a missed
// cluster costs recall, never correctness. Returns selected ids, capped and
// stable-sorted so the reduce prompt is reproducible.
func mapPhase(ctx context.Context, cat *Catalog, prompt string) []string {

	// Constant system prompt given to the LLM.
	// This tells the model exactly what output format is expected.
	const system = `You route a user's automation request to integration nodes.
Given the request and a catalog cluster index (one node per line: "id — label — description"),
return JSON: {"nodes": ["id", ...]} listing ONLY ids from this index that could plausibly be
used to fulfill the request. Be selective — at most 8. Return {"nodes": []} if none apply.`

	// Maximum number of nodes we want to accept from a single cluster.
	perCluster := 8

	// Declare multiple variables together.
	var (

		// Mutex protects shared data from being modified by
		// multiple goroutines at the same time.
		mu sync.Mutex

		// Stores all selected node IDs from every cluster.
		selected []string

		// WaitGroup waits until every goroutine has finished.
		wg sync.WaitGroup

		// Semaphore bounding parallel LLM calls.
		sem = make(chan struct{}, mapConcurrency)
	)

	// Partition clusters: hand-written ones ALL go to the model (as before);
	// pieces.<slug> clusters go through the lexical prescreen first.
	var work, pieceKeys []string
	for _, key := range cat.sortedClusters() {

		// Ignore the builtin cluster.
		if key == "builtin" {
			continue
		}
		if strings.HasPrefix(key, "pieces.") {
			pieceKeys = append(pieceKeys, key)
			continue
		}
		work = append(work, key)
	}
	work = append(work, topPieceClusters(cat, prompt, pieceKeys, maxPieceClusterCalls)...)

	// Iterate through the clusters that made the cut.
	for _, key := range work {

		// Get the catalog information for this cluster.
		f := cat.Clusters[key]

		// Tell the WaitGroup that one more goroutine is about to start.
		wg.Add(1)

		// Start a new goroutine.
		// Every cluster runs independently, bounded by the semaphore.
		go func(f catalogFile) {

			// This will automatically execute when the goroutine exits,
			// even if we return early because of an error.
			defer wg.Done()

			// Respect the concurrency bound.
			sem <- struct{}{}
			defer func() { <-sem }()

			// Build the user prompt that will be sent to the LLM.
			user := "Request: " + prompt +
				"\n\nCluster \"" + f.Label + "\":\n" +
				clusterIndex(f) // is this sending the cluster? Because we do need send the whole cluster(with  id + label + description to the LLM)

			// Ask the LLM to choose relevant node IDs.
			raw, err := planLLMWith(
				ctx,
				mapModel(),
				512,
				12*time.Second,
				system,
				[]llmMsg{
					{
						Role:    "user",
						Content: user,
					},
				},
			)

			// If the LLM call failed, stop this goroutine.
			if err != nil {
				return
			}

			// Anonymous struct used only for decoding JSON.
			// Expected JSON:
			//
			// {
			//     "nodes": ["id1", "id2"]
			// }
			var out struct {
				Nodes []string `json:"nodes"`
			}

			// Convert the JSON string into the Go struct.
			if err := json.Unmarshal(
				[]byte(extractJSON(raw)),
				&out,
			); err != nil {
				return
			}

			// Create a slice to hold only valid node IDs.
			valid := make([]string, 0, len(out.Nodes))

			// Check every node returned by the LLM.
			for _, id := range out.Nodes {

				// Look up the node in the catalog.
				n, ok := cat.ByID[id]

				// Accept the node only if:
				// 1. It actually exists.
				// 2. It belongs to the current cluster.
				if ok && n.Cluster == f.Cluster {
					valid = append(valid, id)
				}

				// Stop after collecting enough nodes.
				if len(valid) == perCluster {
					break
				}
			}

			// Lock before modifying shared data.
			mu.Lock()

			// Append every valid node into the global result slice.
			// The ... expands the slice so every element is appended.
			selected = append(selected, valid...)

			// Unlock so other goroutines can access selected.
			mu.Unlock()

		}(f) // Immediately call the anonymous function with f.

	}

	// Wait until every goroutine has called wg.Done().
	wg.Wait()

	// Sort the final list alphabetically.
	sort.Strings(selected)

	// Maximum total number of selected nodes.
	const totalCap = 32

	// Trim the slice if it is too large.
	if len(selected) > totalCap {
		selected = selected[:totalCap]
	}

	// Return the final list of node IDs.
	return selected
}

/* -------------------------------- reduce phase -------------------------------- */

// reduceGraphSpec is the JSON the reduce model must emit.
type reduceGraphSpec struct {
	Name         string         `json:"name"`
	Description  string         `json:"description"`
	Schedule     string         `json:"schedule"`
	DefaultInput map[string]any `json:"defaultInput"`
	Nodes        []struct {
		Key       string         `json:"key"`
		Type      string         `json:"type"`
		Name      string         `json:"name"`
		DependsOn []string       `json:"dependsOn"`
		Config    map[string]any `json:"config"`
	} `json:"nodes"`
}

// compileGraphLLM runs map → reduce → validate → repair. ok=false means the caller
// must use the deterministic fallback (no key, hard timeout, unrepairable output).
func compileGraphLLM(ctx context.Context, cat *Catalog, prompt string, p *Plan) ([]domain.GraphNode, bool) {
	selected := mapPhase(ctx, cat, prompt)
	system := reduceSystemPrompt(cat, selected)

	msgs := []llmMsg{{Role: "user", Content: prompt}}
	const attempts = 3 // 1 author + 2 repairs
	for i := 0; i < attempts; i++ {
		raw, err := planLLMWith(ctx, reduceModel(), 4096, 45*time.Second, system, msgs)
		if err != nil {
			return nil, false // provider-level failure: retrying won't change it
		}
		var spec reduceGraphSpec
		if err := json.Unmarshal([]byte(extractJSON(raw)), &spec); err != nil {
			msgs = append(msgs,
				llmMsg{Role: "assistant", Content: raw},
				llmMsg{Role: "user", Content: "That was not a single valid JSON object (" + err.Error() + "). Return the full corrected JSON object and nothing else."},
			)
			continue
		}
		nodes := toGraphNodes(spec)
		if errs := validateGraph(nodes, cat); len(errs) > 0 {
			msgs = append(msgs,
				llmMsg{Role: "assistant", Content: raw},
				llmMsg{Role: "user", Content: "Your graph failed validation:\n- " + strings.Join(errs, "\n- ") + "\nReturn the full corrected JSON object and nothing else."},
			)
			continue
		}
		overlayMeta(spec, p)
		return nodes, true
	}
	return nil, false
}

// reduceSystemPrompt assembles the node contract: graph rules + full schemas for
// the built-in core and every map-selected integration node. This is the ONLY
// world knowledge we give the model — no source cookbook, no examples of "good"
// services. The model is trusted to design; the contract keeps it executable.
func reduceSystemPrompt(cat *Catalog, selected []string) string {
	var b strings.Builder
	b.WriteString(`You are an expert workflow architect. Convert the user's request into a directed
acyclic graph of typed nodes that the execution engine runs. Design the best possible workflow:
decompose the request, parallelize independent work, and wire data between nodes precisely.

Return ONE JSON object:
{
  "name": "short title",
  "description": "one sentence",
  "schedule": "daily HH:MM" | "hourly" | "",
  "defaultInput": { "topic": "...", ... },
  "nodes": [
    { "key": "snake_case_unique", "type": "<node type id>", "name": "Human label",
      "dependsOn": ["upstream_key", ...], "config": { ... } }
  ]
}

GRAPH RULES (validated mechanically — violations are returned to you to fix):
- Every "type" must be one of the node types listed below. No other types exist.
- "key" values are unique; every "dependsOn" entry names an existing key; no cycles.
- Fill every required config field of a node's schema.
- Start with exactly one trigger node: trigger.schedule when the user wants a cadence
  (also set "schedule"), else trigger.manual. Triggers have no dependsOn.
- End with output node(s) matching how the user wants results delivered (email, slack,
  report). When in doubt, finish with output.report.

DATA FLOW (exactness matters — unknown references resolve to EMPTY STRING, silently):
- Config strings support {{input.x}} (run input; put user-tunable values like topic or
  email into defaultInput and reference them this way) and {{outputs.<key>.<field>}}
  where <key> is an upstream node's key and <field> is one of ITS listed outputs.
  Only reference fields listed in the upstream node's "outputs".
- logic.branch / logic.filter conditions: "<lhs> <op> <rhs>" with == != > < >= <=,
  each side a dotted ref (outputs.fetch.status) or literal — e.g. "outputs.fetch.status == 200".
- logic.loop fans out its downstream branch once per item: config.items is a dotted ref
  to a list (e.g. outputs.fetch.items); inside the body use {{item}} for the current
  element; collected results appear as outputs.<loop_key>.results.

DESIGN GUIDANCE:
- agent.llm is the workhorse for reasoning, extraction, summarizing and reformatting —
  prefer it over tool.code for text/JSON wrangling. Give it a crisp system prompt and
  reference upstream outputs explicitly in its prompt.
- tool.http can call ANY public API you know of (exact documented URLs only) — the
  integration nodes below are conveniences, not limits. tool.browser reads pages that
  need a real browser.
- Integration nodes declaring credentials run only where the operator configured those
  env vars; otherwise they record intent without failing the run. Still use them when
  they fit — but when a keyless route exists (public API via tool.http), prefer it.
- Node types prefixed "pieces." are Activepieces-backed actions (full-fidelity vendor
  integrations). When a hand-written node and a pieces.* node cover the same capability,
  prefer the hand-written one; reach for pieces.* when it's the only coverage or when
  its action fits the request more precisely.
- Keep graphs as small as the request allows. Parallel branches that later join are good.

NODE TYPES AVAILABLE:

`)
	// Built-in core first, always.
	for _, n := range cat.Clusters["builtin"].Nodes {
		b.WriteString(nodeSchema(n))
		b.WriteString("\n")
	}
	// Then the map-selected integrations.
	for _, id := range selected {
		if n, ok := cat.ByID[id]; ok {
			b.WriteString(nodeSchema(n))
			b.WriteString("\n")
		}
	}
	return b.String()
}

// toGraphNodes converts the model's spec into domain nodes: role derived from the
// type's kind, model left to per-node config / engine defaults, layout done later.
func toGraphNodes(spec reduceGraphSpec) []domain.GraphNode {
	nodes := make([]domain.GraphNode, 0, len(spec.Nodes))
	for _, n := range spec.Nodes {
		cfg := n.Config
		if cfg == nil {
			cfg = map[string]any{}
		}
		model := ""
		if m, ok := cfg["model"].(string); ok {
			model = m
		}
		name := n.Name
		if strings.TrimSpace(name) == "" {
			name = n.Key
		}
		nodes = append(nodes, domain.GraphNode{
			Key: n.Key, AgentDefinitionID: nil, Name: name, Role: roleForType(n.Type),
			Model: model, DependsOn: n.DependsOn, Type: n.Type, Config: cfg,
		})
	}
	return nodes
}

// overlayMeta copies the reduce model's plan-level fields onto p (only when set —
// the deterministic floor stays intact otherwise).
func overlayMeta(spec reduceGraphSpec, p *Plan) {
	if spec.Name != "" {
		p.Name = spec.Name
	}
	if spec.Description != "" {
		p.Description = spec.Description
	}
	if spec.Schedule != "" {
		s := spec.Schedule
		p.Schedule = &s
	}
	for k, v := range spec.DefaultInput {
		p.DefaultInput[k] = v
	}
}

/* ------------------------------ fallback graph ------------------------------ */

// fallbackGraph is the deterministic floor when no LLM is reachable: a small TYPED
// graph (trigger → research agent → report [→ email]) that the reasoner executes
// as-is. Replaces the old fixed five-source digest shape.
func fallbackGraph(p Plan) []domain.GraphNode {
	trigger := domain.GraphNode{
		Key: "trigger", Name: "Manual trigger", Role: domain.RoleOrchestrator,
		Type: "trigger.manual", Config: map[string]any{},
	}
	if p.Schedule != nil {
		trigger.Key = "schedule"
		trigger.Name = "Schedule"
		trigger.Type = "trigger.schedule"
	}
	research := domain.GraphNode{
		Key: "research", Name: "Research Agent", Role: domain.RoleOrchestrator,
		Model: "claude-opus-4-8", DependsOn: []string{trigger.Key}, Type: "agent.llm",
		Config: map[string]any{
			"system": "You are a thorough research agent. Produce a well-structured, sourced digest.",
			"prompt": "Research the following and write a concise, well-organized digest with the most important findings first:\n\nTopic: {{input.topic}}",
		},
	}
	report := domain.GraphNode{
		Key: "report", Name: "Report", Role: domain.RoleWriter,
		DependsOn: []string{"research"}, Type: "output.report",
		Config: map[string]any{"title": p.Name, "format": "markdown"},
	}
	nodes := []domain.GraphNode{trigger, research, report}
	if _, ok := p.DefaultInput["email"]; ok {
		nodes = append(nodes, domain.GraphNode{
			Key: "email", Name: "Email digest", Role: domain.RoleWriter,
			DependsOn: []string{"research"}, Type: "output.email",
			Config: map[string]any{"to": "{{input.email}}", "subject": p.Name},
		})
	}
	return nodes
}

/* ------------------------------- small helpers ------------------------------ */

// servicesOf lists the distinct integration services a graph touches ("slack",
// "github", …) for the UI preview chip row. Built-in kinds are skipped; a
// pieces.<slug>.<action> node counts as its slug.
func servicesOf(nodes []domain.GraphNode) []string {
	builtin := map[string]bool{"trigger": true, "agent": true, "tool": true, "logic": true, "output": true}
	seen := map[string]bool{}
	var out []string
	for _, n := range nodes {
		svc := strings.SplitN(n.Type, ".", 2)[0]
		if svc == "pieces" {
			svc = pieceSlugOf(n.Type)
		}
		if svc == "" || builtin[svc] || seen[svc] {
			continue
		}
		seen[svc] = true
		out = append(out, svc)
	}
	sort.Strings(out)
	return out
}

// extractJSON tolerates models that wrap the object in prose or code fences: it
// returns the substring from the first '{' to the matching final '}'.
func extractJSON(raw string) string {
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start >= 0 && end > start {
		return raw[start : end+1]
	}
	return raw
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

// Unused-import guards for helpers kept for other callers.
var _ = fmt.Sprintf
var _ = reSubreddit
var _ = toAnySlice
