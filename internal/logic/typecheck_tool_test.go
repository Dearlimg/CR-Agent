package logic

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"CR-Agent/internal/dao"
	"CR-Agent/internal/model"
)

func TestGoTypecheckUsesCompileOnlyCommand(t *testing.T) {
	command := goTypecheckCommand()
	if command.Name != "Go typecheck" || !reflect.DeepEqual(command.Args, []string{"go", "build", "./..."}) {
		t.Fatalf("typecheck command=%#v", command)
	}
}

func TestGoTypecheckRequiresModuleAndConfiguredSandbox(t *testing.T) {
	moduleArchive := makeTypecheckArchive(t, map[string]string{
		"go.mod":  "module example.com/review\n\ngo 1.22\n",
		"main.go": "package main\nfunc main() {}\n",
	})
	result := runE2BGoTypecheck(context.Background(), Config{}, moduleArchive)
	if result.Status != "not_run" || !strings.Contains(result.Message, "E2B_API_KEY") {
		t.Fatalf("without sandbox credentials result=%#v", result)
	}

	noModule := makeTypecheckArchive(t, map[string]string{"main.go": "package main\n"})
	result = runE2BGoTypecheck(context.Background(), Config{E2BAPIKey: "test-only"}, noModule)
	if result.Status != "not_run" || !strings.Contains(result.Message, "go.mod") {
		t.Fatalf("without Go module result=%#v", result)
	}
}

func TestHarnessRegistersTypecheckWithSandboxPermission(t *testing.T) {
	root := t.TempDir()
	service := NewService(dao.NewJobStore(filepath.Join(root, "jobs")), Config{
		SkillsDir: "../../skills", MemoryDir: filepath.Join(root, "memory"),
		TasksDir: filepath.Join(root, "tasks"), BackgroundTasksDir: filepath.Join(root, "background"),
		TeamMailboxDir: filepath.Join(root, "team"), CronFile: filepath.Join(root, "cron.json"),
		E2BAPIKey: "configured-for-test",
	})
	job := &model.ReviewJob{ID: "typecheck-tool", Source: "inline", Trace: []model.TraceEvent{}}
	ctx, flush := service.withReviewHarness(context.Background(), job, "diff")
	defer flush()
	harness := newReviewHarness()
	ctx.Value(harnessSetupKey{}).(func(*ReviewHarness))(harness)
	definition, ok := harness.tools.Get("typecheck")
	if !ok {
		t.Fatal("typecheck was not registered in the model tool registry")
	}
	if definition.Permission != PermissionSandboxExec || definition.InputSchema["type"] != "object" {
		t.Fatalf("typecheck metadata=%#v", definition.ToolMetadata)
	}
	result, err := definition.Run(ctx, ToolInput{Args: map[string]any{}})
	if err != nil {
		t.Fatalf("run typecheck without a PR snapshot: %v", err)
	}
	var output goTypecheckToolResult
	if err := json.Unmarshal([]byte(result.Output), &output); err != nil {
		t.Fatalf("decode typecheck output %q: %v", result.Output, err)
	}
	if output.Status != "not_run" || !strings.Contains(output.Message, "PR 固定 head") {
		t.Fatalf("typecheck without a PR snapshot=%#v", output)
	}
}

func makeTypecheckArchive(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	for name, content := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return archive.Bytes()
}
