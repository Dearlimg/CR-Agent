package dao

import (
	"CR-Agent/internal/model"
	"gorm.io/gorm"
)

// AutoMigrate is intentionally explicit; startup does not mutate a database.
func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(
		&model.DBReviewJob{},
		&model.DBReviewComment{},
		&model.DBTraceEvent{},
		&model.DBToolCall{},
		&model.DBTodoItem{},
		&model.DBAgentTask{},
		&model.DBAgentTaskDependency{},
		&model.DBBackgroundTask{},
		&model.DBBackgroundTaskEvent{},
		&model.DBTeamMessage{},
		&model.DBAgentRun{},
		&model.DBWorkflowEvent{},
		&model.DBWorkflowCallResult{},
		&model.DBWorkflowLease{},
	)
}
