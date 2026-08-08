package services

// piecetriggers.go is the control plane's client for the pieces worker's HTTP
// trigger runtime (apps/pieces-worker/src/trigger-runtime.ts). Events are
// transformed BEFORE a run exists: webhook deliveries and polling ticks call
// the piece trigger's real run() on the worker, and the resulting events
// launch runs whose trigger node carries the event through
// (run.input.__trigger_event — see docs/pieces-runtime-contract.md).

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/agently/api/internal/domain"
	"github.com/agently/api/internal/domain/validate"
)

// LaunchInputForTriggerEvent builds the run input that carries a trigger event
// into the graph: the reasoner's prepare_piece_node surfaces
// input.__trigger_event as the trigger node's output.
func LaunchInputForTriggerEvent(event any, trigger domain.TriggerType) validate.LaunchRunInput {
	return validate.LaunchRunInput{
		Trigger: trigger,
		Input:   map[string]any{"__trigger_event": event},
	}
}

// PieceTriggerClient posts to the worker's /run-trigger and /trigger-lifecycle.
type PieceTriggerClient struct {
	baseURL string
	http    *http.Client
}

func NewPieceTriggerClient() *PieceTriggerClient {
	base := os.Getenv("PIECES_WORKER_URL")
	if base == "" {
		base = "http://localhost:7391"
	}
	return &PieceTriggerClient{
		baseURL: strings.TrimRight(base, "/"),
		// Polling trigger runs are capped at 60s worker-side; leave headroom.
		http: &http.Client{Timeout: 65 * time.Second},
	}
}

// TriggerCallRequest mirrors the worker's TriggerRequestBase (+ payload / op).
type TriggerCallRequest struct {
	Piece        string         `json:"piece"`
	Trigger      string         `json:"trigger"`
	Props        map[string]any `json:"props"`
	CredentialID string         `json:"credentialId,omitempty"`
	AuthEnvKey   string         `json:"authEnvKey,omitempty"`
	WorkflowID   string         `json:"workflowId"`
	NodeKey      string         `json:"nodeKey"`
	WebhookURL   string         `json:"webhookUrl,omitempty"`
	Payload      map[string]any `json:"payload,omitempty"`
	Op           string         `json:"op,omitempty"`
}

type RunTriggerResult struct {
	OK        bool   `json:"ok"`
	Events    []any  `json:"events"`
	Error     string `json:"error"`
	ErrorType string `json:"errorType"`
}

func (c *PieceTriggerClient) RunTrigger(req TriggerCallRequest) (RunTriggerResult, error) {
	var out RunTriggerResult
	err := c.post("/run-trigger", req, &out)
	return out, err
}

func (c *PieceTriggerClient) Lifecycle(req TriggerCallRequest) (map[string]any, error) {
	out := map[string]any{}
	err := c.post("/trigger-lifecycle", req, &out)
	return out, err
}

func (c *PieceTriggerClient) post(path string, req any, out any) error {
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}
	resp, err := c.http.Post(c.baseURL+path, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(out)
}

// BuildTriggerRequest derives the worker call from a `pieces.<slug>.<name>`
// trigger node: piece package, trigger name, env key, props (config minus the
// reserved __credentialId / _position keys), and the DB credential id.
// ok=false for non-pieces nodes (e.g. the builtin trigger.webhook).
func BuildTriggerRequest(workflowID string, node domain.GraphNode) (TriggerCallRequest, bool) {
	parts := strings.SplitN(node.Type, ".", 3)
	if len(parts) != 3 || parts[0] != "pieces" || parts[1] == "" || parts[2] == "" {
		return TriggerCallRequest{}, false
	}
	slug := parts[1]
	props := map[string]any{}
	credID := ""
	for k, v := range node.Config {
		switch k {
		case "__credentialId":
			credID, _ = v.(string)
		case "_position":
			// builder presentation state, not a trigger prop
		default:
			props[k] = v
		}
	}
	return TriggerCallRequest{
		Piece:        "@activepieces/piece-" + slug,
		Trigger:      parts[2],
		Props:        props,
		CredentialID: credID,
		AuthEnvKey:   "AP_" + strings.ToUpper(strings.ReplaceAll(slug, "-", "_")) + "_AUTH",
		WorkflowID:   workflowID,
		NodeKey:      node.Key,
	}, true
}

// WebhookURLFor is the public ingress URL a provider should deliver to.
func WebhookURLFor(slug, nodeKey string) string {
	base := os.Getenv("WEBHOOK_PUBLIC_BASE")
	if base == "" {
		base = "http://localhost:8090"
	}
	return strings.TrimRight(base, "/") + "/api/hooks/" + slug + "/" + nodeKey
}
