package queue

import (
	"context"
	"encoding/json"
	"fmt"
)

// graph.go is the worker's view of a run's AGENT GRAPH — the multi-agent DAG that
// the control plane materialized into run_agents rows at launch (each row carries
// its resolved depends_on as run-agent ids). The worker reads this graph and
// executes it, so the DAG, like everything else, lives in Postgres, not memory —
// which is what makes a multi-agent run durably resumable after a crash.

// GraphAgent is one node of the run's agent DAG.
type GraphAgent struct {
	ID        string
	Name      string
	Role      string
	Model     string
	Status    string   // idle | running | succeeded | failed | blocked | waiting
	DependsOn []string // run-agent ids this node waits for
}

// LoadAgents returns all agents for a run. Empty slice ⇒ a single-agent run with
// no materialized graph (older/native runs); the caller falls back to the linear
// plan in that case.
func (q *Queue) LoadAgents(ctx context.Context, runID string) ([]GraphAgent, error) {
	rows, err := q.pool.Query(ctx,
		`select id, name, role, model, status, depends_on
		   from run_agents where run_id=$1 order by col, "row"`, runID)
	if err != nil {
		return nil, fmt.Errorf("load agents: %w", err)
	}
	defer rows.Close()
	var out []GraphAgent
	for rows.Next() {
		var g GraphAgent
		var deps []string
		if err := rows.Scan(&g.ID, &g.Name, &g.Role, &g.Model, &g.Status, &deps); err != nil {
			return nil, fmt.Errorf("scan agent: %w", err)
		}
		g.DependsOn = deps
		out = append(out, g)
	}
	return out, nil
}

// SetAgentStatus updates one agent's status (and stamps started/finished time as
// appropriate). Scoped to the owning run+worker so a lost-lease worker can't mutate.
func (q *Queue) SetAgentStatus(ctx context.Context, runID, workerID, agentID, status string) error {
	var stamp string
	switch status {
	case "running":
		stamp = ", started_at = coalesce(started_at, now())"
	case "succeeded", "failed":
		stamp = ", finished_at = now()"
	}
	sql := `update run_agents set status = $3` + stamp +
		` where id = $1 and run_id = $2
		    and exists (select 1 from runs r where r.id = $2 and r.claimed_by = $4)`
	tag, err := q.pool.Exec(ctx, sql, agentID, runID, status, workerID)
	if err != nil {
		return fmt.Errorf("set agent status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrLeaseLost
	}
	return nil
}

// SetAgentResult writes an agent's summary + metrics (tokens/cost/progress) when
// it completes. metrics is the run_agents.metrics jsonb the UI already understands.
func (q *Queue) SetAgentResult(ctx context.Context, agentID, summary string, tokens int, costUSD, progress float64, toolCalls, runtimeMs int) error {
	metrics, _ := json.Marshal(map[string]any{
		"tokens":    tokens,
		"costUsd":   costUSD,
		"runtimeMs": runtimeMs,
		"toolCalls": toolCalls,
		"progress":  progress,
	})
	_, err := q.pool.Exec(ctx,
		`update run_agents set summary = $2, metrics = $3 where id = $1`,
		agentID, summary, string(metrics))
	if err != nil {
		return fmt.Errorf("set agent result: %w", err)
	}
	return nil
}

// AddMessage records an inter-agent hand-off (from → to, with a short label) so
// the run's communication feed is real. Best-effort: a failure is non-fatal.
func (q *Queue) AddMessage(ctx context.Context, runID, fromAgentID, toAgentID, label string) error {
	id := genID("msg")
	_, err := q.pool.Exec(ctx,
		`insert into agent_messages (id, run_id, from_agent_id, to_agent_id, label, at)
		 values ($1,$2,$3,$4,$5, now())`,
		id, runID, fromAgentID, toAgentID, label)
	if err != nil {
		return fmt.Errorf("add message: %w", err)
	}
	return nil
}
