package logic

import (
	"CR-Agent/internal/dao"
	"CR-Agent/internal/model"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Service struct {
	Store          dao.Store
	Config         Config
	Loop           *AgentLoop
	SkillLoader    *SkillLoader
	SkillError     error
	MemoryStore    *MemoryStore
	TaskStore      TaskRepository
	Background     BackgroundRepository
	Cron           *CronScheduler
	CronError      error
	Workflows      *WorkflowRuntime
	MCP            *MCPManager
	PreflightCache *PreflightCache
}

func NewService(store dao.Store, cfg Config) *Service {
	return NewServiceWithRuntime(store, cfg, RuntimeRepositories{})
}

func NewServiceWithRuntime(store dao.Store, cfg Config, repositories RuntimeRepositories) *Service {
	skillLoader := NewSkillLoader(cfg.SkillsDir)
	skillErr := skillLoader.Scan()
	registry := NewToolRegistry()
	preflightCache := NewPreflightCache()
	workflowRegistry := NewWorkflowRegistry()
	if err := registerReviewWorkflow(workflowRegistry); err != nil {
		panic(err)
	}
	mcp := NewMCPManager(DefaultMCPHostPolicy())
	_ = mcp.RegisterServer("docs", newDocsMCPServer)
	_ = mcp.RegisterServer("deploy", newDeployMCPServer)
	mcp.RegisterConnectTool(registry)
	registry.RegisterWithPermission("preflight_analysis", PermissionStaticAnalysis, func(ctx context.Context, in ToolInput) (ToolResult, error) {
		if in.Artifacts == nil {
			return ToolResult{}, fmt.Errorf("preflight_analysis 缺少结果接收器")
		}
		artifacts, err := preflightCache.Run(ctx, in.Job.Source, in.Diff)
		if err != nil {
			return ToolResult{}, err
		}
		*in.Artifacts = artifacts
		return ToolResult{
			Output: artifacts.TraceSummary(), CacheHit: artifacts.CacheHit,
			ToolVersion: preflightVersion, InputDigest: artifacts.DiffDigest,
		}, nil
	})
	record := func(jobID, traceID, tool, status, input, output, callErr string, durationMs int64) error {
		return store.RecordToolCall(jobID, traceID, tool, status, input, output, callErr, durationMs)
	}
	plan := []LoopStep{{Tool: "preflight_analysis", Reason: "解析 diff、扫描密钥并执行一次确定性检查"}}
	repositories = ensureRuntimeRepositories(cfg, repositories)
	workflows := NewWorkflowRuntime(cfg.WorkflowDir, workflowRegistry)
	if repositories.Workflow != nil {
		workflows = NewWorkflowRuntimeWithPersistence(workflowRegistry, repositories.Workflow)
	}
	service := &Service{
		Store:          store,
		Config:         cfg,
		Loop:           &AgentLoop{Registry: registry, Plan: plan, MaxSteps: len(plan), Record: record, Policy: DefaultPermissionPolicy(), Hooks: NewHookBus()},
		SkillLoader:    skillLoader,
		SkillError:     skillErr,
		MemoryStore:    NewMemoryStore(cfg),
		TaskStore:      nil,
		Background:     nil,
		MCP:            mcp,
		PreflightCache: preflightCache,
		Workflows:      workflows,
	}
	service.TaskStore = repositories.Tasks
	service.Background = repositories.Background
	cron, cronErr := NewCronScheduler(
		cfg.CronFile,
		time.Duration(cfg.CronPollIntervalMs)*time.Millisecond,
		func(job model.CronJob) (string, error) {
			idle, err := service.Background.IsIdle()
			if err != nil {
				return "", err
			}
			if !idle {
				return "", errCronAgentBusy
			}
			review, err := service.Create(model.ReviewRequest{
				Source:      job.Source,
				MemoryQuery: job.MemoryQuery,
			})
			if err != nil {
				return "", err
			}
			return review.BackgroundTaskID, nil
		},
	)
	service.Cron = cron
	service.CronError = cronErr
	return service
}

func (s *Service) Start() error {
	if s.CronError != nil {
		return fmt.Errorf("初始化定时任务: %w", s.CronError)
	}
	if s.Cron != nil {
		s.Cron.Start()
	}
	return nil
}

func (s *Service) Stop() {
	if s.Cron != nil {
		s.Cron.Stop()
	}
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
	now := time.Now().UTC()
	todos := []model.TodoItem{
		{Content: "扫描并解析代码变更", Status: "pending", Order: 0},
		{Content: "执行单 Agent 代码审查", Status: "pending", Order: 1},
		{Content: "核对代码证据、第二轮复核并去重审查发现", Status: "pending", Order: 2},
	}
	j := &model.ReviewJob{
		ID:          id(req.Source + req.Diff),
		Status:      "queued",
		Source:      req.Source,
		StartedAt:   now,
		UpdatedAt:   now,
		Comments:    []model.ReviewComment{},
		ReviewScope: model.ReviewScope{Checks: []model.ReviewCheck{}},
		Trace:       []model.TraceEvent{},
		Todos:       todos,
		TeamEvents:  []model.TeamEvent{},
	}
	recorder := newTraceRecorder(j)
	taskSpan := recorder.Start("input", "task_create", "task", reviewTaskSubject(req), "")
	task, err := s.TaskStore.Create(reviewTaskSubject(req), "由 Code Review Agent 自动创建的审查任务")
	if err != nil {
		return nil, err
	}
	taskSpan.End(TraceResult{Output: task.ID})
	j.TaskID = task.ID
	claimSpan := recorder.Start("input", "task_claim", "task", task.ID, "")
	if _, err := s.TaskStore.Claim(task.ID, "review-agent"); err != nil {
		return nil, err
	}
	claimSpan.End(TraceResult{Output: "owner=review-agent"})
	backgroundSpan := recorder.Start("input", "background_create", "background", "代码审查", "")
	backgroundTask, err := s.Background.Create("后台代码审查")
	if err != nil {
		return nil, err
	}
	backgroundSpan.End(TraceResult{Output: backgroundTask.ID})
	j.BackgroundTaskID = backgroundTask.ID
	recorder.Flush()
	if err := s.Store.Save(j); err != nil {
		return nil, err
	}
	if err := s.Background.Launch(backgroundTask.ID, func(ctx context.Context) (string, error) {
		s.runWithTracer(ctx, j, req, recorder)
		if j.Status != "completed" && j.Status != "completed_with_warnings" {
			return "", fmt.Errorf("审查任务失败：%s", j.Error)
		}
		return fmt.Sprintf("审查任务 %s 已完成", j.ID), nil
	}); err != nil {
		finished := time.Now().UTC()
		j.Status = "failed"
		j.ReviewOutcome = "failed"
		j.Error = fmt.Sprintf("启动后台审查失败：%v", err)
		j.FinishedAt = &finished
		j.UpdatedAt = finished
		_ = s.Store.Save(j)
		return nil, err
	}
	return j, nil
}
func (s *Service) run(ctx context.Context, j *model.ReviewJob, req model.ReviewRequest) {
	s.runWithTracer(ctx, j, req, newTraceRecorder(j))
}

func (s *Service) runWithTracer(ctx context.Context, j *model.ReviewJob, req model.ReviewRequest, recorder *TraceRecorder) {
	if recorder == nil {
		recorder = newTraceRecorder(j)
	}
	ctx = withTraceRecorder(ctx, recorder)
	if j.StartedAt.IsZero() {
		j.StartedAt = time.Now().UTC()
	}
	j.Status = "running"
	j.ReviewOutcome = ""
	j.UpdatedAt = time.Now().UTC()
	_ = s.Store.Save(j)
	defer func() {
		if j.ReviewOutcome == "" {
			if j.Status == "failed" {
				j.ReviewOutcome = "failed"
			} else {
				j.ReviewOutcome = "incomplete"
			}
		}
		if j.ReviewOutcome == "incomplete" && j.Status == "completed" {
			j.Status = "completed_with_warnings"
		}
		s.completeReviewTask(j, recorder)
		recorder.Flush()
		finished := time.Now().UTC()
		j.FinishedAt = &finished
		j.UpdatedAt = finished
		// Publish the terminal status, final timestamps, and complete trace together.
		_ = s.Store.Save(j)
	}()
	if strings.TrimSpace(req.Diff) == "" {
		fetchSpan := recorder.Start("tool", "diff_fetcher", "action", req.Source, "")
		if decision := s.Loop.Policy.Decide(PermissionNetworkFetch); decision != PermissionAllow {
			err := permissionError("diff_fetcher", PermissionNetworkFetch, decision)
			fetchSpan.End(TraceResult{Status: "denied", Err: err, Origin: "orchestrator"})
			j.Status = "failed"
			j.Error = err.Error()
			return
		}
		fetchStarted := time.Now()
		resolved, diff, err := fetchDiff(ctx, req.Source, s.Config)
		fetchEnded := time.Now()
		fetchDuration := fetchEnded.Sub(fetchStarted).Milliseconds()
		fetchResult := TraceResult{Output: fmt.Sprintf("diff_bytes=%d", len(diff)), Err: err, Origin: "orchestrator"}
		if err != nil {
			fetchResult.Output = ""
		}
		fetchSpan.End(fetchResult)
		fetchTraceID := fetchSpan.ID()
		if err != nil {
			_ = s.Store.RecordToolCall(j.ID, fetchTraceID, "diff_fetcher", "failed", req.Source, "", err.Error(), fetchDuration)
			setReviewFailure(j, err, err.Error())
			return
		}
		_ = s.Store.RecordToolCall(j.ID, fetchTraceID, "diff_fetcher", "succeeded", req.Source, fmt.Sprintf("diff_bytes=%d", len(diff)), "", fetchDuration)
		j.Source, req.Diff = resolved, diff
	}
	artifacts := &ReviewArtifacts{}
	if err := s.Loop.Run(ctx, ToolInput{Job: j, Diff: req.Diff, Tracer: recorder, Artifacts: artifacts}); err != nil {
		setReviewFailure(j, err, err.Error())
		recorder.Flush()
		return
	}
	j.ReviewScope = model.ReviewScope{
		FilesReviewed: len(artifacts.Files),
		AddedLines:    artifacts.AddedLines,
		TestsRan:      false,
		Checks:        make([]model.ReviewCheck, 0, len(artifacts.Checks)+2),
	}
	for _, check := range artifacts.Checks {
		j.ReviewScope.Checks = append(j.ReviewScope.Checks, model.ReviewCheck{
			Name: check.Name, Status: check.Status, Message: check.Message,
		})
	}
	j.ReviewScope.Checks = append(j.ReviewScope.Checks, model.ReviewCheck{
		Name: "automated_tests", Status: "not_run", Message: "本次审查流程未运行自动化测试",
	})
	recorder.Flush()
	if decision := s.Loop.Policy.Decide(PermissionLLMInference); decision != PermissionAllow {
		j.Status = "failed"
		j.Error = permissionError("subagent_review", PermissionLLMInference, decision).Error()
		return
	}
	incompleteReason := preflightIncompleteReason(*artifacts)
	if s.SkillError != nil {
		j.Status = "failed"
		j.Error = fmt.Sprintf("加载 Agent skills 失败：%v", s.SkillError)
		return
	}
	skillSpan := recorder.Start("input", "load_skill", "skill", "code-review", "")
	skill, err := s.SkillLoader.Load("code-review")
	if err != nil {
		skillSpan.End(TraceResult{Err: err})
		j.Status = "failed"
		j.Error = fmt.Sprintf("加载 code-review skill 失败：%v", err)
		return
	}
	skillSpan.End(TraceResult{Output: "已加载完整 SKILL.md"})
	skillTraceID := skillSpan.ID()
	skillDuration := skillSpan.DurationMs()
	_ = s.Store.RecordToolCall(j.ID, skillTraceID, "load_skill", "succeeded", skill.Name, "已加载完整 SKILL.md", "", skillDuration)
	memoryQuery := strings.TrimSpace(req.MemoryQuery)
	if memoryQuery == "" {
		memoryQuery = j.Source + "\n" + redact(req.Diff)
	}
	memorySpan := recorder.Start("input", "memory_recall", "memory", "review request", "")
	memories, memoryErr := s.MemoryStore.Recall(memoryQuery)
	if memoryErr != nil {
		memorySpan.End(TraceResult{Err: memoryErr})
		_ = s.Store.RecordToolCall(j.ID, memorySpan.ID(), "memory_recall", "failed", "review request", "", memoryErr.Error(), memorySpan.DurationMs())
	} else {
		memoryOutput := fmt.Sprintf("召回 %d 条相关持久记忆", len(memories))
		memorySpan.End(TraceResult{Output: memoryOutput})
		_ = s.Store.RecordToolCall(j.ID, memorySpan.ID(), "memory_recall", "succeeded", "review request", memoryOutput, "", memorySpan.DurationMs())
	}
	promptContext := ReviewPromptContext{
		Catalog:      s.SkillLoader.Catalog(),
		SkillContent: skill.Content,
		Memories:     redact(renderMemories(memories)),
		Evidence:     artifacts.PromptSummary(),
	}

	reviewCtx, flushHarness := s.withReviewHarness(ctx, j, artifacts.SanitizedDiff, *artifacts)
	reviewCtx = withGoalCondition(reviewCtx, req.Goal)
	if s.Loop.Hooks != nil {
		s.Loop.Hooks.Emit(ctx, HookPreToolUse, HookContext{
			JobID: j.ID, Tool: "review_agent",
			Permission: PermissionLLMInference, Reason: "执行单 Agent 代码审查",
		})
	}
	result := RunReviewAgent(reviewCtx, s.Config, artifacts.SanitizedDiff, promptContext)
	flushHarness()
	setTodoStatus(j, 1, "completed")
	toolName := "review_agent"
	traceID := result.TraceID
	if traceID == "" {
		traceID = id(toolName + j.ID)
	}
	if result.Error != nil {
		_ = s.Store.RecordToolCall(j.ID, traceID, toolName, "failed", "单 Agent 代码审查", "", redact(result.Error.Error()), result.DurationMs)
		if s.Loop.Hooks != nil {
			s.Loop.Hooks.Emit(ctx, HookToolError, HookContext{
				JobID: j.ID, Tool: toolName, Permission: PermissionLLMInference,
				Reason: "执行单 Agent 代码审查", Error: result.Error, DurationMs: result.DurationMs,
			})
		}
	} else {
		_ = s.Store.RecordToolCall(j.ID, traceID, toolName, "succeeded", "单 Agent 代码审查", "审查候选已生成", "", result.DurationMs)
		if s.Loop.Hooks != nil {
			s.Loop.Hooks.Emit(ctx, HookPostToolUse, HookContext{
				JobID: j.ID, Tool: toolName, Permission: PermissionLLMInference,
				Reason: "执行单 Agent 代码审查", Output: "审查候选已生成", DurationMs: result.DurationMs,
			})
		}
	}
	_ = s.Store.Save(j)
	reply := result.Summary
	synthesisErr := result.Error
	modelTraceID := traceID
	var goalStop *GoalStopError
	if errors.As(synthesisErr, &goalStop) {
		j.Status = "completed_with_warnings"
		j.ReviewOutcome = "incomplete"
		j.Error = goalStop.Error()
		updateReviewCheck(&j.ReviewScope, "finding_verification", "incomplete", "最终审查步骤未能生成完整结论")
		return
	}
	if synthesisErr != nil {
		if isIncompleteReviewError(synthesisErr) {
			setReviewFailure(j, synthesisErr, "审查未完成："+redact(synthesisErr.Error()))
			updateReviewCheck(&j.ReviewScope, "finding_verification", "incomplete", "最终审查步骤超时、被截断或未返回可解析内容")
			return
		}
		setReviewFailure(j, synthesisErr, "模型调用失败："+redact(synthesisErr.Error()))
		updateReviewCheck(&j.ReviewScope, "finding_verification", "failed", "最终审查模型调用失败")
		return
	}
	j.SpentCents = 1
	findings, parseErr := parseFindingsStrict(reply)
	if parseErr != nil {
		recorder.Record("input", "finding_verification", "verification", "解析模型 finding", "", TraceResult{
			Err: parseErr, Origin: "orchestrator",
		})
		j.Status = "completed_with_warnings"
		j.ReviewOutcome = "incomplete"
		j.Error = "审查未完成：模型输出不是有效的 finding JSON 数组。"
		updateReviewCheck(&j.ReviewScope, "finding_verification", "incomplete", "模型输出格式无效，未能完成 finding 核验")
		return
	}
	withEvidence, rejectedEvidence := validateFindingEvidence(findings, artifacts.SanitizedDiff)
	confirmed := make([]ReviewFinding, 0, len(withEvidence))
	rejectedByVerifier := 0
	incompleteVerifications := 0
	for _, finding := range withEvidence {
		isReal, reason, verifyTraceID, verifyErr := verifyFindingIndependently(ctx, findingVerificationRequest{
			Config: s.Config, Diff: artifacts.SanitizedDiff, Finding: finding, Recorder: recorder,
		})
		if verifyErr != nil {
			if isIncompleteReviewError(verifyErr) {
				incompleteVerifications++
				if incompleteReason == "" {
					incompleteReason = "部分问题的第二轮复核没有完成。"
				}
				continue
			}
			j.Comments = verifiedComments(confirmed, *artifacts, modelTraceID)
			setReviewFailure(j, verifyErr, "第二轮复核模型调用失败："+redact(verifyErr.Error()))
			updateReviewCheck(&j.ReviewScope, "finding_verification", "failed", "第二轮复核模型调用失败")
			return
		}
		if !isReal {
			rejectedByVerifier++
			continue
		}
		finding.VerificationStatus = "second_pass_review_passed"
		finding.VerificationReason = reason
		finding.VerificationTraceID = verifyTraceID
		confirmed = append(confirmed, finding)
	}
	j.Comments = verifiedComments(confirmed, *artifacts, modelTraceID)
	verificationStatus := "passed"
	verificationMessage := fmt.Sprintf(
		"候选=%d；证据匹配=%d；第二轮复核确认=%d；第二轮复核排除=%d",
		len(findings), len(withEvidence), len(j.Comments), rejectedByVerifier,
	)
	if len(findings) == 0 && incompleteReason == "" {
		verificationStatus = "not_needed"
		verificationMessage = "模型未报告候选问题，无需逐条复核"
	} else if incompleteReason != "" || rejectedEvidence > 0 {
		verificationStatus = "incomplete"
		verificationMessage += fmt.Sprintf("；证据不足=%d；复核未完成=%d", rejectedEvidence, incompleteVerifications)
		if incompleteReason != "" {
			verificationMessage += "；" + incompleteReason
		}
	}
	updateReviewCheck(&j.ReviewScope, "finding_verification", verificationStatus, verificationMessage)
	recorder.Record("input", "finding_verification", "verification", "代码证据与第二轮复核", "", TraceResult{
		Output: verificationMessage, Origin: "orchestrator",
	})
	if rejectedEvidence > 0 && incompleteReason == "" {
		incompleteReason = "有候选问题缺少与变更行完全匹配的代码证据，审查未能完整核验。"
	}
	if incompleteReason != "" {
		j.Status = "completed_with_warnings"
		j.ReviewOutcome = "incomplete"
		j.Error = incompleteReason
	} else if len(j.Comments) > 0 {
		j.ReviewOutcome = "completed_with_findings"
	} else {
		j.ReviewOutcome = "completed_no_findings"
	}
	setTodoStatus(j, 2, "completed")
	if err := ctx.Err(); err != nil {
		setReviewFailure(j, err, "后台审查已取消")
		return
	}
	s.extractReviewMemories(ctx, j, req)
	if j.ReviewOutcome == "incomplete" {
		j.Status = "completed_with_warnings"
	} else {
		j.Status = "completed"
	}
}

func updateReviewCheck(scope *model.ReviewScope, name, status, message string) {
	for index := range scope.Checks {
		if scope.Checks[index].Name == name {
			scope.Checks[index].Status = status
			scope.Checks[index].Message = message
			return
		}
	}
	scope.Checks = append(scope.Checks, model.ReviewCheck{Name: name, Status: status, Message: message})
}

func preflightIncompleteReason(artifacts ReviewArtifacts) string {
	failedChecks := 0
	for _, check := range artifacts.Checks {
		if check.Status == "failed" {
			failedChecks++
		}
	}
	if failedChecks > 0 || len(artifacts.SecretFindings) > 0 {
		return fmt.Sprintf("前置检查发现 %d 项失败、%d 个疑似敏感信息命中，需人工确认。", failedChecks, len(artifacts.SecretFindings))
	}
	return ""
}

func setReviewFailure(job *model.ReviewJob, err error, message string) {
	job.Error = message
	if isIncompleteReviewError(err) {
		job.Status = "completed_with_warnings"
		job.ReviewOutcome = "incomplete"
		return
	}
	job.Status = "failed"
	job.ReviewOutcome = "failed"
}

func isIncompleteReviewError(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, errIncompleteReview) ||
		isTruncatedReviewError(err) ||
		errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, context.Canceled) ||
		strings.Contains(err.Error(), "空响应") ||
		strings.Contains(strings.ToLower(err.Error()), "timeout") ||
		strings.Contains(strings.ToLower(err.Error()), "timed out")
}

func reviewTaskSubject(req model.ReviewRequest) string {
	if source := strings.TrimSpace(req.Source); source != "" {
		return "代码审查: " + source
	}
	return "代码审查: 已提交 diff"
}

func (s *Service) completeReviewTask(job *model.ReviewJob, recorder *TraceRecorder) {
	if job.TaskID == "" {
		return
	}
	var span *TraceSpan
	if recorder != nil {
		span = recorder.Start("input", "task_complete", "task", job.TaskID, "")
	}
	_, unblocked, err := s.TaskStore.Complete(job.TaskID, "review-agent")
	traceID := id("task_complete" + job.TaskID)
	if err != nil {
		if span != nil {
			span.End(TraceResult{Err: err, Origin: "orchestrator"})
			traceID = span.ID()
		}
		_ = s.Store.RecordToolCall(job.ID, traceID, "task_complete", "failed", job.TaskID, "", err.Error(), span.DurationMs())
		return
	}
	output := "任务已完成"
	if len(unblocked) > 0 {
		subjects := make([]string, 0, len(unblocked))
		for _, task := range unblocked {
			subjects = append(subjects, task.Subject)
		}
		output += "；解锁: " + strings.Join(subjects, ", ")
	}
	if span != nil {
		span.End(TraceResult{Output: output, Origin: "orchestrator"})
		traceID = span.ID()
	}
	_ = s.Store.RecordToolCall(job.ID, traceID, "task_complete", "succeeded", job.TaskID, output, "", span.DurationMs())
}

func (s *Service) extractReviewMemories(ctx context.Context, job *model.ReviewJob, req model.ReviewRequest) {
	if strings.TrimSpace(s.Config.DeepSeekAPIKey) == "" {
		return
	}
	confirmed := make([]model.ReviewComment, 0, len(job.Comments))
	for _, comment := range job.Comments {
		if comment.Confidence == "high" && comment.Severity != "info" {
			confirmed = append(confirmed, comment)
		}
	}
	if len(confirmed) == 0 {
		return
	}
	comments := redact(jsonString(confirmed))
	prompt := fmt.Sprintf(`你是 Code Review 记忆提取器。只从已确认的审查结论中提取未来审查仍会复用的信息。
输出 JSON 数组；字段为 name,description,type,body,scope。scope 仅可为 persistent 或 current_task。
仅输出稳定的用户偏好、长期反馈、项目约束或外部参考。不要保存临时任务、具体 diff、敏感数据、凭据或不确定推测。没有可保存内容时输出 []。

审查来源：%s
审查结论：%s`, req.Source, comments)
	recorder := traceRecorderFrom(ctx)
	var span *TraceSpan
	if recorder != nil {
		span = recorder.Start("model", "memory_extract", "memory", "review findings", "")
	}
	modelCtx := ctx
	if span != nil {
		modelCtx = withTraceParent(modelCtx, span.ID())
	}
	reply, err := EinoReviewAgent(modelCtx, s.Config, prompt)
	reply = redact(reply)
	traceID := id("memory_extract" + job.ID)
	if err != nil {
		if span != nil {
			span.End(TraceResult{Err: err, Origin: "model"})
			traceID = span.ID()
		}
		_ = s.Store.RecordToolCall(job.ID, traceID, "memory_extract", "failed", "review findings", "", err.Error(), span.DurationMs())
		return
	}
	candidates := parseMemoryCandidates(reply)
	stored := 0
	for _, candidate := range candidates {
		_, saved, saveErr := s.MemoryStore.Save(candidate)
		if saveErr == nil && saved {
			stored++
		}
	}
	output := fmt.Sprintf("提取 %d 条候选，保存 %d 条持久记忆", len(candidates), stored)
	if span != nil {
		span.End(TraceResult{Output: output, ModelReply: reply, Origin: "model"})
		traceID = span.ID()
	}
	_ = s.Store.RecordToolCall(job.ID, traceID, "memory_extract", "succeeded", "review findings", output, "", span.DurationMs())
}
