package services

// node_data.go backs the builder's debugging loop (n8n's "execute step" +
// data panel):
//
//   - NodeData returns each graph node's most recent output/error across the
//     workflow's recent runs, for the inspector's Input/Output tabs and for
//     drag-to-map.
//   - stepExec turns a "Run step" request into the reserved run-input key
//     `__exec` the reasoner reads (workflow.py): run only the target node and
//     its ancestors, reuse the ancestors' last successful outputs instead of
//     re-running them, and/or serve pinned nodes their pinned data.
//
// `__exec` is stripped by the reasoner's dispatcher before the input reaches
// any template, so {{input.*}} never sees it.

import (
	"github.com/agently/api/internal/domain"
	"github.com/agently/api/internal/domain/validate"
)

// ExecKey is the reserved run-input key carrying test-run options.
const ExecKey = "__exec"

// NodeSample is one node's latest recorded execution.
type NodeSample struct {
	NodeKey    string             `json:"nodeKey"`
	Status     domain.AgentStatus `json:"status"`
	Output     map[string]any     `json:"output,omitempty"`
	Error      string             `json:"error,omitempty"`
	Attempts   int                `json:"attempts,omitempty"`
	RunID      domain.RunId       `json:"runId"`
	RunNumber  int                `json:"runNumber"`
	FinishedAt *domain.Timestamp  `json:"finishedAt"`
}

// nodeDataRuns bounds how far back NodeData looks.
const nodeDataRuns = 25

// NodeData returns node key → latest sample (newest run first wins).
func (s *RunService) NodeData(slug string) (map[string]NodeSample, error) {
	wf, ok := s.deps.Repos.Workflows.GetBySlug(slug)
	if !ok {
		return nil, domain.NotFound("workflow")
	}
	return s.latestSamples(wf.ID, false), nil
}

// latestSamples walks recent runs newest first. succeededOnly keeps only
// samples whose output can stand in for re-running the node.
func (s *RunService) latestSamples(workflowID domain.WorkflowId, succeededOnly bool) map[string]NodeSample {
	out := map[string]NodeSample{}
	runs := s.deps.Repos.Runs.ListByWorkflow(workflowID) // number desc
	if len(runs) > nodeDataRuns {
		runs = runs[:nodeDataRuns]
	}
	for _, run := range runs {
		for _, a := range s.deps.Repos.RunAgents.ListByRun(run.ID) {
			if a.NodeKey == "" {
				continue
			}
			if _, seen := out[a.NodeKey]; seen {
				continue
			}
			ran := a.Status == domain.AgentSucceeded || a.Status == domain.AgentFailed
			if !ran || (succeededOnly && (a.Status != domain.AgentSucceeded || a.Output == nil)) {
				continue
			}
			out[a.NodeKey] = NodeSample{
				NodeKey: a.NodeKey, Status: a.Status, Output: a.Output, Error: a.Error,
				Attempts: a.Attempts, RunID: run.ID, RunNumber: run.Number, FinishedAt: a.FinishedAt,
			}
		}
	}
	return out
}

// stepExec builds the `__exec` value for a builder test run, or nil.
func (s *RunService) stepExec(wf domain.Workflow, in validate.LaunchRunInput) (map[string]any, error) {
	if in.TargetNode == "" && !in.UsePins {
		return nil, nil
	}
	exec := map[string]any{"usePins": in.UsePins}
	if in.TargetNode == "" {
		return exec, nil
	}
	var nodes []domain.GraphNode
	if wf.CurrentVersionID != nil {
		if v, ok := s.deps.Repos.Versions.GetByID(*wf.CurrentVersionID); ok {
			nodes = v.Nodes
		}
	}
	ancestors, ok := ancestorsOf(nodes, in.TargetNode)
	if !ok {
		return nil, domain.BadRequest("node '" + in.TargetNode + "' is not in this workflow — save the graph first")
	}
	samples := s.latestSamples(wf.ID, true)
	seed := map[string]any{}
	for _, k := range ancestors {
		if smp, ok := samples[k]; ok {
			seed[k] = smp.Output
		}
	}
	exec["targetNode"] = in.TargetNode
	exec["seed"] = seed
	return exec, nil
}

// ancestorsOf returns every node target depends on, transitively.
func ancestorsOf(nodes []domain.GraphNode, target string) ([]string, bool) {
	byKey := map[string]domain.GraphNode{}
	for _, n := range nodes {
		byKey[n.Key] = n
	}
	if _, ok := byKey[target]; !ok {
		return nil, false
	}
	seen := map[string]bool{}
	var out []string
	var walk func(string)
	walk = func(k string) {
		for _, d := range byKey[k].DependsOn {
			if !seen[d] {
				seen[d] = true
				out = append(out, d)
				walk(d)
			}
		}
	}
	walk(target)
	return out, true
}
