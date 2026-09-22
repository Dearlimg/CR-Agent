package logic

import (
	"CR-Agent/internal/model"
	"context"
	"fmt"
	"time"
)

type ToolInput struct {
	Job  *model.ReviewJob
	Diff string
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
type ToolRegistry struct{ tools map[string]ToolDefinition }

func NewToolRegistry() *ToolRegistry { return &ToolRegistry{tools: map[string]ToolDefinition{}} }
func (r *ToolRegistry) Register(name string, tool Tool) {
	r.RegisterWithPermission(name, PermissionReadDiff, tool)
}
func (r *ToolRegistry) RegisterWithPermission(name string, permission Permission, tool Tool) {
	r.tools[name] = ToolDefinition{Permission: permission, Run: tool}
}
func (r *ToolRegistry) Get(name string) (ToolDefinition, bool) { t, ok := r.tools[name]; return t, ok }

type LoopStep struct {
	Tool   string
	Reason string
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
	if a.MaxSteps <= 0 {
		a.MaxSteps = len(a.Plan)
	}
	if a.Hooks != nil {
		a.Hooks.Emit(ctx, HookLoopStart, HookContext{JobID: input.Job.ID})
	}
	for i, step := range a.Plan {
		if i >= a.MaxSteps {
			return fmt.Errorf("agent loop 超过最大步数 %d", a.MaxSteps)
		}
		tool, ok := a.Registry.Get(step.Tool)
		if !ok {
			return fmt.Errorf("工具未注册: %s", step.Tool)
		}
		setTodoStatus(input.Job, i, "in_progress")
		decision := a.Policy.Decide(tool.Permission)
		if decision != PermissionAllow {
			err := permissionError(step.Tool, tool.Permission, decision)
			traceID := id(step.Tool + err.Error())
			input.Job.Trace = append(input.Job.Trace, model.TraceEvent{ID: traceID, Tool: step.Tool, Input: step.Reason, Output: err.Error(), At: time.Now(), Phase: "permission"})
			if a.Record != nil {
				_ = a.Record(input.Job.ID, traceID, step.Tool, string(decision), step.Reason, "", err.Error(), 0)
			}
			if a.Hooks != nil {
				a.Hooks.Emit(ctx, HookPermissionDenied, HookContext{JobID: input.Job.ID, Tool: step.Tool, Permission: tool.Permission, Reason: step.Reason, Error: err})
			}
			return err
		}
		if a.Hooks != nil {
			a.Hooks.Emit(ctx, HookPreToolUse, HookContext{JobID: input.Job.ID, Tool: step.Tool, Permission: tool.Permission, Reason: step.Reason})
		}
		started := time.Now()
		result, err := tool.Run(ctx, input)
		traceID := id(step.Tool + step.Reason + time.Now().String())
		if err != nil {
			duration := time.Since(started).Milliseconds()
			input.Job.Trace = append(input.Job.Trace, model.TraceEvent{ID: traceID, Tool: step.Tool, Input: step.Reason, Output: err.Error(), At: time.Now(), DurationMs: duration, Phase: "action"})
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
		input.Job.Trace = append(input.Job.Trace, model.TraceEvent{ID: traceID, Tool: step.Tool, Input: step.Reason, Output: result.Output, At: time.Now(), DurationMs: duration, Phase: "action"})
		if a.Record != nil {
			_ = a.Record(input.Job.ID, traceID, step.Tool, "succeeded", step.Reason, result.Output, "", duration)
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
			if a.Hooks != nil {
				a.Hooks.Emit(ctx, HookLoopStop, HookContext{JobID: input.Job.ID})
			}
			return nil
		}
	}
	if a.Hooks != nil {
		a.Hooks.Emit(ctx, HookLoopStop, HookContext{JobID: input.Job.ID})
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
