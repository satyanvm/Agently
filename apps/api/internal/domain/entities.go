package domain

// The canonical domain model. Each struct is the single source of truth for an
// entity's shape, mirroring packages/contracts/src/entities.ts 1:1. Nullable
// fields (.nullable() in the zod schemas) are pointer types so they marshal as
// JSON null; optional config maps use the zero value (an empty map).

/* ============================ Tenancy ============================ */

type Workspace struct {
	ID            WorkspaceId   `json:"id"`
	Name          string        `json:"name"`
	Slug          string        `json:"slug"`
	Plan          WorkspacePlan `json:"plan"`
	DefaultRegion string        `json:"defaultRegion"`
	CreatedAt     Timestamp     `json:"createdAt"`
}

type Member struct {
	ID          MemberId    `json:"id"`
	WorkspaceID WorkspaceId `json:"workspaceId"`
	Name        string      `json:"name"`
	Email       string      `json:"email"`
	Initials    string      `json:"initials"`
	Role        MemberRole  `json:"role"`
	CreatedAt   Timestamp   `json:"createdAt"`
}

type ApiKey struct {
	ID          ApiKeyId    `json:"id"`
	WorkspaceID WorkspaceId `json:"workspaceId"`
	Name        string      `json:"name"`
	MaskedKey   string      `json:"maskedKey"`
	Scopes      []string    `json:"scopes"`
	LastUsedAt  *Timestamp  `json:"lastUsedAt"`
	CreatedAt   Timestamp   `json:"createdAt"`
}

/* ======================= Agent definitions ====================== */

type AgentDefinition struct {
	ID          AgentDefinitionId `json:"id"`
	WorkspaceID WorkspaceId       `json:"workspaceId"`
	Name        string            `json:"name"`
	Role        AgentRole         `json:"role"`
	Model       string            `json:"model"`
	Description string            `json:"description"`
	Tools       []string          `json:"tools"`
	Config      map[string]any    `json:"config"`
	CreatedAt   Timestamp         `json:"createdAt"`
	UpdatedAt   Timestamp         `json:"updatedAt"`
}

/* ========================== Workflows =========================== */

type GraphNode struct {
	Key               string             `json:"key"`
	AgentDefinitionID *AgentDefinitionId `json:"agentDefinitionId"`
	Name              string             `json:"name"`
	Role              AgentRole          `json:"role"`
	Model             string             `json:"model"`
	Col               int                `json:"col"`
	Row               int                `json:"row"`
	DependsOn         []string           `json:"dependsOn"`
	// Type is the builder palette's catalog id (e.g. "agent.llm", "tool.http").
	// Empty on graphs compiled from a prompt; treated as "agent.llm" downstream.
	Type string `json:"type"`
	// Config is per-node configuration whose shape depends on Type. Open by design.
	Config map[string]any `json:"config"`
}

type WorkflowVersion struct {
	ID         WorkflowVersionId `json:"id"`
	WorkflowID WorkflowId        `json:"workflowId"`
	Version    int               `json:"version"`
	Nodes      []GraphNode       `json:"nodes"`
	Note       string            `json:"note,omitempty"`
	CreatedBy  *MemberId         `json:"createdBy"`
	CreatedAt  Timestamp         `json:"createdAt"`
}

type Workflow struct {
	ID               WorkflowId         `json:"id"`
	WorkspaceID      WorkspaceId        `json:"workspaceId"`
	Slug             string             `json:"slug"`
	Name             string             `json:"name"`
	Description      string             `json:"description"`
	Trigger          TriggerType        `json:"trigger"`
	Schedule         *string            `json:"schedule"`
	Tags             []string           `json:"tags"`
	OwnerID          *MemberId          `json:"ownerId"`
	AgentCount       int                `json:"agentCount"`
	CurrentVersionID *WorkflowVersionId `json:"currentVersionId"`
	DefaultInput     map[string]any     `json:"defaultInput"`
	CreatedAt        Timestamp          `json:"createdAt"`
	UpdatedAt        Timestamp          `json:"updatedAt"`
	ArchivedAt       *Timestamp         `json:"archivedAt"`
}

type WorkflowStats struct {
	SuccessRate  float64   `json:"successRate"`
	AvgRuntimeMs int       `json:"avgRuntimeMs"`
	AvgCostUsd   float64   `json:"avgCostUsd"`
	TotalRuns    int       `json:"totalRuns"`
	Recent       []int     `json:"recent"` // 0|1 per run
	Trend        []float64 `json:"trend"`
}

// WorkflowSummary is the read model the workflows list/detail return: workflow
// plus derived view fields.
type WorkflowSummary struct {
	Workflow
	Status    RunStatus     `json:"status"`
	Owner     *Principal    `json:"owner"`
	LastRunAt *Timestamp    `json:"lastRunAt"`
	Stats     WorkflowStats `json:"stats"`
}

/* ============================= Runs ============================= */

type RunAgent struct {
	ID                RunAgentId         `json:"id"`
	RunID             RunId              `json:"runId"`
	AgentDefinitionID *AgentDefinitionId `json:"agentDefinitionId"`
	Name              string             `json:"name"`
	Role              AgentRole          `json:"role"`
	Model             string             `json:"model"`
	Status            AgentStatus        `json:"status"`
	DependsOn         []RunAgentId       `json:"dependsOn"`
	Col               int                `json:"col"`
	Row               int                `json:"row"`
	Summary           string             `json:"summary"`
	Metrics           AgentMetrics       `json:"metrics"`
	StartedAt         *Timestamp         `json:"startedAt"`
	FinishedAt        *Timestamp         `json:"finishedAt"`
}

type AgentMessage struct {
	ID          AgentMessageId `json:"id"`
	RunID       RunId          `json:"runId"`
	FromAgentID RunAgentId     `json:"fromAgentId"`
	ToAgentID   RunAgentId     `json:"toAgentId"`
	Label       string         `json:"label"`
	At          Timestamp      `json:"at"`
}

type Artifact struct {
	ID                ArtifactId   `json:"id"`
	RunID             RunId        `json:"runId"`
	Name              string       `json:"name"`
	Kind              ArtifactKind `json:"kind"`
	SizeBytes         *int         `json:"sizeBytes"`
	ProducedByAgentID *RunAgentId  `json:"producedByAgentId"`
	ProducedByName    string       `json:"producedByName"`
	StorageKey        *string      `json:"storageKey"`
	Preview           *string      `json:"preview"`
	CreatedAt         Timestamp    `json:"createdAt"`
}

type Run struct {
	ID                RunId              `json:"id"`
	WorkspaceID       WorkspaceId        `json:"workspaceId"`
	WorkflowID        WorkflowId         `json:"workflowId"`
	WorkflowVersionID *WorkflowVersionId `json:"workflowVersionId"`
	WorkflowName      string             `json:"workflowName"`
	WorkflowSlug      string             `json:"workflowSlug"`
	Number            int                `json:"number"`
	Status            RunStatus          `json:"status"`
	Trigger           TriggerType        `json:"trigger"`
	Input             map[string]any     `json:"input"`
	TriggeredBy       Principal          `json:"triggeredBy"`
	Region            string             `json:"region"`
	Steps             StepProgress       `json:"steps"`
	CurrentStep       string             `json:"currentStep"`
	CostUsd           float64            `json:"costUsd"`
	Usage             Usage              `json:"usage"`
	Error             *string            `json:"error"`
	BrowserSessionID  *BrowserSessionId  `json:"browserSessionId"`
	// Engine names the execution plane that owns this run: "native" (the in-house
	// Go worker, default) or "temporal" (the Temporal + LangGraph reasoner).
	Engine string `json:"engine"`
	// LangfuseTraceID deep-links the run to its trace in self-hosted Langfuse.
	// Set by the reasoner for temporal runs; nil otherwise.
	LangfuseTraceID *string    `json:"langfuseTraceId"`
	QueuedAt        Timestamp  `json:"queuedAt"`
	StartedAt       *Timestamp `json:"startedAt"`
	FinishedAt      *Timestamp `json:"finishedAt"`
}

// RunDetail is a Run plus its expanded children, returned by the run detail
// endpoint.
type RunDetail struct {
	Run
	Agents    []RunAgent     `json:"agents"`
	Messages  []AgentMessage `json:"messages"`
	Artifacts []Artifact     `json:"artifacts"`
}

/* ============================= Logs ============================= */

type LogEntry struct {
	ID        LogId      `json:"id"`
	RunID     RunId      `json:"runId"`
	Seq       int        `json:"seq"`
	Ts        Timestamp  `json:"ts"`
	OffsetMs  int        `json:"offsetMs"`
	Level     LogLevel   `json:"level"`
	Channel   LogChannel `json:"channel"`
	Source    string     `json:"source"`
	Message   string     `json:"message"`
	Detail    *string    `json:"detail"`
	Reasoning bool       `json:"reasoning"`
}

/* ======================== Browser sessions ====================== */

type BrowserAction struct {
	ID         BrowserActionId   `json:"id"`
	SessionID  BrowserSessionId  `json:"sessionId"`
	Ts         Timestamp         `json:"ts"`
	Type       BrowserActionType `json:"type"`
	Target     string            `json:"target"`
	Value      *string           `json:"value"`
	Status     string            `json:"status"` // ok | error | pending
	DurationMs int               `json:"durationMs"`
}

type BrowserShot struct {
	ID         BrowserShotId    `json:"id"`
	SessionID  BrowserSessionId `json:"sessionId"`
	Ts         Timestamp        `json:"ts"`
	URL        string           `json:"url"`
	Title      string           `json:"title"`
	Label      string           `json:"label"`
	StorageKey *string          `json:"storageKey"`
}

type BrowserConsoleLine struct {
	Ts    Timestamp `json:"ts"`
	Level LogLevel  `json:"level"`
	Text  string    `json:"text"`
}

type BrowserSession struct {
	ID           BrowserSessionId `json:"id"`
	RunID        RunId            `json:"runId"`
	AgentName    string           `json:"agentName"`
	Status       RunStatus        `json:"status"`
	CurrentURL   string           `json:"currentUrl"`
	PageTitle    string           `json:"pageTitle"`
	Viewport     Viewport         `json:"viewport"`
	PagesVisited int              `json:"pagesVisited"`
	ActionsCount int              `json:"actionsCount"`
	StartedAt    Timestamp        `json:"startedAt"`
	FinishedAt   *Timestamp       `json:"finishedAt"`
}

type BrowserSessionDetail struct {
	BrowserSession
	Shots   []BrowserShot        `json:"shots"`
	Actions []BrowserAction      `json:"actions"`
	Console []BrowserConsoleLine `json:"console"`
}

/* ====================== Notifications & feed ==================== */

type Notification struct {
	ID           NotificationId   `json:"id"`
	WorkspaceID  WorkspaceId      `json:"workspaceId"`
	RecipientID  *MemberId        `json:"recipientId"`
	Type         NotificationType `json:"type"`
	Severity     Severity         `json:"severity"`
	Title        string           `json:"title"`
	Body         string           `json:"body"`
	WorkflowSlug *string          `json:"workflowSlug"`
	RunID        *RunId           `json:"runId"`
	RunNumber    *int             `json:"runNumber"`
	ReadAt       *Timestamp       `json:"readAt"`
	CreatedAt    Timestamp        `json:"createdAt"`
}

type ActivityEvent struct {
	ID           ActivityId   `json:"id"`
	WorkspaceID  WorkspaceId  `json:"workspaceId"`
	Kind         ActivityKind `json:"kind"`
	Actor        string       `json:"actor"`
	Text         string       `json:"text"`
	WorkflowSlug *string      `json:"workflowSlug"`
	RunID        *RunId       `json:"runId"`
	At           Timestamp    `json:"at"`
}

// Integration is a workspace's connection to a third-party account via OAuth2
// (e.g. Google, so the worker can send mail as that Gmail account). The refresh
// token is a secret — never serialized to API clients (see the read model).
type Integration struct {
	ID           IntegrationId `json:"id"`
	WorkspaceID  WorkspaceId   `json:"workspaceId"`
	Provider     string        `json:"provider"` // "google"
	AccountEmail string        `json:"accountEmail"`
	RefreshToken string        `json:"-"` // secret, never marshaled
	Scopes       string        `json:"scopes"`
	CreatedAt    Timestamp     `json:"createdAt"`
	UpdatedAt    Timestamp     `json:"updatedAt"`
}

/* ========================= Dashboard ========================== */

type WorkspaceStats struct {
	ActiveRuns     int       `json:"activeRuns"`
	RunsToday      int       `json:"runsToday"`
	SuccessRate    float64   `json:"successRate"`
	SpendTodayUsd  float64   `json:"spendTodayUsd"`
	SpendBudgetUsd float64   `json:"spendBudgetUsd"`
	ComputeHours   float64   `json:"computeHours"`
	TokensToday    int       `json:"tokensToday"`
	RunVolume      []int     `json:"runVolume"`
	SpendSeries    []float64 `json:"spendSeries"`
}
