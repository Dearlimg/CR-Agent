package model

import "time"

type ReviewRequest struct {
	Source      string `json:"source"`
	Diff        string `json:"diff"`
	MemoryQuery string `json:"memory_query"`
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
	ID         string    `json:"id"`
	Tool       string    `json:"tool"`
	Input      string    `json:"input"`
	Output     string    `json:"output"`
	Prompt     string    `json:"prompt,omitempty"`
	ModelReply string    `json:"model_reply,omitempty"`
	At         time.Time `json:"at"`
	DurationMs int64     `json:"duration_ms"`
	Phase      string    `json:"phase,omitempty"`
}
type ReviewJob struct {
	ID         string          `json:"id"`
	Status     string          `json:"status"`
	Source     string          `json:"source"`
	Comments   []ReviewComment `json:"comments"`
	Todos      []TodoItem      `json:"todos"`
	Trace      []TraceEvent    `json:"trace"`
	SpentCents int             `json:"spent_cents"`
	UpdatedAt  time.Time       `json:"updated_at"`
	Error      string          `json:"error,omitempty"`
}
