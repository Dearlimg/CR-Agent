package logic

import (
	"CR-Agent/internal/dao"
	"CR-Agent/internal/model"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

type Service struct {
	Store  dao.Store
	Config Config
	Loop   *AgentLoop
}

func NewService(store dao.Store, cfg Config) *Service {
	registry := NewToolRegistry()
	registry.RegisterWithPermission("todo_write", PermissionReadDiff, func(_ context.Context, in ToolInput) (ToolResult, error) {
		return ToolResult{Output: renderTodos(in.Job.Todos)}, nil
	})
	registry.RegisterWithPermission("diff_reader", PermissionReadDiff, func(_ context.Context, in ToolInput) (ToolResult, error) {
		return ToolResult{Output: fmt.Sprintf("读取并脱敏完成，diff_bytes=%d", len(in.Diff))}, nil
	})
	registry.RegisterWithPermission("static_check", PermissionStaticAnalysis, func(_ context.Context, in ToolInput) (ToolResult, error) {
		hits := []string{}
		if strings.Contains(in.Diff, "TODO") {
			hits = append(hits, "TODO")
		}
		if strings.Contains(in.Diff, "panic(") {
			hits = append(hits, "panic")
		}
		return ToolResult{Output: fmt.Sprintf("静态检查完成，命中=%v", hits)}, nil
	})
	registerReviewTools(registry)
	record := func(jobID, traceID, tool, status, input, output, callErr string, durationMs int64) error {
		return store.RecordToolCall(jobID, traceID, tool, status, input, output, callErr, durationMs)
	}
	plan := []LoopStep{{Tool: "todo_write", Reason: "创建并确认审查计划"}, {Tool: "diff_reader", Reason: "读取并脱敏 diff"}, {Tool: "parse_diff", Reason: "解析文件和变更范围"}, {Tool: "get_changed_lines", Reason: "提取新增行"}, {Tool: "static_check", Reason: "执行确定性规则检查"}, {Tool: "syntax_check", Reason: "前置语法和冲突检查"}, {Tool: "format_check", Reason: "前置格式检查"}, {Tool: "secret_scan", Reason: "扫描疑似敏感信息"}, {Tool: "dependency_diff", Reason: "检查依赖文件变更"}, {Tool: "get_file_context", Reason: "补充安全上下文"}, {Tool: "normalize_finding", Reason: "规范化审查输出"}}
	return &Service{Store: store, Config: cfg, Loop: &AgentLoop{Registry: registry, Plan: plan, MaxSteps: len(plan), Record: record, Policy: DefaultPermissionPolicy(), Hooks: NewHookBus()}}
}
func renderTodos(todos []model.TodoItem) string {
	lines := []string{}
	for _, todo := range todos {
		mark := "[ ]"
		if todo.Status == "in_progress" {
			mark = "[>]"
		}
		if todo.Status == "completed" {
			mark = "[x]"
		}
		lines = append(lines, fmt.Sprintf("%s %s", mark, todo.Content))
	}
	return strings.Join(lines, "\n")
}
func id(s string) string {
	h := sha256.Sum256([]byte(s + time.Now().String()))
	return hex.EncodeToString(h[:])[:16]
}
func (s *Service) Create(req model.ReviewRequest) (*model.ReviewJob, error) {
	todos := []model.TodoItem{}
	for order, step := range s.Loop.Plan {
		todos = append(todos, model.TodoItem{Content: step.Reason, Status: "pending", Order: order})
	}
	j := &model.ReviewJob{ID: id(req.Source + req.Diff), Status: "queued", Source: req.Source, UpdatedAt: time.Now(), Comments: []model.ReviewComment{}, Trace: []model.TraceEvent{}, Todos: todos}
	if err := s.Store.Save(j); err != nil {
		return nil, err
	}
	go s.run(context.Background(), j, req)
	return j, nil
}
func (s *Service) run(ctx context.Context, j *model.ReviewJob, req model.ReviewRequest) {
	j.Status = "running"
	_ = s.Store.Save(j)
	if strings.TrimSpace(req.Diff) == "" {
		if decision := s.Loop.Policy.Decide(PermissionNetworkFetch); decision != PermissionAllow {
			j.Status = "failed"
			j.Error = permissionError("diff_fetcher", PermissionNetworkFetch, decision).Error()
			_ = s.Store.Save(j)
			return
		}
		fetchStarted := time.Now()
		resolved, diff, err := fetchDiff(ctx, req.Source)
		fetchTraceID := id("diff_fetcher" + j.ID)
		fetchDuration := time.Since(fetchStarted).Milliseconds()
		if err != nil {
			_ = s.Store.RecordToolCall(j.ID, fetchTraceID, "diff_fetcher", "failed", req.Source, "", err.Error(), fetchDuration)
			j.Status = "failed"
			j.Error = err.Error()
			j.Trace = append(j.Trace, model.TraceEvent{ID: id(req.Source), Tool: "diff_fetcher", Input: req.Source, Output: err.Error(), At: time.Now(), Phase: "action"})
			_ = s.Store.Save(j)
			return
		}
		_ = s.Store.RecordToolCall(j.ID, fetchTraceID, "diff_fetcher", "succeeded", req.Source, fmt.Sprintf("diff_bytes=%d", len(diff)), "", fetchDuration)
		j.Source, req.Diff = resolved, diff
	}
	if err := s.Loop.Run(ctx, ToolInput{Job: j, Diff: req.Diff}); err != nil {
		j.Status = "failed"
		j.Error = err.Error()
		_ = s.Store.Save(j)
		return
	}
	if decision := s.Loop.Policy.Decide(PermissionLLMInference); decision != PermissionAllow {
		j.Status = "failed"
		j.Error = permissionError("subagent_review", PermissionLLMInference, decision).Error()
		_ = s.Store.Save(j)
		return
	}

	// Each sub-agent starts with a fresh model conversation. The parent consumes
	// only their final JSON reports, so the review focus does not inflate its context.
	if s.Loop.Hooks != nil {
		for _, name := range []string{"correctness", "security", "dependency"} {
			s.Loop.Hooks.Emit(ctx, HookPreToolUse, HookContext{JobID: j.ID, Tool: "subagent_" + name, Permission: PermissionLLMInference, Reason: "委派专项审查"})
		}
	}
	subResults := RunReviewSubagents(ctx, s.Config, req.Diff)
	subagentReports := make([]string, 0, len(subResults))
	for _, result := range subResults {
		toolName := "subagent_" + result.Name
		traceID := id(toolName + j.ID)
		if result.Error != nil {
			j.Trace = append(j.Trace, model.TraceEvent{ID: traceID, Tool: toolName, Input: "独立专项审查", Output: result.Error.Error(), At: time.Now(), DurationMs: result.DurationMs, Phase: "subagent"})
			_ = s.Store.RecordToolCall(j.ID, traceID, toolName, "failed", "独立专项审查", "", result.Error.Error(), result.DurationMs)
			if s.Loop.Hooks != nil {
				s.Loop.Hooks.Emit(ctx, HookToolError, HookContext{JobID: j.ID, Tool: toolName, Permission: PermissionLLMInference, Reason: "委派专项审查", Error: result.Error, DurationMs: result.DurationMs})
			}
			continue
		}
		j.Trace = append(j.Trace, model.TraceEvent{ID: traceID, Tool: toolName, Input: "独立专项审查", Output: "子 Agent 审查完成", ModelReply: result.Summary, At: time.Now(), DurationMs: result.DurationMs, Phase: "subagent"})
		_ = s.Store.RecordToolCall(j.ID, traceID, toolName, "succeeded", "独立专项审查", "子 Agent 审查完成", "", result.DurationMs)
		if s.Loop.Hooks != nil {
			s.Loop.Hooks.Emit(ctx, HookPostToolUse, HookContext{JobID: j.ID, Tool: toolName, Permission: PermissionLLMInference, Reason: "委派专项审查", Output: "子 Agent 审查完成", DurationMs: result.DurationMs})
		}
		subagentReports = append(subagentReports, result.Name+"\n"+truncateSubagentReport(result.Summary, 8000))
	}
	// Persist this checkpoint before the final synthesis, which also lets the SSE
	// endpoint show completed child tasks when the final model call is slow.
	_ = s.Store.Save(j)
	readID := ""
	if len(j.Trace) > 0 {
		readID = j.Trace[len(j.Trace)-1].ID
	}
	prompt := "你是 Code Review 父 Agent。以下是拥有独立 diff 上下文的子 Agent 最终报告。只基于这些报告去重、校正严重级别，并输出 JSON 数组，字段为 file,line,severity,confidence,body,suggestion；没有问题输出 []。不要补充报告中不存在的事实。\n\n" + strings.Join(subagentReports, "\n\n")
	modelStarted := time.Now()
	reply, err := EinoReviewAgent(ctx, s.Config, prompt)
	modelTraceID := id("deepseek-review" + j.ID)
	modelDuration := time.Since(modelStarted).Milliseconds()
	if err != nil {
		_ = s.Store.RecordToolCall(j.ID, modelTraceID, "deepseek_review", "failed", "prompt diff summary", "", err.Error(), modelDuration)
	} else {
		_ = s.Store.RecordToolCall(j.ID, modelTraceID, "deepseek_review", "succeeded", "prompt diff summary", "DeepSeek 审查完成", "", modelDuration)
	}
	if err != nil {
		j.Trace = append(j.Trace, model.TraceEvent{ID: id(err.Error()), Tool: "review-fallback", Input: "diff summary", Output: err.Error(), At: time.Now(), Phase: "reasoning"})
		j.Comments = []model.ReviewComment{{File: "diff", Line: 1, Severity: "info", Confidence: "low", Body: "DeepSeek 调用失败：" + err.Error(), TraceID: readID}}
	} else {
		traceID := modelTraceID
		j.Trace = append(j.Trace, model.TraceEvent{ID: traceID, Tool: "deepseek-review", Input: "prompt diff summary", Output: "DeepSeek 审查完成", ModelReply: reply, At: time.Now(), Phase: "reasoning"})
		j.SpentCents = 1
		findings := parseFindings(reply)
		j.Comments = []model.ReviewComment{}
		for _, f := range findings {
			body := f.Body
			if f.Suggestion != "" {
				body += "\n建议：" + f.Suggestion
			}
			j.Comments = append(j.Comments, model.ReviewComment{File: f.File, Line: f.Line, Severity: f.Severity, Confidence: f.Confidence, Body: body, TraceID: traceID})
		}
		if len(j.Comments) == 0 {
			j.Comments = []model.ReviewComment{{File: "diff", Line: 1, Severity: "info", Confidence: "high", Body: "未发现需要评论的问题。", TraceID: traceID}}
		}
	}
	j.Status = "completed"
	j.UpdatedAt = time.Now()
	_ = s.Store.Save(j)
}

func truncateSubagentReport(report string, limit int) string {
	if len(report) <= limit {
		return report
	}
	return report[:limit] + "\n[子 Agent 报告已截断]"
}
