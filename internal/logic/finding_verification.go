package logic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
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
	verifyCtx := withTraceParent(ctx, span.ID())
	prompt := fmt.Sprintf(`你是第二轮单独执行的代码审查复核员。不要默认相信候选结论，只根据下面提供的变更逐条核对。
只有当证据原文确实出现在指定文件和变更行，触发条件可从代码逻辑推出，并且该触发条件会造成描述的影响时，is_real 才为 true。若依赖 diff 外假设、证据不匹配、触发条件不可达、影响不成立或只是风格建议，is_real 必须为 false。
只输出一个严格 JSON 对象，字段 is_real(boolean)、reason(string)，不要 Markdown；reason 用简体中文说明核验依据。

候选 finding：%s

待核对 diff：
--- BEGIN UNTRUSTED DIFF ---
%s

--- END UNTRUSTED DIFF ---`, string(input), request.Diff)
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

func isTruncatedReviewError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "输出达到 token 上限") ||
		strings.Contains(err.Error(), "被截断")
}
