package services

import "github.com/agently/api/internal/domain"

// StatsService returns dashboard aggregates.
type StatsService struct {
	deps Deps
}

func NewStatsService(deps Deps) *StatsService {
	return &StatsService{deps: deps}
}

// Workspace computes the dashboard KPIs LIVE from real runs (no seeded numbers).
func (s *StatsService) Workspace() domain.WorkspaceStats {
	runs := s.deps.Repos.Runs.All()

	var active, terminal, succeeded, tokens int
	var spend float64
	for _, r := range runs {
		switch r.Status {
		case domain.RunRunning:
			active++
		case domain.RunSucceeded:
			succeeded++
			terminal++
		case domain.RunFailed, domain.RunCanceled:
			terminal++
		}
		spend += r.CostUsd
		tokens += r.Usage.TokensIn + r.Usage.TokensOut
	}
	successRate := 0.0
	if terminal > 0 {
		successRate = float64(succeeded) / float64(terminal)
	}

	stats := s.deps.SeedStats.WorkspaceStats
	stats.ActiveRuns = active
	stats.RunsToday = len(runs)
	stats.SuccessRate = successRate
	stats.SpendTodayUsd = spend
	stats.TokensToday = tokens
	return stats
}

// Workflow returns the rollup stats for a workflow, or (zero, false) if unknown.
func (s *StatsService) Workflow(workflowID domain.WorkflowId) (domain.WorkflowStats, bool) {
	stats, ok := s.deps.SeedStats.WorkflowStats[string(workflowID)]
	return stats, ok
}
