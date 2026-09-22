package model

import (
	"gorm.io/gorm"
	"time"
)

// DBReviewJob is the durable aggregate root for one review execution.
type DBReviewJob struct {
	ID           uint              `gorm:"primaryKey"`
	PublicID     string            `gorm:"size:32;not null;uniqueIndex"`
	SourceURL    string            `gorm:"type:text"`
	InputHash    string            `gorm:"size:64;not null;index"`
	Status       string            `gorm:"size:24;not null;index"`
	BudgetCents  int               `gorm:"not null;default:1000"`
	SpentCents   int               `gorm:"not null;default:0"`
	ErrorMessage string            `gorm:"type:text"`
	CreatedAt    time.Time         `gorm:"not null;index"`
	UpdatedAt    time.Time         `gorm:"not null;index"`
	DeletedAt    gorm.DeletedAt    `gorm:"index"`
	Comments     []DBReviewComment `gorm:"foreignKey:JobID"`
	Traces       []DBTraceEvent    `gorm:"foreignKey:JobID"`
	Todos        []DBTodoItem      `gorm:"foreignKey:JobID"`
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
	ID         uint      `gorm:"primaryKey"`
	JobID      uint      `gorm:"not null;index"`
	TraceID    string    `gorm:"size:32;not null;index"`
	Tool       string    `gorm:"size:64;not null;index"`
	Status     string    `gorm:"size:16;not null;index"`
	InputHash  string    `gorm:"size:64;not null"`
	Output     string    `gorm:"type:longtext"`
	Error      string    `gorm:"type:text"`
	DurationMs int64     `gorm:"not null;default:0"`
	CreatedAt  time.Time `gorm:"not null;index"`
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
