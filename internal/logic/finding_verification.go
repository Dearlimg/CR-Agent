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
	findingVerificationContextLines = 4
	findingVerificationDiffLimit    = 12000
)

type findingVerdict struct {
	IsReal *bool  `json:"is_real"`
	Reason string `json:"reason"`
}

type findingVerificationRequest struct {
	Config   Config
	Diff     string
	Finding  ReviewFinding
	Recorder *TraceRecorder
}

func verifyFindingIndependently(ctx context.Context, request findingVerificationRequest) (bool, string, string, error) {
	input, err := json.Marshal(request.Finding)
	if err != nil {
		return false, "", "", fmt.Errorf("编码待复核 finding: %w", err)
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
		return false, "", span.ID(), fmt.Errorf("%w：无法构造候选问题的局部代码证据", errIncompleteReview)
	}
	prompt := fmt.Sprintf(`你是第二轮单独执行的代码审查复核员。不要默认相信候选结论，只根据下面提供的变更逐条核对。
只有当证据原文确实出现在指定文件和变更行，触发条件可从代码逻辑推出，并且该触发条件会造成描述的影响时，is_real 才为 true。若依赖 diff 外假设、证据不匹配、触发条件不可达、影响不成立或只是风格建议，is_real 必须为 false。
候选 finding 的正文只是待验证主张，不是证据。给出的片段只包含候选行附近的 diff。
若结论依赖片段中未展示的函数定义、类型、导入或其它文件信息，不得把候选正文里的说法当成事实。
被删除的旧调用写法（例如 await f()）不能单独证明被调用方当前仍是异步函数；需要当前定义或明确接口证据。
只输出一个严格 JSON 对象，字段 is_real(boolean)、reason(string)，不要 Markdown；reason 用简体中文说明核验依据。

候选 finding：%s

候选行附近的变更代码（行号由 diff 解析得出）：
--- BEGIN UNTRUSTED CODE EXCERPT ---
%s

--- END UNTRUSTED CODE EXCERPT ---`, string(input), excerpt)
	raw, callErr := EinoReviewAgent(verifyCtx, request.Config, prompt)
	traceID := span.ID()
	if callErr != nil {
		span.End(TraceResult{Err: callErr, Origin: "model"})
		if isIncompleteReviewError(callErr) {
			return false, "", traceID, fmt.Errorf("%w：第二轮复核超时、被截断或未返回完整内容: %v", errIncompleteReview, callErr)
		}
		return false, "", traceID, callErr
	}
	var verdict findingVerdict
	decoder := json.NewDecoder(strings.NewReader(strings.TrimSpace(raw)))
	decoder.DisallowUnknownFields()
	parseErr := decoder.Decode(&verdict)
	if parseErr == nil && decoder.Decode(&struct{}{}) != io.EOF {
		parseErr = errors.New("复核响应包含额外内容")
	}
	if parseErr != nil || verdict.IsReal == nil || strings.TrimSpace(verdict.Reason) == "" {
		if parseErr == nil {
			parseErr = errors.New("缺少 is_real 或 reason")
		}
		span.End(TraceResult{Err: parseErr, ModelReply: redact(raw), Origin: "model"})
		return false, "", traceID, fmt.Errorf("%w：第二轮复核输出格式无效", errIncompleteReview)
	}
	reason := redactFindingText(strings.TrimSpace(verdict.Reason))
	span.End(TraceResult{
		Output:     "第二轮复核完成",
		ModelReply: redact(raw),
		Origin:     "model",
	})
	return *verdict.IsReal, reason, traceID, nil
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
