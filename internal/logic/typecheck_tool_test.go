package logic

import (
	"CR-Agent/internal/dao"
	"CR-Agent/internal/model"
	"context"
	"path/filepath"
	"testing"
)

func TestHarnessDoesNotRegisterSandboxTypecheck(t *testing.T) {
	root := t.TempDir()
	service := NewService(dao.NewJobStore(filepath.Join(root, "jobs")), Config{
		SkillsDir: "../../skills", MemoryDir: filepath.Join(root, "memory"),
		TasksDir: filepath.Join(root, "tasks"), BackgroundTasksDir: filepath.Join(root, "background"),
		TeamMailboxDir: filepath.Join(root, "team"), CronFile: filepath.Join(root, "cron.json"),
	})
	job := &model.ReviewJob{ID: "typecheck-tool", Source: "inline", Trace: []model.TraceEvent{}}
	ctx, flush := service.withReviewHarness(context.Background(), job, "diff")
	defer flush()
	harness := newReviewHarness()
	ctx.Value(harnessSetupKey{}).(func(*ReviewHarness))(harness)
	if _, ok := harness.tools.Get("typecheck"); ok {
		t.Fatal("sandbox typecheck must stay disabled in review tools")
	}
}
