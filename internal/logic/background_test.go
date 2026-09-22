package logic

import (
	"context"
	"testing"
	"time"

	"CR-Agent/internal/model"
)

func TestBackgroundManagerCompletesAndCollectsOnce(t *testing.T) {
	manager := NewBackgroundManager(t.TempDir())
	task, err := manager.Create("完整测试")
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Launch(task.ID, func(context.Context) (string, error) {
		return "all tests passed", nil
	}); err != nil {
		t.Fatal(err)
	}
	completed := waitForBackgroundTask(t, manager, task.ID, model.BackgroundTaskCompleted)
	if completed.Result != "all tests passed" {
		t.Fatalf("result = %q", completed.Result)
	}
	notifications, err := manager.Collect()
	if err != nil || len(notifications) != 1 || notifications[0].ID != task.ID {
		t.Fatalf("notifications = %#v, err = %v", notifications, err)
	}
	notifications, err = manager.Collect()
	if err != nil || len(notifications) != 0 {
		t.Fatalf("second notifications = %#v, err = %v", notifications, err)
	}
}

func TestBackgroundManagerCancellationAndRecovery(t *testing.T) {
	root := t.TempDir()
	manager := NewBackgroundManager(root)
	running, err := manager.Create("可取消任务")
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	if err := manager.Launch(running.ID, func(ctx context.Context) (string, error) {
		close(started)
		<-ctx.Done()
		return "", ctx.Err()
	}); err != nil {
		t.Fatal(err)
	}
	<-started
	cancelled, err := manager.Cancel(running.ID)
	if err != nil || cancelled.Status != model.BackgroundTaskCancelled {
		t.Fatalf("cancelled = %#v, err = %v", cancelled, err)
	}

	pending, err := manager.Create("重启前待执行")
	if err != nil {
		t.Fatal(err)
	}
	recoveredManager := NewBackgroundManager(root)
	recovered, err := recoveredManager.Get(pending.ID)
	if err != nil || recovered.Status != model.BackgroundTaskFailed {
		t.Fatalf("recovered = %#v, err = %v", recovered, err)
	}
}

func waitForBackgroundTask(t *testing.T, manager *BackgroundManager, taskID string, expected model.BackgroundTaskStatus) model.BackgroundTask {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		task, err := manager.Get(taskID)
		if err == nil && task.Status == expected {
			return task
		}
		time.Sleep(10 * time.Millisecond)
	}
	task, err := manager.Get(taskID)
	t.Fatalf("background task %s did not reach %s: %#v, err = %v", taskID, expected, task, err)
	return model.BackgroundTask{}
}
