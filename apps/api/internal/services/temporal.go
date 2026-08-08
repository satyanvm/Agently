package services

import "context"

// TemporalStarter is the control plane's handle to the Temporal cluster: it starts
// the reasoner workflow for a run. Implemented by platform.TemporalClient; nil
// means the reasoner dispatcher will start queued runs from Postgres.
type TemporalStarter interface {
	StartReasoning(ctx context.Context, runID, slug string, input map[string]any) error
}
