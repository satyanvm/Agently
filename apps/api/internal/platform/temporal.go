package platform

import (
	"context"
	"fmt"
	"os"

	"go.temporal.io/sdk/client"
)

// TemporalClient starts reasoning workflows on the Temporal cluster. It is the
// Temporal-native dispatch path: the moment a run is requested, the control plane
// hands it to Temporal, which owns durability/retries/resume from then on. (No
// Postgres polling sits between the API and Temporal.)
//
// It is optional: when TEMPORAL_HOSTPORT is unset the API runs without it and
// queued runs are started by the reasoner's dispatcher. The dispatcher also covers
// the rare case where a direct start fails after the run row is written.
type TemporalClient struct {
	c         client.Client
	taskQueue string
	log       Logger
}

// ReasoningWorkflowType is the workflow type name registered by the Python reasoner
// (the @workflow.defn class name). Must stay in sync with apps/reasoner.
const ReasoningWorkflowType = "ReasoningWorkflow"

// NewTemporalClient dials Temporal using TEMPORAL_HOSTPORT / TEMPORAL_NAMESPACE /
// TEMPORAL_TASK_QUEUE. Returns (nil, nil) when TEMPORAL_HOSTPORT is unset so the
// caller can run without Temporal. A non-nil error means it was configured but the
// dial failed.
func NewTemporalClient(log Logger) (*TemporalClient, error) {
	hostPort := os.Getenv("TEMPORAL_HOSTPORT")
	if hostPort == "" {
		return nil, nil
	}
	namespace := os.Getenv("TEMPORAL_NAMESPACE")
	if namespace == "" {
		namespace = "default"
	}
	taskQueue := os.Getenv("TEMPORAL_TASK_QUEUE")
	if taskQueue == "" {
		taskQueue = "agently-reasoner"
	}
	c, err := client.Dial(client.Options{HostPort: hostPort, Namespace: namespace})
	if err != nil {
		return nil, fmt.Errorf("temporal dial %s: %w", hostPort, err)
	}
	log.Info("connected to Temporal", "hostPort", hostPort, "namespace", namespace, "taskQueue", taskQueue)
	return &TemporalClient{c: c, taskQueue: taskQueue, log: log}, nil
}

// StartReasoning starts the reasoner workflow for a run. The workflow id is derived
// from the run id so a duplicate start (a retry, or the reconciler racing us) is
// rejected by Temporal rather than double-executing — the run runs exactly once.
func (t *TemporalClient) StartReasoning(ctx context.Context, runID, slug string, input map[string]any) error {
	if input == nil {
		input = map[string]any{}
	}
	params := map[string]any{"run_id": runID, "slug": slug, "input": input}
	opts := client.StartWorkflowOptions{
		ID:        "agently-run-" + runID,
		TaskQueue: t.taskQueue,
	}
	_, err := t.c.ExecuteWorkflow(ctx, opts, ReasoningWorkflowType, params)
	if err != nil {
		return fmt.Errorf("start reasoning workflow for run %s: %w", runID, err)
	}
	return nil
}

// Close releases the Temporal client connection.
func (t *TemporalClient) Close() {
	if t != nil && t.c != nil {
		t.c.Close()
	}
}
