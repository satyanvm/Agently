package domain

// Reusable value objects and envelope types used across entities and the API.
// Storage-agnostic and deliberately small, mirroring
// packages/contracts/src/primitives.ts.

// Timestamp is an ISO-8601 UTC timestamp kept as a string end-to-end so the
// same value crosses the JSON boundary unchanged (no Date drift).
type Timestamp string

// Usage is token accounting for a run or agent.
type Usage struct {
	TokensIn  int `json:"tokensIn"`
	TokensOut int `json:"tokensOut"`
}

type Viewport struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// StepProgress is the done/total step counter shown on run progress bars.
type StepProgress struct {
	Done  int `json:"done"`
	Total int `json:"total"`
}

// AgentMetrics are per-agent live metrics surfaced in the graph and inspector.
type AgentMetrics struct {
	Tokens    int     `json:"tokens"`
	CostUsd   float64 `json:"costUsd"`
	RuntimeMs *int    `json:"runtimeMs"` // nullable
	ToolCalls int     `json:"toolCalls"`
	Progress  float64 `json:"progress"` // 0..1
}

// Principal is a person reference used for owners / triggered-by / actors.
type Principal struct {
	Name     string `json:"name"`
	Initials string `json:"initials"`
}

// Page is a cursor-paginated list response — the only model that survives growth.
type Page[T any] struct {
	Items      []T     `json:"items"`
	NextCursor *string `json:"nextCursor"`
	Total      *int    `json:"total,omitempty"`
}

// PageQuery is the shared cursor-based pagination query.
type PageQuery struct {
	Cursor string // opaque cursor returned by a previous page
	Limit  int    // 1..100, default 25
}
