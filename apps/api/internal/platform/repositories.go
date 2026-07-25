package platform

import "github.com/agently/api/internal/domain"

// Storage interfaces. Services depend only on these, never on a concrete store,
// so the in-memory implementation can be replaced by a Postgres one without
// changing any service or handler. Mirrors
// packages/core/src/platform/repositories/types.ts.
//
// Updates take dedicated *Patch structs with pointer fields: a non-nil pointer
// means "set this field". This is the typed, reflect-free Go analogue of the
// TS Partial<Entity> spread.

type WorkspaceRepo interface {
	Get() domain.Workspace
}

type MemberRepo interface {
	All() []domain.Member
	GetByID(id domain.MemberId) (domain.Member, bool)
}

type AgentDefinitionRepo interface {
	All() []domain.AgentDefinition
	GetByID(id domain.AgentDefinitionId) (domain.AgentDefinition, bool)
	Insert(agent domain.AgentDefinition) domain.AgentDefinition
}

// WorkflowPatch carries the mutable fields of a workflow update.
type WorkflowPatch struct {
	AgentCount       *int
	CurrentVersionID *domain.WorkflowVersionId
	DefaultInput     *map[string]any
	UpdatedAt        *domain.Timestamp
	ArchivedAt       *domain.Timestamp
}

type WorkflowRepo interface {
	All() []domain.Workflow
	GetByID(id domain.WorkflowId) (domain.Workflow, bool)
	GetBySlug(slug string) (domain.Workflow, bool)
	Insert(workflow domain.Workflow) domain.Workflow
	Update(id domain.WorkflowId, patch WorkflowPatch) (domain.Workflow, error)
}

type WorkflowVersionRepo interface {
	GetByID(id domain.WorkflowVersionId) (domain.WorkflowVersion, bool)
	ListByWorkflow(workflowID domain.WorkflowId) []domain.WorkflowVersion
	Insert(version domain.WorkflowVersion) domain.WorkflowVersion
}

// RunPatch carries the mutable fields of a run update.
type RunPatch struct {
	Status      *domain.RunStatus
	Steps       *domain.StepProgress
	CurrentStep *string
	CostUsd     *float64
	Usage       *domain.Usage
	Error       **string // double pointer: set vs set-to-null
	StartedAt   *domain.Timestamp
	FinishedAt  *domain.Timestamp
}

type RunRepo interface {
	All() []domain.Run
	GetByID(id domain.RunId) (domain.Run, bool)
	ListByWorkflow(workflowID domain.WorkflowId) []domain.Run
	Insert(run domain.Run) domain.Run
	Update(id domain.RunId, patch RunPatch) (domain.Run, error)
	NextNumber(workflowID domain.WorkflowId) int
}

// RunAgentPatch carries the mutable fields of a run-agent update.
type RunAgentPatch struct {
	Status     *domain.AgentStatus
	Summary    *string
	Metrics    *domain.AgentMetrics
	StartedAt  *domain.Timestamp
	FinishedAt *domain.Timestamp
}

type RunAgentRepo interface {
	ListByRun(runID domain.RunId) []domain.RunAgent
	GetByID(id domain.RunAgentId) (domain.RunAgent, bool)
	InsertMany(agents []domain.RunAgent)
	Update(id domain.RunAgentId, patch RunAgentPatch) (domain.RunAgent, error)
}

type MessageRepo interface {
	ListByRun(runID domain.RunId) []domain.AgentMessage
	Insert(message domain.AgentMessage) domain.AgentMessage
}

type ArtifactRepo interface {
	ListByRun(runID domain.RunId) []domain.Artifact
	Insert(artifact domain.Artifact) domain.Artifact
}

type LogRepo interface {
	ListByRun(runID domain.RunId) []domain.LogEntry
	Insert(entry domain.LogEntry) domain.LogEntry
	// MaxSeq returns the highest seq written for a run (-1 if none).
	MaxSeq(runID domain.RunId) int
}

// BrowserSessionPatch carries the mutable fields of a browser-session update.
type BrowserSessionPatch struct {
	Status       *domain.RunStatus
	CurrentURL   *string
	PageTitle    *string
	PagesVisited *int
	ActionsCount *int
	FinishedAt   *domain.Timestamp
}

type BrowserRepo interface {
	GetByID(id domain.BrowserSessionId) (domain.BrowserSession, bool)
	GetByRun(runID domain.RunId) (domain.BrowserSession, bool)
	InsertSession(session domain.BrowserSession) domain.BrowserSession
	Update(id domain.BrowserSessionId, patch BrowserSessionPatch) (domain.BrowserSession, error)
	ListActions(id domain.BrowserSessionId) []domain.BrowserAction
	ListShots(id domain.BrowserSessionId) []domain.BrowserShot
	ListConsole(id domain.BrowserSessionId) []domain.BrowserConsoleLine
	InsertAction(action domain.BrowserAction) domain.BrowserAction
	InsertShot(shot domain.BrowserShot) domain.BrowserShot
	InsertConsole(id domain.BrowserSessionId, line domain.BrowserConsoleLine)
}

// NotificationPatch carries the mutable fields of a notification update.
type NotificationPatch struct {
	ReadAt *domain.Timestamp
}

type NotificationRepo interface {
	All() []domain.Notification
	GetByID(id domain.NotificationId) (domain.Notification, bool)
	Insert(notification domain.Notification) domain.Notification
	Update(id domain.NotificationId, patch NotificationPatch) (domain.Notification, error)
	MarkAllRead(workspaceID domain.WorkspaceId, at domain.Timestamp) int
}

type ActivityRepo interface {
	All() []domain.ActivityEvent
	Insert(event domain.ActivityEvent) domain.ActivityEvent
}

// IntegrationRepo stores third-party account connections (OAuth2). Upsert keys on
// (workspace, provider) so re-connecting refreshes the stored token in place.
type IntegrationRepo interface {
	GetByWorkspaceProvider(workspaceID domain.WorkspaceId, provider string) (domain.Integration, bool)
	Upsert(integration domain.Integration) domain.Integration
	DeleteByWorkspaceProvider(workspaceID domain.WorkspaceId, provider string) bool
}

// CredentialPatch carries the mutable fields of a credential update. Data is the
// FULL post-merge secret map (the service merges values per-key before calling).
type CredentialPatch struct {
	Name      *string
	Data      *map[string]any
	UpdatedAt *domain.Timestamp
}

// CredentialRepo stores DB-backed credentials (docs/credentials-contract.md §5).
type CredentialRepo interface {
	ListByWorkspace(workspaceID domain.WorkspaceId) []domain.Credential
	GetByID(id domain.CredentialId) (domain.Credential, bool)
	Insert(credential domain.Credential) domain.Credential
	Update(id domain.CredentialId, patch CredentialPatch) (domain.Credential, error)
	Delete(id domain.CredentialId) bool
}

// Repositories is the full set of repositories handed to services.
type Repositories struct {
	Workspace     WorkspaceRepo
	Members       MemberRepo
	Agents        AgentDefinitionRepo
	Workflows     WorkflowRepo
	Versions      WorkflowVersionRepo
	Runs          RunRepo
	RunAgents     RunAgentRepo
	Messages      MessageRepo
	Artifacts     ArtifactRepo
	Logs          LogRepo
	Browser       BrowserRepo
	Notifications NotificationRepo
	Activity      ActivityRepo
	Integrations  IntegrationRepo
	Credentials   CredentialRepo
}
