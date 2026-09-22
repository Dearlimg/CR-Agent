package logic

import (
	"CR-Agent/internal/dao"
	"CR-Agent/internal/model"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTaskStoreBlocksClaimsAndUnlocksDependencies(t *testing.T) {
	store := NewTaskStore(t.TempDir())
	schema, err := store.Create("创建审查表", "保存审查任务")
	if err != nil {
		t.Fatal(err)
	}
	api, err := store.Create("实现审查 API", "依赖任务表")
	if err != nil {
		t.Fatal(err)
	}
	tests, err := store.Create("编写 API 测试", "依赖 API")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AddDependencies(api.ID, []string{schema.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AddDependencies(tests.ID, []string{api.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Claim(api.ID, "api-agent"); err == nil {
		t.Fatal("blocked task was claimed")
	}
	if _, err := store.Claim(schema.ID, "schema-agent"); err != nil {
		t.Fatal(err)
	}
	_, unblocked, err := store.Complete(schema.ID, "schema-agent")
	if err != nil {
		t.Fatal(err)
	}
	if len(unblocked) != 1 || unblocked[0].ID != api.ID {
		t.Fatalf("unblocked = %#v", unblocked)
	}
	if _, err := store.Claim(api.ID, "api-agent"); err != nil {
		t.Fatal(err)
	}
	_, unblocked, err = store.Complete(api.ID, "api-agent")
	if err != nil {
		t.Fatal(err)
	}
	if len(unblocked) != 1 || unblocked[0].ID != tests.ID {
		t.Fatalf("unblocked = %#v", unblocked)
	}

	reopenedStore := NewTaskStore(store.root)
	persisted, err := reopenedStore.Get(schema.ID)
	if err != nil || persisted.Status != model.TaskCompleted {
		t.Fatalf("persisted = %#v, err = %v", persisted, err)
	}
}

func TestTaskStoreRejectsCycles(t *testing.T) {
	store := NewTaskStore(t.TempDir())
	first, err := store.Create("first", "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Create("second", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AddDependencies(second.ID, []string{first.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AddDependencies(first.ID, []string{second.ID}); err == nil {
		t.Fatal("cyclic dependency was accepted")
	}
}

func TestReviewTaskCompletesWithReviewJob(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "code-review")
	if err := os.Mkdir(skillDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: code-review\ndescription: Review\n---\n# Review"), 0600); err != nil {
		t.Fatal(err)
	}
	service := NewService(dao.NewJobStore(t.TempDir()), Config{SkillsDir: root, TasksDir: filepath.Join(root, "tasks"), MemoryDir: filepath.Join(root, "memory")})
	job, err := service.Create(model.ReviewRequest{Diff: "diff --git a/a.go b/a.go\n+package a"})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		task, getErr := service.TaskStore.Get(job.TaskID)
		if getErr == nil && task.Status == model.TaskCompleted {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	task, getErr := service.TaskStore.Get(job.TaskID)
	t.Fatalf("task did not complete: %#v, err = %v", task, getErr)
}
