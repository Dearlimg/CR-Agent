package logic

import (
	"CR-Agent/internal/dao"
	"CR-Agent/internal/model"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestEinoAdapterSendsDynamicMCPToolsOverHTTP(t *testing.T) {
	round := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		round++
		var request struct {
			Tools []struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tools"`
			Messages []struct {
				Role       string `json:"role"`
				Content    string `json:"content"`
				ToolCallID string `json:"tool_call_id"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			http.Error(w, "bad request", 400)
			return
		}
		message := map[string]any{"role": "assistant", "content": "[]"}
		finish := "stop"
		if round == 1 {
			message["tool_calls"] = []schema.ToolCall{testToolCall("connect", "connect_mcp", `{"name":"docs"}`)}
			finish = "tool_calls"
		}
		if round == 2 {
			found := false
			for _, tool := range request.Tools {
				if tool.Function.Name == "mcp__docs__get_version" {
					found = true
				}
			}
			if !found {
				t.Error("dynamic tool missing in HTTP request")
			}
			message["tool_calls"] = []schema.ToolCall{testToolCall("version", "mcp__docs__get_version", `{}`)}
			finish = "tool_calls"
		}
		if round == 3 {
			last := request.Messages[len(request.Messages)-1]
			if last.ToolCallID != "version" || last.Content != "docs-api v1" {
				t.Errorf("last=%#v", last)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "test", "object": "chat.completion",
			"choices": []any{map[string]any{"index": 0, "message": message, "finish_reason": finish}},
		})
	}))
	defer server.Close()
	ctx := context.WithValue(context.Background(), harnessSetupKey{}, func(*ReviewHarness) {})
	answer, err := EinoReviewAgent(ctx, Config{DeepSeekAPIKey: "test-only", DeepSeekBaseURL: server.URL}, "test")
	if err != nil || answer != "[]" || round != 3 {
		t.Fatalf("answer=%q rounds=%d err=%v", answer, round, err)
	}
}

func TestServiceHarnessBackgroundCompletionWakesModelAndRecordsTrace(t *testing.T) {
	root := t.TempDir()
	s := NewService(dao.NewJobStore(filepath.Join(root, "jobs")), Config{
		SkillsDir: "../../skills", MemoryDir: filepath.Join(root, "memory"), TasksDir: filepath.Join(root, "tasks"),
		BackgroundTasksDir: filepath.Join(root, "background"), TeamMailboxDir: filepath.Join(root, "team"), CronFile: filepath.Join(root, "cron.json"),
	})
	job := &model.ReviewJob{ID: "test-job", Trace: []model.TraceEvent{}}
	ctx, flush := s.withReviewHarness(context.Background(), job, "diff --git a/a.go b/a.go\n+package a")
	h := newReviewHarness()
	ctx.Value(harnessSetupKey{}).(func(*ReviewHarness))(h)
	round := 0
	notified := false
	h.Model = func(_ context.Context, messages []*schema.Message, _ []*schema.ToolInfo) (*schema.Message, error) {
		round++
		if round == 1 {
			return &schema.Message{Role: schema.Assistant, ToolCalls: []schema.ToolCall{testToolCall("bg", "background_check", `{"check":"syntax_check"}`)}}, nil
		}
		for _, message := range messages {
			if strings.Contains(message.Content, "<task_notification>") {
				notified = true
			}
		}
		return &schema.Message{Role: schema.Assistant, Content: "done"}, nil
	}
	if _, err := h.Run(ctx, "review"); err != nil {
		t.Fatal(err)
	}
	flush()
	if !notified || len(job.Trace) < 2 {
		t.Fatalf("notified=%v trace=%#v", notified, job.Trace)
	}
}

func testToolCall(id, name, args string) schema.ToolCall {
	return schema.ToolCall{ID: id, Type: "function", Function: schema.FunctionCall{Name: name, Arguments: args}}
}

func TestHarnessDiscoversMCPOnNextRoundAndReturnsPairedErrors(t *testing.T) {
	h := newReviewHarness()
	round := 0
	h.Model = func(_ context.Context, messages []*schema.Message, tools []*schema.ToolInfo) (*schema.Message, error) {
		round++
		found := false
		for _, tool := range tools {
			if tool.Name == "mcp__docs__search" {
				found = true
			}
		}
		switch round {
		case 1:
			if found {
				t.Fatal("MCP exposed before connect")
			}
			return &schema.Message{Role: schema.Assistant, ToolCalls: []schema.ToolCall{testToolCall("connect", "connect_mcp", `{"name":"docs"}`)}}, nil
		case 2:
			if !found {
				t.Fatal("MCP absent after connect")
			}
			return &schema.Message{Role: schema.Assistant, ToolCalls: []schema.ToolCall{
				testToolCall("bad", "mcp__docs__search", `{`), testToolCall("good", "mcp__docs__get_version", `{}`),
			}}, nil
		case 3:
			last := messages[len(messages)-2:]
			if last[0].ToolCallID != "bad" || !strings.Contains(last[0].Content, "Tool error:") {
				t.Fatalf("bad result: %#v", last[0])
			}
			if last[1].ToolCallID != "good" || last[1].Content != "docs-api v1" {
				t.Fatalf("good result: %#v", last[1])
			}
			return &schema.Message{Role: schema.Assistant, Content: "done"}, nil
		}
		return nil, errors.New("unexpected round")
	}
	answer, err := h.Run(context.Background(), "test")
	if err != nil || answer != "done" {
		t.Fatalf("%q %v", answer, err)
	}
}

func TestHarnessPreHookCanBlockWithoutExecutingAndStopAlwaysRuns(t *testing.T) {
	h := newReviewHarness()
	executed, stopped := false, false
	h.add("blocked", "test", map[string]any{"type": "object"}, func(context.Context, map[string]any) (string, error) { executed = true; return "oops", nil })
	h.Hooks.Register(HookPreToolUse, func(_ context.Context, _ HookEvent, p HookContext) *HookContext {
		p.Error = errors.New("blocked by host")
		return &p
	})
	h.Hooks.Register(HookLoopStop, func(_ context.Context, _ HookEvent, p HookContext) *HookContext { stopped = true; return &p })
	round := 0
	h.Model = func(_ context.Context, messages []*schema.Message, _ []*schema.ToolInfo) (*schema.Message, error) {
		round++
		if round == 1 {
			return &schema.Message{Role: schema.Assistant, ToolCalls: []schema.ToolCall{testToolCall("id", "blocked", "{}")}}, nil
		}
		if !strings.Contains(messages[len(messages)-1].Content, "blocked by host") {
			t.Fatal("blocked result missing")
		}
		return nil, errors.New("provider failed")
	}
	if _, err := h.Run(context.Background(), "test"); err == nil {
		t.Fatal("expected provider failure")
	}
	if executed || !stopped {
		t.Fatalf("executed=%v stopped=%v", executed, stopped)
	}
}

func TestHarnessRetryDoesNotReplaySuccessfulTool(t *testing.T) {
	h := newReviewHarness()
	executions, calls := 0, 0
	h.add("count", "test", map[string]any{"type": "object"}, func(context.Context, map[string]any) (string, error) { executions++; return "ok", nil })
	h.Model = func(ctx context.Context, _ []*schema.Message, _ []*schema.ToolInfo) (*schema.Message, error) {
		return retryHarnessInference(ctx, Config{ModelMaxRetries: 1, ModelRetryBaseMs: 1}, func() (*schema.Message, error) {
			calls++
			switch calls {
			case 1:
				return &schema.Message{Role: schema.Assistant, ToolCalls: []schema.ToolCall{testToolCall("id", "count", "{}")}}, nil
			case 2:
				return nil, errors.New("HTTP 429")
			default:
				return &schema.Message{Role: schema.Assistant, Content: "done"}, nil
			}
		})
	}
	if _, err := h.Run(context.Background(), "test"); err != nil {
		t.Fatal(err)
	}
	if executions != 1 || calls != 3 {
		t.Fatalf("executions=%d calls=%d", executions, calls)
	}
}

func TestMCPHostDenialCannotBeOverriddenByGenericPermission(t *testing.T) {
	h := newReviewHarness()
	h.Policy.grants[PermissionRepositoryExec] = PermissionAllow
	h.MCP.policy = NewMCPHostPolicy(map[MCPToolKey]PermissionDecision{{Server: "deploy", Tool: "trigger"}: PermissionDeny})
	if _, err := h.MCP.Connect(context.Background(), "deploy"); err != nil {
		t.Fatal(err)
	}
	_, tools, err := h.pool()
	if err != nil {
		t.Fatal(err)
	}
	result := h.execute(context.Background(), testToolCall("id", "mcp__deploy__trigger", `{"service":"web"}`), tools)
	if !strings.Contains(result, "deny") || strings.Contains(result, "queued") {
		t.Fatal(result)
	}
}

func TestHarnessCompactionKeepsMultiToolRoundAndLimitsArchiveAccess(t *testing.T) {
	h := newReviewHarness()
	root := t.TempDir()
	h.Compactor = NewContextCompactor(Config{
		ContextOutputDir:     filepath.Join(root, "results"),
		ContextTranscriptDir: filepath.Join(root, "history"), ContextMaxMessages: 4,
	})
	messages := []*schema.Message{
		{Role: schema.User, Content: "request"},
		{Role: schema.Assistant, Content: "old"},
		{Role: schema.User, Content: "continue"},
		{Role: schema.Assistant, ToolCalls: []schema.ToolCall{testToolCall("a", "first", "{}"), testToolCall("b", "second", "{}")}},
		{Role: schema.Tool, ToolCallID: "a", Content: "one"},
		{Role: schema.Tool, ToolCallID: "b", Content: "two"},
	}
	compacted, err := h.compact(context.Background(), messages)
	if err != nil {
		t.Fatal(err)
	}
	if len(compacted) != 5 || len(compacted[2].ToolCalls) != 2 || compacted[3].ToolCallID != "a" || compacted[4].ToolCallID != "b" {
		t.Fatalf("lost pair: %#v", compacted)
	}
	_, tools, err := h.pool()
	if err != nil {
		t.Fatal(err)
	}
	result := h.execute(context.Background(), testToolCall("read", "read_archive", `{"reference":"../../.env"}`), tools)
	if !strings.Contains(result, "未知或其他会话") {
		t.Fatal(result)
	}
}
