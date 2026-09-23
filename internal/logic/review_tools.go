package logic

import (
	"CR-Agent/internal/model"
	"context"
	"fmt"
	"regexp"
	"sort"
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
		out = append(out, model.ReviewComment{
			File: file, Line: finding.Line, Severity: severity,
			Confidence: confidence, Body: body, TraceID: traceID,
		})
	}
	priority := map[string]int{"high": 0, "medium": 1, "low": 2}
	sort.SliceStable(out, func(i, j int) bool {
		return priority[out[i].Severity] < priority[out[j].Severity]
	})
	return out
}
