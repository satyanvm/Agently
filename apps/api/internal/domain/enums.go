package domain

// Closed vocabularies. These mirror the values the frontend renders and the
// canonical enums in packages/contracts/src/enums.ts. Each is a named string
// type with a const block and a validity set used by the validation layer.

type RunStatus string

const (
	RunQueued    RunStatus = "queued"
	RunRunning   RunStatus = "running"
	RunSucceeded RunStatus = "succeeded"
	RunFailed    RunStatus = "failed"
	RunCanceled  RunStatus = "canceled"
	RunPaused    RunStatus = "paused"
)

var ValidRunStatuses = map[string]bool{
	"queued": true, "running": true, "succeeded": true,
	"failed": true, "canceled": true, "paused": true,
}

var TerminalRunStatuses = []RunStatus{RunSucceeded, RunFailed, RunCanceled}

func IsTerminalRunStatus(s RunStatus) bool {
	for _, t := range TerminalRunStatuses {
		if t == s {
			return true
		}
	}
	return false
}

type AgentStatus string

const (
	AgentIdle      AgentStatus = "idle"
	AgentRunning   AgentStatus = "running"
	AgentSucceeded AgentStatus = "succeeded"
	AgentFailed    AgentStatus = "failed"
	AgentBlocked   AgentStatus = "blocked"
	AgentWaiting   AgentStatus = "waiting"
)

var ValidAgentStatuses = map[string]bool{
	"idle": true, "running": true, "succeeded": true,
	"failed": true, "blocked": true, "waiting": true,
}

type AgentRole string

const (
	RoleOrchestrator AgentRole = "orchestrator"
	RoleResearcher   AgentRole = "researcher"
	RoleBrowser      AgentRole = "browser"
	RoleCoder        AgentRole = "coder"
	RoleAnalyst      AgentRole = "analyst"
	RoleWriter       AgentRole = "writer"
	RoleValidator    AgentRole = "validator"
)

var ValidAgentRoles = map[string]bool{
	"orchestrator": true, "researcher": true, "browser": true, "coder": true,
	"analyst": true, "writer": true, "validator": true,
}

type LogLevel string

const (
	LevelDebug   LogLevel = "debug"
	LevelInfo    LogLevel = "info"
	LevelSuccess LogLevel = "success"
	LevelWarn    LogLevel = "warn"
	LevelError   LogLevel = "error"
)

var ValidLogLevels = map[string]bool{
	"debug": true, "info": true, "success": true, "warn": true, "error": true,
}

type LogChannel string

const (
	ChannelSystem  LogChannel = "system"
	ChannelAgent   LogChannel = "agent"
	ChannelTool    LogChannel = "tool"
	ChannelBrowser LogChannel = "browser"
	ChannelModel   LogChannel = "model"
)

var ValidLogChannels = map[string]bool{
	"system": true, "agent": true, "tool": true, "browser": true, "model": true,
}

type TriggerType string

const (
	TriggerManual   TriggerType = "manual"
	TriggerSchedule TriggerType = "schedule"
	TriggerWebhook  TriggerType = "webhook"
	TriggerEvent    TriggerType = "event"
)

var ValidTriggerTypes = map[string]bool{
	"manual": true, "schedule": true, "webhook": true, "event": true,
}

type ArtifactKind string

const (
	ArtifactFile    ArtifactKind = "file"
	ArtifactJSON    ArtifactKind = "json"
	ArtifactImage   ArtifactKind = "image"
	ArtifactDataset ArtifactKind = "dataset"
	ArtifactReport  ArtifactKind = "report"
	ArtifactURL     ArtifactKind = "url"
	ArtifactCode    ArtifactKind = "code"
)

var ValidArtifactKinds = map[string]bool{
	"file": true, "json": true, "image": true, "dataset": true,
	"report": true, "url": true, "code": true,
}

type BrowserActionType string

const (
	BrowserNavigate   BrowserActionType = "navigate"
	BrowserClick      BrowserActionType = "click"
	BrowserType       BrowserActionType = "type"
	BrowserScroll     BrowserActionType = "scroll"
	BrowserExtract    BrowserActionType = "extract"
	BrowserWait       BrowserActionType = "wait"
	BrowserScreenshot BrowserActionType = "screenshot"
	BrowserSubmit     BrowserActionType = "submit"
)

var ValidBrowserActionTypes = map[string]bool{
	"navigate": true, "click": true, "type": true, "scroll": true,
	"extract": true, "wait": true, "screenshot": true, "submit": true,
}

type NotificationType string

const (
	NotifWorkflowCompleted NotificationType = "workflow.completed"
	NotifWorkflowFailed    NotificationType = "workflow.failed"
	NotifBrowserError      NotificationType = "browser.error"
	NotifCostAlert         NotificationType = "cost.alert"
	NotifAgentBlocked      NotificationType = "agent.blocked"
	NotifRunStarted        NotificationType = "run.started"
)

var ValidNotificationTypes = map[string]bool{
	"workflow.completed": true, "workflow.failed": true, "browser.error": true,
	"cost.alert": true, "agent.blocked": true, "run.started": true,
}

type Severity string

const (
	SeverityInfo    Severity = "info"
	SeveritySuccess Severity = "success"
	SeverityWarning Severity = "warning"
	SeverityError   Severity = "error"
)

var ValidSeverities = map[string]bool{
	"info": true, "success": true, "warning": true, "error": true,
}

type ActivityKind string

const (
	ActivityDeploy   ActivityKind = "deploy"
	ActivityRun      ActivityKind = "run"
	ActivityComplete ActivityKind = "complete"
	ActivityFail     ActivityKind = "fail"
	ActivityComment  ActivityKind = "comment"
	ActivityScale    ActivityKind = "scale"
)

type MemberRole string

const (
	MemberOwner  MemberRole = "owner"
	MemberAdmin  MemberRole = "admin"
	MemberMember MemberRole = "member"
)

type WorkspacePlan string

const (
	PlanFree       WorkspacePlan = "free"
	PlanPro        WorkspacePlan = "pro"
	PlanEnterprise WorkspacePlan = "enterprise"
)
