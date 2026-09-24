package logic

import (
	"CR-Agent/internal/model"
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var filePattern = regexp.MustCompile(`^diff --git a/(.+) b/(.+)$`)

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

func syntaxCheckTool(_ context.Context, in ToolInput) (ToolResult, error) {
	return preflightCheckTool(in.Diff, "syntax_check"), nil
}

func formatCheckTool(_ context.Context, in ToolInput) (ToolResult, error) {
	return preflightCheckTool(in.Diff, "format_check"), nil
}

func preflightCheckTool(diff, name string) ToolResult {
	for _, check := range analyzeDiff(diff).Checks {
		if check.Name == name {
			return ToolResult{Output: check.Status + ": " + check.Message}
		}
	}
	return ToolResult{Output: "not_run: 检查不可用"}
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
		changed[file.Path] = lines
	}
	seen := map[string]bool{}
	out := []model.ReviewComment{}
	for _, finding := range findings {
		file := strings.TrimSpace(finding.File)
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

func validateFindingEvidence(findings []ReviewFinding, diff string) ([]ReviewFinding, int) {
	added := addedLineContent(diff)
	verified := make([]ReviewFinding, 0, len(findings))
	rejected := 0
	for _, finding := range findings {
		file := strings.TrimSpace(finding.File)
		line, evidence, evidenceMatches := locateAddedEvidence(
			added[file], finding.Line, finding.Evidence,
		)
		complete := strings.TrimSpace(finding.Body) != "" &&
			strings.TrimSpace(finding.Trigger) != "" &&
			strings.TrimSpace(finding.Impact) != "" &&
			strings.TrimSpace(finding.Suggestion) != ""
		if !evidenceMatches || !complete {
			rejected++
			continue
		}
		finding.File = file
		finding.Line = line
		finding.Evidence = evidence
		verified = append(verified, finding)
	}
	return verified, rejected
}

// locateAddedEvidence keeps a valid anchor, or relocates it only when the quoted
// block has exactly one match among the same file's added lines.
func locateAddedEvidence(added map[int]string, reportedLine int, evidence string) (int, string, bool) {
	if actual, ok := matchAddedEvidence(added, reportedLine, evidence); ok {
		return reportedLine, actual, true
	}

	var matchedLine int
	var matchedEvidence string
	for line := range added {
		actual, ok := matchAddedEvidence(added, line, evidence)
		if !ok {
			continue
		}
		if matchedLine != 0 {
			return 0, "", false
		}
		matchedLine = line
		matchedEvidence = actual
	}
	if matchedLine == 0 {
		return 0, "", false
	}
	return matchedLine, matchedEvidence, true
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

func addedLineContent(diff string) map[string]map[int]string {
	added := map[string]map[int]string{}
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
			lineNumber++
		}
	}
	return added
}
