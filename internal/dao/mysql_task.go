package dao

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"CR-Agent/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type MySQLTaskRepository struct {
	db *gorm.DB
}

func NewMySQLTaskRepository(db *gorm.DB) *MySQLTaskRepository {
	return &MySQLTaskRepository{db: db}
}

func (s *MySQLTaskRepository) Create(subject, description string) (model.Task, error) {
	if strings.TrimSpace(subject) == "" {
		return model.Task{}, fmt.Errorf("任务主题不能为空")
	}

	id, err := newRuntimeID("task_", 4)
	if err != nil {
		return model.Task{}, err
	}
	now := time.Now().UTC()
	row := model.DBAgentTask{
		ID:          id,
		Subject:     strings.TrimSpace(subject),
		Description: strings.TrimSpace(description),
		Status:      string(model.TaskPending),
		Version:     0,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.db.Create(&row).Error; err != nil {
		return model.Task{}, fmt.Errorf("创建任务: %w", err)
	}
	return taskFromRow(row, []model.DBAgentTaskDependency{}), nil
}

func (s *MySQLTaskRepository) Get(taskID string) (model.Task, error) {
	var row model.DBAgentTask
	if err := s.db.Where("id = ?", taskID).First(&row).Error; err != nil {
		return model.Task{}, fmt.Errorf("读取任务 %s: %w", taskID, err)
	}
	return s.loadTask(row)
}

func (s *MySQLTaskRepository) List() ([]model.Task, error) {
	var rows []model.DBAgentTask
	if err := s.db.Order("created_at asc").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("列出任务: %w", err)
	}
	return s.loadTasks(rows)
}

func (s *MySQLTaskRepository) AddDependencies(taskID string, blockedBy []string) (model.Task, error) {
	var result model.Task
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var row model.DBAgentTask
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", taskID).First(&row).Error; err != nil {
			return fmt.Errorf("读取任务 %s: %w", taskID, err)
		}
		if row.Status != string(model.TaskPending) || row.Owner != "" {
			return fmt.Errorf("任务 %s 已认领或不再 pending，不能修改依赖", taskID)
		}

		rows, err := s.listRows(tx)
		if err != nil {
			return err
		}
		dependencies, err := s.dependenciesFor(tx, taskID)
		if err != nil {
			return err
		}
		known := map[string]bool{}
		graph := map[string][]string{}
		for _, task := range rows {
			known[task.ID] = true
		}
		for _, dependency := range dependencies {
			graph[taskID] = append(graph[taskID], dependency.DependsOnID)
		}
		for _, dependencyID := range blockedBy {
			if dependencyID == taskID {
				return fmt.Errorf("任务不能依赖自身")
			}
			if !known[dependencyID] {
				return fmt.Errorf("依赖任务不存在: %s", dependencyID)
			}
			if !containsID(graph[taskID], dependencyID) {
				graph[taskID] = append(graph[taskID], dependencyID)
			}
		}
		for _, task := range rows {
			if task.ID == taskID {
				continue
			}
			deps, depsErr := s.dependenciesFor(tx, task.ID)
			if depsErr != nil {
				return depsErr
			}
			for _, dependency := range deps {
				graph[task.ID] = append(graph[task.ID], dependency.DependsOnID)
			}
		}
		if graphHasCycle(graph) {
			return fmt.Errorf("添加依赖会形成任务环")
		}

		existing := map[string]bool{}
		for _, dependency := range dependencies {
			existing[dependency.DependsOnID] = true
		}
		for _, dependencyID := range blockedBy {
			if existing[dependencyID] {
				continue
			}
			dependency := model.DBAgentTaskDependency{
				TaskID:      taskID,
				DependsOnID: dependencyID,
				CreatedAt:   time.Now().UTC(),
			}
			if err := tx.Create(&dependency).Error; err != nil {
				return fmt.Errorf("保存任务依赖: %w", err)
			}
		}
		row.Version++
		row.UpdatedAt = time.Now().UTC()
		if err := tx.Model(&row).Updates(map[string]any{
			"version":    row.Version,
			"updated_at": row.UpdatedAt,
		}).Error; err != nil {
			return fmt.Errorf("更新任务依赖版本: %w", err)
		}
		result = taskFromRow(row, append(dependencies, newDependencies(taskID, blockedBy, existing)...))
		return nil
	})
	return result, err
}

func (s *MySQLTaskRepository) Claim(taskID, owner string) (model.Task, error) {
	if strings.TrimSpace(owner) == "" {
		return model.Task{}, fmt.Errorf("任务 owner 不能为空")
	}
	var result model.Task
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var row model.DBAgentTask
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", taskID).First(&row).Error; err != nil {
			return fmt.Errorf("读取任务 %s: %w", taskID, err)
		}
		if row.Status != string(model.TaskPending) {
			return fmt.Errorf("任务 %s 为 %s，不能认领", taskID, row.Status)
		}
		dependencies, err := s.dependenciesFor(tx, taskID)
		if err != nil {
			return err
		}
		incomplete, err := s.incompleteDependencies(tx, dependencies)
		if err != nil {
			return err
		}
		if len(incomplete) > 0 {
			return fmt.Errorf("任务被依赖阻塞: %s", strings.Join(incomplete, ", "))
		}
		now := time.Now().UTC()
		row.Owner = strings.TrimSpace(owner)
		row.Status = string(model.TaskInProgress)
		row.UpdatedAt = now
		row.Version++
		if err := tx.Save(&row).Error; err != nil {
			return fmt.Errorf("认领任务: %w", err)
		}
		result = taskFromRow(row, dependencies)
		return nil
	})
	return result, err
}

func (s *MySQLTaskRepository) Complete(taskID, owner string) (model.Task, []model.Task, error) {
	var result model.Task
	var unblocked []model.Task
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var row model.DBAgentTask
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", taskID).First(&row).Error; err != nil {
			return fmt.Errorf("读取任务 %s: %w", taskID, err)
		}
		if row.Status != string(model.TaskInProgress) || row.Owner != owner {
			return fmt.Errorf("任务 %s 当前 owner 或状态不允许完成", taskID)
		}
		before, err := s.readyTasks(tx)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		row.Status = string(model.TaskCompleted)
		row.UpdatedAt = now
		row.CompletedAt = &now
		row.Version++
		if err := tx.Save(&row).Error; err != nil {
			return fmt.Errorf("完成任务: %w", err)
		}
		after, err := s.readyTasks(tx)
		if err != nil {
			return err
		}
		allTasks, err := s.listRows(tx)
		if err != nil {
			return err
		}
		for _, item := range allTasks {
			if after[item.ID] && !before[item.ID] {
				task, taskErr := s.loadTaskTx(tx, item)
				if taskErr != nil {
					return taskErr
				}
				unblocked = append(unblocked, task)
			}
		}
		result, err = s.loadTaskTx(tx, row)
		return err
	})
	return result, unblocked, err
}

func (s *MySQLTaskRepository) loadTask(row model.DBAgentTask) (model.Task, error) {
	return s.loadTaskTx(s.db, row)
}

func (s *MySQLTaskRepository) loadTaskTx(tx *gorm.DB, row model.DBAgentTask) (model.Task, error) {
	dependencies, err := s.dependenciesFor(tx, row.ID)
	if err != nil {
		return model.Task{}, err
	}
	return taskFromRow(row, dependencies), nil
}

func (s *MySQLTaskRepository) loadTasks(rows []model.DBAgentTask) ([]model.Task, error) {
	if len(rows) == 0 {
		return []model.Task{}, nil
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	dependencies, err := s.dependenciesForTasks(s.db, ids)
	if err != nil {
		return nil, err
	}
	result := make([]model.Task, 0, len(rows))
	for _, row := range rows {
		result = append(result, taskFromRow(row, dependencies[row.ID]))
	}
	return result, nil
}

func (s *MySQLTaskRepository) listRows(tx *gorm.DB) ([]model.DBAgentTask, error) {
	var rows []model.DBAgentTask
	if err := tx.Order("created_at asc").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("读取任务图: %w", err)
	}
	return rows, nil
}

func (s *MySQLTaskRepository) dependenciesFor(tx *gorm.DB, taskID string) ([]model.DBAgentTaskDependency, error) {
	var dependencies []model.DBAgentTaskDependency
	if err := tx.Where("task_id = ?", taskID).Order("created_at asc").Find(&dependencies).Error; err != nil {
		return nil, fmt.Errorf("读取任务依赖: %w", err)
	}
	return dependencies, nil
}

func (s *MySQLTaskRepository) dependenciesForTasks(tx *gorm.DB, taskIDs []string) (map[string][]model.DBAgentTaskDependency, error) {
	dependencies := map[string][]model.DBAgentTaskDependency{}
	if len(taskIDs) == 0 {
		return dependencies, nil
	}
	var rows []model.DBAgentTaskDependency
	if err := tx.Where("task_id IN ?", taskIDs).Order("created_at asc").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("读取任务依赖: %w", err)
	}
	for _, row := range rows {
		dependencies[row.TaskID] = append(dependencies[row.TaskID], row)
	}
	return dependencies, nil
}

func (s *MySQLTaskRepository) incompleteDependencies(tx *gorm.DB, dependencies []model.DBAgentTaskDependency) ([]string, error) {
	incomplete := []string{}
	for _, dependency := range dependencies {
		var task model.DBAgentTask
		if err := tx.Where("id = ?", dependency.DependsOnID).First(&task).Error; err != nil {
			return nil, fmt.Errorf("读取依赖任务 %s: %w", dependency.DependsOnID, err)
		}
		if task.Status != string(model.TaskCompleted) {
			incomplete = append(incomplete, dependency.DependsOnID)
		}
	}
	return incomplete, nil
}

func (s *MySQLTaskRepository) readyTasks(tx *gorm.DB) (map[string]bool, error) {
	rows, err := s.listRows(tx)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(rows))
	statusByID := make(map[string]string, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
		statusByID[row.ID] = row.Status
	}
	dependenciesByTask, err := s.dependenciesForTasks(tx, ids)
	if err != nil {
		return nil, err
	}
	ready := map[string]bool{}
	for _, row := range rows {
		if row.Status != string(model.TaskPending) {
			continue
		}
		dependencies := dependenciesByTask[row.ID]
		if len(dependencies) == 0 {
			continue
		}
		allCompleted := true
		for _, dependency := range dependencies {
			if statusByID[dependency.DependsOnID] != string(model.TaskCompleted) {
				allCompleted = false
				break
			}
		}
		if allCompleted {
			ready[row.ID] = true
		}
	}
	return ready, nil
}

func taskFromRow(row model.DBAgentTask, dependencies []model.DBAgentTaskDependency) model.Task {
	blockedBy := make([]string, 0, len(dependencies))
	for _, dependency := range dependencies {
		blockedBy = append(blockedBy, dependency.DependsOnID)
	}
	return model.Task{
		ID:          row.ID,
		Subject:     row.Subject,
		Description: row.Description,
		Status:      model.TaskStatus(row.Status),
		Owner:       row.Owner,
		BlockedBy:   blockedBy,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}
}

func newDependencies(taskID string, dependencies []string, existing map[string]bool) []model.DBAgentTaskDependency {
	result := []model.DBAgentTaskDependency{}
	for _, dependencyID := range dependencies {
		if existing[dependencyID] {
			continue
		}
		result = append(result, model.DBAgentTaskDependency{TaskID: taskID, DependsOnID: dependencyID})
	}
	return result
}

func containsID(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func graphHasCycle(graph map[string][]string) bool {
	visiting := map[string]bool{}
	visited := map[string]bool{}
	var walk func(string) bool
	walk = func(taskID string) bool {
		if visiting[taskID] {
			return true
		}
		if visited[taskID] {
			return false
		}
		visiting[taskID] = true
		for _, dependencyID := range graph[taskID] {
			if walk(dependencyID) {
				return true
			}
		}
		visiting[taskID] = false
		visited[taskID] = true
		return false
	}
	keys := make([]string, 0, len(graph))
	for key := range graph {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if walk(key) {
			return true
		}
	}
	return false
}
