package logic

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudwego/eino/schema"
)

func waitWorkflow(t *testing.T, runtime *WorkflowRuntime, runID string, expected WorkflowStatus) WorkflowTask {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		task, err := runtime.Get(runID)
		if err == nil && task.Status == expected {
			return task
		}
		time.Sleep(5 * time.Millisecond)
	}
	task, err := runtime.Get(runID)
	t.Fatalf("workflow %s did not reach %s: %#v err=%v", runID, expected, task, err)
	return WorkflowTask{}
}

func TestWorkflowMetaValidationAndRegistry(t *testing.T) {
	registry := NewWorkflowRegistry()
	if err := registry.Register(WorkflowMeta{Name: "bad/name", Description: "x"}, func(*WorkflowExecutionState, map[string]any) (any, error) { return nil, nil }); err == nil {
		t.Fatal("unsafe workflow name accepted")
	}
	if err := registry.Register(WorkflowMeta{Name: "review.v1", Description: "review", Phases: []string{"Review"}}, func(*WorkflowExecutionState, map[string]any) (any, error) { return map[string]any{}, nil }); err != nil {
		t.Fatal(err)
	}
	if metas := registry.List(); len(metas) != 1 || metas[0].Name != "review.v1" {
		t.Fatalf("metas=%#v", metas)
	}
}

func TestWorkflowReviewUsesPipelineParallelAndResumesFromJournal(t *testing.T) {
	root := t.TempDir()
	registry := NewWorkflowRegistry()
	if err := registerReviewWorkflow(registry); err != nil {
		t.Fatal(err)
	}
	runtime := NewWorkflowRuntime(root, registry)
	var calls int32
	runner := func(_ context.Context, prompt string, _ map[string]any, _ string) (WorkflowAgentResult, error) {
		atomic.AddInt32(&calls, 1)
		if strings.Contains(prompt, "验证该 finding") {
			return WorkflowAgentResult{Value: map[string]any{"is_real": true, "reason": "confirmed"}}, nil
		}
		return WorkflowAgentResult{Value: map[string]any{"findings": []any{map[string]any{"title": "bug", "body": "bad"}}}}, nil
	}
	launch, err := runtime.Launch(context.Background(), "review-changes", map[string]any{"changes": "diff"}, "", runner)
	if err != nil || !launch.Launched || launch.Task.Status != WorkflowRunning {
		t.Fatalf("launch=%#v err=%v", launch, err)
	}
	task := waitWorkflow(t, runtime, launch.RunID, WorkflowCompleted)
	if task.AgentCount != 6 || atomic.LoadInt32(&calls) != 6 {
		t.Fatalf("task=%#v calls=%d", task, calls)
	}

	before := atomic.LoadInt32(&calls)
	resumed, err := runtime.Launch(context.Background(), "review-changes", map[string]any{"changes": "ignored"}, launch.RunID, func(context.Context, string, map[string]any, string) (WorkflowAgentResult, error) {
		t.Fatal("resume called model after journal cache")
		return WorkflowAgentResult{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	waitWorkflow(t, runtime, resumed.RunID, WorkflowCompleted)
	if atomic.LoadInt32(&calls) != before {
		t.Fatalf("resume replayed agents: %d -> %d", before, calls)
	}
	if _, err := os.Stat(filepath.Join(root, launch.RunID+".journal.jsonl")); err != nil {
		t.Fatal(err)
	}
	if event, err := runtime.Collect(launch.RunID); err != nil || event == "" {
		t.Fatalf("notification=%q err=%v", event, err)
	}
	if event, err := runtime.Collect(launch.RunID); err != nil || event != "" {
		t.Fatalf("notification repeated=%q err=%v", event, err)
	}
}

func TestWorkflowStructuredOutputRetriesOnce(t *testing.T) {
	registry := NewWorkflowRegistry()
	if err := registry.Register(WorkflowMeta{Name: "structured", Description: "test"}, func(state *WorkflowExecutionState, _ map[string]any) (any, error) {
		return state.Agent("return", map[string]any{"type": "object", "required": []any{"ok"}}, "one", "")
	}); err != nil {
		t.Fatal(err)
	}
	runtime := NewWorkflowRuntime(t.TempDir(), registry)
	var attempts int32
	runner := func(context.Context, string, map[string]any, string) (WorkflowAgentResult, error) {
		if atomic.AddInt32(&attempts, 1) == 1 {
			return WorkflowAgentResult{Value: "invalid"}, nil
		}
		return WorkflowAgentResult{Value: map[string]any{"ok": true}}, nil
	}
	launch, err := runtime.Launch(context.Background(), "structured", nil, "", runner)
	if err != nil {
		t.Fatal(err)
	}
	task := waitWorkflow(t, runtime, launch.RunID, WorkflowCompleted)
	if task.Error != "" || attempts != 2 {
		t.Fatalf("task=%#v attempts=%d", task, attempts)
	}
}

func TestWorkflowStructuredOutputValidatesNestedTypes(t *testing.T) {
	registry := NewWorkflowRegistry()
	if err := registry.Register(WorkflowMeta{Name: "typed", Description: "test"}, func(state *WorkflowExecutionState, _ map[string]any) (any, error) {
		return state.Agent("return", map[string]any{
			"type":     "object",
			"required": []any{"ok", "score", "items"},
			"properties": map[string]any{
				"ok":    map[string]any{"type": "boolean"},
				"score": map[string]any{"type": "number"},
				"items": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			},
		}, "typed", "")
	}); err != nil {
		t.Fatal(err)
	}
	runtime := NewWorkflowRuntime(t.TempDir(), registry)
	var attempts int32
	runner := func(context.Context, string, map[string]any, string) (WorkflowAgentResult, error) {
		if atomic.AddInt32(&attempts, 1) == 1 {
			return WorkflowAgentResult{Value: map[string]any{"ok": "yes", "score": "high", "items": []any{"ok", 1}}}, nil
		}
		return WorkflowAgentResult{Value: map[string]any{"ok": true, "score": 0.9, "items": []any{"ok"}}}, nil
	}
	launch, err := runtime.Launch(context.Background(), "typed", nil, "", runner)
	if err != nil {
		t.Fatal(err)
	}
	task := waitWorkflow(t, runtime, launch.RunID, WorkflowCompleted)
	if task.Error != "" || attempts != 2 {
		t.Fatalf("task=%#v attempts=%d", task, attempts)
	}
}

func TestWorkflowLockAndUnknownInput(t *testing.T) {
	runtime := NewWorkflowRuntime(t.TempDir(), NewWorkflowRegistry())
	if _, err := runtime.Launch(context.Background(), "missing", nil, "", func(context.Context, string, map[string]any, string) (WorkflowAgentResult, error) {
		return WorkflowAgentResult{}, nil
	}); err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("err=%v", err)
	}
	registry := runtime.Registry()
	_ = registry.Register(WorkflowMeta{Name: "slow", Description: "slow"}, func(ctx *WorkflowExecutionState, _ map[string]any) (any, error) {
		<-ctx.ctx.Done()
		return nil, ctx.ctx.Err()
	})
	first, err := runtime.Launch(context.Background(), "slow", nil, "", func(context.Context, string, map[string]any, string) (WorkflowAgentResult, error) {
		return WorkflowAgentResult{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Launch(context.Background(), "slow", nil, first.RunID, func(context.Context, string, map[string]any, string) (WorkflowAgentResult, error) {
		return WorkflowAgentResult{}, nil
	}); err == nil {
		t.Fatal("concurrent resume accepted")
	}
	if err := runtime.Cancel(first.RunID); err != nil {
		t.Fatal(err)
	}
	waitWorkflow(t, runtime, first.RunID, WorkflowCancelled)
}

func TestWorkflowToolLaunchesAndHarnessAwaitsNotification(t *testing.T) {
	registry := NewWorkflowRegistry()
	if err := registerReviewWorkflow(registry); err != nil {
		t.Fatal(err)
	}
	runtime := NewWorkflowRuntime(t.TempDir(), registry)
	harness := newReviewHarness()
	harness.Workflow = runtime
	var pending string
	harness.WorkflowRunner = func(_ context.Context, prompt string, _ map[string]any, _ string) (WorkflowAgentResult, error) {
		if strings.Contains(prompt, "验证该 finding") {
			return WorkflowAgentResult{Value: map[string]any{"is_real": true}}, nil
		}
		return WorkflowAgentResult{Value: map[string]any{"findings": []any{}}}, nil
	}
	harness.WorkflowLaunched = func(runID string) { pending = runID }
	harness.Notify = func() ([]string, error) {
		if pending == "" {
			return []string{}, nil
		}
		event, err := runtime.Collect(pending)
		if event != "" {
			pending = ""
			return []string{event}, err
		}
		return []string{}, err
	}
	harness.Await = func(ctx context.Context) ([]string, error) {
		for pending != "" {
			events, err := harness.Notify()
			if err != nil || len(events) > 0 {
				return events, err
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Millisecond * 5):
			}
		}
		return []string{}, nil
	}
	rounds := 0
	harness.Model = func(_ context.Context, messages []*schema.Message, tools []*schema.ToolInfo) (*schema.Message, error) {
		rounds++
		if rounds == 1 {
			found := false
			for _, tool := range tools {
				if tool.Name == "Workflow" {
					found = true
				}
			}
			if !found {
				t.Fatal("Workflow tool missing")
			}
			return &schema.Message{Role: schema.Assistant, ToolCalls: []schema.ToolCall{testToolCall("workflow", "Workflow", `{"name":"review-changes","args":{"changes":"diff"}}`)}}, nil
		}
		if rounds == 2 {
			return &schema.Message{Role: schema.Assistant, Content: "workflow launched"}, nil
		}
		if len(messages) < 3 || !strings.Contains(messages[len(messages)-1].Content, "task_notification") {
			t.Fatalf("notification missing: %#v", messages)
		}
		return &schema.Message{Role: schema.Assistant, Content: "done"}, nil
	}
	answer, err := harness.Run(context.Background(), "run review")
	if err != nil || answer != "done" || rounds != 3 {
		t.Fatalf("answer=%q rounds=%d err=%v", answer, rounds, err)
	}
}
