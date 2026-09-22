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
type ToolRegistry struct{ tools map[string]Tool }

func NewToolRegistry() *ToolRegistry                    { return &ToolRegistry{tools: map[string]Tool{}} }
func (r *ToolRegistry) Register(name string, tool Tool) { r.tools[name] = tool }
func (r *ToolRegistry) Get(name string) (Tool, bool)    { t, ok := r.tools[name]; return t, ok }

type LoopStep struct {
	Tool   string
	Reason string
}
type AgentLoop struct {
	Registry *ToolRegistry
	Plan     []LoopStep
	MaxSteps int
	Record   func(string, string, string, string, string, string, int64) error
}

func (a *AgentLoop) Run(ctx context.Context, input ToolInput) error {
	if a.MaxSteps <= 0 {
		a.MaxSteps = len(a.Plan)
	}
	for i, step := range a.Plan {
		if i >= a.MaxSteps {
			return fmt.Errorf("agent loop 超过最大步数 %d", a.MaxSteps)
		}
		tool, ok := a.Registry.Get(step.Tool)
		if !ok {
			return fmt.Errorf("工具未注册: %s", step.Tool)
		}
		started := time.Now()
		result, err := tool(ctx, input)
		traceID := id(step.Tool + step.Reason + time.Now().String())
		if err != nil {
			duration := time.Since(started).Milliseconds()
			input.Job.Trace = append(input.Job.Trace, model.TraceEvent{ID: traceID, Tool: step.Tool, Input: step.Reason, Output: err.Error(), At: time.Now(), DurationMs: duration, Phase: "action"})
			if a.Record != nil {
				_ = a.Record(input.Job.ID, traceID, step.Tool, "failed", step.Reason, "", err.Error(), duration)
			}
			return err
		}
		duration := time.Since(started).Milliseconds()
		input.Job.Trace = append(input.Job.Trace, model.TraceEvent{ID: traceID, Tool: step.Tool, Input: step.Reason, Output: result.Output, At: time.Now(), DurationMs: duration, Phase: "action"})
		if a.Record != nil {
			_ = a.Record(input.Job.ID, traceID, step.Tool, "succeeded", step.Reason, result.Output, "", duration)
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
