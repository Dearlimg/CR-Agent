package logic

import (
	"CR-Agent/internal/model"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

func (s *Service) runWithTracer(ctx context.Context, job *model.ReviewJob, request model.ReviewRequest, recorder *TraceRecorder) {
	s.runCheckpointed(ctx, job, request, recorder)
}

func (s *Service) runCheckpointed(
	ctx context.Context,
	job *model.ReviewJob,
	request model.ReviewRequest,
	recorder *TraceRecorder,
) {
	if recorder == nil {
		recorder = newTraceRecorder(job)
	}
	ctx = withTraceRecorder(ctx, recorder)
	persistFailure := func(err error, message string) {
		setReviewFailure(job, err, message)
		finished := time.Now().UTC()
		job.FinishedAt = &finished
		job.UpdatedAt = finished
		_ = s.Store.Save(job)
	}
	checkpoint, err := loadReviewCheckpoint(job)
	if job.CheckpointJSON == "" {
		checkpoint = newReviewCheckpoint(request)
	} else if err != nil {
		persistFailure(err, err.Error())
		return
	}
	if checkpoint.Stage != reviewStageAccepted {
		request = requestForResume(checkpoint)
	}
	if request.Source == "" {
		request.Source = job.Source
	}

	saveCheckpoint := func(stage string) error {
		if stage != "" {
			checkpoint.Stage = stage
		}
		if err := persistReviewCheckpoint(job, checkpoint); err != nil {
			return err
		}
		recorder.Flush()
		job.UpdatedAt = time.Now().UTC()
		if err := s.Store.Save(job); err != nil {
			return fmt.Errorf("保存审查阶段检查点: %w", err)
		}
		return nil
	}

	meter := newReviewBudgetMeter(job, s.Config, func() error {
		recorder.Flush()
		job.UpdatedAt = time.Now().UTC()
		if err := s.Store.Save(job); err != nil {
			return fmt.Errorf("预算检查点持久化失败")
		}
		return nil
	})
	ctx = withReviewBudget(ctx, meter)
	if err := meter.recoverReservations(); err != nil {
		persistFailure(err, "恢复模型预算预留失败")
		return
	}
	if job.StartedAt.IsZero() {
		job.StartedAt = time.Now().UTC()
	}
	job.Status = "running"
	job.ReviewOutcome = ""
	job.FinishedAt = nil
	job.UpdatedAt = time.Now().UTC()
	if err := s.Store.Save(job); err != nil {
		setReviewFailure(job, err, "保存审查运行状态失败")
		return
	}
	defer func() {
		if checkpoint.Stage == reviewStageCompleted && job.Status == "running" {
			job.Status = checkpoint.FinalStatus
			job.ReviewOutcome = checkpoint.FinalOutcome
			job.Error = checkpoint.FinalError
		}
		if job.ReviewOutcome == "" {
			if job.Status == "failed" {
				job.ReviewOutcome = "failed"
			} else {
				job.ReviewOutcome = "incomplete"
			}
		}
		if job.ReviewOutcome == "incomplete" && job.Status == "completed" {
			job.Status = "completed_with_warnings"
		}
		if checkpoint.Stage == reviewStageCompleted {
			s.completeReviewTask(job, recorder)
		}
		recorder.Flush()
		finished := time.Now().UTC()
		job.FinishedAt = &finished
		job.UpdatedAt = finished
		_ = s.Store.Save(job)
	}()

	artifacts := checkpoint.Artifacts
	artifacts.SanitizedDiff = checkpoint.Diff
	if !checkpointReached(checkpoint, reviewStagePreflight) {
		if strings.TrimSpace(request.Diff) == "" {
			resolved, diff, fetchErr := s.fetchReviewDiff(ctx, job, request, recorder)
			if fetchErr != nil {
				setReviewFailure(job, fetchErr, fetchErr.Error())
				return
			}
			job.Source = resolved
			request.Source = resolved
			request.Diff = diff
		}
		preflightArtifacts := &ReviewArtifacts{}
		if err := s.Loop.Run(ctx, ToolInput{
			Job: job, Diff: request.Diff, Tracer: recorder, Artifacts: preflightArtifacts,
		}); err != nil {
			setReviewFailure(job, err, err.Error())
			recorder.Flush()
			return
		}
		artifacts = *preflightArtifacts
		checkpoint.Artifacts = artifacts
		checkpoint.Diff = artifacts.SanitizedDiff
		checkpoint.Request = safeCheckpointRequest(request)
		if err := saveCheckpoint(reviewStagePreflight); err != nil {
			setReviewFailure(job, err, "前置检查结果持久化失败")
			return
		}
	} else {
		request = requestForResume(checkpoint)
		request.Source = job.Source
		artifacts = checkpoint.Artifacts
		artifacts.SanitizedDiff = checkpoint.Diff
	}
	job.ReviewScope = reviewScopeFromArtifacts(artifacts)
	incompleteReason := preflightIncompleteReason(artifacts)

	var initialSourceSnapshot *reviewSourceSnapshot
	var promptContext ReviewPromptContext
	if checkpointReached(checkpoint, reviewStageContext) {
		initialSourceSnapshot = restoreSnapshotFromCheckpoint(job.Source, s.Config, checkpoint.InitialSource)
		promptContext = checkpoint.PromptContext
	} else {
		if decision := s.Loop.Policy.Decide(PermissionLLMInference); decision != PermissionAllow {
			setReviewFailure(job, permissionError("subagent_review", PermissionLLMInference, decision),
				permissionError("subagent_review", PermissionLLMInference, decision).Error())
			return
		}
		if s.SkillError != nil {
			setReviewFailure(job, s.SkillError, fmt.Sprintf("加载 Agent skills 失败：%v", s.SkillError))
			return
		}
		skillSpan := recorder.Start("input", "load_skill", "skill", "code-review", "")
		skill, skillErr := s.SkillLoader.Load("code-review")
		if skillErr != nil {
			skillSpan.End(TraceResult{Err: skillErr})
			setReviewFailure(job, skillErr, fmt.Sprintf("加载 code-review skill 失败：%v", skillErr))
			return
		}
		skillSpan.End(TraceResult{Output: "已加载完整 SKILL.md"})
		_ = s.Store.RecordToolCall(job.ID, skillSpan.ID(), "load_skill", "succeeded", skill.Name, "已加载完整 SKILL.md", "", skillSpan.DurationMs())

		memoryQuery := strings.TrimSpace(request.MemoryQuery)
		if memoryQuery == "" {
			memoryQuery = job.Source + "\n" + redact(artifacts.SanitizedDiff)
		}
		memorySpan := recorder.Start("input", "memory_recall", "memory", "review request", "")
		memories, memoryErr := s.MemoryStore.Recall(memoryQuery)
		if memoryErr != nil {
			memorySpan.End(TraceResult{Err: memoryErr})
			_ = s.Store.RecordToolCall(job.ID, memorySpan.ID(), "memory_recall", "failed", "review request", "", memoryErr.Error(), memorySpan.DurationMs())
		} else {
			memoryOutput := fmt.Sprintf("召回 %d 条相关持久记忆", len(memories))
			memorySpan.End(TraceResult{Output: memoryOutput})
			_ = s.Store.RecordToolCall(job.ID, memorySpan.ID(), "memory_recall", "succeeded", "review request", memoryOutput, "", memorySpan.DurationMs())
		}
		promptContext = ReviewPromptContext{
			Catalog:      redactReviewInput(s.SkillLoader.Catalog()),
			SkillContent: redactReviewInput(skill.Content),
			Memories:     redactReviewInput(renderMemories(memories)),
			Evidence:     redactReviewInput(artifacts.PromptSummary()),
		}
		initialSourceSnapshot = s.prepareFirstPassReviewSource(ctx, job, recorder)
		promptContext.SourceContextAvailable = initialSourceSnapshot != nil
		checkpoint.PromptContext = promptContext
		checkpoint.InitialSource = snapshotForCheckpoint(initialSourceSnapshot)
		if err := saveCheckpoint(reviewStageContext); err != nil {
			setReviewFailure(job, err, "审查上下文持久化失败")
			return
		}
	}

	if !checkpointReached(checkpoint, reviewStageCandidates) {
		if decision := s.Loop.Policy.Decide(PermissionLLMInference); decision != PermissionAllow {
			denied := permissionError("review_agent", PermissionLLMInference, decision)
			setReviewFailure(job, denied, denied.Error())
			return
		}
		reviewCtx := withReviewSourceSnapshot(ctx, initialSourceSnapshot)
		reviewCtx, flushHarness := s.withReviewHarness(reviewCtx, job, artifacts.SanitizedDiff, artifacts)
		reviewCtx = withGoalCondition(reviewCtx, request.Goal)
		if s.Loop.Hooks != nil {
			s.Loop.Hooks.Emit(ctx, HookPreToolUse, HookContext{
				JobID: job.ID, Tool: "review_agent", Permission: PermissionLLMInference,
				Reason: "执行单 Agent 代码审查",
			})
		}
		result := RunReviewAgent(reviewCtx, s.Config, artifacts.SanitizedDiff, promptContext)
		flushHarness()
		setTodoStatus(job, 1, "completed")
		traceID := result.TraceID
		if traceID == "" {
			traceID = id("review_agent" + job.ID)
		}
		if result.Error != nil {
			_ = s.Store.RecordToolCall(job.ID, traceID, "review_agent", "failed", "单 Agent 代码审查", "", redact(result.Error.Error()), result.DurationMs)
			if s.Loop.Hooks != nil {
				s.Loop.Hooks.Emit(ctx, HookToolError, HookContext{
					JobID: job.ID, Tool: "review_agent", Permission: PermissionLLMInference,
					Reason: "执行单 Agent 代码审查", Error: result.Error, DurationMs: result.DurationMs,
				})
			}
			var goalStop *GoalStopError
			if errors.As(result.Error, &goalStop) {
				job.Status = "completed_with_warnings"
				job.ReviewOutcome = "incomplete"
				job.Error = goalStop.Error()
				updateReviewCheck(&job.ReviewScope, "finding_verification", "incomplete", "最终审查步骤未能生成完整结论")
				return
			}
			setReviewFailure(job, result.Error, "审查未完成："+redact(result.Error.Error()))
			return
		}
		_ = s.Store.RecordToolCall(job.ID, traceID, "review_agent", "succeeded", "单 Agent 代码审查", "审查候选已生成", "", result.DurationMs)
		if s.Loop.Hooks != nil {
			s.Loop.Hooks.Emit(ctx, HookPostToolUse, HookContext{
				JobID: job.ID, Tool: "review_agent", Permission: PermissionLLMInference,
				Reason: "执行单 Agent 代码审查", Output: "审查候选已生成", DurationMs: result.DurationMs,
			})
		}
		findings, parseErr := parseFindingsStrict(result.Summary)
		if parseErr != nil {
			recorder.Record("input", "finding_verification", "verification", "解析模型 finding", "", TraceResult{
				Err: parseErr, Origin: "orchestrator",
			})
			job.Status = "completed_with_warnings"
			job.ReviewOutcome = "incomplete"
			job.Error = "审查未完成：模型输出不是有效的 finding JSON 数组。"
			updateReviewCheck(&job.ReviewScope, "finding_verification", "incomplete", "模型输出格式无效，未能完成 finding 核验")
			return
		}
		withEvidence, rejectedEvidence := validateFindingEvidence(findings, artifacts.SanitizedDiff)
		for index := range withEvidence {
			withEvidence[index] = redactCheckpointFinding(withEvidence[index])
		}
		checkpoint.Candidates = withEvidence
		checkpoint.RejectedEvidence = rejectedEvidence
		checkpoint.ModelTraceID = traceID
		checkpoint.CandidateSource = snapshotForCheckpoint(initialSourceSnapshot)
		checkpoint.IncompleteReason = incompleteReason
		if err := saveCheckpoint(reviewStageCandidates); err != nil {
			setReviewFailure(job, err, "首轮审查结果持久化失败")
			return
		}
	} else {
		initialSourceSnapshot = restoreSnapshotFromCheckpoint(job.Source, s.Config, checkpoint.CandidateSource)
		incompleteReason = checkpoint.IncompleteReason
	}
	if !checkpointReached(checkpoint, reviewStageFindingContext) {
		initialSourceSnapshot = snapshotForCandidateContext(
			ctx, s, job, checkpoint.Candidates, artifacts, initialSourceSnapshot, recorder,
			&checkpoint.SourceContextError,
		)
		checkpoint.CandidateSource = snapshotForCheckpoint(initialSourceSnapshot)
		if err := saveCheckpoint(reviewStageFindingContext); err != nil {
			setReviewFailure(job, err, "finding 源码上下文检查点持久化失败")
			return
		}
	} else {
		initialSourceSnapshot = restoreSnapshotFromCheckpoint(job.Source, s.Config, checkpoint.CandidateSource)
	}

	if checkpointReached(checkpoint, reviewStageFinalizing) {
		s.finishCheckpointedReview(ctx, job, request, recorder, &checkpoint, saveCheckpoint)
		return
	}
	if checkpoint.Stage != reviewStageVerification {
		if err := saveCheckpoint(reviewStageVerification); err != nil {
			setReviewFailure(job, err, "逐条复核检查点持久化失败")
			return
		}
	}

	for checkpoint.VerificationCursor < len(checkpoint.Candidates) {
		index := checkpoint.VerificationCursor
		finding := checkpoint.Candidates[index]
		sourceFiles := map[string]string{}
		if initialSourceSnapshot != nil {
			sourceFiles = initialSourceSnapshot.files
		}
		sourceExcerpt := findingSourceExcerpt(sourceFiles, finding, 12000)
		findingContextError := checkpoint.SourceContextError
		if sourceExcerpt == "" && len(sourceFiles) > 0 {
			findingContextError = "固定提交源码与审查 diff 行不一致，或对应文件未读取"
		}
		verdict, reason, verifyTraceID, verifyErr := verifyFindingIndependently(ctx, findingVerificationRequest{
			Config: s.Config, Diff: artifacts.SanitizedDiff, Finding: finding,
			SourceExcerpt: sourceExcerpt, SourceContextError: findingContextError,
			SourceSnapshot: initialSourceSnapshot, Policy: s.Loop.Policy,
			RecordTool: func(
				toolCtx context.Context,
				name string,
				callID string,
				status string,
				input string,
				output string,
				started time.Time,
				ended time.Time,
				duration int64,
			) {
				traceID, _ := recorder.RecordAt(
					"tool", name, "verification", input,
					traceParentFrom(toolCtx), started, ended,
					TraceResult{Status: status, Output: output, Origin: "model", ToolCallID: callID},
				)
				_ = s.Store.RecordToolCall(
					job.ID, traceID, name, status, input, output, "", duration,
				)
			},
			Recorder: recorder,
		})
		checkpoint.CandidateSource = snapshotForCheckpoint(initialSourceSnapshot)
		if verifyErr != nil {
			if isIncompleteReviewError(verifyErr) {
				checkpoint.LastVerificationError = "部分问题的第二轮复核没有完成。"
				if err := saveCheckpoint(reviewStageVerification); err != nil {
					setReviewFailure(job, err, "逐条复核恢复点持久化失败")
					return
				}
				job.Status = "completed_with_warnings"
				job.ReviewOutcome = "incomplete"
				job.Error = checkpoint.LastVerificationError
				updateReviewCheck(&job.ReviewScope, "finding_verification", "incomplete", checkpoint.LastVerificationError)
				return
			}
			setReviewFailure(job, verifyErr, "第二轮复核模型调用失败："+redact(verifyErr.Error()))
			updateReviewCheck(&job.ReviewScope, "finding_verification", "failed", "第二轮复核模型调用失败")
			return
		}
		checkpoint.LastVerificationError = ""
		switch verdict {
		case findingRejected:
			checkpoint.VerificationResults = append(checkpoint.VerificationResults, checkpointVerification{Finding: finding, Verdict: string(findingRejected)})
		case findingInconclusive:
			checkpoint.VerificationResults = append(checkpoint.VerificationResults, checkpointVerification{Finding: finding, Verdict: string(findingInconclusive)})
		default:
			finding.VerificationStatus = "second_pass_review_passed"
			finding.VerificationReason = reason
			finding.VerificationTraceID = verifyTraceID
			checkpoint.VerificationResults = append(checkpoint.VerificationResults, checkpointVerification{Finding: finding, Verdict: string(findingConfirmed)})
		}
		checkpoint.VerificationCursor++
		if err := saveCheckpoint(reviewStageVerification); err != nil {
			setReviewFailure(job, err, "逐条复核结果持久化失败")
			return
		}
	}

	s.finishCheckpointedReview(ctx, job, request, recorder, &checkpoint, saveCheckpoint)
}

func redactCheckpointFinding(finding ReviewFinding) ReviewFinding {
	finding.File = redactFindingText(finding.File)
	finding.Body = redactFindingText(finding.Body)
	finding.Evidence = redactFindingText(finding.Evidence)
	finding.Trigger = redactFindingText(finding.Trigger)
	finding.Impact = redactFindingText(finding.Impact)
	finding.Suggestion = redactFindingText(finding.Suggestion)
	finding.VerificationReason = redactFindingText(finding.VerificationReason)
	return finding
}

func (s *Service) fetchReviewDiff(
	ctx context.Context,
	job *model.ReviewJob,
	request model.ReviewRequest,
	recorder *TraceRecorder,
) (string, string, error) {
	span := recorder.Start("tool", "diff_fetcher", "action", request.Source, "")
	if decision := s.Loop.Policy.Decide(PermissionNetworkFetch); decision != PermissionAllow {
		err := permissionError("diff_fetcher", PermissionNetworkFetch, decision)
		span.End(TraceResult{Status: "denied", Err: err, Origin: "orchestrator"})
		return "", "", err
	}
	started := time.Now()
	resolved, diff, err := fetchDiff(ctx, request.Source, s.Config)
	duration := time.Since(started).Milliseconds()
	output := fmt.Sprintf("diff_bytes=%d", len(diff))
	if err != nil {
		output = ""
	}
	span.End(TraceResult{Output: output, Err: err, Origin: "orchestrator"})
	status := "succeeded"
	callError := ""
	if err != nil {
		status = "failed"
		callError = err.Error()
	}
	_ = s.Store.RecordToolCall(job.ID, span.ID(), "diff_fetcher", status, request.Source, output, callError, duration)
	return resolved, diff, err
}

func reviewScopeFromArtifacts(artifacts ReviewArtifacts) model.ReviewScope {
	scope := model.ReviewScope{
		FilesReviewed: len(artifacts.Files),
		AddedLines:    artifacts.AddedLines,
		Checks:        make([]model.ReviewCheck, 0, len(artifacts.Checks)+2),
	}
	for _, check := range artifacts.Checks {
		scope.Checks = append(scope.Checks, model.ReviewCheck{
			Name: check.Name, Status: check.Status, Message: check.Message,
		})
	}
	return scope
}

func snapshotForCandidateContext(
	ctx context.Context,
	service *Service,
	job *model.ReviewJob,
	findings []ReviewFinding,
	artifacts ReviewArtifacts,
	current *reviewSourceSnapshot,
	recorder *TraceRecorder,
	errorMessage *string,
) *reviewSourceSnapshot {
	if len(findings) == 0 {
		return current
	}
	if _, supported := parseReviewGitHubPR(job.Source); !supported {
		return current
	}
	permission := PermissionRepositoryRead
	decision := service.Loop.Policy.Decide(permission)
	if decision == PermissionAllow {
		permission = PermissionNetworkFetch
		decision = service.Loop.Policy.Decide(permission)
	}
	if decision != PermissionAllow {
		*errorMessage = permissionError("source_context_fetch", permission, decision).Error()
		recorder.Record("tool", "source_context_fetch", "verification", "读取 PR 固定提交源码", "", TraceResult{
			Status: "denied", Err: errors.New(*errorMessage), Origin: "orchestrator",
		})
		return current
	}
	paths := make([]string, 0, len(artifacts.Files)+len(findings))
	seen := map[string]bool{}
	for _, finding := range findings {
		if !seen[finding.File] {
			paths = append(paths, finding.File)
			seen[finding.File] = true
		}
	}
	for _, file := range artifacts.Files {
		if !seen[file.Path] {
			paths = append(paths, file.Path)
			seen[file.Path] = true
		}
	}
	saved := snapshotForCheckpoint(current)
	snapshot, err := loadReviewSourceReaderWithFindings(ctx, reviewSourceSnapshotRequest{
		Source: job.Source, Config: service.Config, Paths: paths, Findings: findings,
		PinnedHeadSHA: saved.HeadSHA, PinnedOwner: saved.Owner, PinnedRepo: saved.Repo, ExistingFiles: saved.Files,
	})
	if snapshot != nil {
		current = snapshot
	}
	if err != nil {
		*errorMessage = redactFindingText(err.Error())
		recorder.Record("tool", "source_context_fetch", "verification", "读取 PR 固定提交源码", "", TraceResult{
			Err: err, Origin: "orchestrator",
		})
		return current
	}
	output := "无固定提交源码上下文"
	if current != nil {
		output = fmt.Sprintf("固定 head 可用文件=%d", len(current.files))
	}
	recorder.Record("tool", "source_context_fetch", "verification", "读取 PR 固定提交源码", "", TraceResult{
		Output: output, Origin: "orchestrator",
	})
	return current
}

func (s *Service) finishCheckpointedReview(
	ctx context.Context,
	job *model.ReviewJob,
	request model.ReviewRequest,
	recorder *TraceRecorder,
	checkpoint *reviewCheckpoint,
	saveCheckpoint func(string) error,
) {
	confirmed := make([]ReviewFinding, 0, len(checkpoint.VerificationResults))
	rejectedByVerifier := 0
	inconclusive := 0
	for _, result := range checkpoint.VerificationResults {
		switch result.Verdict {
		case string(findingConfirmed):
			confirmed = append(confirmed, result.Finding)
		case string(findingRejected):
			rejectedByVerifier++
		case string(findingInconclusive):
			inconclusive++
		}
	}
	job.Comments = verifiedComments(confirmed, checkpoint.Artifacts, checkpoint.ModelTraceID)
	verificationStatus := "passed"
	verificationMessage := fmt.Sprintf(
		"候选=%d；证据匹配=%d；第二轮复核确认=%d；第二轮复核排除=%d；证据待定=%d",
		len(checkpoint.Candidates)+checkpoint.RejectedEvidence,
		len(checkpoint.Candidates), len(job.Comments), rejectedByVerifier, inconclusive,
	)
	if len(checkpoint.Candidates)+checkpoint.RejectedEvidence == 0 && checkpoint.IncompleteReason == "" {
		verificationStatus = "not_needed"
		verificationMessage = "模型未报告候选问题，无需逐条复核"
	} else if checkpoint.IncompleteReason != "" || checkpoint.LastVerificationError != "" ||
		checkpoint.RejectedEvidence > 0 || inconclusive > 0 {
		verificationStatus = "incomplete"
		verificationMessage += fmt.Sprintf("；证据不足=%d", checkpoint.RejectedEvidence)
		if checkpoint.LastVerificationError != "" {
			verificationMessage += "；复核未完成=1；" + checkpoint.LastVerificationError
		}
	}
	updateReviewCheck(&job.ReviewScope, "finding_verification", verificationStatus, verificationMessage)
	recorder.Record("input", "finding_verification", "verification", "代码证据与第二轮复核", "", TraceResult{
		Output: verificationMessage, Origin: "orchestrator",
	})
	if checkpoint.IncompleteReason == "" && checkpoint.RejectedEvidence > 0 {
		checkpoint.IncompleteReason = "有候选问题缺少与变更行完全匹配的代码证据，审查未能完整核验。"
	}
	if checkpoint.IncompleteReason == "" && inconclusive > 0 {
		checkpoint.IncompleteReason = "部分候选问题缺少判定所需的代码上下文，审查未能完整核验。"
	}
	if checkpoint.IncompleteReason != "" || checkpoint.LastVerificationError != "" {
		checkpoint.FinalStatus = "completed_with_warnings"
		checkpoint.FinalOutcome = "incomplete"
		checkpoint.FinalError = checkpoint.IncompleteReason
		if checkpoint.FinalError == "" {
			checkpoint.FinalError = checkpoint.LastVerificationError
		}
	} else if len(job.Comments) > 0 {
		checkpoint.FinalOutcome = "completed_with_findings"
		checkpoint.FinalStatus = "completed"
		checkpoint.FinalError = ""
	} else {
		checkpoint.FinalOutcome = "completed_no_findings"
		checkpoint.FinalStatus = "completed"
		checkpoint.FinalError = ""
	}
	setTodoStatus(job, 2, "completed")
	if err := ctx.Err(); err != nil {
		setReviewFailure(job, err, "后台审查已取消")
		return
	}
	if checkpoint.Stage != reviewStageFinalizing {
		checkpoint.Stage = reviewStageFinalizing
		if err := saveCheckpoint(reviewStageFinalizing); err != nil {
			setReviewFailure(job, err, "审查结论检查点持久化失败")
			return
		}
	}
	if !checkpoint.MemoryExtractionStarted {
		checkpoint.MemoryExtractionStarted = true
		if err := saveCheckpoint(reviewStageFinalizing); err != nil {
			setReviewFailure(job, err, "记忆提取检查点持久化失败")
			return
		}
		s.extractReviewMemories(ctx, job, request)
		if err := saveCheckpoint(reviewStageFinalizing); err != nil {
			setReviewFailure(job, err, "记忆提取结果检查点持久化失败")
			return
		}
	}
	checkpoint.Stage = reviewStageCompleted
	if err := saveCheckpoint(reviewStageCompleted); err != nil {
		setReviewFailure(job, err, "审查完成状态持久化失败")
	}
}
