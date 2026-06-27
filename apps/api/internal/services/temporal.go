package services

import "context"

// TemporalStarter is the control plane's handle to the Temporal cluster: it starts
// the reasoner workflow for an engine='temporal' run. Implemented by
// platform.TemporalClient; nil when Temporal isn't configured (the reasoner's
// reconciler then provides the fallback dispatch).
type TemporalStarter interface {
	StartReasoning(ctx context.Context, runID, slug string, input map[string]any) error
}
