package logic

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

const (
	reviewContextToolMaxQuery       = 512
	reviewContextToolMaxRange       = 200
	reviewContextToolMaxResults     = 8
	reviewContextToolMaxChars       = 16000
	reviewContextToolDefaultRadius  = 12
	reviewContextToolMaxRadius      = 40
	reviewContextToolMaxLine        = reviewSourceMaxFileBytes
	reviewContextToolDescription    = "从本次 PR 固定 head 提交中检索源码；指定文件路径可按需拉取并搜索符号或读取行段。"
	reviewContextToolName           = "get_review_context"
	reviewContextToolSystemGuidance = "若候选依赖未展示的定义、调用方、类型、循环控制或 API 用法，先调用 get_review_context 查证，再给出 verdict。只使用工具返回的固定提交源码；拿不到上下文时仍应返回 inconclusive，不可猜测。"
	reviewAgentContextToolGuidance  = "首轮审查遇到需要确认的配置类型、函数定义、调用方、测试或 API 用法时，先调用 get_review_context 查证；首次读取文件时同时提供仓库相对 file 路径和 query，已读取文件可只给 query。不要仅因 diff 没展示上下文就跳过候选。只依据工具返回的本次 PR 固定 head 源码。"
)

func reviewContextToolSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "要查证的符号或简短关键词，例如 max_retry_count、all_tool_messages。",
			},
			"file": map[string]any{
				"type":        "string",
				"description": "首次读取时提供仓库相对路径，工具仅从该 PR 固定 head SHA 拉取此文件。",
			},
			"start_line": map[string]any{
				"type":        "integer",
				"minimum":     1,
				"maximum":     reviewContextToolMaxLine,
				"description": "与 end_line 一起指定要读取的首行。",
			},
			"end_line": map[string]any{
				"type":        "integer",
				"minimum":     1,
				"maximum":     reviewContextToolMaxLine,
				"description": "与 start_line 一起指定要读取的末行，最多读取 200 行。",
			},
			"radius": map[string]any{
				"type":        "integer",
				"minimum":     0,
				"maximum":     reviewContextToolMaxRadius,
				"description": "符号搜索命中行前后的上下文行数，默认 12，最大 40。",
			},
		},
		"required":             []string{"query"},
		"additionalProperties": false,
	}
}

func runReviewContextTool(
	ctx context.Context,
	snapshot *reviewSourceSnapshot,
	policy *PermissionPolicy,
	args map[string]any,
) (string, error) {
	if snapshot == nil {
		return "", fmt.Errorf("当前审查没有可读取的固定提交源码")
	}
	if policy == nil || policy.Decide(PermissionRepositoryRead) != PermissionAllow ||
		policy.Decide(PermissionNetworkFetch) != PermissionAllow {
		return "", fmt.Errorf("读取仓库源码的权限未授予")
	}
	query, err := requiredString(args, "query")
	if err != nil {
		return "", err
	}
	if len(query) > reviewContextToolMaxQuery {
		return "", fmt.Errorf("源码查询超过长度限制")
	}
	filePath, err := optionalString(args, "file")
	if err != nil {
		return "", err
	}
	startLine, err := optionalInt(args, "start_line")
	if err != nil {
		return "", err
	}
	endLine, err := optionalInt(args, "end_line")
	if err != nil {
		return "", err
	}
	radius, err := optionalInt(args, "radius")
	if err != nil {
		return "", err
	}
	if radius == 0 {
		radius = reviewContextToolDefaultRadius
	}
	if radius < 0 || radius > reviewContextToolMaxRadius {
		return "", fmt.Errorf("上下文范围必须在 1 到 %d 行之间", reviewContextToolMaxRadius)
	}
	if startLine < 0 || endLine < 0 {
		return "", fmt.Errorf("行号不能为负数")
	}
	_, hasStartLine := args["start_line"]
	_, hasEndLine := args["end_line"]
	if hasStartLine != hasEndLine || (hasStartLine && (startLine == 0 || endLine == 0)) {
		return "", fmt.Errorf("start_line 和 end_line 必须同时提供正整数")
	}
	if filePath != "" {
		if !reviewSourceValidPath(filePath) {
			return "", fmt.Errorf("无效的仓库相对路径")
		}
		if _, err := snapshot.fetchPath(ctx, filePath); err != nil {
			return "", err
		}
	}
	if startLine > 0 {
		if filePath == "" || startLine > endLine || endLine-startLine+1 > reviewContextToolMaxRange {
			return "", fmt.Errorf("行段需指定文件，且范围不能超过 %d 行", reviewContextToolMaxRange)
		}
		return snapshot.readRange(filePath, startLine, endLine)
	}
	return snapshot.search(query, filePath, radius)
}

func optionalString(args map[string]any, key string) (string, error) {
	value, exists := args[key]
	if !exists {
		return "", nil
	}
	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("%s 必须是字符串", key)
	}
	return strings.TrimSpace(text), nil
}

func optionalInt(args map[string]any, key string) (int, error) {
	value, exists := args[key]
	if !exists {
		return 0, nil
	}
	number, ok := value.(float64)
	if !ok || number < 0 || number > float64(reviewContextToolMaxLine) ||
		number != float64(int(number)) {
		return 0, fmt.Errorf("%s 必须是整数", key)
	}
	return int(number), nil
}

func (s *reviewSourceSnapshot) readRange(filePath string, startLine, endLine int) (string, error) {
	s.mu.Lock()
	content, exists := s.files[filePath]
	s.mu.Unlock()
	if !exists {
		return "", fmt.Errorf("文件不在固定提交快照中")
	}
	lines := strings.Split(content, "\n")
	if startLine > len(lines) {
		return "", fmt.Errorf("start_line 超出文件范围")
	}
	endLine = min(endLine, len(lines))
	return s.formatLines(filePath, startLine, lines[startLine-1:endLine])
}

func (s *reviewSourceSnapshot) search(query, filePath string, radius int) (string, error) {
	s.mu.Lock()
	files := make(map[string]string, len(s.files))
	for path, content := range s.files {
		files[path] = content
	}
	headSHA, owner, repo := s.headSHA, s.owner, s.repo
	s.mu.Unlock()
	if filePath != "" {
		if _, exists := files[filePath]; !exists {
			return "", fmt.Errorf("文件不在固定提交快照中")
		}
	}
	paths := make([]string, 0, len(files))
	for path := range files {
		if filePath == "" || path == filePath {
			paths = append(paths, path)
		}
	}
	slices.Sort(paths)
	tokens := reviewIdentifierPattern.FindAllString(query, -1)
	if len(tokens) == 0 {
		tokens = []string{strings.ToLower(query)}
	}
	type sourceHit struct {
		path  string
		line  int
		score int
	}
	hits := make([]sourceHit, 0)
	for _, path := range paths {
		for lineNumber, line := range strings.Split(files[path], "\n") {
			score := 0
			for _, token := range tokens {
				if reviewSourceHasIdentifier(line, token) ||
					strings.Contains(strings.ToLower(line), strings.ToLower(token)) {
					score++
				}
			}
			if score > 0 {
				hits = append(hits, sourceHit{path: path, line: lineNumber + 1, score: score})
			}
		}
	}
	slices.SortFunc(hits, func(left, right sourceHit) int {
		if left.score != right.score {
			return right.score - left.score
		}
		if left.path != right.path {
			return strings.Compare(left.path, right.path)
		}
		return left.line - right.line
	})
	if len(hits) == 0 {
		return fmt.Sprintf(
			"固定提交 %s (%s/%s)：在已读取的 %d 个文件中未找到查询词。可提供具体仓库相对路径继续读取。",
			headSHA, owner, repo, len(files),
		), nil
	}
	if len(hits) > reviewContextToolMaxResults {
		hits = hits[:reviewContextToolMaxResults]
	}
	var output strings.Builder
	fmt.Fprintf(
		&output,
		"固定提交源码 %s/%s@%s；文件数=%d；命中段=%d\n",
		owner, repo, headSHA, len(files), len(hits),
	)
	for _, hit := range hits {
		lines := strings.Split(files[hit.path], "\n")
		start := max(1, hit.line-radius)
		end := min(len(lines), hit.line+radius)
		fmt.Fprintf(&output, "file: %s; match line: %d; range: %d-%d\n", hit.path, hit.line, start, end)
		for lineNumber := start; lineNumber <= end; lineNumber++ {
			fmt.Fprintf(&output, "%d | %s\n", lineNumber, reviewSourceLine(lines[lineNumber-1]))
		}
		if output.Len() >= reviewContextToolMaxChars {
			break
		}
	}
	return reviewSourceTrim(output.String(), reviewContextToolMaxChars), nil
}

func (s *reviewSourceSnapshot) formatLines(filePath string, startLine int, lines []string) (string, error) {
	s.mu.Lock()
	headSHA, owner, repo := s.headSHA, s.owner, s.repo
	s.mu.Unlock()
	var output strings.Builder
	fmt.Fprintf(&output, "固定提交源码 %s/%s@%s；file: %s；lines: %d-%d\n", owner, repo, headSHA, filePath, startLine, startLine+len(lines)-1)
	for offset, line := range lines {
		fmt.Fprintf(&output, "%d | %s\n", startLine+offset, reviewSourceLine(line))
		if output.Len() >= reviewContextToolMaxChars {
			break
		}
	}
	return reviewSourceTrim(output.String(), reviewContextToolMaxChars), nil
}

func reviewContextToolTraceSummary(output string) string {
	lines := strings.Split(output, "\n")
	summary := make([]string, 0, 1+reviewContextToolMaxResults)
	if len(lines) == 0 {
		return ""
	}
	summary = append(summary, lines[0])
	for _, line := range lines[1:] {
		if strings.HasPrefix(line, "file:") {
			summary = append(summary, line)
			if len(summary) == cap(summary) {
				break
			}
		}
	}
	return strings.Join(summary, "\n")
}
