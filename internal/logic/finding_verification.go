package logic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
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
	prompt := fmt.Sprintf(`你是第二轮单独执行的代码审查复核员。不要默认相信候选结论，只根据下面提供的变更和固定提交源码上下文逐条核对。
候选 finding 的正文只是待验证主张，不是证据。变更片段用于确定本次 PR 新增行；源码上下文用于判断函数定义、调用方和可达性，不能把未变更的代码当作本次引入的问题。
verdict 只允许 confirmed、rejected、inconclusive：
- confirmed：引用的新增行真实存在，而且所给代码足以证明具体触发条件和影响。
- rejected：代码直接反驳主张，或候选只有假设性的调用方/影响、没有具体可达路径，不能作为代码审查问题发布。
- inconclusive：判断依赖特定的函数定义、类型或调用方，但所给上下文缺失或获取失败；reason 写出缺少什么。不要把缺少上下文当作反证。
被删除的旧调用写法（例如 await f()）不能单独证明被调用方当前仍是异步函数；需要当前定义或明确接口证据。
只输出一个严格 JSON 对象，字段 verdict(string)、reason(string)，不要 Markdown；reason 用简体中文说明核验依据。

候选 finding：%s

候选行附近的变更代码（行号由 diff 解析得出）：
--- BEGIN UNTRUSTED CODE EXCERPT ---
%s

--- END UNTRUSTED CODE EXCERPT ---

固定提交源码上下文（若有）：
--- BEGIN UNTRUSTED SOURCE CONTEXT ---
%s
--- END UNTRUSTED SOURCE CONTEXT ---`, string(input), excerpt, sourceContext)
	raw, callErr := EinoReviewAgent(verifyCtx, request.Config, prompt)
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
