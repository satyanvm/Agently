package platform

import (
	"github.com/agently/api/internal/domain"
)

// Minimal real bootstrap. No fake runs/logs/notifications/browser sessions — the
// system starts empty and fills with REAL runs the worker executes. We seed only:
//   - one workspace + one owner member (identity the app needs to exist)
//   - the agent definitions the AI Digest workflow references
//   - the "AI Digest" workflow + its version graph (fetchers → editor)
//
// This is the line between a product demo and a product: launch the AI Digest
// workflow with real input (sources, topic, email) and the worker does real work.

const (
	// NowISO anchors any timestamp a stats read-model might need. Kept for the
	// Platform's clock default; nothing fake hangs off it anymore.
	NowISO = "2026-06-06T17:42:00Z"
)

const ws = domain.WorkspaceId("ws_agently")

// Seed bundles the data plus materialized stat read-models (now empty/zero —
// stats derive from real runs as they accumulate).
type Seed struct {
	Data           MemoryData
	WorkflowStats  map[string]domain.WorkflowStats
	WorkspaceStats domain.WorkspaceStats
}

// BuildSeed constructs the minimal real bootstrap.
func BuildSeed(_ Clock) Seed {
	workspace := domain.Workspace{
		ID: ws, Name: "My Workspace", Slug: "my-workspace", Plan: domain.PlanPro,
		DefaultRegion: "us-east-1", CreatedAt: "2026-01-01T00:00:00Z",
	}

	owner := domain.Member{
		ID: "mem_owner", WorkspaceID: ws, Name: "You", Email: "you@example.com",
		Initials: "YOU", Role: domain.MemberOwner, CreatedAt: "2026-01-01T00:00:00Z",
	}
	members := []domain.Member{owner}

	// Agent definitions referenced by the AI Digest workflow.
	agentDef := func(id, name string, role domain.AgentRole, model, desc string, tools []string) domain.AgentDefinition {
		return domain.AgentDefinition{
			ID: domain.AgentDefinitionId(id), WorkspaceID: ws, Name: name, Role: role,
			Model: model, Description: desc, Tools: tools, Config: map[string]any{},
			CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z",
		}
	}
	agents := []domain.AgentDefinition{
		agentDef("agt_arxiv", "arXiv Fetcher", domain.RoleResearcher, "gpt-4o-mini", "Pulls latest AI research papers from arXiv.", []string{"fetch.arxiv"}),
		agentDef("agt_hn", "HN Fetcher", domain.RoleResearcher, "gpt-4o-mini", "Pulls top AI/startup stories from Hacker News.", []string{"fetch.hn"}),
		agentDef("agt_reddit", "Reddit Fetcher", domain.RoleResearcher, "gpt-4o-mini", "Pulls hot posts from AI subreddits.", []string{"fetch.reddit"}),
		agentDef("agt_web", "Web Fetcher", domain.RoleBrowser, "gpt-4o-mini", "Fetches arbitrary pages (browser when needed).", []string{"browser.navigate", "fetch.web"}),
		agentDef("agt_editor", "Editor", domain.RoleWriter, "gpt-4o-mini", "Synthesizes sources into a clean digest and emails it.", []string{"write", "summarize", "email"}),
	}

	// The AI Digest workflow: four fetchers run in parallel, the Editor synthesizes
	// their findings into a digest (and triggers the email notification).
	ownerID := owner.ID
	digest := domain.Workflow{
		ID: "wf_aidigest", WorkspaceID: ws, Slug: "ai-digest", Name: "AI Digest",
		Description: "Fetches the latest AI research, AI news, and startup news from arXiv, Hacker News, Reddit and the web, then synthesizes a digest and emails it to you.",
		Trigger:     domain.TriggerManual, Schedule: nil, Tags: []string{"research", "news", "digest"},
		OwnerID: &ownerID, AgentCount: 5,
		CurrentVersionID: domain.Ptr(domain.WorkflowVersionId("wfv_aidigest_1")),
		DefaultInput: map[string]any{
			"topic":      "AI",
			"arxivQuery": "cat:cs.AI OR cat:cs.LG OR cat:cs.CL",
			"subreddits": []any{"MachineLearning", "artificial"},
			"email":      "",
		},
		CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z", ArchivedAt: nil,
	}
	workflows := []domain.Workflow{digest}

	// Version graph: 4 fetchers (no deps → parallel) → editor (depends on all 4).
	node := func(key, name string, role domain.AgentRole, model string, col, row int, deps []string) domain.GraphNode {
		return domain.GraphNode{Key: key, AgentDefinitionID: nil, Name: name, Role: role, Model: model, Col: col, Row: row, DependsOn: deps}
	}
	nodes := []domain.GraphNode{
		node("arxiv", "arXiv Fetcher", domain.RoleResearcher, "gpt-4o-mini", 0, 0, nil),
		node("hn", "HN Fetcher", domain.RoleResearcher, "gpt-4o-mini", 0, 1, nil),
		node("reddit", "Reddit Fetcher", domain.RoleResearcher, "gpt-4o-mini", 0, 2, nil),
		node("web", "Web Fetcher", domain.RoleBrowser, "gpt-4o-mini", 0, 3, nil),
		node("editor", "Editor", domain.RoleWriter, "gpt-4o-mini", 1, 1, []string{"arxiv", "hn", "reddit", "web"}),
	}
	versions := []domain.WorkflowVersion{{
		ID: "wfv_aidigest_1", WorkflowID: "wf_aidigest", Version: 1, Note: "Initial AI Digest graph",
		CreatedBy: &ownerID, CreatedAt: "2026-01-01T00:00:00Z", Nodes: nodes,
	}}

	data := MemoryData{
		Workspace: workspace,
		Members:   members,
		Agents:    agents,
		Workflows: workflows,
		Versions:  versions,
		// everything else starts empty — real runs fill it
	}

	return Seed{
		Data:          data,
		WorkflowStats: map[string]domain.WorkflowStats{},
		WorkspaceStats: domain.WorkspaceStats{
			ActiveRuns: 0, RunsToday: 0, SuccessRate: 0, SpendTodayUsd: 0, SpendBudgetUsd: 100,
			ComputeHours: 0, TokensToday: 0, RunVolume: []int{}, SpendSeries: []float64{},
		},
	}
}
