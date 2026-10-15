package services

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"
)

// fakeSectionModel writes 80 nodes over six sections: a trigger, 13 agent.llm
// nodes per section chained to the section before, and a closing report. Its
// first try at section 3 references an output agent.llm does not have, so the
// per-section repair loop has to catch it.
func fakeSectionModel(t *testing.T, calls *int) func(context.Context, string, int, time.Duration, string, []llmMsg) (string, error) {
	reSection := regexp.MustCompile(`Write section (\d+) of (\d+)`)
	return func(_ context.Context, _ string, _ int, _ time.Duration, _ string, msgs []llmMsg) (string, error) {
		*calls++
		m := reSection.FindStringSubmatch(msgs[0].Content)
		if m == nil {
			t.Fatalf("unexpected reduce request: %.200s", msgs[0].Content)
		}
		var sec, total int
		fmt.Sscan(m[1], &sec)
		fmt.Sscan(m[2], &total)
		repairing := len(msgs) > 1

		type node struct {
			Key       string         `json:"key"`
			Type      string         `json:"type"`
			Name      string         `json:"name"`
			DependsOn []string       `json:"dependsOn"`
			Config    map[string]any `json:"config"`
		}
		var nodes []node
		prev := fmt.Sprintf("s%d_n13", sec-1)
		if sec == 1 {
			nodes = append(nodes, node{Key: "start", Type: "trigger.manual", Name: "Start", Config: map[string]any{}})
			prev = "start"
		}
		for i := 1; i <= 13; i++ {
			key := fmt.Sprintf("s%d_n%d", sec, i)
			field := "text"
			if prev == "start" {
				field = "input"
			}
			if sec == 3 && i == 1 && !repairing {
				field = "summary" // agent.llm exposes text and model only
			}
			nodes = append(nodes, node{
				Key: key, Type: "agent.llm", Name: key, DependsOn: []string{prev},
				Config: map[string]any{"prompt": "Continue from {{outputs." + prev + "." + field + "}} about {{input.topic}}"},
			})
			prev = key
		}
		if sec == total {
			nodes = append(nodes, node{Key: "report", Type: "output.report", Name: "Report",
				DependsOn: []string{prev}, Config: map[string]any{"title": "Done"}})
		}
		b, _ := json.Marshal(map[string]any{"nodes": nodes})
		return string(b), nil
	}
}

func TestAuthorGraphSections_Builds80NodeGraph(t *testing.T) {
	calls := 0
	orig := reduceLLM
	reduceLLM = fakeSectionModel(t, &calls)
	t.Cleanup(func() { reduceLLM = orig })

	var outline graphOutline
	outline.Name = "Big"
	outline.EstimatedNodes = 80
	outline.DefaultInput = map[string]any{"topic": "AI"}
	for i := 1; i <= 6; i++ {
		outline.Sections = append(outline.Sections, struct {
			Key   string `json:"key"`
			Goal  string `json:"goal"`
			Nodes int    `json:"nodes"`
		}{Key: fmt.Sprintf("part_%d", i), Goal: "do part", Nodes: 13})
	}
	if !outline.sectioned() {
		t.Fatal("an 80-node, 6-section outline must be written section by section")
	}

	p := basePlan("research things", "")
	nodes, err := authorGraphSections(context.Background(), LoadCatalog(), "system", "research things", outline, &p)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 80 {
		t.Fatalf("got %d nodes, want 80", len(nodes))
	}
	if calls != 7 { // six sections plus one repair of section 3
		t.Fatalf("reduce calls = %d, want 7", calls)
	}
	if errs := graphProblems(nodes, LoadCatalog(), map[string]bool{"topic": true}); len(errs) > 0 {
		t.Fatalf("assembled graph is invalid: %v", errs)
	}
	if p.Name != "Big" {
		t.Fatalf("outline metadata not applied: name %q", p.Name)
	}
}

func TestSectionProblems_TriggerPlacement(t *testing.T) {
	trig := toGraphNodes(reduceGraphSpec{Nodes: []struct {
		Key       string         `json:"key"`
		Type      string         `json:"type"`
		Name      string         `json:"name"`
		DependsOn []string       `json:"dependsOn"`
		Config    map[string]any `json:"config"`
	}{{Key: "t", Type: "trigger.manual"}}})
	if errs := sectionProblems(trig, 1, 3); len(errs) == 0 || !strings.Contains(errs[0], "first section") {
		t.Fatalf("a trigger in a later section must be rejected, got %v", errs)
	}
	if errs := sectionProblems(nil, 0, 3); len(errs) == 0 {
		t.Fatal("an empty section must be rejected")
	}
}

func TestOutlineSectioned_SmallGraphStaysSingleShot(t *testing.T) {
	var o graphOutline
	o.EstimatedNodes = 12
	o.Sections = make([]struct {
		Key   string `json:"key"`
		Goal  string `json:"goal"`
		Nodes int    `json:"nodes"`
	}, 2)
	if o.sectioned() {
		t.Fatal("a 12-node outline should be written in one call")
	}
}
