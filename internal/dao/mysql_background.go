package dao

import (
	"context"
	"fmt"
	"sync"
	"time"

	"CR-Agent/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const backgroundLeaseDuration = 2 * time.Minute

type MySQLBackgroundRepository struct {
	db      *gorm.DB
	worker  string
	mu      sync.Mutex
	cancels map[string]context.CancelFunc
}

func NewMySQLBackgroundRepository(db *gorm.DB) (*MySQLBackgroundRepository, error) {
	worker, err := newRuntimeID("worker_", 8)
	if err != nil {
		return nil, err
	}
	repository := &MySQLBackgroundRepository{
		db:      db,
		worker:  worker,
		cancels: map[string]context.CancelFunc{},
	}
	if err := repository.recover(); err != nil {
		return nil, err
	}
	return repository, nil
}

func (s *MySQLBackgroundRepository) Create(subject string) (model.BackgroundTask, error) {
	if subject == "" {
		return model.BackgroundTask{}, fmt.Errorf("后台任务主题不能为空")
	}
	id, err := newRuntimeID("bg_", 4)
	if err != nil {
		return model.BackgroundTask{}, err
	}
	now := time.Now().UTC()
	row := model.DBBackgroundTask{
		ID:         id,
		Subject:    subject,
		RunnerKind: "registered",
		Status:     string(model.BackgroundTaskPending),
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := s.db.Create(&row).Error; err != nil {
		return model.BackgroundTask{}, fmt.Errorf("创建后台任务: %w", err)
	}
	return backgroundTaskFromRow(row), nil
}

func (s *MySQLBackgroundRepository) Launch(taskID string, runner model.BackgroundRunner) error {
	if runner == nil {
		return fmt.Errorf("后台任务 Runner 不能为空")
	}
	now := time.Now().UTC()
	leaseUntil := now.Add(backgroundLeaseDuration)
	result := s.db.Model(&model.DBBackgroundTask{}).
		Where("id = ? AND status = ?", taskID, model.BackgroundTaskPending).
		Updates(map[string]any{
			"status":      model.BackgroundTaskRunning,
			"started_at":  now,
			"updated_at":  now,
			"lease_owner": s.worker,
			"lease_until": leaseUntil,
			"attempt":     gorm.Expr("attempt + 1"),
			"version":     gorm.Expr("version + 1"),
		})
	if result.Error != nil {
		return fmt.Errorf("启动后台任务: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("后台任务 %s 不是 pending 状态", taskID)
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	s.cancels[taskID] = cancel
	s.mu.Unlock()
	go s.run(taskID, ctx, runner)
	return nil
}

func (s *MySQLBackgroundRepository) Get(taskID string) (model.BackgroundTask, error) {
	var row model.DBBackgroundTask
	if err := s.db.Where("id = ?", taskID).First(&row).Error; err != nil {
		return model.BackgroundTask{}, fmt.Errorf("读取后台任务 %s: %w", taskID, err)
	}
	return backgroundTaskFromRow(row), nil
}

func (s *MySQLBackgroundRepository) List() ([]model.BackgroundTask, error) {
	var rows []model.DBBackgroundTask
	if err := s.db.Order("created_at asc").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("列出后台任务: %w", err)
	}
	result := make([]model.BackgroundTask, 0, len(rows))
	for _, row := range rows {
		result = append(result, backgroundTaskFromRow(row))
	}
	return result, nil
}

func (s *MySQLBackgroundRepository) IsIdle() (bool, error) {
	var count int64
	if err := s.db.Model(&model.DBBackgroundTask{}).
		Where("status IN ?", []model.BackgroundTaskStatus{model.BackgroundTaskPending, model.BackgroundTaskRunning}).
		Count(&count).Error; err != nil {
		return false, fmt.Errorf("查询后台任务状态: %w", err)
	}
	return count == 0, nil
}

func (s *MySQLBackgroundRepository) Cancel(taskID string) (model.BackgroundTask, error) {
	s.mu.Lock()
	cancel := s.cancels[taskID]
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	var row model.DBBackgroundTask
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", taskID).First(&row).Error; err != nil {
			return fmt.Errorf("读取后台任务 %s: %w", taskID, err)
		}
		if row.Status != string(model.BackgroundTaskPending) && row.Status != string(model.BackgroundTaskRunning) {
			return fmt.Errorf("后台任务 %s 已结束，不能取消", taskID)
		}
		now := time.Now().UTC()
		row.Status = string(model.BackgroundTaskCancelled)
		row.Result = "任务已取消"
		row.CancelRequested = true
		row.FinishedAt = &now
		row.UpdatedAt = now
		row.Version++
		return tx.Save(&row).Error
	})
	if err != nil {
		return model.BackgroundTask{}, err
	}
	return backgroundTaskFromRow(row), nil
}

func (s *MySQLBackgroundRepository) Collect() ([]model.BackgroundTask, error) {
	var result []model.BackgroundTask
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var rows []model.DBBackgroundTask
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("status IN ? AND notified_at IS NULL", []string{
				string(model.BackgroundTaskCompleted),
				string(model.BackgroundTaskFailed),
				string(model.BackgroundTaskCancelled),
			}).Order("created_at asc").Find(&rows).Error; err != nil {
			return err
		}
		result = make([]model.BackgroundTask, 0, len(rows))
		for _, row := range rows {
			now := time.Now().UTC()
			if err := tx.Model(&row).Updates(map[string]any{
				"notified_at": now,
				"updated_at":  now,
				"version":     gorm.Expr("version + 1"),
			}).Error; err != nil {
				return err
			}
			row.NotifiedAt = &now
			result = append(result, backgroundTaskFromRow(row))
		}
		return nil
	})
	return result, err
}

func (s *MySQLBackgroundRepository) recover() error {
	now := time.Now().UTC()
	return s.db.Model(&model.DBBackgroundTask{}).
		Where("status IN ?", []string{
			string(model.BackgroundTaskPending),
			string(model.BackgroundTaskRunning),
		}).Updates(map[string]any{
		"status":        model.BackgroundTaskFailed,
		"error_message": "服务重启，后台 Runner 未恢复",
		"finished_at":   now,
		"updated_at":    now,
		"version":       gorm.Expr("version + 1"),
	}).Error
}

func (s *MySQLBackgroundRepository) run(taskID string, ctx context.Context, runner model.BackgroundRunner) {
	result, runErr := runner(ctx)
	now := time.Now().UTC()
	updates := map[string]any{
		"finished_at": now,
		"updated_at":  now,
		"lease_owner": "",
		"lease_until": nil,
		"version":     gorm.Expr("version + 1"),
	}
	if runErr != nil {
		updates["status"] = model.BackgroundTaskFailed
		updates["error_message"] = runErr.Error()
	} else {
		updates["status"] = model.BackgroundTaskCompleted
		updates["result"] = truncateBackgroundResult(result, 4000)
	}
	_ = s.db.Model(&model.DBBackgroundTask{}).
		Where("id = ? AND status = ? AND lease_owner = ?", taskID, model.BackgroundTaskRunning, s.worker).
		Updates(updates).Error
	s.mu.Lock()
	delete(s.cancels, taskID)
	s.mu.Unlock()
}

func backgroundTaskFromRow(row model.DBBackgroundTask) model.BackgroundTask {
	return model.BackgroundTask{
		ID:         row.ID,
		Subject:    row.Subject,
		Status:     model.BackgroundTaskStatus(row.Status),
		Result:     row.Result,
		Error:      row.ErrorMessage,
		CreatedAt:  row.CreatedAt,
		StartedAt:  row.StartedAt,
		FinishedAt: row.FinishedAt,
		NotifiedAt: row.NotifiedAt,
	}
}

func truncateBackgroundResult(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}
