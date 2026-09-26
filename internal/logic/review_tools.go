package logic

import (
	"CR-Agent/internal/model"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var filePattern = regexp.MustCompile(`^diff --git a/(.+) b/(.+)$`)

func normalizeReviewFindingsJSON(raw string) (string, []ReviewFinding, error) {
	return normalizeReviewFindingsJSONWithEmpty(raw, false)
}

func normalizeReviewFindingsJSONWithEmpty(raw string, allowEmpty bool) (string, []ReviewFinding, error) {
	findings, err := parseFindingsStrict(raw)
	if err != nil {
		return "", nil, fmt.Errorf("finding JSON 无效：%w", err)
	}
	if len(findings) == 0 && !allowEmpty {
		return "", nil, fmt.Errorf("finding JSON 不能为空数组")
	}

	encoded, err := json.Marshal(findings)
	if err != nil {
		return "", nil, fmt.Errorf("finding JSON 规范化失败：%w", err)
	}
	normalized := sanitizeModelReply(string(encoded))
	safeFindings, err := parseFindingsStrict(normalized)
	if err != nil {
		return "", nil, fmt.Errorf("finding JSON 脱敏后无效：%w", err)
	}
	return normalized, safeFindings, nil
}

func parseDiffTool(_ context.Context, in ToolInput) (ToolResult, error) {
	files := []string{}
	for _, file := range analyzeDiff(in.Diff).Files {
		files = append(files, file.Path)
	}
	return ToolResult{Output: fmt.Sprintf("解析完成 files=%v", files)}, nil
}

func changedLinesTool(_ context.Context, in ToolInput) (ToolResult, error) {
	return ToolResult{Output: fmt.Sprintf("diff 新增行=%d", analyzeDiff(in.Diff).AddedLines)}, nil
}

func secretScanTool(ctx context.Context, in ToolInput) (ToolResult, error) {
	findings, err := scanRawDiff(ctx, in.Diff)
	if err != nil {
		return ToolResult{}, err
	}
	return ToolResult{Output: fmt.Sprintf("secret_scan 命中=%d（仅统计 diff 新增行）", len(findings))}, nil
}

func dependencyDiffTool(_ context.Context, in ToolInput) (ToolResult, error) {
	files := analyzeDiff(in.Diff).DependencyFiles
	return ToolResult{Output: fmt.Sprintf("依赖文件变更=%v", files)}, nil
}

// verifiedComments validates model output against actual changed lines.
// It keeps the first copy of an identical finding and clamps enum values.
func verifiedComments(findings []ReviewFinding, artifacts ReviewArtifacts, traceID string) []model.ReviewComment {
	changed := map[string]map[int]bool{}
	for _, file := range artifacts.Files {
		lines := map[int]bool{}
		for _, line := range file.AddedLines {
			lines[line] = true
		}
		changed[normalizeReviewFilePath(file.Path)] = lines
	}
	seen := map[string]bool{}
	out := []model.ReviewComment{}
	for _, finding := range findings {
		file := normalizeReviewFilePath(strings.TrimSpace(finding.File))
		body := strings.TrimSpace(finding.Body)
		if !changed[file][finding.Line] || body == "" {
			continue
		}
		if suggestion := strings.TrimSpace(finding.Suggestion); suggestion != "" {
			body += "\n建议：" + suggestion
		}
		body = redactFindingText(body)
		key := fmt.Sprintf("%s:%d:%s", file, finding.Line, body)
		if seen[key] {
			continue
		}
		seen[key] = true
		severity := strings.ToLower(finding.Severity)
		if severity != "high" && severity != "medium" && severity != "low" {
			severity = "low"
		}
		confidence := strings.ToLower(finding.Confidence)
		if confidence != "high" && confidence != "medium" && confidence != "low" {
			confidence = "low"
		}
		commentTraceID := finding.VerificationTraceID
		if commentTraceID == "" {
			commentTraceID = traceID
		}
		out = append(out, model.ReviewComment{
			File: file, Line: finding.Line, Severity: severity,
			Confidence: confidence, Body: redactFindingText(strings.TrimSpace(finding.Body)),
			Evidence:           redactFindingText(strings.TrimSpace(finding.Evidence)),
			Trigger:            redactFindingText(strings.TrimSpace(finding.Trigger)),
			Impact:             redactFindingText(strings.TrimSpace(finding.Impact)),
			Suggestion:         redactFindingText(strings.TrimSpace(finding.Suggestion)),
			VerificationStatus: finding.VerificationStatus,
			VerificationReason: redactFindingText(strings.TrimSpace(finding.VerificationReason)),
			TraceID:            commentTraceID,
		})
	}
	priority := map[string]int{"high": 0, "medium": 1, "low": 2}
	sort.SliceStable(out, func(i, j int) bool {
		return priority[out[i].Severity] < priority[out[j].Severity]
	})
	return out
}

// pendingVerificationComment merges plausible findings into one unanchored
// PR-level comment. An empty File marks it as a pending-confirmation question
// to the author, never an inline defect claim; only confirmed findings become
// anchored comments.
func pendingVerificationComment(results []checkpointVerification, testsRan bool, traceID string) *model.ReviewComment {
	items := make([]checkpointVerification, 0, len(results))
	for _, result := range results {
		if result.Verdict == findingPlausible {
			items = append(items, result)
		}
	}
	if len(items) == 0 {
		return nil
	}
	severityRank := map[string]int{"high": 0, "medium": 1, "low": 2}
	confidenceRank := map[string]int{"high": 0, "medium": 1, "low": 2}
	severity, confidence := "low", "high"
	var builder strings.Builder
	fmt.Fprintf(&builder, "以下 %d 个疑点有代码依据，但第二轮复核仍有待核实前提，未作为缺陷报告；请确认前提是否成立：", len(items))
	for i, item := range items {
		finding := item.Finding
		if rank := severityRank[strings.ToLower(finding.Severity)]; rank < severityRank[severity] {
			severity = strings.ToLower(finding.Severity)
		}
		itemConfidence := strings.ToLower(finding.Confidence)
		assumptions := []string{}
		if item.Assessment != nil {
			itemConfidence = strings.ToLower(item.Assessment.Confidence)
			for _, assumption := range item.Assessment.Assumptions {
				if trimmed := strings.TrimSpace(assumption); trimmed != "" {
					assumptions = append(assumptions, redactFindingText(trimmed))
				}
			}
		}
		if rank, ok := confidenceRank[itemConfidence]; ok && rank > confidenceRank[confidence] {
			confidence = itemConfidence
		}
		fmt.Fprintf(&builder, "\n\n%d. %s:%d %s", i+1, finding.File, finding.Line, redactFindingText(strings.TrimSpace(finding.Body)))
		if len(assumptions) > 0 {
			fmt.Fprintf(&builder, "\n待核实前提：%s", strings.Join(assumptions, "；"))
		}
		if suggestion := strings.TrimSpace(finding.Suggestion); suggestion != "" {
			fmt.Fprintf(&builder, "\n建议：%s", redactFindingText(suggestion))
		}
	}
	coverage := "以上内容不是缺陷指控；前提确认后才构成缺陷。"
	if !testsRan {
		coverage = "本轮未运行测试；以上内容不是缺陷指控，前提确认后才构成缺陷。"
	}
	builder.WriteString("\n\n覆盖说明：" + coverage)
	commentTraceID := items[0].TraceID
	if commentTraceID == "" {
		commentTraceID = traceID
	}
	return &model.ReviewComment{
		File:               "",
		Line:               0,
		Severity:           severity,
		Confidence:         confidence,
		Body:               builder.String(),
		VerificationStatus: "second_pass_review_plausible",
		VerificationReason: "有代码依据的疑点汇总；复核未发现反证，前提待人工确认。",
		TraceID:            commentTraceID,
	}
}

// Evidence gate rejection reasons. A rejected finding contributes its primary
// reason plus missing_field when any narrative field is empty, so the sum of
// reasons can exceed the rejected count.
const (
	evidenceReasonMissingField    = "missing_field"
	evidenceReasonUnknownFile     = "unknown_file"
	evidenceReasonTextMismatch    = "text_mismatch"
	evidenceReasonContextOnly     = "context_only"
	evidenceReasonAmbiguousAnchor = "ambiguous_anchor"
)

// formatEvidenceRejections renders the rejection breakdown in a stable order
// for the finding_verification check message.
func formatEvidenceRejections(reasons map[string]int) string {
	if len(reasons) == 0 {
		return ""
	}
	order := []string{
		evidenceReasonUnknownFile,
		evidenceReasonTextMismatch,
		evidenceReasonContextOnly,
		evidenceReasonAmbiguousAnchor,
		evidenceReasonMissingField,
	}
	parts := make([]string, 0, len(reasons))
	for _, reason := range order {
		if reasons[reason] > 0 {
			parts = append(parts, fmt.Sprintf("%s=%d", reason, reasons[reason]))
		}
	}
	return strings.Join(parts, ", ")
}

func validateFindingEvidence(findings []ReviewFinding, diff string) ([]ReviewFinding, int, map[string]int) {
	added, context := diffLineContent(diff)
	verified := make([]ReviewFinding, 0, len(findings))
	rejected := 0
	reasons := map[string]int{}
	for _, finding := range findings {
		file := strings.TrimSpace(finding.File)
		line, evidence, reason := locateAddedEvidence(added, context, file, finding.Line, finding.Evidence)
		complete := strings.TrimSpace(finding.Body) != "" &&
			strings.TrimSpace(finding.Trigger) != "" &&
			strings.TrimSpace(finding.Impact) != "" &&
			strings.TrimSpace(finding.Suggestion) != ""
		if reason != "" {
			rejected++
			reasons[reason]++
			if !complete {
				reasons[evidenceReasonMissingField]++
			}
			continue
		}
		if !complete {
			rejected++
			reasons[evidenceReasonMissingField]++
			continue
		}
		finding.File = file
		finding.Line = line
		finding.Evidence = evidence
		verified = append(verified, finding)
	}
	return verified, rejected, reasons
}

// locateAddedEvidence keeps a valid anchor, or relocates it only when the quoted
// block has exactly one match among the same file's added lines. The returned
// reason is empty on success and otherwise classifies the failure: the quoted
// block may sit on unchanged context lines (context_only), match several added
// positions (ambiguous_anchor), not exist in the file (unknown_file), or not
// match any added line (text_mismatch).
func locateAddedEvidence(added, context map[string]map[int]string, file string, reportedLine int, evidence string) (int, string, string) {
	fileLines, hasFile := added[file]
	if !hasFile {
		return 0, "", evidenceReasonUnknownFile
	}
	if actual, ok := matchAddedEvidence(fileLines, reportedLine, evidence); ok {
		return reportedLine, actual, ""
	}
	if contextLines := context[file]; contextLines != nil {
		if _, ok := matchAddedEvidence(contextLines, reportedLine, evidence); ok {
			return 0, "", evidenceReasonContextOnly
		}
	}
	var matchedLine int
	var matchedEvidence string
	for line := range fileLines {
		actual, ok := matchAddedEvidence(fileLines, line, evidence)
		if !ok {
			continue
		}
		if matchedLine != 0 {
			return 0, "", evidenceReasonAmbiguousAnchor
		}
		matchedLine = line
		matchedEvidence = actual
	}
	if matchedLine == 0 {
		return 0, "", evidenceReasonTextMismatch
	}
	return matchedLine, matchedEvidence, ""
}

func matchAddedEvidence(added map[int]string, startLine int, evidence string) (string, bool) {
	if startLine <= 0 || strings.TrimSpace(evidence) == "" {
		return "", false
	}

	quotedLines := strings.Split(strings.TrimSpace(strings.ReplaceAll(evidence, "\r\n", "\n")), "\n")

	actualLines := make([]string, 0, len(quotedLines))
	for offset, quoted := range quotedLines {
		actual, exists := added[startLine+offset]
		if !exists || strings.TrimSpace(actual) != strings.TrimSpace(quoted) {
			return "", false
		}
		actualLines = append(actualLines, actual)
	}
	return strings.Join(actualLines, "\n"), true
}

// diffLineContent maps file -> new-side line number -> text, separately for
// added lines and unchanged context lines.
func diffLineContent(diff string) (map[string]map[int]string, map[string]map[int]string) {
	added := map[string]map[int]string{}
	context := map[string]map[int]string{}
	file := ""
	lineNumber := 0
	if !strings.Contains(diff, "diff --git ") {
		file = "diff"
		lineNumber = 1
		added[file] = map[int]string{}
	}
	for _, line := range strings.Split(diff, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if strings.HasPrefix(line, "diff --git ") {
			file = diffFilePath(line)
			lineNumber = 0
			if added[file] == nil {
				added[file] = map[int]string{}
			}
			if context[file] == nil {
				context[file] = map[int]string{}
			}
			continue
		}
		if match := hunkPattern.FindStringSubmatch(line); len(match) == 2 {
			lineNumber, _ = strconv.Atoi(match[1])
			continue
		}
		if lineNumber == 0 {
			continue
		}
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			added[file][lineNumber] = strings.TrimPrefix(line, "+")
			lineNumber++
			continue
		}
		if strings.HasPrefix(line, " ") {
			context[file][lineNumber] = strings.TrimPrefix(line, " ")
			lineNumber++
		}
	}
	return added, context
}

func addedLineContent(diff string) map[string]map[int]string {
	added, _ := diffLineContent(diff)
	return added
}
