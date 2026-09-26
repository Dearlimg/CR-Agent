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

func TestHarnessKeepsModelJSONToolAfterActionToolBudgetIsUsed(t *testing.T) {
	harness := newReviewHarness()
	harness.Policy = DefaultPermissionPolicy()
	harness.MaxToolRounds = 1
	harness.MaxRounds = 4
	harness.add("action_probe", "test action", objectSchema("name"), func(_ context.Context, args map[string]any) (string, error) {
		return args["name"].(string), nil
	})
	validated := ""
	spec := modelJSONToolSpec{
		Name:        parseGoalDecisionJSONTool,
		Description: "validate test JSON",
		Validate: func(raw string) (string, error) {
			normalized, _, err := normalizeModelJSON(raw, nil)
			return normalized, err
		},
	}
	registerModelJSONTool(harness, spec, func(normalized string) { validated = normalized })
	turn := 0
	harness.Model = func(_ context.Context, _ []*schema.Message, tools []*schema.ToolInfo) (*schema.Message, error) {
		turn++
		switch turn {
		case 1:
			if !hasToolInfo(tools, "action_probe") || !hasToolInfo(tools, spec.Name) {
				t.Fatalf("first turn tools=%v", toolNames(tools))
			}
			return &schema.Message{Role: schema.Assistant, ToolCalls: []schema.ToolCall{testToolCall("action", "action_probe", `{"name":"done"}`)}}, nil
		case 2:
			if len(tools) != 1 || !hasToolInfo(tools, spec.Name) {
				t.Fatalf("tools after action budget exhausted=%v", toolNames(tools))
			}
			return &schema.Message{Role: schema.Assistant, ToolCalls: []schema.ToolCall{
				testToolCall("json", spec.Name, `{"json":"{\"ok\":true}"}`),
			}}, nil
		default:
			return &schema.Message{Role: schema.Assistant, Content: "done"}, nil
		}
	}

	answer, err := harness.Run(context.Background(), "start")
	if err != nil || answer != "done" || turn != 3 || validated != `{"ok":true}` {
		t.Fatalf("answer=%q turns=%d validated=%q err=%v", answer, turn, validated, err)
	}
}

func TestWorkflowAgentRepairsOutputWithJSONValidationTool(t *testing.T) {
	harness := newReviewHarness()
	turn := 0
	harness.Model = func(_ context.Context, messages []*schema.Message, tools []*schema.ToolInfo) (*schema.Message, error) {
		turn++
		if len(tools) != 1 || tools[0].Name != validateWorkflowJSONTool {
			t.Fatalf("workflow JSON tools=%v", toolNames(tools))
		}
		usage := &schema.ResponseMeta{Usage: &schema.TokenUsage{TotalTokens: 4}}
		if turn == 1 {
			return &schema.Message{
				Role: schema.Assistant,
				ToolCalls: []schema.ToolCall{
					testToolCall("bad-workflow-json", validateWorkflowJSONTool, `{"json":"not JSON"}`),
				},
				ResponseMeta: usage,
			}, nil
		}
		if len(messages) != 2 || messages[1].Role != schema.User {
			t.Fatalf("repair history contains an unmatched tool call: %#v", messages)
		}
		return &schema.Message{
			Role: schema.Assistant,
			ToolCalls: []schema.ToolCall{
				testToolCall("workflow-json", validateWorkflowJSONTool, `{"json":"{\"ok\":true}"}`),
			},
			ResponseMeta: usage,
		}, nil
	}
	service := &Service{}
	runner := service.workflowAgentRunner(harness)
	result, err := runner(context.Background(), "return ok", map[string]any{
		"type":     "object",
		"required": []string{"ok"},
		"properties": map[string]any{
			"ok": map[string]any{"type": "boolean"},
		},
	}, "test")
	if err != nil {
		t.Fatalf("workflow result error: %v", err)
	}
	value, ok := result.Value.(map[string]any)
	if !ok || value["ok"] != true || result.Tokens != 8 || turn != 2 {
		t.Fatalf("result=%#v turns=%d", result, turn)
	}
}

func hasToolInfo(tools []*schema.ToolInfo, name string) bool {
	for _, tool := range tools {
		if tool != nil && tool.Name == name {
			return true
		}
	}
	return false
}

func toolNames(tools []*schema.ToolInfo) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		if tool != nil {
			names = append(names, tool.Name)
		}
	}
	return names
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
	if h.MaxToolRounds != 8 || h.MaxStalledRounds != 2 {
		t.Fatalf("review tool limits=%d/%d", h.MaxToolRounds, h.MaxStalledRounds)
	}
	if h.System != nil {
		t.Fatal("review setup should not add static request context to system")
	}
	if state := h.State(); state != "" {
		t.Fatalf("empty plan should not enter request: %q", state)
	}
	round := 0
	notified := false
	h.Model = func(_ context.Context, messages []*schema.Message, _ []*schema.ToolInfo) (*schema.Message, error) {
		round++
		if round == 1 {
			return &schema.Message{Role: schema.Assistant, ToolCalls: []schema.ToolCall{testToolCall("bg", "background_check", `{"check":"conflict_marker_check"}`)}}, nil
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

func TestHarnessEnvelopeKeepsDataAndSessionStateOutOfSystem(t *testing.T) {
	h := newReviewHarness()
	h.State = func() string { return "TODO_FROM_TOOL" }
	h.Model = func(_ context.Context, messages []*schema.Message, _ []*schema.ToolInfo) (*schema.Message, error) {
		if len(messages) != 3 || messages[0].Role != schema.System ||
			messages[1].Role != schema.User || messages[2].Role != schema.User {
			t.Fatalf("unexpected message roles: %#v", messages)
		}
		if !strings.Contains(messages[0].Content, "SELECTED_SKILL") ||
			strings.Contains(messages[0].Content, "DIFF_DATA") ||
			strings.Contains(messages[0].Content, "TODO_FROM_TOOL") {
			t.Fatalf("system contains task data: %q", messages[0].Content)
		}
		if !strings.Contains(messages[1].Content, "TODO_FROM_TOOL") ||
			!strings.Contains(messages[2].Content, "DIFF_DATA") {
			t.Fatalf("user data missing: %#v", messages)
		}
		return &schema.Message{Role: schema.Assistant, Content: "[]"}, nil
	}
	answer, err := h.RunEnvelope(withReviewPrompt(context.Background()), PromptEnvelope{
		System: "SELECTED_SKILL",
		User:   "DIFF_DATA",
	})
	if err != nil || answer != "[]" {
		t.Fatalf("answer=%q err=%v", answer, err)
	}
}

func TestEinoConsumesEnvelopeBeforeToolExecution(t *testing.T) {
	round := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		round++
		var request struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if round == 1 {
			if len(request.Messages) < 2 || !strings.Contains(request.Messages[0].Content, "SELECTED_SKILL") ||
				strings.Contains(request.Messages[0].Content, "DIFF_DATA") ||
				request.Messages[1].Role != "user" || !strings.Contains(request.Messages[1].Content, "DIFF_DATA") {
				t.Errorf("provider request mixed prompt roles: %#v", request.Messages)
			}
		}
		message := map[string]any{"role": "assistant", "content": "[]"}
		finish := "stop"
		if round == 1 {
			message["tool_calls"] = []schema.ToolCall{testToolCall("probe", "probe_context", `{}`)}
			finish = "tool_calls"
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"index": 0, "message": message, "finish_reason": finish}},
		})
	}))
	defer server.Close()
	seenTool := false
	ctx := context.WithValue(context.Background(), harnessSetupKey{}, func(h *ReviewHarness) {
		h.add("probe_context", "check prompt scope", map[string]any{"type": "object"},
			func(ctx context.Context, _ map[string]any) (string, error) {
				seenTool = true
				if _, ok := promptEnvelopeFrom(ctx); ok {
					t.Error("nested tool inherited the outer prompt envelope")
				}
				return "ok", nil
			})
	})
	ctx = withPromptEnvelope(ctx, PromptEnvelope{System: "SELECTED_SKILL", User: "DIFF_DATA"})
	answer, err := EinoReviewAgent(ctx, Config{DeepSeekAPIKey: "test-only", DeepSeekBaseURL: server.URL}, "ignored")
	if err != nil || answer != "[]" || !seenTool || round != 2 {
		t.Fatalf("answer=%q err=%v seenTool=%v rounds=%d", answer, err, seenTool, round)
	}
}

func TestHarnessStopsRepeatedFailedToolRoundsWithFinalAnswer(t *testing.T) {
	h := newReviewHarness()
	h.MaxStalledRounds = 2
	h.add("broken", "always fails", map[string]any{"type": "object"}, func(context.Context, map[string]any) (string, error) {
		return "", errors.New("unavailable")
	})
	rounds := 0
	h.Model = func(_ context.Context, messages []*schema.Message, tools []*schema.ToolInfo) (*schema.Message, error) {
		rounds++
		if rounds <= 2 {
			if len(tools) == 0 {
				t.Fatal("tool budget exhausted too early")
			}
			return &schema.Message{Role: schema.Assistant, ToolCalls: []schema.ToolCall{
				testToolCall(string(rune('a'+rounds)), "broken", `{}`),
			}}, nil
		}
		if len(tools) != 0 || !strings.Contains(messages[len(messages)-1].Content, "Tool error:") {
			t.Fatalf("final model turn still has tools or lost errors: tools=%d messages=%#v", len(tools), messages)
		}
		return &schema.Message{Role: schema.Assistant, Content: "[]"}, nil
	}
	answer, err := h.Run(context.Background(), "review")
	if err != nil || answer != "[]" || rounds != 3 {
		t.Fatalf("answer=%q rounds=%d err=%v", answer, rounds, err)
	}
}

func TestHarnessToolRoundCapOnlyWhenConfigured(t *testing.T) {
	h := newReviewHarness()
	if h.MaxToolRounds != 0 || h.MaxStalledRounds != 0 {
		t.Fatal("generic harness unexpectedly has review tool limits")
	}
	h.MaxToolRounds = 2
	h.add("evidence", "read evidence", map[string]any{"type": "object"}, func(context.Context, map[string]any) (string, error) {
		return "fact", nil
	})
	rounds := 0
	h.Model = func(_ context.Context, _ []*schema.Message, tools []*schema.ToolInfo) (*schema.Message, error) {
		rounds++
		switch rounds {
		case 1:
			return &schema.Message{Role: schema.Assistant, ToolCalls: []schema.ToolCall{
				testToolCall("first", "evidence", `{"step":1}`),
			}}, nil
		case 2:
			return &schema.Message{Role: schema.Assistant, ToolCalls: []schema.ToolCall{
				testToolCall("second", "evidence", `{"step":2}`),
			}}, nil
		default:
			if len(tools) != 0 {
				t.Fatalf("tools remain available after configured cap: %d", len(tools))
			}
			return &schema.Message{Role: schema.Assistant, Content: "[]"}, nil
		}
	}
	answer, err := h.Run(context.Background(), "review")
	if err != nil || answer != "[]" || rounds != 3 {
		t.Fatalf("answer=%q rounds=%d err=%v", answer, rounds, err)
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

func TestHarnessGoalBlocksStopUntilIndependentEvaluatorSeesEvidence(t *testing.T) {
	h := newReviewHarness()
	evaluations := 0
	goal, err := NewGoalController("syntax_check 必须成功", GoalEvaluatorFunc(func(_ context.Context, condition string, messages []*schema.Message) (GoalDecision, error) {
		evaluations++
		if condition != "syntax_check 必须成功" {
			t.Fatalf("condition=%q", condition)
		}
		for _, message := range messages {
			if strings.Contains(message.Content, "syntax_check 成功") {
				return GoalDecision{OK: true, Reason: "验证结果已出现"}, nil
			}
		}
		return GoalDecision{Reason: "对话中还没有 syntax_check 成功的结果"}, nil
	}), 3)
	if err != nil {
		t.Fatal(err)
	}
	h.Goal = goal
	rounds := 0
	h.Model = func(_ context.Context, messages []*schema.Message, _ []*schema.ToolInfo) (*schema.Message, error) {
		rounds++
		switch rounds {
		case 1:
			return &schema.Message{Role: schema.Assistant, Content: "我已经完成"}, nil
		case 2:
			if !strings.Contains(messages[len(messages)-1].Content, "goal_feedback") {
				t.Fatalf("goal feedback missing: %#v", messages)
			}
			return &schema.Message{Role: schema.Assistant, Content: "syntax_check 成功"}, nil
		default:
			return nil, errors.New("unexpected round")
		}
	}
	answer, err := h.Run(context.Background(), "review")
	if err != nil || answer != "syntax_check 成功" || rounds != 2 || evaluations != 2 {
		t.Fatalf("answer=%q rounds=%d evaluations=%d err=%v", answer, rounds, evaluations, err)
	}
}

func TestHarnessGoalRefusesFalseCompletionAndPreservesLimit(t *testing.T) {
	h := newReviewHarness()
	goal, err := NewGoalController("tests pass", GoalEvaluatorFunc(func(context.Context, string, []*schema.Message) (GoalDecision, error) {
		return GoalDecision{Reason: "缺少测试退出码"}, nil
	}), 1)
	if err != nil {
		t.Fatal(err)
	}
	h.Goal = goal
	h.Model = func(context.Context, []*schema.Message, []*schema.ToolInfo) (*schema.Message, error) {
		return &schema.Message{Role: schema.Assistant, Content: "done"}, nil
	}
	_, err = h.Run(context.Background(), "review")
	if err == nil || !strings.Contains(err.Error(), "连续阻止结束达到上限") || goal.State.Blocks != 1 {
		t.Fatalf("err=%v goal=%#v", err, goal.State)
	}
}

func TestHarnessGoalStopsWhenEvaluatorMarksGoalImpossible(t *testing.T) {
	h := newReviewHarness()
	goal, err := NewGoalController("deploy", GoalEvaluatorFunc(func(context.Context, string, []*schema.Message) (GoalDecision, error) {
		return GoalDecision{Impossible: true, Reason: "部署权限被拒绝"}, nil
	}), 1)
	if err != nil {
		t.Fatal(err)
	}
	h.Goal = goal
	h.Model = func(context.Context, []*schema.Message, []*schema.ToolInfo) (*schema.Message, error) {
		return &schema.Message{Role: schema.Assistant, Content: "done"}, nil
	}
	_, err = h.Run(context.Background(), "review")
	if err == nil || !strings.Contains(err.Error(), "goal 无法完成：部署权限被拒绝") {
		t.Fatalf("err=%v", err)
	}
}

func TestGoalProgressResetsConsecutiveStopLimit(t *testing.T) {
	goal, err := NewGoalController("tests pass", GoalEvaluatorFunc(func(context.Context, string, []*schema.Message) (GoalDecision, error) {
		return GoalDecision{Reason: "缺少测试退出码"}, nil
	}), 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := goal.EvaluateAfterTurn(context.Background(), nil, false); err != nil {
		t.Fatal(err)
	}
	goal.RecordProgress()
	if _, err := goal.EvaluateAfterTurn(context.Background(), nil, false); err != nil {
		t.Fatalf("progress should reset the consecutive stop limit: %v", err)
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

func TestReviewPromptContextPreservesCodeWhileMaskingCredentialValues(t *testing.T) {
	const secret = "not-a-real-credential-123"
	prompt := "+log.Printf(\"password=%s\", password)\n+password := \"" + secret + "\""
	var received string
	newHarness := func() *ReviewHarness {
		harness := newReviewHarness()
		harness.tools = NewToolRegistry()
		harness.Model = func(_ context.Context, messages []*schema.Message, _ []*schema.ToolInfo) (*schema.Message, error) {
			received = messages[len(messages)-1].Content
			return &schema.Message{Role: schema.Assistant, Content: "done"}, nil
		}
		return harness
	}
	if _, err := newHarness().Run(withReviewPrompt(context.Background()), prompt); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(received, `+log.Printf("password=%s", password)`) || strings.Contains(received, secret) {
		t.Fatalf("review prompt lost code or leaked a literal credential: %q", received)
	}
	if _, err := newHarness().Run(context.Background(), prompt); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(received, `+log.Printf("password=%s", password)`) {
		t.Fatalf("non-review harness must retain conservative redaction: %q", received)
	}
}

func TestReviewHarnessDispatchesNewToolFromSharedMetadata(t *testing.T) {
	h := newReviewHarness()
	var received string
	definition := ToolDefinition{
		ToolMetadata: ToolMetadata{
			Name: "registered_echo", Description: "回显指定文本",
			InputSchema: map[string]any{
				"type": "object", "additionalProperties": false,
				"properties": map[string]any{"value": map[string]any{"type": "string"}},
				"required":   []string{"value"},
			},
			Permission: PermissionReadDiff,
		},
		Run: func(_ context.Context, input ToolInput) (ToolResult, error) {
			value, err := requiredString(input.Args, "value")
			received = value
			return ToolResult{Output: value}, err
		},
	}
	if err := h.RegisterTool(definition); err != nil {
		t.Fatal(err)
	}
	round := 0
	h.Model = func(_ context.Context, messages []*schema.Message, tools []*schema.ToolInfo) (*schema.Message, error) {
		round++
		if round == 1 {
			found := false
			for _, info := range tools {
				if info.Name == definition.Name && info.Desc == definition.Description && info.ParamsOneOf != nil {
					found = true
				}
			}
			if !found {
				t.Fatal("registered metadata was not exposed to the model")
			}
			return &schema.Message{Role: schema.Assistant, ToolCalls: []schema.ToolCall{
				testToolCall("echo-call", definition.Name, `{"value":"typed metadata"}`),
			}}, nil
		}
		last := messages[len(messages)-1]
		if last.Role != schema.Tool || last.ToolCallID != "echo-call" || last.Content != "typed metadata" {
			t.Fatalf("registered handler result=%#v", last)
		}
		return &schema.Message{Role: schema.Assistant, Content: "done"}, nil
	}
	answer, err := h.Run(context.Background(), "try the registered tool")
	if err != nil || answer != "done" || received != "typed metadata" {
		t.Fatalf("answer=%q received=%q err=%v", answer, received, err)
	}
}
