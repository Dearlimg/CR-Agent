package model

import "time"

type BackgroundTaskStatus string

const (
	BackgroundTaskPending   BackgroundTaskStatus = "pending"
	BackgroundTaskRunning   BackgroundTaskStatus = "running"
	BackgroundTaskCompleted BackgroundTaskStatus = "completed"
	BackgroundTaskFailed    BackgroundTaskStatus = "failed"
	BackgroundTaskCancelled BackgroundTaskStatus = "cancelled"
)

type BackgroundTask struct {
	ID         string               `json:"id"`
	Subject    string               `json:"subject"`
	Status     BackgroundTaskStatus `json:"status"`
	Result     string               `json:"result,omitempty"`
	Error      string               `json:"error,omitempty"`
	CreatedAt  time.Time            `json:"created_at"`
	StartedAt  *time.Time           `json:"started_at,omitempty"`
	FinishedAt *time.Time           `json:"finished_at,omitempty"`
	NotifiedAt *time.Time           `json:"notified_at,omitempty"`
}
