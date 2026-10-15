package services

// planner_sections.go — writing large graphs section by section.
//
// One reduce call has to fit the whole graph JSON in its output budget, which
// caps a single-shot graph at roughly 30 nodes. So the reduce model first writes
// a short OUTLINE (plan metadata plus ordered sections, each with a goal and a
// rough node count) — sized by the graph it needs, not by the prompt's length.
// Up to sectionThreshold nodes the graph is then written in one call, as
// before. Above it the planner writes ONE SECTION PER CALL,
// seeing the nodes already written so it can depend on and reference them.
// Each section is validated against the accumulated graph and repaired on its
// own, so a mistake in section 5 never re-emits sections 1–4. A graph the model
// misjudged as small and that overflows the output budget or times out is
// re-planned in sections.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/agently/api/internal/domain"
)

// reduceLLM is the reduce-model call; tests replace it.
var reduceLLM = withQuotaFallback(planLLMWith)

type llmCall = func(ctx context.Context, model string, maxTokens int, timeout time.Duration, system string, msgs []llmMsg) (string, error)

// withQuotaFallback retries a call on PLANNER_FALLBACK_MODEL when the requested
// model's provider quota is used up for the day.
func withQuotaFallback(call llmCall) llmCall {
	return func(ctx context.Context, model string, maxTokens int, timeout time.Duration, system string, msgs []llmMsg) (string, error) {
		out, err := call(ctx, model, maxTokens, timeout, system, msgs)
		fallback := strings.TrimSpace(os.Getenv("PLANNER_FALLBACK_MODEL"))
		if errors.Is(err, errQuotaExhausted) && fallback != "" && fallback != model {
			return call(ctx, fallback, maxTokens, timeout, system, msgs)
		}
		return out, err
	}
}

func envInt(name string, def int) int {
	if v, err := strconv.Atoi(strings.TrimSpace(os.Getenv(name))); err == nil && v > 0 {
		return v
	}
	return def
}

// reduceMaxTokens / reduceTimeout bound one reduce call (whole graph or one section).
func reduceMaxTokens() int { return envInt("PLANNER_REDUCE_MAX_TOKENS", 32000) }
func reduceTimeout() time.Duration {
	return time.Duration(envInt("PLANNER_REDUCE_TIMEOUT_S", 180)) * time.Second
}

// errOutputCapped marks a model response cut off by its output-token budget;
// errTimedOut one that ran past its timeout.
var (
	errOutputCapped = errors.New("output token cap reached")
	errTimedOut     = errors.New("deadline exceeded")
)

// sectionThreshold is the estimated node count above which the graph is
// written section by section.
func sectionThreshold() int { return envInt("PLANNER_SECTION_THRESHOLD", 25) }

type graphOutline struct {
	Name           string         `json:"name"`
	Description    string         `json:"description"`
	Schedule       string         `json:"schedule"`
	DefaultInput   map[string]any `json:"defaultInput"`
	EstimatedNodes int            `json:"estimatedNodes"`
	Sections       []struct {
		Key   string `json:"key"`
		Goal  string `json:"goal"`
		Nodes int    `json:"nodes"`
	} `json:"sections"`
}

func (o graphOutline) sectioned() bool {
	return len(o.Sections) >= 2 && o.EstimatedNodes > sectionThreshold()
}

const outlineInstructions = `

OUTLINE MODE — for THIS call do not write any nodes. Return ONE JSON object:
{
  "name": "short title",
  "description": "one sentence",
  "schedule": "daily HH:MM" | "hourly" | "",
  "defaultInput": { "topic": "...", ... },
  "estimatedNodes": <how many nodes the complete graph needs>,
  "sections": [ { "key": "snake_case", "goal": "what this part of the graph does", "nodes": <count> } ]
}
Sections are written in order, one per later call, and a section may only use
nodes from earlier sections. The first section starts with the trigger; the last
ends with the output node(s). Aim for 8–15 nodes per section. defaultInput lists
every run-input field any section will reference as {{input.x}}.`

// outlineGraph asks the reduce model for the plan metadata and section list.
// forceSplit is set after a single-call attempt proved too large.
func outlineGraph(ctx context.Context, system, prompt string, forceSplit bool) (graphOutline, error) {
	instructions := outlineInstructions
	if forceSplit {
		instructions += "\nThis graph proved too large to write in one call: split it into at least 3 sections."
	}
	msgs := []llmMsg{{Role: "user", Content: prompt}}
	var out graphOutline
	for attempt := 0; attempt < 2; attempt++ {
		raw, err := reduceLLM(ctx, reduceModel(), 4096, reduceTimeout(), system+instructions, msgs)
		if err != nil {
			return out, fmt.Errorf("outline phase: %w", err)
		}
		if err := json.Unmarshal([]byte(extractJSON(raw)), &out); err == nil {
			if forceSplit && len(out.Sections) < 2 {
				return out, fmt.Errorf("outline phase: %s could not split an oversized graph into sections", reduceModel())
			}
			return out, nil
		} else {
			msgs = append(msgs,
				llmMsg{Role: "assistant", Content: raw},
				llmMsg{Role: "user", Content: "That was not a single valid JSON object (" + err.Error() + "). Return the corrected outline JSON and nothing else."},
			)
		}
	}
	return out, fmt.Errorf("outline phase: %s did not return a valid outline JSON object", reduceModel())
}

const sectionInstructions = `

SECTION MODE — the graph is being written one section at a time. For THIS call
return ONE JSON object {"nodes": [ ...nodes of THIS section only... ]}, using the
node format above. Do not repeat nodes that already exist; every key must be new.
New nodes may dependOn existing keys and reference their outputs. Only the first
section contains a trigger node; only the last section needs output node(s).`

// authorGraphSections writes the outline's sections in order, validating each
// against everything written before it.
func authorGraphSections(ctx context.Context, cat *Catalog, system, prompt string, outline graphOutline, p *Plan) ([]domain.GraphNode, error) {
	p.DefaultInput = mergeInput(p.DefaultInput, outline.DefaultInput)
	inputKeys := map[string]bool{}
	for k := range p.DefaultInput {
		inputKeys[k] = true
	}

	var graph []domain.GraphNode
	for i := range outline.Sections {
		nodes, err := authorSection(ctx, cat, system, prompt, outline, i, graph, inputKeys)
		if err != nil {
			return nil, err
		}
		graph = append(graph, nodes...)
	}
	overlayMeta(reduceGraphSpec{Name: outline.Name, Description: outline.Description, Schedule: outline.Schedule}, p)
	return graph, nil
}

func authorSection(ctx context.Context, cat *Catalog, system, prompt string, outline graphOutline, idx int, prior []domain.GraphNode, inputKeys map[string]bool) ([]domain.GraphNode, error) {
	sec := outline.Sections[idx]
	msgs := []llmMsg{{Role: "user", Content: sectionRequest(cat, prompt, outline, idx, prior, inputKeys)}}
	const attempts = 3 // 1 author + 2 repairs
	var lastComplaint string
	timeouts := 0
	for i := 0; i < attempts; i++ {
		raw, err := reduceLLM(ctx, reduceModel(), reduceMaxTokens(), reduceTimeout(), system+sectionInstructions, msgs)
		if errors.Is(err, errTimedOut) && timeouts < 1 && ctx.Err() == nil {
			// One slow response is not a verdict on the section: ask again
			// without spending a repair attempt.
			timeouts++
			i--
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("reduce phase (section %q): %w", sec.Key, err)
		}
		var spec reduceGraphSpec
		if err := json.Unmarshal([]byte(extractJSON(raw)), &spec); err != nil {
			lastComplaint = "not a single valid JSON object: " + err.Error()
			msgs = append(msgs,
				llmMsg{Role: "assistant", Content: raw},
				llmMsg{Role: "user", Content: "That was not a single valid JSON object (" + err.Error() + "). Return the corrected {\"nodes\": [...]} object and nothing else."},
			)
			continue
		}
		nodes := toGraphNodes(spec)
		problems := sectionProblems(nodes, idx, len(outline.Sections))
		if len(problems) == 0 {
			problems = graphProblems(append(append([]domain.GraphNode{}, prior...), nodes...), cat, inputKeys)
		}
		if len(problems) > 0 {
			lastComplaint = strings.Join(problems, "; ")
			msgs = append(msgs,
				llmMsg{Role: "assistant", Content: raw},
				llmMsg{Role: "user", Content: "This section failed validation:\n- " + strings.Join(problems, "\n- ") + "\nReturn the corrected {\"nodes\": [...]} object for this section and nothing else."},
			)
			continue
		}
		return nodes, nil
	}
	return nil, fmt.Errorf("reduce phase: %s could not author section %q in %d attempts; last problem: %s",
		reduceModel(), sec.Key, attempts, lastComplaint)
}

// sectionProblems checks the rules that only hold per section: the trigger
// lives in the first section, and a section is never empty.
func sectionProblems(nodes []domain.GraphNode, idx, total int) []string {
	if len(nodes) == 0 {
		return []string{"the section has no nodes"}
	}
	triggers := 0
	for _, n := range nodes {
		if strings.HasPrefix(n.Type, "trigger.") {
			triggers++
		}
	}
	switch {
	case idx == 0 && triggers != 1:
		return []string{fmt.Sprintf("the first section must contain exactly one trigger node, found %d", triggers)}
	case idx > 0 && triggers > 0:
		return []string{"only the first section may contain a trigger node; the graph already has one"}
	}
	return nil
}

// sectionRequest is the user message for one section: the request, the
// outline, and every node written so far with the outputs it exposes.
func sectionRequest(cat *Catalog, prompt string, outline graphOutline, idx int, prior []domain.GraphNode, inputKeys map[string]bool) string {
	var b strings.Builder
	b.WriteString("Full request:\n" + prompt + "\n\nWorkflow outline:\n")
	for i, s := range outline.Sections {
		marker := ""
		if i == idx {
			marker = "  <-- WRITE THIS SECTION NOW"
		}
		fmt.Fprintf(&b, "%d. %s — %s (~%d nodes)%s\n", i+1, s.Key, s.Goal, s.Nodes, marker)
	}
	if len(inputKeys) > 0 {
		keys := make([]string, 0, len(inputKeys))
		for k := range inputKeys {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		b.WriteString("\nRun-input fields available as {{input.<field>}}: " + strings.Join(keys, ", ") + "\n")
	}
	if len(prior) == 0 {
		b.WriteString("\nNo nodes exist yet. Start this section with the trigger node.\n")
	} else {
		b.WriteString("\nNodes already written (depend on and reference these by key):\n")
		for _, n := range prior {
			outs := "result"
			if c, ok := cat.ByID[n.Type]; ok && len(c.Outputs) > 0 {
				outs = strings.Join(c.Outputs, ", ")
			}
			fmt.Fprintf(&b, "- %s (%s) %q outputs: %s\n", n.Key, n.Type, n.Name, outs)
		}
	}
	sec := outline.Sections[idx]
	fmt.Fprintf(&b, "\nWrite section %d of %d now: %s — %s.", idx+1, len(outline.Sections), sec.Key, sec.Goal)
	if idx == len(outline.Sections)-1 {
		b.WriteString(" This is the last section: end the graph with the output node(s).")
	}
	return b.String()
}
