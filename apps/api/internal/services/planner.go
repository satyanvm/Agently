package services

// planner.go is the heart of "a prompt becomes a running agent crew." It compiles a
// natural-language prompt into a TYPED node graph over the shared integration
// catalog (packages/nodes) — any topology, any of the hundreds of catalog nodes —
// executed by the Temporal reasoner (apps/reasoner).
//
// The compiler is MAP-REDUCE over the catalog, because the node universe is far too
// large to hand a model whole:
//
//   ROUTE  — ALL clusters (~700 pieces.<slug> plus the hand-written dozen) are
//            first narrowed to a handful by ONE small-model call over the
//            cluster directory (piecesrouter.go), prefiltered by the offline
//            embedding index (piecesembed.go).
//   MAP    — for every routed cluster in parallel, a small fast model reads
//            that cluster's compact index ("id — label — description") plus the
//            user's request and returns the node ids that could plausibly
//            serve it.
//   REDUCE — the big model receives ONLY the selected nodes' full schemas (config
//            keys, output fields, credential slots) plus the built-in core, and
//            authors the complete graph: keys, types, dependsOn, per-node config
//            with {{input.x}} / {{outputs.key.field}} templating.
//
// The reduce output goes through a VALIDATE → REPAIR loop (structural rules
// mirrored from reasoner/plan.py in nodecatalog.go); validator errors are fed back
// to the model verbatim for up to two repair rounds. That loop stays — it is a
// retry, not a fallback.
//
// What is GONE is the deterministic floor. This compiler used to guarantee that
// "creating a workflow never fails": with no key, on timeout, or after every
// repair failed, it emitted a fixed trigger → research → report graph. That graph
// was indistinguishable in the UI from one the model actually authored, so a
// misconfigured install looked like a working one. Every stage now fails with the
// reason, and the reason reaches the user.

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

// planCache memoizes compiled plans by (prompt,name,email,schedule) so the create
// dialog's debounced live preview (POST /workflows/plan on every pause in typing)
// doesn't re-run the full map-reduce for the same text, and Create reuses the
// preview's work. Small and process-local by design. Only SUCCESSFUL compiles are
// cached — a transient provider error must not pin a failure to a prompt.
var planCache = struct {
	sync.Mutex
	m map[string]Plan
}{m: map[string]Plan{}}

const planCacheMax = 64

// CompilePrompt turns a prompt into a Plan. name/schedule, when non-empty, override
// what the planner infers. An error means the prompt was NOT compiled — there is no
// deterministic floor beneath this any more, by design.
func CompilePrompt(ctx context.Context, prompt, name, email, schedule string) (Plan, error) {
	// Fail on configuration before spending a round trip to discover it.
	if err := RequireAnthropicKey(); err != nil {
		return Plan{}, err
	}
	if err := RequireVoyageKey(); err != nil {
		return Plan{}, err
	}
	cacheKey := prompt + "\x00" + name + "\x00" + email + "\x00" + schedule
	planCache.Lock()
	if p, ok := planCache.m[cacheKey]; ok {
		planCache.Unlock()
		return p, nil
	}
	planCache.Unlock()

	cat := LoadCatalog()
	p := basePlan(prompt, email)

	compiled, err := compileGraphLLM(ctx, cat, prompt, &p)
	if err != nil {
		return Plan{}, err
	}
	p.Nodes = compiled

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
	return p, nil
}

// basePlan fills in the plan metadata the compiler does not need a model for:
// name/description/schedule/input extracted with regexes, no network. The reduce
// model overrides any of these it has an opinion about (see overlayMeta). The
// graph itself always comes from the model.
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

// The cluster universe (~700 pieces + a dozen hand-written) would explode the
// map fan-out if each cluster got its own LLM call. ALL of them are narrowed
// by the semantic ROUTER (piecesrouter.go) — hand-written clusters compete on
// relevance like everything else — so the map phase stays a handful of calls
// no matter how large the installed surface grows. A router failure fails the
// compile; there is no lexical prescreen behind it any more.
const maxClusterCalls = 12

// totalCap bounds how many selected node schemas reach the reduce prompt.
const totalCap = 32

// mapConcurrency bounds parallel map-phase LLM calls (rate-limit hygiene).
const mapConcurrency = 8

// mapPhase selects candidate node ids for the reduce prompt (builtin is always
// in the reduce context and never routed). The router picks the relevant
// clusters — hand-written and pieces alike, most relevant first — and each
// routed cluster then gets one small-model call over its compact index. The
// union is capped fairly across clusters in the router's relevance order, then
// stable-sorted so the reduce prompt is reproducible.
//
// A cluster whose map call fails used to be dropped silently on the theory that
// "a missed cluster costs recall, never correctness." That is only true if you
// never notice: the dropped cluster is usually the integration the user actually
// asked for, and its absence shows up as a graph that quietly routes around it.
// Any failure now fails the compile.
func mapPhase(ctx context.Context, cat *Catalog, prompt string) ([]string, error) {
	const system = `You route a user's automation request to integration nodes.
Given the request and a catalog cluster index (one node per line: "id — label — description"),
return JSON: {"nodes": ["id", ...]} listing ONLY ids from this index that could plausibly be
used to fulfill the request. Be selective — at most 8. Return {"nodes": []} if none apply.`
	const perCluster = 8

	var (
		mu        sync.Mutex
		byCluster = map[string][]string{} // cluster → ids in the model's own order (its ranking)
		failures  []error
		wg        sync.WaitGroup
		sem       = make(chan struct{}, mapConcurrency)
	)

	fail := func(f catalogFile, err error) {
		mu.Lock()
		failures = append(failures, fmt.Errorf("cluster %q: %w", f.Cluster, err))
		mu.Unlock()
	}

	runCluster := func(f catalogFile) {
		defer wg.Done()
		sem <- struct{}{}
		defer func() { <-sem }()

		user := "Request: " + prompt + "\n\nCluster \"" + f.Label + "\":\n" + clusterIndex(f)
		raw, err := planLLMWith(ctx, mapModel(), 512, 12*time.Second, system, []llmMsg{{Role: "user", Content: user}})
		if err != nil {
			fail(f, err)
			return
		}
		var out struct {
			Nodes []string `json:"nodes"`
		}
		if err := json.Unmarshal([]byte(extractJSON(raw)), &out); err != nil {
			fail(f, fmt.Errorf("unparseable JSON: %w", err))
			return
		}
		valid := make([]string, 0, len(out.Nodes))
		for _, id := range out.Nodes {
			if n, ok := cat.ByID[id]; ok && n.Cluster == f.Cluster {
				valid = append(valid, id)
			}
			if len(valid) == perCluster {
				break
			}
		}
		// An empty selection is a legitimate answer — the router casts wide on
		// purpose and expects this stage to narrow. Only a broken CALL is an error.
		if len(valid) == 0 {
			return
		}
		mu.Lock()
		byCluster[f.Cluster] = valid
		mu.Unlock()
	}

	// Every non-builtin cluster — hand-written and pieces alike — competes in
	// the router on relevance.
	var clusterKeys []string
	for _, key := range cat.sortedClusters() {
		if key != "builtin" {
			clusterKeys = append(clusterKeys, key)
		}
	}
	routed, err := routeClusters(ctx, cat, prompt, clusterKeys, maxClusterCalls)
	if err != nil {
		return nil, err
	}
	for _, key := range routed {
		wg.Add(1)
		go runCluster(cat.Clusters[key])
	}
	wg.Wait()

	if len(failures) > 0 {
		// Deterministic message regardless of goroutine completion order.
		msgs := make([]string, len(failures))
		for i, e := range failures {
			msgs[i] = e.Error()
		}
		sort.Strings(msgs)
		return nil, fmt.Errorf("map phase failed for %d of %d cluster(s): %s",
			len(failures), len(routed), strings.Join(msgs, "; "))
	}

	// routed doubles as the relevance order for the cap: most relevant cluster first.
	return capFairly(byCluster, routed, totalCap), nil
}

// capFairly trims the union of per-cluster selections to max. Its predecessor
// sorted ALL ids alphabetically and truncated — silently dropping every node
// late in the alphabet regardless of what the map models judged. Instead,
// clusters take turns contributing their next-best id (each map call's output
// order is its ranking) until the cap. Turn order is the given relevance
// order (the router's ranking), so when the cap lands mid-round the extra
// node goes to the MOST RELEVANT cluster — never to whoever sorts first
// alphabetically. The final sort is cosmetic: it fixes the reduce-prompt
// ordering for reproducibility and costs nanoseconds; it never changes WHICH
// ids survive.
func capFairly(byCluster map[string][]string, order []string, max int) []string {
	clusters := make([]string, 0, len(byCluster))
	seen := make(map[string]bool, len(byCluster))
	total := 0
	for _, k := range order {
		if ids, ok := byCluster[k]; ok && !seen[k] {
			seen[k] = true
			clusters = append(clusters, k)
			total += len(ids)
		}
	}
	// Defensive: clusters that produced ids but are missing from order still
	// contribute, after the ranked ones.
	var rest []string
	for k, ids := range byCluster {
		if !seen[k] {
			rest = append(rest, k)
			total += len(ids)
		}
	}
	sort.Strings(rest)
	clusters = append(clusters, rest...)
	if total > max {
		total = max
	}
	selected := make([]string, 0, total)
	for round := 0; len(selected) < total; round++ {
		for _, k := range clusters {
			if ids := byCluster[k]; round < len(ids) {
				selected = append(selected, ids[round])
				if len(selected) == total {
					break
				}
			}
		}
	}
	sort.Strings(selected)
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

// compileGraphLLM runs map → reduce → validate → repair.
//
// The repair loop stays: a model that returns malformed JSON or a graph that
// fails structural validation gets told exactly what was wrong and asked again,
// twice. That is a retry with new information, not a fallback. Only when the
// repairs are exhausted — or the provider itself fails — does this return an
// error, and it carries the last thing the validator objected to so the failure
// is actionable rather than "couldn't compile".
//
// Reduce gets a larger output budget because graph JSON grows quickly once full
// integration configuration is included.
func compileGraphLLM(ctx context.Context, cat *Catalog, prompt string, p *Plan) ([]domain.GraphNode, error) {
	selected, err := mapPhase(ctx, cat, prompt)
	if err != nil {
		return nil, err
	}
	system := reduceSystemPrompt(cat, selected)

	msgs := []llmMsg{{Role: "user", Content: prompt}}
	const attempts = 3 // 1 author + 2 repairs
	var lastComplaint string
	for i := 0; i < attempts; i++ {
		raw, err := planLLMWith(ctx, reduceModel(), 16000, 90*time.Second, system, msgs)
		if err != nil {
			// Provider-level failure: retrying won't change it.
			return nil, fmt.Errorf("reduce phase: %w", err)
		}
		var spec reduceGraphSpec
		if err := json.Unmarshal([]byte(extractJSON(raw)), &spec); err != nil {
			lastComplaint = "not a single valid JSON object: " + err.Error()
			msgs = append(msgs,
				llmMsg{Role: "assistant", Content: raw},
				llmMsg{Role: "user", Content: "That was not a single valid JSON object (" + err.Error() + "). Return the full corrected JSON object and nothing else."},
			)
			continue
		}
		nodes := toGraphNodes(spec)
		if errs := validateGraph(nodes, cat); len(errs) > 0 {
			lastComplaint = strings.Join(errs, "; ")
			msgs = append(msgs,
				llmMsg{Role: "assistant", Content: raw},
				llmMsg{Role: "user", Content: "Your graph failed validation:\n- " + strings.Join(errs, "\n- ") + "\nReturn the full corrected JSON object and nothing else."},
			)
			continue
		}
		overlayMeta(spec, p)
		return nodes, nil
	}
	return nil, fmt.Errorf("reduce phase: %s could not author a valid graph in %d attempts; last problem: %s",
		reduceModel(), attempts, lastComplaint)
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
- Integration nodes declaring credentials require the operator to configure them;
  missing values fail the node. Prefer a keyless public API when it satisfies the
  request, but use credentialed integrations when they are the correct capability.
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
// type's kind, model left to per-node config / reasoner defaults, layout done later.
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
//
//	branch 1: "at 9:30pm" / "by 8" / "around 17:00"  (lead-in word + time)
//	branch 2: "9:30pm" / "17:00"                      (HH:MM, optional meridiem)
//	branch 3: "9 am" / "9am"                          (hour + required meridiem)
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
