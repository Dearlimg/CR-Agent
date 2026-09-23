package model

import "time"

type ReviewRequest struct {
	Source      string `json:"source"`
	Diff        string `json:"diff"`
	MemoryQuery string `json:"memory_query"`
	Goal        string `json:"goal"`
	BudgetCents int    `json:"budget_cents"`
}
type ReviewComment struct {
	File       string `json:"file"`
	Line       int    `json:"line"`
	Severity   string `json:"severity"`
	Confidence string `json:"confidence"`
	Body       string `json:"body"`
	TraceID    string `json:"trace_id"`
}
type TodoItem struct {
	Content string `json:"content"`
	Status  string `json:"status"`
	Order   int    `json:"order"`
}
type TraceEvent struct {
	ID           string     `json:"id"`
	ParentID     string     `json:"parent_id,omitempty"`
	Kind         string     `json:"kind,omitempty"`
	Status       string     `json:"status,omitempty"`
	Round        int        `json:"round,omitempty"`
	RetryCount   int        `json:"retry_count,omitempty"`
	InputTokens  int        `json:"input_tokens,omitempty"`
	OutputTokens int        `json:"output_tokens,omitempty"`
	FinishReason string     `json:"finish_reason,omitempty"`
	Origin       string     `json:"origin,omitempty"`
	CacheHit     bool       `json:"cache_hit,omitempty"`
	ToolCallID   string     `json:"tool_call_id,omitempty"`
	ToolVersion  string     `json:"tool_version,omitempty"`
	InputDigest  string     `json:"input_digest,omitempty"`
	Tool         string     `json:"tool"`
	Input        string     `json:"input"`
	Output       string     `json:"output"`
	Prompt       string     `json:"prompt,omitempty"`
	ModelReply   string     `json:"model_reply,omitempty"`
	At           time.Time  `json:"at"`
	StartedAt    time.Time  `json:"started_at,omitempty"`
	EndedAt      *time.Time `json:"ended_at,omitempty"`
	DurationMs   int64      `json:"duration_ms"`
	Phase        string     `json:"phase,omitempty"`
}
type TeamEvent struct {
	ID      string    `json:"id,omitempty"`
	From    string    `json:"from"`
	To      string    `json:"to"`
	Type    string    `json:"type"`
	TaskID  string    `json:"task_id,omitempty"`
	Content string    `json:"content"`
	At      time.Time `json:"at"`
}
type ReviewJob struct {
	ID               string          `json:"id"`
	TaskID           string          `json:"task_id"`
	BackgroundTaskID string          `json:"background_task_id"`
	Status           string          `json:"status"`
	Source           string          `json:"source"`
	Comments         []ReviewComment `json:"comments"`
	Todos            []TodoItem      `json:"todos"`
	Trace            []TraceEvent    `json:"trace"`
	TeamEvents       []TeamEvent     `json:"team_events"`
	SpentCents       int             `json:"spent_cents"`
	StartedAt        time.Time       `json:"started_at"`
	FinishedAt       *time.Time      `json:"finished_at,omitempty"`
	UpdatedAt        time.Time       `json:"updated_at"`
	Error            string          `json:"error,omitempty"`
}
