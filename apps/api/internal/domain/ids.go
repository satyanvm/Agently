package domain

import "crypto/rand"

// Prefixed identifiers.
//
// Every entity has a string id with a stable, human-legible prefix
// (wf_compint, run_8842, log_19f3…). The prefix makes ids self-describing in
// logs, URLs, and API payloads. Each id is its own named string type, which
// gives compile-time safety inside Go (a RunId is not assignable to a
// WorkflowId) while still marshaling as a plain JSON string. This is the Go
// analogue of the branded ids in packages/contracts/src/ids.ts.

type (
	WorkspaceId       string
	MemberId          string
	ApiKeyId          string
	WorkflowId        string
	WorkflowVersionId string
	AgentDefinitionId string
	RunId             string
	RunAgentId        string
	AgentMessageId    string
	LogId             string
	ArtifactId        string
	BrowserSessionId  string
	BrowserActionId   string
	BrowserShotId     string
	NotificationId    string
	ActivityId        string
	EventId           string
	IntegrationId     string
	CredentialId      string
)

// EntityKind keys the IDPrefix map. The single place prefixes are declared.
type EntityKind string

const (
	KindWorkspace       EntityKind = "workspace"
	KindMember          EntityKind = "member"
	KindApiKey          EntityKind = "apiKey"
	KindWorkflow        EntityKind = "workflow"
	KindWorkflowVersion EntityKind = "workflowVersion"
	KindAgentDefinition EntityKind = "agentDefinition"
	KindRun             EntityKind = "run"
	KindRunAgent        EntityKind = "runAgent"
	KindAgentMessage    EntityKind = "agentMessage"
	KindLog             EntityKind = "log"
	KindArtifact        EntityKind = "artifact"
	KindBrowserSession  EntityKind = "browserSession"
	KindBrowserAction   EntityKind = "browserAction"
	KindBrowserShot     EntityKind = "browserShot"
	KindNotification    EntityKind = "notification"
	KindActivity        EntityKind = "activity"
	KindEvent           EntityKind = "event"
	KindIntegration     EntityKind = "integration"
	KindCredential      EntityKind = "credential"
)

// IDPrefix maps each entity kind to its id prefix.
var IDPrefix = map[EntityKind]string{
	KindWorkspace:       "ws",
	KindMember:          "mem",
	KindApiKey:          "key",
	KindWorkflow:        "wf",
	KindWorkflowVersion: "wfv",
	KindAgentDefinition: "agt",
	KindRun:             "run",
	KindRunAgent:        "ra",
	KindAgentMessage:    "msg",
	KindLog:             "log",
	KindArtifact:        "art",
	KindBrowserSession:  "bs",
	KindBrowserAction:   "ba",
	KindBrowserShot:     "shot",
	KindNotification:    "ntf",
	KindActivity:        "act",
	KindEvent:           "evt",
	KindIntegration:     "int",
	KindCredential:      "cred",
}

const base36 = "0123456789abcdefghijklmnopqrstuvwxyz"

// randomSuffix returns a lowercase base36 random suffix, mirroring the TS
// implementation: size random bytes, each reduced mod 36 to a base36 char.
func randomSuffix(size int) string {
	bytes := make([]byte, size)
	if _, err := rand.Read(bytes); err != nil {
		// crypto/rand failing is catastrophic and unrecoverable.
		panic("crypto/rand unavailable: " + err.Error())
	}
	out := make([]byte, size)
	for i, b := range bytes {
		out[i] = base36[int(b)%36]
	}
	return string(out)
}

// NewID mints a new id string for an entity kind.
func NewID(kind EntityKind) string {
	return IDPrefix[kind] + "_" + randomSuffix(20)
}

// Strongly-typed minters for ergonomics at call sites.
func NewWorkspaceId() WorkspaceId             { return WorkspaceId(NewID(KindWorkspace)) }
func NewMemberId() MemberId                   { return MemberId(NewID(KindMember)) }
func NewApiKeyId() ApiKeyId                   { return ApiKeyId(NewID(KindApiKey)) }
func NewWorkflowId() WorkflowId               { return WorkflowId(NewID(KindWorkflow)) }
func NewWorkflowVersionId() WorkflowVersionId { return WorkflowVersionId(NewID(KindWorkflowVersion)) }
func NewAgentDefinitionId() AgentDefinitionId { return AgentDefinitionId(NewID(KindAgentDefinition)) }
func NewRunId() RunId                         { return RunId(NewID(KindRun)) }
func NewRunAgentId() RunAgentId               { return RunAgentId(NewID(KindRunAgent)) }
func NewAgentMessageId() AgentMessageId       { return AgentMessageId(NewID(KindAgentMessage)) }
func NewLogId() LogId                         { return LogId(NewID(KindLog)) }
func NewArtifactId() ArtifactId               { return ArtifactId(NewID(KindArtifact)) }
func NewBrowserSessionId() BrowserSessionId   { return BrowserSessionId(NewID(KindBrowserSession)) }
func NewBrowserActionId() BrowserActionId     { return BrowserActionId(NewID(KindBrowserAction)) }
func NewBrowserShotId() BrowserShotId         { return BrowserShotId(NewID(KindBrowserShot)) }
func NewNotificationId() NotificationId       { return NotificationId(NewID(KindNotification)) }
func NewActivityId() ActivityId               { return ActivityId(NewID(KindActivity)) }
func NewEventId() EventId                     { return EventId(NewID(KindEvent)) }
func NewIntegrationId() IntegrationId         { return IntegrationId(NewID(KindIntegration)) }
func NewCredentialId() CredentialId           { return CredentialId(NewID(KindCredential)) }
