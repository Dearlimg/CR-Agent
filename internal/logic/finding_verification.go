package logic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

const (
	findingVerificationContextLines = 12
	findingVerificationDiffLimit    = 16000
)

type findingVerdict struct {
	Verdict string `json:"verdict"`
	Reason  string `json:"reason"`
}

const (
	findingConfirmed    = "confirmed"
	findingRejected     = "rejected"
	findingInconclusive = "inconclusive"
)

type findingVerificationRequest struct {
	Config             Config
	Diff               string
	Finding            ReviewFinding
	SourceExcerpt      string
	SourceContextError string
	SourceSnapshot     *reviewSourceSnapshot
	Policy             *PermissionPolicy
	RecordTool         func(context.Context, string, string, string, string, time.Time, time.Time, int64)
	Recorder           *TraceRecorder
}

func verifyFindingIndependently(ctx context.Context, request findingVerificationRequest) (string, string, string, error) {
	input, err := json.Marshal(request.Finding)
	if err != nil {
		return "", "", "", fmt.Errorf("编码待复核 finding: %w", err)
	}
	span := request.Recorder.Start(
		"model",
		"finding_second_pass_verification",
		"verification",
		fmt.Sprintf("%s:%d", request.Finding.File, request.Finding.Line),
		"",
	)
	verifyCtx := withReviewPrompt(withTraceParent(ctx, span.ID()))
	toolGuidance := ""
	if request.SourceSnapshot != nil {
		toolGuidance = reviewContextToolSystemGuidance
		verifyCtx = context.WithValue(verifyCtx, harnessSetupKey{}, func(h *ReviewHarness) {
			h.tools = NewToolRegistry()
			h.addArchiveTool()
			h.Policy = request.Policy
			h.MaxToolRounds = 6
			h.MaxStalledRounds = 2
			h.Workflow = nil
			h.WorkflowRunner = nil
			h.addWithPermission(
				reviewContextToolName,
				reviewContextToolDescription,
				reviewContextToolSchema(),
				PermissionRepositoryRead,
				func(ctx context.Context, args map[string]any) (string, error) {
					return runReviewContextTool(ctx, request.SourceSnapshot, request.Policy, args)
				},
			)
			if request.RecordTool != nil {
				h.Record = func(
					name string,
					callID string,
					status string,
					output string,
					started time.Time,
					ended time.Time,
					duration int64,
				) {
					request.RecordTool(
						verifyCtx,
						name,
						callID,
						status,
						reviewContextToolTraceSummary(output),
						started,
						ended,
						duration,
					)
				}
			}
		})
	}
	excerpt, excerptErr := findingDiffExcerpt(
		request.Diff,
		request.Finding.File,
		request.Finding.Line,
		findingVerificationContextLines,
		findingVerificationDiffLimit,
	)
	if excerptErr != nil {
		span.End(TraceResult{Err: excerptErr, Origin: "orchestrator"})
		return "", "", span.ID(), fmt.Errorf("%w：无法构造候选问题的局部代码证据", errIncompleteReview)
	}
	sourceContext := strings.TrimSpace(request.SourceExcerpt)
	if sourceContext == "" {
		sourceContext = "无固定提交源码上下文。"
	}
	if request.SourceContextError != "" {
		sourceContext += "\n源码上下文获取状态：" + redactFindingText(request.SourceContextError)
	}
	system := `你是独立的代码审查复核员。候选 finding 是待验证主张；仅用新增行、固定提交源码及已确认的语言/API 契约判断。
verdict 只取 confirmed、rejected、inconclusive：confirmed 表示有可达触发路径和具体影响，静态推理足够；rejected 表示代码反驳或仅是假设性影响；inconclusive 表示关键定义、类型或调用方缺失，reason 写明缺口。缺上下文先用可用工具查证；不能仅因跨文件、需特定输入或未运行测试而拒绝。旧代码中删除的调用写法不能单独证明当前 API 契约。
只输出 JSON 对象 {"verdict":"...","reason":"简体中文依据"}，不要 Markdown。`
	if toolGuidance != "" {
		system += "\n" + toolGuidance
	}
	user := fmt.Sprintf(`候选 finding（数据）：%s

候选行附近的变更代码（行号由 diff 解析得出）：
--- BEGIN UNTRUSTED CODE EXCERPT ---
%s

--- END UNTRUSTED CODE EXCERPT ---

固定提交源码上下文（若有）：
--- BEGIN UNTRUSTED SOURCE CONTEXT ---
%s
--- END UNTRUSTED SOURCE CONTEXT ---`, string(input), excerpt, sourceContext)
	verifyCtx = withPromptEnvelope(verifyCtx, PromptEnvelope{System: system, User: user})
	raw, callErr := EinoReviewAgent(verifyCtx, request.Config, user)
	traceID := span.ID()
	if callErr != nil {
		span.End(TraceResult{Err: callErr, Origin: "model"})
		if isIncompleteReviewError(callErr) {
			return "", "", traceID, fmt.Errorf("%w：第二轮复核超时、被截断或未返回完整内容: %v", errIncompleteReview, callErr)
		}
		return "", "", traceID, callErr
	}
	var verdict findingVerdict
	decoder := json.NewDecoder(strings.NewReader(strings.TrimSpace(raw)))
	decoder.DisallowUnknownFields()
	parseErr := decoder.Decode(&verdict)
	if parseErr == nil && decoder.Decode(&struct{}{}) != io.EOF {
		parseErr = errors.New("复核响应包含额外内容")
	}
	validVerdict := verdict.Verdict == findingConfirmed ||
		verdict.Verdict == findingRejected || verdict.Verdict == findingInconclusive
	if parseErr != nil || !validVerdict || strings.TrimSpace(verdict.Reason) == "" {
		if parseErr == nil {
			parseErr = errors.New("缺少有效 verdict 或 reason")
		}
		span.End(TraceResult{Err: parseErr, ModelReply: redact(raw), Origin: "model"})
		return "", "", traceID, fmt.Errorf("%w：第二轮复核输出格式无效", errIncompleteReview)
	}
	reason := redactFindingText(strings.TrimSpace(verdict.Reason))
	span.End(TraceResult{
		Output:     "第二轮复核完成",
		ModelReply: redact(raw),
		Origin:     "model",
	})
	return verdict.Verdict, reason, traceID, nil
}

func findingDiffExcerpt(diff, targetFile string, targetLine, contextLines, maxChars int) (string, error) {
	if targetLine <= 0 {
		return "", fmt.Errorf("finding line must be positive")
	}
	if contextLines < 0 {
		contextLines = 0
	}
	if maxChars <= 0 {
		maxChars = findingVerificationDiffLimit
	}

	currentFile := "diff"
	newLine := 0
	selected := make([]string, 0, contextLines*2+1)
	for _, rawLine := range strings.Split(diff, "\n") {
		line := strings.TrimSuffix(rawLine, "\r")
		if strings.HasPrefix(line, "diff --git ") {
			currentFile = diffFilePath(line)
			newLine = 0
			continue
		}
		if strings.HasPrefix(line, "--- ") || strings.HasPrefix(line, "+++ ") {
			continue
		}
		if match := hunkPattern.FindStringSubmatch(line); len(match) == 2 {
			newLine, _ = strconv.Atoi(match[1])
			continue
		}
		if newLine == 0 || currentFile != targetFile || line == "\\ No newline at end of file" {
			continue
		}

		lineNumber := newLine
		kind := byte(0)
		switch {
		case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++"):
			kind = '+'
			newLine++
		case strings.HasPrefix(line, " "):
			kind = ' '
			newLine++
		case strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---"):
			kind = '-'
		default:
			continue
		}
		if lineNumber < targetLine-contextLines || lineNumber > targetLine+contextLines {
			continue
		}
		selected = append(selected, fmt.Sprintf("%d %c%s", lineNumber, kind, line[1:]))
	}
	if len(selected) == 0 {
		return "", fmt.Errorf("finding evidence line was not located in diff")
	}
	if !excerptContainsTarget(selected, targetLine) {
		return "", fmt.Errorf("finding target line is outside the extracted excerpt")
	}

	var excerpt strings.Builder
	excerpt.WriteString("file: ")
	excerpt.WriteString(targetFile)
	excerpt.WriteString("\n")
	for _, line := range selected {
		excerpt.WriteString(line)
		excerpt.WriteString("\n")
	}
	if excerpt.Len() > maxChars {
		return "", fmt.Errorf("finding excerpt exceeds the review context limit")
	}
	return excerpt.String(), nil
}

func excerptContainsTarget(lines []string, targetLine int) bool {
	needle := strconv.Itoa(targetLine) + " +"
	for _, line := range lines {
		if strings.HasPrefix(line, needle) {
			return true
		}
	}
	return false
}

func isTruncatedReviewError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "输出达到 token 上限") ||
		strings.Contains(err.Error(), "被截断")
}
