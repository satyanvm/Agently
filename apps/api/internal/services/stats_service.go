package services

import "github.com/agently/api/internal/domain"

// StatsService returns dashboard aggregates.
type StatsService struct {
	deps Deps
}

func NewStatsService(deps Deps) *StatsService {
	return &StatsService{deps: deps}
}

// Workspace recomputes the volatile counters live and keeps the seeded series.
func (s *StatsService) Workspace() domain.WorkspaceStats {
	stats := s.deps.SeedStats.WorkspaceStats
	activeRuns := 0
	for _, r := range s.deps.Repos.Runs.All() {
		if r.Status == domain.RunRunning {
			activeRuns++
		}
	}
	stats.ActiveRuns = activeRuns
	return stats
}

// Workflow returns the rollup stats for a workflow, or (zero, false) if unknown.
func (s *StatsService) Workflow(workflowID domain.WorkflowId) (domain.WorkflowStats, bool) {
	stats, ok := s.deps.SeedStats.WorkflowStats[string(workflowID)]
	return stats, ok
}
