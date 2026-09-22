package model

import (
	"gorm.io/gorm"
	"time"
)

// DBReviewJob is the durable aggregate root for one review execution.
type DBReviewJob struct {
	ID               uint              `gorm:"primaryKey"`
	PublicID         string            `gorm:"size:32;not null;uniqueIndex"`
	TaskID           string            `gorm:"size:32;index"`
	BackgroundTaskID string            `gorm:"size:32;index"`
	SourceURL        string            `gorm:"type:text"`
	InputHash        string            `gorm:"size:64;not null;index"`
	Status           string            `gorm:"size:24;not null;index"`
	BudgetCents      int               `gorm:"not null;default:1000"`
	SpentCents       int               `gorm:"not null;default:0"`
	ErrorMessage     string            `gorm:"type:text"`
	Version          int64             `gorm:"not null;default:0"`
	CreatedAt        time.Time         `gorm:"not null;index"`
	UpdatedAt        time.Time         `gorm:"not null;index"`
	DeletedAt        gorm.DeletedAt    `gorm:"index"`
	Comments         []DBReviewComment `gorm:"foreignKey:JobID"`
	Traces           []DBTraceEvent    `gorm:"foreignKey:JobID"`
	Todos            []DBTodoItem      `gorm:"foreignKey:JobID"`
}

func (DBReviewJob) TableName() string { return "review_jobs" }

type DBReviewComment struct {
	ID         uint      `gorm:"primaryKey"`
	JobID      uint      `gorm:"not null;index"`
	TraceID    string    `gorm:"size:32;not null;index"`
	File       string    `gorm:"size:512;not null"`
	Line       int       `gorm:"not null;default:1"`
	Severity   string    `gorm:"size:16;not null;index"`
	Confidence string    `gorm:"size:16;not null;index"`
	Body       string    `gorm:"type:text;not null"`
	Status     string    `gorm:"size:16;not null;default:'open';index"`
	CreatedAt  time.Time `gorm:"not null"`
}

func (DBReviewComment) TableName() string { return "review_comments" }

type DBTraceEvent struct {
	ID         uint      `gorm:"primaryKey"`
	JobID      uint      `gorm:"not null;index"`
	TraceID    string    `gorm:"size:32;not null;uniqueIndex"`
	Tool       string    `gorm:"size:64;not null;index"`
	Phase      string    `gorm:"size:32;not null;index"`
	Input      string    `gorm:"type:longtext"`
	Output     string    `gorm:"type:longtext"`
	Prompt     string    `gorm:"type:longtext"`
	ModelReply string    `gorm:"type:longtext"`
	DurationMs int64     `gorm:"not null;default:0"`
	CreatedAt  time.Time `gorm:"not null;index"`
}

func (DBTraceEvent) TableName() string { return "trace_events" }

type DBToolCall struct {
	ID             uint      `gorm:"primaryKey"`
	JobID          uint      `gorm:"not null;index"`
	TraceID        string    `gorm:"size:32;not null;index"`
	IdempotencyKey string    `gorm:"size:128;not null;uniqueIndex"`
	Tool           string    `gorm:"size:64;not null;index"`
	Status         string    `gorm:"size:16;not null;index"`
	InputHash      string    `gorm:"size:64;not null"`
	Output         string    `gorm:"type:longtext"`
	Error          string    `gorm:"type:text"`
	DurationMs     int64     `gorm:"not null;default:0"`
	CreatedAt      time.Time `gorm:"not null;index"`
}

func (DBToolCall) TableName() string { return "tool_calls" }

type DBTodoItem struct {
	ID        uint      `gorm:"primaryKey"`
	JobID     uint      `gorm:"not null;index"`
	Content   string    `gorm:"type:text;not null"`
	Status    string    `gorm:"size:16;not null;index"`
	SortOrder int       `gorm:"not null"`
	CreatedAt time.Time `gorm:"not null"`
	UpdatedAt time.Time `gorm:"not null"`
}

func (DBTodoItem) TableName() string { return "todo_items" }

type DBAgentTask struct {
	ID           string     `gorm:"primaryKey;size:32"`
	ParentTaskID string     `gorm:"size:32;index"`
	ReviewJobID  string     `gorm:"size:32;index"`
	Subject      string     `gorm:"size:512;not null"`
	Description  string     `gorm:"type:text"`
	Status       string     `gorm:"size:16;not null;index"`
	Owner        string     `gorm:"size:128;index"`
	LeaseUntil   *time.Time `gorm:"index"`
	Version      int64      `gorm:"not null;default:0"`
	CreatedAt    time.Time  `gorm:"not null;index"`
	UpdatedAt    time.Time  `gorm:"not null;index"`
	CompletedAt  *time.Time `gorm:"index"`
}

func (DBAgentTask) TableName() string { return "agent_tasks" }

type DBAgentTaskDependency struct {
	TaskID      string    `gorm:"primaryKey;size:32"`
	DependsOnID string    `gorm:"primaryKey;size:32"`
	CreatedAt   time.Time `gorm:"not null"`
}

func (DBAgentTaskDependency) TableName() string { return "agent_task_dependencies" }

type DBBackgroundTask struct {
	ID              string     `gorm:"primaryKey;size:32"`
	Subject         string     `gorm:"size:512;not null"`
	RunnerKind      string     `gorm:"size:64;not null;index"`
	Status          string     `gorm:"size:16;not null;index"`
	Result          string     `gorm:"type:longtext"`
	ErrorMessage    string     `gorm:"type:text"`
	Attempt         int        `gorm:"not null;default:0"`
	CancelRequested bool       `gorm:"not null;default:false;index"`
	LeaseOwner      string     `gorm:"size:128;index"`
	LeaseUntil      *time.Time `gorm:"index"`
	NotifiedAt      *time.Time `gorm:"index"`
	Version         int64      `gorm:"not null;default:0"`
	CreatedAt       time.Time  `gorm:"not null;index"`
	StartedAt       *time.Time `gorm:"index"`
	FinishedAt      *time.Time `gorm:"index"`
	UpdatedAt       time.Time  `gorm:"not null;index"`
}

func (DBBackgroundTask) TableName() string { return "background_tasks" }

type DBBackgroundTaskEvent struct {
	ID         uint      `gorm:"primaryKey"`
	TaskID     string    `gorm:"size:32;not null;index"`
	FromStatus string    `gorm:"size:16;not null"`
	ToStatus   string    `gorm:"size:16;not null;index"`
	Reason     string    `gorm:"size:512"`
	Payload    string    `gorm:"type:json"`
	CreatedAt  time.Time `gorm:"not null;index"`
}

func (DBBackgroundTaskEvent) TableName() string { return "background_task_events" }

type DBTeamMessage struct {
	ID             uint       `gorm:"primaryKey"`
	MessageID      string     `gorm:"size:64;not null;uniqueIndex"`
	JobID          string     `gorm:"size:32;index"`
	FromAgent      string     `gorm:"size:128;not null;index"`
	ToAgent        string     `gorm:"size:128;not null;index"`
	MessageType    string     `gorm:"size:32;not null;index"`
	Content        string     `gorm:"type:longtext;not null"`
	DeliveryStatus string     `gorm:"size:16;not null;index"`
	LeaseOwner     string     `gorm:"size:128;index"`
	LeaseUntil     *time.Time `gorm:"index"`
	ConsumedAt     *time.Time `gorm:"index"`
	CreatedAt      time.Time  `gorm:"not null;index"`
}

func (DBTeamMessage) TableName() string { return "team_messages" }

type DBAgentRun struct {
	RunID        string     `gorm:"primaryKey;size:64"`
	RunType      string     `gorm:"size:32;not null;index"`
	ReviewJobID  string     `gorm:"size:32;index"`
	WorkflowName string     `gorm:"size:128;index"`
	Status       string     `gorm:"size:16;not null;index"`
	ArgsJSON     string     `gorm:"type:json"`
	StateJSON    string     `gorm:"type:json"`
	ResultJSON   string     `gorm:"type:json"`
	ErrorMessage string     `gorm:"type:text"`
	AgentCount   int        `gorm:"not null;default:0"`
	TokenUsage   int        `gorm:"not null;default:0"`
	NotifiedAt   *time.Time `gorm:"index"`
	Version      int64      `gorm:"not null;default:0"`
	CreatedAt    time.Time  `gorm:"not null;index"`
	StartedAt    *time.Time `gorm:"index"`
	FinishedAt   *time.Time `gorm:"index"`
	UpdatedAt    time.Time  `gorm:"not null;index"`
}

func (DBAgentRun) TableName() string { return "agent_runs" }

type DBWorkflowEvent struct {
	ID        uint      `gorm:"primaryKey"`
	RunID     string    `gorm:"size:64;not null;index"`
	EventSeq  int64     `gorm:"not null"`
	EventType string    `gorm:"size:64;not null;index"`
	Phase     string    `gorm:"size:128;index"`
	Label     string    `gorm:"size:256;index"`
	Message   string    `gorm:"type:longtext"`
	Status    string    `gorm:"size:16;index"`
	CallKey   string    `gorm:"size:128;index"`
	CreatedAt time.Time `gorm:"not null;index"`
}

func (DBWorkflowEvent) TableName() string { return "workflow_events" }

type DBWorkflowCallResult struct {
	RunID      string    `gorm:"primaryKey;size:64"`
	CallKey    string    `gorm:"primaryKey;size:128"`
	Label      string    `gorm:"size:256"`
	PromptHash string    `gorm:"size:64;not null"`
	SchemaHash string    `gorm:"size:64;not null"`
	Status     string    `gorm:"size:16;not null;index"`
	ResultJSON string    `gorm:"type:json"`
	Tokens     int       `gorm:"not null;default:0"`
	CreatedAt  time.Time `gorm:"not null"`
	UpdatedAt  time.Time `gorm:"not null;index"`
}

func (DBWorkflowCallResult) TableName() string { return "workflow_call_results" }
