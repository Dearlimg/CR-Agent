package logic

import (
	"CR-Agent/internal/model"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

var taskIDPattern = regexp.MustCompile(`^task_[0-9a-f]{8}$`)

type TaskStore struct {
	root string
	mu   sync.Mutex
}

func NewTaskStore(root string) *TaskStore {
	if strings.TrimSpace(root) == "" {
		root = ".tasks"
	}
	return &TaskStore{root: root}
}

func (s *TaskStore) Create(subject, description string) (model.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(subject) == "" {
		return model.Task{}, fmt.Errorf("任务主题不能为空")
	}
	if err := os.MkdirAll(s.root, 0755); err != nil {
		return model.Task{}, fmt.Errorf("创建任务目录: %w", err)
	}
	for range 10 {
		id, err := newTaskID()
		if err != nil {
			return model.Task{}, err
		}
		if _, err := os.Stat(s.taskPath(id)); !os.IsNotExist(err) {
			continue
		}
		now := time.Now().UTC()
		task := model.Task{
			ID:          id,
			Subject:     strings.TrimSpace(subject),
			Description: strings.TrimSpace(description),
			Status:      model.TaskPending,
			BlockedBy:   []string{},
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		if err := s.writeTask(task); err != nil {
			return model.Task{}, err
		}
		return task, nil
	}
	return model.Task{}, fmt.Errorf("生成唯一任务 ID 失败")
}

func (s *TaskStore) Get(taskID string) (model.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readTask(taskID)
}

func (s *TaskStore) List() ([]model.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listTasks()
}

func (s *TaskStore) AddDependencies(taskID string, blockedBy []string) (model.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	task, err := s.readTask(taskID)
	if err != nil {
		return model.Task{}, err
	}
	if task.Status != model.TaskPending || task.Owner != "" {
		return model.Task{}, fmt.Errorf("任务 %s 已认领或不再 pending，不能修改依赖", taskID)
	}
	allTasks, err := s.listTasks()
	if err != nil {
		return model.Task{}, err
	}
	known := map[string]bool{}
	for _, item := range allTasks {
		known[item.ID] = true
	}
	dependencies := append([]string{}, task.BlockedBy...)
	for _, dependencyID := range blockedBy {
		if dependencyID == taskID {
			return model.Task{}, fmt.Errorf("任务不能依赖自身")
		}
		if !known[dependencyID] {
			return model.Task{}, fmt.Errorf("依赖任务不存在: %s", dependencyID)
		}
		if !containsTaskID(dependencies, dependencyID) {
			dependencies = append(dependencies, dependencyID)
		}
	}
	task.BlockedBy = dependencies
	if createsTaskCycle(task, allTasks) {
		return model.Task{}, fmt.Errorf("添加依赖会形成任务环")
	}
	task.UpdatedAt = time.Now().UTC()
	if err := s.writeTask(task); err != nil {
		return model.Task{}, err
	}
	return task, nil
}

func (s *TaskStore) Claim(taskID, owner string) (model.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(owner) == "" {
		return model.Task{}, fmt.Errorf("任务 owner 不能为空")
	}
	task, err := s.readTask(taskID)
	if err != nil {
		return model.Task{}, err
	}
	if task.Status != model.TaskPending {
		return model.Task{}, fmt.Errorf("任务 %s 为 %s，不能认领", taskID, task.Status)
	}
	if dependencies, err := s.incompleteDependencies(task); err != nil {
		return model.Task{}, err
	} else if len(dependencies) > 0 {
		return model.Task{}, fmt.Errorf("任务被依赖阻塞: %s", strings.Join(dependencies, ", "))
	}
	task.Owner = strings.TrimSpace(owner)
	task.Status = model.TaskInProgress
	task.UpdatedAt = time.Now().UTC()
	if err := s.writeTask(task); err != nil {
		return model.Task{}, err
	}
	return task, nil
}

func (s *TaskStore) Complete(taskID, owner string) (model.Task, []model.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	task, err := s.readTask(taskID)
	if err != nil {
		return model.Task{}, nil, err
	}
	if task.Status != model.TaskInProgress || task.Owner != owner {
		return model.Task{}, nil, fmt.Errorf("任务 %s 当前 owner 或状态不允许完成", taskID)
	}
	allTasks, err := s.listTasks()
	if err != nil {
		return model.Task{}, nil, err
	}
	readyBefore, err := s.readyTaskIDs(allTasks)
	if err != nil {
		return model.Task{}, nil, err
	}
	task.Status = model.TaskCompleted
	task.UpdatedAt = time.Now().UTC()
	if err := s.writeTask(task); err != nil {
		return model.Task{}, nil, err
	}
	allTasks, err = s.listTasks()
	if err != nil {
		return model.Task{}, nil, err
	}
	readyAfter, err := s.readyTaskIDs(allTasks)
	if err != nil {
		return model.Task{}, nil, err
	}
	unblocked := []model.Task{}
	for _, item := range allTasks {
		if readyAfter[item.ID] && !readyBefore[item.ID] {
			unblocked = append(unblocked, item)
		}
	}
	return task, unblocked, nil
}

func (s *TaskStore) incompleteDependencies(task model.Task) ([]string, error) {
	incomplete := []string{}
	for _, dependencyID := range task.BlockedBy {
		dependency, err := s.readTask(dependencyID)
		if err != nil || dependency.Status != model.TaskCompleted {
			incomplete = append(incomplete, dependencyID)
		}
	}
	return incomplete, nil
}

func (s *TaskStore) readyTaskIDs(tasks []model.Task) (map[string]bool, error) {
	ready := map[string]bool{}
	for _, task := range tasks {
		if task.Status != model.TaskPending || len(task.BlockedBy) == 0 {
			continue
		}
		incomplete, err := s.incompleteDependencies(task)
		if err != nil {
			return nil, err
		}
		if len(incomplete) == 0 {
			ready[task.ID] = true
		}
	}
	return ready, nil
}

func (s *TaskStore) readTask(taskID string) (model.Task, error) {
	if !taskIDPattern.MatchString(taskID) {
		return model.Task{}, fmt.Errorf("无效任务 ID: %q", taskID)
	}
	content, err := os.ReadFile(s.taskPath(taskID))
	if err != nil {
		return model.Task{}, fmt.Errorf("读取任务 %s: %w", taskID, err)
	}
	var task model.Task
	if err := json.Unmarshal(content, &task); err != nil {
		return model.Task{}, fmt.Errorf("解析任务 %s: %w", taskID, err)
	}
	if task.ID != taskID || !validTaskStatus(task.Status) {
		return model.Task{}, fmt.Errorf("任务 %s 内容无效", taskID)
	}
	if task.BlockedBy == nil {
		task.BlockedBy = []string{}
	}
	return task, nil
}

func (s *TaskStore) listTasks() ([]model.Task, error) {
	if err := os.MkdirAll(s.root, 0755); err != nil {
		return nil, fmt.Errorf("创建任务目录: %w", err)
	}
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return nil, fmt.Errorf("读取任务目录: %w", err)
	}
	tasks := []model.Task{}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".json")
		if !taskIDPattern.MatchString(id) {
			continue
		}
		task, err := s.readTask(id)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].CreatedAt.Before(tasks[j].CreatedAt)
	})
	return tasks, nil
}

func (s *TaskStore) writeTask(task model.Task) error {
	content, err := json.MarshalIndent(task, "", "  ")
	if err != nil {
		return fmt.Errorf("编码任务 %s: %w", task.ID, err)
	}
	path := s.taskPath(task.ID)
	if err := os.WriteFile(path, content, 0600); err != nil {
		return fmt.Errorf("写入任务 %s: %w", task.ID, err)
	}
	return nil
}

func (s *TaskStore) taskPath(taskID string) string {
	return filepath.Join(s.root, taskID+".json")
}

func newTaskID() (string, error) {
	bytes := make([]byte, 4)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("生成任务 ID: %w", err)
	}
	return "task_" + hex.EncodeToString(bytes), nil
}

func validTaskStatus(status model.TaskStatus) bool {
	return status == model.TaskPending || status == model.TaskInProgress || status == model.TaskCompleted
}

func containsTaskID(taskIDs []string, target string) bool {
	for _, taskID := range taskIDs {
		if taskID == target {
			return true
		}
	}
	return false
}

func createsTaskCycle(updated model.Task, tasks []model.Task) bool {
	graph := map[string][]string{updated.ID: updated.BlockedBy}
	for _, task := range tasks {
		if task.ID != updated.ID {
			graph[task.ID] = task.BlockedBy
		}
	}
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
	for taskID := range graph {
		if walk(taskID) {
			return true
		}
	}
	return false
}
