package logic

import (
	"CR-Agent/internal/model"
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var secretPattern = regexp.MustCompile(`(?i)(api[_-]?key|secret|password|token)\s*[:=]\s*["']?[A-Za-z0-9_\-/+=]{8,}`)
var filePattern = regexp.MustCompile(`^diff --git a/(.+) b/(.+)$`)

func registerReviewTools(reg *ToolRegistry) {
	reg.RegisterWithPermission("parse_diff", PermissionReadDiff, parseDiffTool)
	reg.RegisterWithPermission("get_changed_lines", PermissionReadDiff, changedLinesTool)
	reg.RegisterWithPermission("syntax_check", PermissionStaticAnalysis, syntaxCheckTool)
	reg.RegisterWithPermission("format_check", PermissionStaticAnalysis, formatCheckTool)
	reg.RegisterWithPermission("secret_scan", PermissionStaticAnalysis, secretScanTool)
	reg.RegisterWithPermission("dependency_diff", PermissionStaticAnalysis, dependencyDiffTool)
	reg.RegisterWithPermission("get_file_context", PermissionRepositoryRead, contextTool)
	reg.RegisterWithPermission("normalize_finding", PermissionReadDiff, normalizeFinding)
}

func parseDiffTool(_ context.Context, in ToolInput) (ToolResult, error) {
	files := []string{}
	for _, line := range strings.Split(in.Diff, "\n") {
		m := filePattern.FindStringSubmatch(line)
		if len(m) == 3 {
			files = append(files, m[2])
		}
	}
	return ToolResult{Output: fmt.Sprintf("解析完成 files=%v", files)}, nil
}
func changedLinesTool(_ context.Context, in ToolInput) (ToolResult, error) {
	n := 0
	for _, line := range strings.Split(in.Diff, "\n") {
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			n++
		}
	}
	return ToolResult{Output: fmt.Sprintf("新增代码行=%d", n)}, nil
}
func syntaxCheckTool(_ context.Context, in ToolInput) (ToolResult, error) {
	if strings.Contains(in.Diff, "<<<<<<<") || strings.Contains(in.Diff, "=======") || strings.Contains(in.Diff, ">>>>>>>") {
		return ToolResult{Output: "发现未解决的 merge conflict 标记"}, nil
	}
	return ToolResult{Output: "diff 未发现明显冲突标记；完整语法检查需要安全沙箱和仓库 checkout"}, nil
}
func formatCheckTool(_ context.Context, in ToolInput) (ToolResult, error) {
	if strings.Contains(in.Diff, "\t") || strings.Contains(in.Diff, "  ") {
		return ToolResult{Output: "发现可能需要格式化的变更，结果仅供参考"}, nil
	}
	return ToolResult{Output: "未发现明显格式化信号"}, nil
}
func secretScanTool(_ context.Context, in ToolInput) (ToolResult, error) {
	hits := secretPattern.FindAllString(redact(in.Diff), -1)
	return ToolResult{Output: fmt.Sprintf("secret_scan 命中=%d", len(hits))}, nil
}
func dependencyDiffTool(_ context.Context, in ToolInput) (ToolResult, error) {
	lines := []string{}
	for _, line := range strings.Split(in.Diff, "\n") {
		low := strings.ToLower(line)
		if strings.Contains(low, "go.mod") || strings.Contains(low, "package.json") || strings.Contains(low, "requirements.txt") || strings.Contains(low, "go.sum") {
			if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
				lines = append(lines, strings.TrimPrefix(line, "+"))
			}
		}
	}
	return ToolResult{Output: fmt.Sprintf("依赖变更=%v", lines)}, nil
}
func contextTool(_ context.Context, in ToolInput) (ToolResult, error) {
	return ToolResult{Output: "当前输入只有 diff，未配置安全仓库上下文读取器；未执行任意文件读取"}, nil
}
func normalizeFinding(_ context.Context, in ToolInput) (ToolResult, error) {
	return ToolResult{Output: "finding 归一化规则：severity/confidence/file/line/body 必须存在，未知值降级为 low/reference"}, nil
}

func normalizeComments(findings []ReviewFinding, traceID string) []model.ReviewComment {
	out := []model.ReviewComment{}
	for _, f := range findings {
		severity := strings.ToLower(f.Severity)
		if severity != "high" && severity != "medium" && severity != "low" {
			severity = "low"
		}
		confidence := strings.ToLower(f.Confidence)
		if confidence != "high" && confidence != "medium" && confidence != "low" {
			confidence = "low"
		}
		out = append(out, model.ReviewComment{File: f.File, Line: f.Line, Severity: severity, Confidence: confidence, Body: f.Body, TraceID: traceID})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Severity < out[j].Severity })
	return out
}
