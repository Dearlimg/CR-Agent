package logic

import (
	"CR-Agent/internal/model"
	"context"
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

var backgroundTaskIDPattern = regexp.MustCompile(`^bg_[0-9a-f]{8}$`)

type BackgroundRunner = model.BackgroundRunner

// BackgroundManager registers short metadata synchronously and runs only
// server-registered functions asynchronously. It deliberately does not accept
// arbitrary shell commands from the HTTP API.
type BackgroundManager struct {
	root    string
	mu      sync.Mutex
	cancels map[string]context.CancelFunc
}

func NewBackgroundManager(root string) *BackgroundManager {
	if strings.TrimSpace(root) == "" {
		root = ".background-tasks"
	}
	manager := &BackgroundManager{root: root, cancels: map[string]context.CancelFunc{}}
	_ = manager.Recover()
	return manager
}

func (m *BackgroundManager) Create(subject string) (model.BackgroundTask, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if strings.TrimSpace(subject) == "" {
		return model.BackgroundTask{}, fmt.Errorf("后台任务主题不能为空")
	}
	if err := os.MkdirAll(m.root, 0755); err != nil {
		return model.BackgroundTask{}, fmt.Errorf("创建后台任务目录: %w", err)
	}
	for range 10 {
		id, err := newBackgroundTaskID()
		if err != nil {
			return model.BackgroundTask{}, err
		}
		if _, err := os.Stat(m.taskPath(id)); !os.IsNotExist(err) {
			continue
		}
		task := model.BackgroundTask{ID: id, Subject: strings.TrimSpace(subject), Status: model.BackgroundTaskPending, CreatedAt: time.Now().UTC()}
		if err := m.writeTask(task); err != nil {
			return model.BackgroundTask{}, err
		}
		return task, nil
	}
	return model.BackgroundTask{}, fmt.Errorf("生成唯一后台任务 ID 失败")
}

func (m *BackgroundManager) Launch(taskID string, runner BackgroundRunner) error {
	m.mu.Lock()
	task, err := m.readTask(taskID)
	if err != nil {
		m.mu.Unlock()
		return err
	}
	if task.Status != model.BackgroundTaskPending {
		m.mu.Unlock()
		return fmt.Errorf("后台任务 %s 为 %s，不能启动", taskID, task.Status)
	}
	startedAt := time.Now().UTC()
	task.Status = model.BackgroundTaskRunning
	task.StartedAt = &startedAt
	if err := m.writeTask(task); err != nil {
		m.mu.Unlock()
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.cancels[taskID] = cancel
	m.mu.Unlock()

	go m.run(taskID, ctx, runner)
	return nil
}

func (m *BackgroundManager) Get(taskID string) (model.BackgroundTask, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.readTask(taskID)
}

func (m *BackgroundManager) List() ([]model.BackgroundTask, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.listTasks()
}

// IsIdle reports whether a scheduled review may start without overlapping an
// existing server-registered background runner.
func (m *BackgroundManager) IsIdle() (bool, error) {
	tasks, err := m.List()
	if err != nil {
		return false, err
	}
	for _, task := range tasks {
		if task.Status == model.BackgroundTaskPending || task.Status == model.BackgroundTaskRunning {
			return false, nil
		}
	}
	return true, nil
}

func (m *BackgroundManager) Cancel(taskID string) (model.BackgroundTask, error) {
	m.mu.Lock()
	task, err := m.readTask(taskID)
	if err != nil {
		m.mu.Unlock()
		return model.BackgroundTask{}, err
	}
	if task.Status != model.BackgroundTaskRunning && task.Status != model.BackgroundTaskPending {
		m.mu.Unlock()
		return model.BackgroundTask{}, fmt.Errorf("后台任务 %s 已结束，不能取消", taskID)
	}
	if cancel, ok := m.cancels[taskID]; ok {
		cancel()
	}
	finishedAt := time.Now().UTC()
	task.Status = model.BackgroundTaskCancelled
	task.FinishedAt = &finishedAt
	task.Result = "任务已取消"
	if err := m.writeTask(task); err != nil {
		m.mu.Unlock()
		return model.BackgroundTask{}, err
	}
	m.mu.Unlock()
	return task, nil
}

// Collect returns each completed notification at most once, independently of
// the original start call, mirroring a later Agent-loop notification.
func (m *BackgroundManager) Collect() ([]model.BackgroundTask, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	tasks, err := m.listTasks()
	if err != nil {
		return nil, err
	}
	notifications := []model.BackgroundTask{}
	for _, task := range tasks {
		if !isTerminalBackgroundStatus(task.Status) || task.NotifiedAt != nil {
			continue
		}
		now := time.Now().UTC()
		task.NotifiedAt = &now
		if err := m.writeTask(task); err != nil {
			return nil, err
		}
		notifications = append(notifications, task)
	}
	return notifications, nil
}

// Recover marks in-flight work failed after a process restart; the function
// cannot safely resume an in-memory runner it no longer owns.
func (m *BackgroundManager) Recover() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	tasks, err := m.listTasks()
	if err != nil {
		return err
	}
	for _, task := range tasks {
		if task.Status != model.BackgroundTaskPending && task.Status != model.BackgroundTaskRunning {
			continue
		}
		finishedAt := time.Now().UTC()
		task.Status = model.BackgroundTaskFailed
		task.Error = "服务重启，后台 Runner 未恢复"
		task.FinishedAt = &finishedAt
		if err := m.writeTask(task); err != nil {
			return err
		}
	}
	return nil
}

func (m *BackgroundManager) run(taskID string, ctx context.Context, runner BackgroundRunner) {
	result, runErr := runner(ctx)
	m.mu.Lock()
	defer m.mu.Unlock()
	task, err := m.readTask(taskID)
	if err != nil || task.Status == model.BackgroundTaskCancelled {
		delete(m.cancels, taskID)
		return
	}
	finishedAt := time.Now().UTC()
	task.FinishedAt = &finishedAt
	if runErr != nil {
		task.Status = model.BackgroundTaskFailed
		task.Error = redact(runErr.Error())
	} else {
		task.Status = model.BackgroundTaskCompleted
		task.Result = redact(truncateRunes(result, 4000))
	}
	_ = m.writeTask(task)
	delete(m.cancels, taskID)
}

func (m *BackgroundManager) readTask(taskID string) (model.BackgroundTask, error) {
	if !backgroundTaskIDPattern.MatchString(taskID) {
		return model.BackgroundTask{}, fmt.Errorf("无效后台任务 ID: %q", taskID)
	}
	content, err := os.ReadFile(m.taskPath(taskID))
	if err != nil {
		return model.BackgroundTask{}, fmt.Errorf("读取后台任务 %s: %w", taskID, err)
	}
	var task model.BackgroundTask
	if err := json.Unmarshal(content, &task); err != nil {
		return model.BackgroundTask{}, fmt.Errorf("解析后台任务 %s: %w", taskID, err)
	}
	if task.ID != taskID || !validBackgroundStatus(task.Status) {
		return model.BackgroundTask{}, fmt.Errorf("后台任务 %s 内容无效", taskID)
	}
	return task, nil
}

func (m *BackgroundManager) listTasks() ([]model.BackgroundTask, error) {
	if err := os.MkdirAll(m.root, 0755); err != nil {
		return nil, fmt.Errorf("创建后台任务目录: %w", err)
	}
	entries, err := os.ReadDir(m.root)
	if err != nil {
		return nil, fmt.Errorf("读取后台任务目录: %w", err)
	}
	tasks := []model.BackgroundTask{}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".json")
		if !backgroundTaskIDPattern.MatchString(id) {
			continue
		}
		task, err := m.readTask(id)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].CreatedAt.Before(tasks[j].CreatedAt) })
	return tasks, nil
}

func (m *BackgroundManager) writeTask(task model.BackgroundTask) error {
	content, err := json.MarshalIndent(task, "", "  ")
	if err != nil {
		return fmt.Errorf("编码后台任务 %s: %w", task.ID, err)
	}
	if err := os.WriteFile(m.taskPath(task.ID), content, 0600); err != nil {
		return fmt.Errorf("写入后台任务 %s: %w", task.ID, err)
	}
	return nil
}

func (m *BackgroundManager) taskPath(taskID string) string {
	return filepath.Join(m.root, taskID+".json")
}

func newBackgroundTaskID() (string, error) {
	bytes := make([]byte, 4)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("生成后台任务 ID: %w", err)
	}
	return "bg_" + hex.EncodeToString(bytes), nil
}

func validBackgroundStatus(status model.BackgroundTaskStatus) bool {
	return status == model.BackgroundTaskPending || status == model.BackgroundTaskRunning || status == model.BackgroundTaskCompleted || status == model.BackgroundTaskFailed || status == model.BackgroundTaskCancelled
}

func isTerminalBackgroundStatus(status model.BackgroundTaskStatus) bool {
	return status == model.BackgroundTaskCompleted || status == model.BackgroundTaskFailed || status == model.BackgroundTaskCancelled
}
