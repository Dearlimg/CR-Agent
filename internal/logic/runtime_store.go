package logic

import "CR-Agent/internal/model"

type TaskRepository interface {
	Create(subject, description string) (model.Task, error)
	Get(taskID string) (model.Task, error)
	List() ([]model.Task, error)
	AddDependencies(taskID string, blockedBy []string) (model.Task, error)
	Claim(taskID, owner string) (model.Task, error)
	Complete(taskID, owner string) (model.Task, []model.Task, error)
}

type BackgroundRepository interface {
	Create(subject string) (model.BackgroundTask, error)
	Launch(taskID string, runner model.BackgroundRunner) error
	Get(taskID string) (model.BackgroundTask, error)
	List() ([]model.BackgroundTask, error)
	IsIdle() (bool, error)
	Cancel(taskID string) (model.BackgroundTask, error)
	Collect() ([]model.BackgroundTask, error)
}

type TeamMailbox interface {
	Send(event model.TeamEvent) error
	Consume(agent, jobID string) ([]model.TeamEvent, error)
	Ack(events []model.TeamEvent) error
}

type RuntimeRepositories struct {
	Tasks      TaskRepository
	Background BackgroundRepository
	Mailbox    TeamMailbox
}

func fileRuntimeRepositories(cfg Config) RuntimeRepositories {
	return RuntimeRepositories{
		Tasks:      NewTaskStore(cfg.TasksDir),
		Background: NewBackgroundManager(cfg.BackgroundTasksDir),
		Mailbox:    NewMessageBus(cfg.TeamMailboxDir),
	}
}

func ensureRuntimeRepositories(cfg Config, repositories RuntimeRepositories) RuntimeRepositories {
	defaults := fileRuntimeRepositories(cfg)
	if repositories.Tasks == nil {
		repositories.Tasks = defaults.Tasks
	}
	if repositories.Background == nil {
		repositories.Background = defaults.Background
	}
	if repositories.Mailbox == nil {
		repositories.Mailbox = defaults.Mailbox
	}
	return repositories
}
