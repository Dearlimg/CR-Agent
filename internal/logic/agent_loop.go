package logic

import (
	"CR-Agent/internal/model"
	"context"
	"fmt"
	"sync"
	"time"
)

type ToolInput struct {
	Job    *model.ReviewJob
	Diff   string
	Args   map[string]any
	Tracer *TraceRecorder
}
type ToolResult struct {
	Output   string
	Diff     string
	Done     bool
	Comments []model.ReviewComment
}
type Tool func(context.Context, ToolInput) (ToolResult, error)
type ToolDefinition struct {
	Permission Permission
	Run        Tool
}
type ToolRegistry struct {
	mu    sync.RWMutex
	tools map[string]ToolDefinition
}

func NewToolRegistry() *ToolRegistry { return &ToolRegistry{tools: map[string]ToolDefinition{}} }
func (r *ToolRegistry) Register(name string, tool Tool) {
	r.RegisterWithPermission(name, PermissionReadDiff, tool)
}
func (r *ToolRegistry) RegisterWithPermission(name string, permission Permission, tool Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[name] = ToolDefinition{Permission: permission, Run: tool}
}
func (r *ToolRegistry) Get(name string) (ToolDefinition, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

type LoopStep struct {
	Tool   string
	Reason string
	Args   map[string]any
}
type AgentLoop struct {
	Registry *ToolRegistry
	Plan     []LoopStep
	MaxSteps int
	Record   func(string, string, string, string, string, string, string, int64) error
	Policy   *PermissionPolicy
	Hooks    *HookBus
}

func (a *AgentLoop) Run(ctx context.Context, input ToolInput) error {
	maxSteps := a.MaxSteps
	if maxSteps <= 0 {
		maxSteps = len(a.Plan)
	}
	if a.Hooks != nil {
		a.Hooks.Emit(ctx, HookLoopStart, HookContext{JobID: input.Job.ID})
		defer a.Hooks.Emit(ctx, HookLoopStop, HookContext{JobID: input.Job.ID})
	}
	roundsSinceTodo := 0
	for i, step := range a.Plan {
		if err := ctx.Err(); err != nil {
			return err
		}
		if i >= maxSteps {
			return fmt.Errorf("agent loop 超过最大步数 %d", maxSteps)
		}
		tool, ok := a.Registry.Get(step.Tool)
		if !ok {
			return fmt.Errorf("工具未注册: %s", step.Tool)
		}
		setTodoStatus(input.Job, i, "in_progress")
		var span *TraceSpan
		if input.Tracer != nil {
			span = input.Tracer.Start("tool", step.Tool, "action", step.Reason, "")
		}
		decision := a.Policy.Decide(tool.Permission)
		if decision != PermissionAllow {
			err := permissionError(step.Tool, tool.Permission, decision)
			traceID := id(step.Tool + err.Error())
			if span != nil {
				span.End(TraceResult{Status: "denied", Err: err})
				traceID = span.ID()
			} else {
				input.Job.Trace = append(input.Job.Trace, model.TraceEvent{ID: traceID, Tool: step.Tool, Input: step.Reason, Output: err.Error(), At: time.Now(), Phase: "permission"})
			}
			if a.Record != nil {
				_ = a.Record(input.Job.ID, traceID, step.Tool, string(decision), step.Reason, "", err.Error(), 0)
			}
			if a.Hooks != nil {
				a.Hooks.Emit(ctx, HookPermissionDenied, HookContext{JobID: input.Job.ID, Tool: step.Tool, Permission: tool.Permission, Reason: step.Reason, Error: err})
			}
			return err
		}
		if a.Hooks != nil {
			pre := a.Hooks.Emit(ctx, HookPreToolUse, HookContext{JobID: input.Job.ID, Tool: step.Tool, Permission: tool.Permission, Reason: step.Reason})
			if pre.Error != nil {
				return pre.Error
			}
		}
		started := time.Now()
		toolInput := input
		if step.Args != nil {
			toolInput.Args = step.Args
		}
		result, err := tool.Run(ctx, toolInput)
		traceID := id(step.Tool + step.Reason + time.Now().String())
		if err != nil {
			duration := time.Since(started).Milliseconds()
			if span != nil {
				span.End(TraceResult{Err: err})
				traceID = span.ID()
			} else {
				input.Job.Trace = append(input.Job.Trace, model.TraceEvent{ID: traceID, Tool: step.Tool, Input: step.Reason, Output: err.Error(), At: time.Now(), DurationMs: duration, Phase: "action"})
			}
			if a.Record != nil {
				_ = a.Record(input.Job.ID, traceID, step.Tool, "failed", step.Reason, "", err.Error(), duration)
			}
			if a.Hooks != nil {
				a.Hooks.Emit(ctx, HookToolError, HookContext{JobID: input.Job.ID, Tool: step.Tool, Permission: tool.Permission, Reason: step.Reason, Error: err, DurationMs: duration})
			}
			return err
		}
		setTodoStatus(input.Job, i, "completed")
		duration := time.Since(started).Milliseconds()
		if span != nil {
			span.End(TraceResult{Output: result.Output})
			traceID = span.ID()
		} else {
			input.Job.Trace = append(input.Job.Trace, model.TraceEvent{ID: traceID, Tool: step.Tool, Input: step.Reason, Output: result.Output, At: time.Now(), DurationMs: duration, Phase: "action"})
		}
		if a.Record != nil {
			_ = a.Record(input.Job.ID, traceID, step.Tool, "succeeded", step.Reason, result.Output, "", duration)
		}
		if step.Tool == "todo_write" {
			roundsSinceTodo = 0
		} else {
			roundsSinceTodo++
		}
		if roundsSinceTodo >= 3 {
			if input.Tracer != nil {
				input.Tracer.Record("input", "todo_reminder", "planning", "任务计划提醒", "", TraceResult{Output: "连续三个工具步骤未更新 Todo，请确认剩余计划和当前目标。"})
			} else {
				input.Job.Trace = append(input.Job.Trace, model.TraceEvent{ID: id("todo_reminder" + input.Job.ID), Tool: "todo_reminder", Input: "任务计划提醒", Output: "连续三个工具步骤未更新 Todo，请确认剩余计划和当前目标。", At: time.Now(), Phase: "planning"})
			}
			roundsSinceTodo = 0
		}
		if a.Hooks != nil {
			a.Hooks.Emit(ctx, HookPostToolUse, HookContext{JobID: input.Job.ID, Tool: step.Tool, Permission: tool.Permission, Reason: step.Reason, Output: result.Output, DurationMs: duration})
		}
		if result.Diff != "" {
			input.Diff = result.Diff
		}
		if len(result.Comments) > 0 {
			input.Job.Comments = result.Comments
		}
		if result.Done {
			return nil
		}
	}
	return nil
}

func setTodoStatus(job *model.ReviewJob, order int, status string) {
	for i := range job.Todos {
		if job.Todos[i].Order == order {
			job.Todos[i].Status = status
			return
		}
	}
}
