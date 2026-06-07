package domain

// Domain events — the spine of the realtime and notification systems. Mirrors
// packages/contracts/src/events.ts. Each event carries an envelope (id,
// occurredAt, workspaceId) plus a `type` discriminator and its own payload, and
// marshals to a flat JSON object exactly like the zod discriminated union.
//
// DomainEvent is the interface every concrete event satisfies. Each event type
// is its own struct with explicit json tags, so encoding/json produces the same
// wire shape the SSE clients expect.

type DomainEvent interface {
	EventID() EventId
	EventType() string
	OccurredAt() Timestamp
	WorkspaceID() WorkspaceId
}

// EventBase is embedded by every concrete event for the shared envelope.
type EventBase struct {
	ID           EventId     `json:"id"`
	OccurredAt_  Timestamp   `json:"occurredAt"`
	WorkspaceID_ WorkspaceId `json:"workspaceId"`
}

func (b EventBase) EventID() EventId         { return b.ID }
func (b EventBase) OccurredAt() Timestamp    { return b.OccurredAt_ }
func (b EventBase) WorkspaceID() WorkspaceId { return b.WorkspaceID_ }

type RunQueuedEvent struct {
	EventBase
	Type       string     `json:"type"`
	RunID      RunId      `json:"runId"`
	WorkflowID WorkflowId `json:"workflowId"`
}

func (RunQueuedEvent) EventType() string { return "run.queued" }

type RunStartedEvent struct {
	EventBase
	Type  string `json:"type"`
	RunID RunId  `json:"runId"`
}

func (RunStartedEvent) EventType() string { return "run.started" }

type RunProgressEvent struct {
	EventBase
	Type        string `json:"type"`
	RunID       RunId  `json:"runId"`
	StepsDone   int    `json:"stepsDone"`
	StepsTotal  int    `json:"stepsTotal"`
	CurrentStep string `json:"currentStep"`
}

func (RunProgressEvent) EventType() string { return "run.progress" }

type RunAgentTransitionedEvent struct {
	EventBase
	Type    string      `json:"type"`
	RunID   RunId       `json:"runId"`
	AgentID RunAgentId  `json:"agentId"`
	From    AgentStatus `json:"from"`
	To      AgentStatus `json:"to"`
}

func (RunAgentTransitionedEvent) EventType() string { return "run.agent.transitioned" }

type RunLogAppendedEvent struct {
	EventBase
	Type  string   `json:"type"`
	RunID RunId    `json:"runId"`
	Log   LogEntry `json:"log"`
}

func (RunLogAppendedEvent) EventType() string { return "run.log.appended" }

type RunBrowserActionEvent struct {
	EventBase
	Type      string           `json:"type"`
	RunID     RunId            `json:"runId"`
	SessionID BrowserSessionId `json:"sessionId"`
	Action    BrowserAction    `json:"action"`
}

func (RunBrowserActionEvent) EventType() string { return "run.browser.action" }

type RunArtifactProducedEvent struct {
	EventBase
	Type       string     `json:"type"`
	RunID      RunId      `json:"runId"`
	ArtifactID ArtifactId `json:"artifactId"`
}

func (RunArtifactProducedEvent) EventType() string { return "run.artifact.produced" }

type RunCostThresholdEvent struct {
	EventBase
	Type         string  `json:"type"`
	RunID        RunId   `json:"runId"`
	CostUsd      float64 `json:"costUsd"`
	ThresholdUsd float64 `json:"thresholdUsd"`
}

func (RunCostThresholdEvent) EventType() string { return "run.cost.threshold" }

type RunFinishedEvent struct {
	EventBase
	Type   string    `json:"type"`
	RunID  RunId     `json:"runId"`
	Status RunStatus `json:"status"`
	Error  *string   `json:"error"`
}

func (RunFinishedEvent) EventType() string { return "run.finished" }

type NotificationCreatedEvent struct {
	EventBase
	Type           string         `json:"type"`
	NotificationID NotificationId `json:"notificationId"`
}

func (NotificationCreatedEvent) EventType() string { return "notification.created" }
