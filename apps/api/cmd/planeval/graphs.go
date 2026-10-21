package main

// graphs.go — `go run ./cmd/planeval -graphs`: compile plain-English marketing
// playbook prompts through the REAL planner (CompilePrompt: embeddings →
// router → map → reduce → validate/repair) and check the graphs' structure:
//
//   - the signal / publishing steps the prompt implies are present
//     (any-of groups; a trailing "." matches a type prefix, e.g. "pieces.linkedin.");
//   - an approval exists when the prompt asks for one, assigned to the person
//     it names (in config.assignee directly or via {{input.x}});
//   - every AI step that writes a draft feeding an approval uses brand context;
//   - the schedule matches the cadence in the prompt.
//
// "Nothing publishes without approval" is not re-checked here: CompilePrompt
// already refuses to return a graph that breaks it (validateGraph).
//
// Like recall, this is an eval, not a test: it costs model calls (about 15 per
// prompt) and its results move with the models. Run it after changing the
// planner prompt, the catalog descriptions or the models.

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/agently/api/internal/domain"
	"github.com/agently/api/internal/services"
)

type graphGolden struct {
	prompt   string
	types    [][]string // each group: the graph has a node of ANY of these types
	approval bool       // the prompt asks for a person's approval
	assignee string     // who should approve ("" = don't check)
	schedule string     // prefix the schedule must start with ("" = don't check)
}

var graphGoldens = []graphGolden{
	{prompt: "When a competitor launches anything AI-related, draft a LinkedIn post in our CEO's voice and send it to Rahul to approve.",
		types: [][]string{{"intel.watchCompetitors", "intel.snapshotPage", "intel.diffPage"}}, approval: true, assignee: "Rahul"},
	{prompt: "Every weekday at 8am check whether a competitor changed their pricing page. If so, draft a LinkedIn post explaining why our pricing is simpler and post it on our LinkedIn page once Ana approves it.",
		types: [][]string{{"intel.watchCompetitors", "intel.snapshotPage"}, {"pieces.linkedin."}}, approval: true, assignee: "Ana", schedule: "weekdays 08:00"},
	{prompt: "Every Monday morning collect this week's news and Reddit discussions about status pages, pick the 3 best content ideas and, after I approve them, save a blog outline for each into a Google Doc.",
		types: [][]string{{"gdelt.searchDocs", "googlenews.fetchRss", "rss.fetchFeed"}, {"reddit.", "pieces.reddit."}, {"pieces.google-docs."}}, approval: true, schedule: "weekly mon"},
	{prompt: "When a competitor's changelog mentions a new integration, alert our product team in Slack.",
		types: [][]string{{"intel.watchCompetitors", "intel.snapshotPage", "intel.diffPage"}, {"slack.", "output.slack", "pieces.slack."}}},
	{prompt: "Every day, check our competitors' careers pages and email me a summary of the roles they started hiring for.",
		types: [][]string{{"intel.watchCompetitors", "intel.snapshotPage"}, {"output.email", "pieces.gmail."}}, schedule: "daily"},
	{prompt: "Search Hacker News every day for mentions of our competitors and record each mention as a signal.",
		types: [][]string{{"hackernews.", "pieces.hackernews."}, {"intel.recordSignal"}}, schedule: "daily"},
	{prompt: "If a competitor cuts their prices, draft an email to our at-risk customers and, once Maria approves it, save it as a Gmail draft.",
		types: [][]string{{"intel.watchCompetitors", "intel.snapshotPage", "intel.diffPage"}, {"pieces.gmail."}}, approval: true, assignee: "Maria"},
	{prompt: "Whenever we publish a new post on our blog's RSS feed, draft a tweet about it and post it on X after marketing approves it.",
		types: [][]string{{"rss.fetchFeed", "pieces.rss.", "tool.http"}, {"pieces.twitter.", "pieces.typefully.", "pieces.buffer."}}, approval: true},
	{prompt: "Watch Acme's pricing and changelog pages daily and send our sales team a Slack summary of anything that changed.",
		types: [][]string{{"intel.watchCompetitors", "intel.snapshotPage"}, {"slack.", "output.slack", "pieces.slack."}}, schedule: "daily"},
	{prompt: "Once a week, find the most upvoted Reddit questions about incident communication and draft a helpful reply for each that Sam has to approve before anything is posted.",
		types: [][]string{{"reddit.", "pieces.reddit."}}, approval: true, assignee: "Sam", schedule: "weekly"},
}

func hasType(nodes []domain.GraphNode, want string) bool {
	for _, n := range nodes {
		if n.Type == want || (strings.HasSuffix(want, ".") && strings.HasPrefix(n.Type, want)) {
			return true
		}
	}
	return false
}

var (
	refInput  = regexp.MustCompile(`\{\{\s*input\.([A-Za-z0-9_]+)\s*\}\}`)
	refOutput = regexp.MustCompile(`\{\{\s*outputs\.([A-Za-z0-9_-]+)\.`)
)

// checkGraph returns the problems with one compiled plan.
func checkGraph(g graphGolden, plan services.Plan) []string {
	var problems []string
	for _, group := range g.types {
		ok := false
		for _, t := range group {
			if hasType(plan.Nodes, t) {
				ok = true
				break
			}
		}
		if !ok {
			problems = append(problems, "no node of type "+strings.Join(group, " | "))
		}
	}

	byKey := map[string]domain.GraphNode{}
	var approvals []domain.GraphNode
	for _, n := range plan.Nodes {
		byKey[n.Key] = n
		if n.Type == "human.approval" {
			approvals = append(approvals, n)
		}
	}
	if g.approval && len(approvals) == 0 {
		problems = append(problems, "the prompt asks for approval but the graph has no human.approval")
	}
	if g.assignee != "" && len(approvals) > 0 {
		for _, a := range approvals {
			who := fmt.Sprint(a.Config["assignee"])
			if m := refInput.FindStringSubmatch(who); m != nil {
				who = fmt.Sprint(plan.DefaultInput[m[1]])
			}
			if !strings.Contains(strings.ToLower(who), strings.ToLower(g.assignee)) {
				problems = append(problems, fmt.Sprintf("approval %s is assigned to %q, want %s", a.Key, who, g.assignee))
			}
		}
	}
	// The AI step whose text an approval reviews (its config.draft) is a draft:
	// it should use brand context. Other dependencies (an analysis used in the
	// title) are not drafts.
	for _, a := range approvals {
		for _, m := range refOutput.FindAllStringSubmatch(fmt.Sprint(a.Config["draft"]), -1) {
			n := byKey[m[1]]
			if n.Type != "agent.llm" {
				continue
			}
			if b := strings.ToLower(fmt.Sprint(n.Config["brand"])); b != "true" {
				problems = append(problems, fmt.Sprintf("draft %s feeds approval %s without brand context (brand=%v)", n.Key, a.Key, n.Config["brand"]))
			}
		}
	}
	// Competitor names the prompt didn't give are invented: they filter the
	// watch step down to competitors that don't exist.
	for _, n := range plan.Nodes {
		if n.Type != "intel.watchCompetitors" {
			continue
		}
		names := fmt.Sprint(n.Config["competitors"])
		if m := refInput.FindStringSubmatch(names); m != nil {
			names = fmt.Sprint(plan.DefaultInput[m[1]])
		}
		for _, name := range strings.FieldsFunc(names, func(r rune) bool { return r == ',' || r == '[' || r == ']' }) {
			name = strings.Trim(strings.TrimSpace(name), `"`)
			if name != "" && name != "<nil>" && !strings.Contains(strings.ToLower(g.prompt), strings.ToLower(name)) {
				problems = append(problems, fmt.Sprintf("%s watches %q, a competitor the prompt never named", n.Key, name))
			}
		}
	}
	if g.schedule != "" {
		got := ""
		if plan.Schedule != nil {
			got = *plan.Schedule
		}
		if !strings.HasPrefix(got, g.schedule) {
			problems = append(problems, fmt.Sprintf("schedule %q, want %s…", got, g.schedule))
		}
	}
	return problems
}

func runGraphEval(ctx context.Context, pause time.Duration, only int, verbose bool) int {
	passed, failed, errored := 0, 0, 0
	for i, g := range graphGoldens {
		if only > 0 && i+1 != only {
			continue
		}
		if i > 0 && pause > 0 && only == 0 {
			time.Sleep(pause)
		}
		start := time.Now()
		plan, err := services.CompilePrompt(ctx, g.prompt, "", "", "")
		took := time.Since(start).Round(time.Second)
		fmt.Printf("\n[%d] %s\n", i+1, g.prompt)
		if err != nil {
			errored++
			fmt.Printf("    ERROR after %s: %v\n", took, err)
			continue
		}
		problems := checkGraph(g, plan)
		if len(problems) == 0 {
			passed++
			fmt.Printf("    PASS (%d nodes, %s)\n", len(plan.Nodes), took)
		} else {
			failed++
			fmt.Printf("    FAIL (%d nodes, %s)\n", len(plan.Nodes), took)
			for _, p := range problems {
				fmt.Println("      - " + p)
			}
		}
		if len(problems) > 0 {
			if cands, err := services.CandidateNodes(ctx, g.prompt); err == nil {
				fmt.Printf("    candidates offered to the graph writer: %s\n", strings.Join(cands, " "))
			}
		}
		if verbose || len(problems) > 0 {
			sched := ""
			if plan.Schedule != nil {
				sched = *plan.Schedule
			}
			fmt.Printf("    schedule=%q defaultInput=%v\n", sched, plan.DefaultInput)
			for _, n := range plan.Nodes {
				fmt.Printf("      %-24s %-42s deps=%v\n", n.Key, n.Type, n.DependsOn)
			}
		}
	}
	fmt.Printf("\ngraph structure: %d passed, %d failed, %d errored (of %d)\n", passed, failed, errored, passed+failed+errored)
	if failed+errored > 0 {
		return 1
	}
	return 0
}
