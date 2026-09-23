package logic

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"CR-Agent/internal/model"
	"github.com/cloudwego/eino/schema"
)

type harnessSetupKey struct{}
type goalConditionKey struct{}

func withGoalCondition(ctx context.Context, condition string) context.Context {
	return context.WithValue(ctx, goalConditionKey{}, condition)
}

// withReviewHarness binds host capabilities, not model-provided permissions.
// Each invocation builds new session state, including the task ownership set.
func (s *Service) withReviewHarness(ctx context.Context, job *model.ReviewJob, diff string, artifacts ...ReviewArtifacts) (context.Context, func()) {
	recorder := traceRecorderFrom(ctx)
	if recorder == nil {
		recorder = newTraceRecorder(job)
		ctx = withTraceRecorder(ctx, recorder)
	}
	bound := context.WithValue(ctx, harnessSetupKey{}, func(h *ReviewHarness) {
		var checks []PreflightCheck
		if len(artifacts) > 0 {
			checks = artifacts[0].Checks
		} else {
			checks = analyzeDiff(diff).Checks
		}
		h.Hooks = s.Loop.Hooks
		h.Policy = s.Loop.Policy
		// The supplied diff and preflight results already contain review evidence.
		// Bound exploratory tool turns only for this job-scoped review path.
		h.MaxToolRounds = 4
		h.MaxStalledRounds = 2
		h.Workflow = s.Workflows
		h.WorkflowRunner = s.workflowAgentRunner(h)
		todos := ""
		h.System = func() string {
			if todos == "" {
				return ""
			}
			return "当前会话计划:\n" + todos
		}
		h.Record = func(name, callID, status, output string, started, ended time.Time, duration int64) {
			parentID := traceParentFrom(ctx)
			traceID, _ := recorder.RecordAt(
				"tool",
				name,
				"harness",
				"model tool call",
				parentID,
				started,
				ended,
				TraceResult{Status: status, Output: output, Origin: "model", ToolCallID: callID},
			)
			_ = s.Store.RecordToolCall(job.ID, traceID, name, status, "model tool call", output, "", duration)
		}
		h.add("load_skill", "按名称加载完整 Skill 指令", objectSchema("name"),
			func(_ context.Context, args map[string]any) (string, error) {
				name, err := requiredString(args, "name")
				if err != nil {
					return "", err
				}
				skill, err := s.SkillLoader.Load(name)
				return skill.Content, err
			})
		h.add("memory_recall", "召回与当前审查相关的长期记忆", objectSchema("query"),
			func(_ context.Context, args map[string]any) (string, error) {
				query, err := requiredString(args, "query")
				if err != nil {
					return "", err
				}
				records, err := s.MemoryStore.Recall(query)
				return renderMemories(records), err
			})
		h.add("todo_write", "替换当前模型会话的审查计划，items 为文本清单", objectSchema("items"),
			func(_ context.Context, args map[string]any) (string, error) {
				items, err := requiredString(args, "items")
				if err == nil {
					todos = items
				}
				return items, err
			})
		owned := map[string]bool{}
		owner := "harness-" + id(job.ID)
		pending := map[string]bool{}
		backgroundChecks := map[string]string{}
		workflowPending := map[string]bool{}
		h.WorkflowLaunched = func(runID string) { workflowPending[runID] = true }
		teamNotifications := []string{}
		for _, event := range job.TeamEvents {
			teamNotifications = append(teamNotifications, jsonString(event))
		}
		h.Notify = func() ([]string, error) {
			events := teamNotifications
			teamNotifications = []string{}
			for taskID := range pending {
				background, err := s.Background.Get(taskID)
				if err != nil {
					return nil, err
				}
				if isTerminalBackgroundStatus(background.Status) {
					events = append(events, jsonString(background))
					delete(pending, taskID)
				}
			}
			for runID := range workflowPending {
				event, err := s.Workflows.Collect(runID)
				if err != nil {
					return nil, err
				}
				if event != "" {
					events = append(events, event)
					delete(workflowPending, runID)
				}
			}
			return events, nil
		}
		h.Await = func(ctx context.Context) ([]string, error) {
			ticker := time.NewTicker(25 * time.Millisecond)
			defer ticker.Stop()
			for len(pending) > 0 || len(workflowPending) > 0 {
				events, err := h.Notify()
				if err != nil || len(events) > 0 {
					return events, err
				}
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-ticker.C:
				}
			}
			return []string{}, nil
		}
		h.Cleanup = func() {
			for taskID := range pending {
				_, _ = s.Background.Cancel(taskID)
			}
			for runID := range workflowPending {
				_ = s.Workflows.Cancel(runID)
			}
		}
		h.add("list_crons", "列出宿主定时审查计划", map[string]any{"type": "object"},
			func(context.Context, map[string]any) (string, error) {
				if s.Cron == nil {
					return "", fmt.Errorf("cron 未初始化")
				}
				return jsonString(s.Cron.List()), nil
			})
		h.add("schedule_cron", "创建持久定时审查计划；后台模型调用需要宿主授权", stringObject("cron", "source"),
			func(_ context.Context, args map[string]any) (string, error) {
				expression, err := requiredString(args, "cron")
				if err != nil {
					return "", err
				}
				source, err := requiredString(args, "source")
				if err != nil {
					return "", err
				}
				if s.Cron == nil {
					return "", fmt.Errorf("cron 未初始化")
				}
				job, err := s.Cron.Schedule(expression, source, "", true, true)
				return jsonString(job), err
			})
		h.add("cancel_cron", "取消宿主定时计划；后台模型调用需要宿主授权", objectSchema("cron_id"),
			func(_ context.Context, args map[string]any) (string, error) {
				cronID, err := requiredString(args, "cron_id")
				if err != nil {
					return "", err
				}
				if s.Cron == nil {
					return "", fmt.Errorf("cron 未初始化")
				}
				job, err := s.Cron.Cancel(cronID)
				return jsonString(job), err
			})
		for _, name := range []string{"schedule_cron", "cancel_cron"} {
			tool := h.tools[name]
			tool.permission = PermissionManageSchedule
			h.tools[name] = tool
		}
		h.add("background_check", "在后台读取已完成的前置检查结果，完成后自动通知当前会话", objectSchema("check"),
			func(_ context.Context, args map[string]any) (string, error) {
				name, err := requiredString(args, "check")
				if err != nil {
					return "", err
				}
				var result string
				for _, check := range checks {
					if check.Name == name {
						result = check.Status + ": " + check.Message
						break
					}
				}
				if result == "" {
					return "", fmt.Errorf("未知前置检查结果 %q", name)
				}
				if _, requested := backgroundChecks[name]; requested {
					return "复用前置检查结果: " + result, nil
				}
				background, err := s.Background.Create("审查检查: " + name)
				if err != nil {
					return "", err
				}
				err = s.Background.Launch(background.ID, func(_ context.Context) (string, error) {
					if recorder := traceRecorderFrom(ctx); recorder != nil {
						recorder.Record("input", "preflight_result_reuse", "background", name, traceParentFrom(ctx), TraceResult{
							Output: result, Origin: "cache", CacheHit: true,
						})
					}
					return result, nil
				})
				if err != nil {
					return "", err
				}
				backgroundChecks[name] = background.ID
				pending[background.ID] = true
				return "后台任务已启动: " + background.ID, nil
			})
		h.add("create_task", "创建当前会话的审查子任务", objectSchema("subject"),
			func(_ context.Context, args map[string]any) (string, error) {
				subject, err := requiredString(args, "subject")
				if err != nil {
					return "", err
				}
				task, err := s.TaskStore.Create(subject, "审查 Job: "+job.ID)
				if err == nil {
					owned[task.ID] = true
				}
				return jsonString(task), err
			})
		for _, action := range []string{"get_task", "claim_task", "complete_task"} {
			h.add(action, "操作本会话创建的任务", objectSchema("task_id"),
				func(_ context.Context, args map[string]any) (string, error) {
					taskID, err := requiredString(args, "task_id")
					if err != nil {
						return "", err
					}
					if !owned[taskID] {
						return "", fmt.Errorf("任务不属于当前模型会话")
					}
					var task model.Task
					switch action {
					case "get_task":
						task, err = s.TaskStore.Get(taskID)
					case "claim_task":
						task, err = s.TaskStore.Claim(taskID, owner)
					case "complete_task":
						task, _, err = s.TaskStore.Complete(taskID, owner)
					}
					return jsonString(task), err
				})
		}
		h.add("list_tasks", "列出本会话创建的审查任务", map[string]any{"type": "object"},
			func(_ context.Context, _ map[string]any) (string, error) {
				tasks := []model.Task{}
				for taskID := range owned {
					task, err := s.TaskStore.Get(taskID)
					if err != nil {
						return "", err
					}
					tasks = append(tasks, task)
				}
				return jsonString(tasks), nil
			})
		h.add("update_task", "为本会话任务增加依赖；blocked_by 为逗号分隔的任务 ID", stringObject("task_id", "blocked_by"),
			func(_ context.Context, args map[string]any) (string, error) {
				taskID, err := requiredString(args, "task_id")
				if err != nil {
					return "", err
				}
				deps, err := requiredString(args, "blocked_by")
				if err != nil {
					return "", err
				}
				ids := strings.Split(deps, ",")
				if !owned[taskID] {
					return "", fmt.Errorf("任务不属于当前模型会话")
				}
				for i := range ids {
					ids[i] = strings.TrimSpace(ids[i])
					if !owned[ids[i]] {
						return "", fmt.Errorf("依赖任务不属于当前模型会话")
					}
				}
				task, err := s.TaskStore.AddDependencies(taskID, ids)
				return jsonString(task), err
			})
	})
	return bound, recorder.Flush
}

func stringObject(names ...string) map[string]any {
	properties := map[string]any{}
	for _, name := range names {
		properties[name] = map[string]any{"type": "string"}
	}
	return map[string]any{"type": "object", "properties": properties, "required": names, "additionalProperties": false}
}

func (s *Service) workflowAgentRunner(h *ReviewHarness) WorkflowAgentRunner {
	return func(ctx context.Context, prompt string, outputSchema map[string]any, label string) (WorkflowAgentResult, error) {
		schemaJSON, _ := json.Marshal(outputSchema)
		instruction := fmt.Sprintf("你是 workflow 子 Agent，标签 %s。只返回符合以下 JSON schema 的 JSON，不要 Markdown：%s\n\n%s", label, schemaJSON, prompt)
		reply, err := h.Model(ctx, []*schema.Message{{Role: schema.User, Content: instruction}}, nil)
		if err != nil {
			return WorkflowAgentResult{}, err
		}
		if reply == nil || len(reply.ToolCalls) > 0 {
			return WorkflowAgentResult{}, fmt.Errorf("workflow agent 返回了工具调用或空响应")
		}
		content := strings.TrimSpace(reply.Content)
		content = strings.TrimPrefix(content, "```json")
		content = strings.TrimPrefix(content, "```")
		content = strings.TrimSuffix(strings.TrimSpace(content), "```")
		var value any
		if err := json.Unmarshal([]byte(strings.TrimSpace(content)), &value); err != nil {
			return WorkflowAgentResult{}, fmt.Errorf("workflow agent %s 输出不是 JSON: %w", label, err)
		}
		tokens := 0
		if reply.ResponseMeta != nil && reply.ResponseMeta.Usage != nil {
			tokens = reply.ResponseMeta.Usage.TotalTokens
		}
		return WorkflowAgentResult{Value: value, Tokens: tokens}, nil
	}
}
