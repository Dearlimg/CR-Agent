package model

import "time"

// CronJob is a durable schedule for the registered code-review runner. It
// intentionally stores a source URL rather than a pasted diff, so runtime
// review input and potentially sensitive code are not persisted as scheduler
// metadata.
type CronJob struct {
	ID                   string    `json:"id"`
	Cron                 string    `json:"cron"`
	Source               string    `json:"source"`
	MemoryQuery          string    `json:"memory_query,omitempty"`
	Recurring            bool      `json:"recurring"`
	Durable              bool      `json:"durable"`
	PendingDelivery      bool      `json:"pending_delivery"`
	LastFired            string    `json:"last_fired,omitempty"`
	LastBackgroundTaskID string    `json:"last_background_task_id,omitempty"`
	LastError            string    `json:"last_error,omitempty"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}
