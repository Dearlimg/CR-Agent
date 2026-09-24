package logic

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	reviewSourceMaxFiles       = 16
	reviewSourceMaxFileBytes   = 256 << 10
	reviewSourceMaxTotalBytes  = 1 << 20
	reviewSourceResponseBytes  = 512 << 10
	reviewSourceMetadataBytes  = 64 << 10
	reviewSourceTotalTimeout   = 20 * time.Second
	reviewSourceRequestTimeout = 8 * time.Second
	reviewSourceDefaultExcerpt = 12000
	reviewSourceMaxExcerpt     = 20000
	reviewSourceContextLines   = 35
)

var (
	reviewGitHubNamePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
	reviewGitHubSHAPattern  = regexp.MustCompile(`^(?:[a-fA-F0-9]{40}|[a-fA-F0-9]{64})$`)
	reviewSymbolCallPattern = regexp.MustCompile(`\b([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
	reviewPythonDefinition  = regexp.MustCompile(`^\s*(?:async\s+)?(?:def|class)\s+([A-Za-z_][A-Za-z0-9_]*)\b`)
	reviewGoDefinition      = regexp.MustCompile(`^\s*func\s+(?:\([^)]*\)\s*)?([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
)

type reviewGitHubPR struct {
	owner  string
	repo   string
	number string
}

type reviewSourceMetadata struct {
	Head struct {
		SHA  string `json:"sha"`
		Repo *struct {
			FullName string `json:"full_name"`
		} `json:"repo"`
	} `json:"head"`
}

type reviewSourceFile struct {
	Type     string `json:"type"`
	Encoding string `json:"encoding"`
	Content  string `json:"content"`
	Size     int64  `json:"size"`
}

// loadReviewSourceSnapshot retrieves bounded source files from the exact head
// commit of a GitHub pull request. Other source kinds have no repository
// snapshot and return nil, nil.
func loadReviewSourceSnapshot(
	ctx context.Context,
	source string,
	cfg Config,
	paths []string,
) (map[string]string, error) {
	pr, supported := parseReviewGitHubPR(source)
	if !supported {
		return nil, nil
	}
	base, officialAPI, err := reviewSourceAPIBase(cfg.GitHubAPIBase)
	if err != nil {
		return nil, err
	}
	uniquePaths, err := reviewSourcePaths(paths)
	if err != nil {
		return nil, err
	}
	files := make(map[string]string, len(uniquePaths))
	if len(uniquePaths) == 0 {
		return files, nil
	}

	ctx, cancel := context.WithTimeout(ctx, reviewSourceTotalTimeout)
	defer cancel()
	client := &http.Client{
		Timeout: reviewSourceRequestTimeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	token := ""
	if officialAPI {
		token = cfg.GitHubToken
	}
	metadataURL := reviewSourceURL(base, "repos", pr.owner, pr.repo, "pulls", pr.number)
	metadataBody, err := reviewSourceGET(ctx, client, metadataURL, token, reviewSourceMetadataBytes)
	if err != nil {
		return nil, fmt.Errorf("读取 PR head 信息: %w", err)
	}
	var metadata reviewSourceMetadata
	if err := json.Unmarshal(metadataBody, &metadata); err != nil {
		return nil, fmt.Errorf("解析 PR head 信息: %w", err)
	}
	if !reviewGitHubSHAPattern.MatchString(metadata.Head.SHA) {
		return nil, fmt.Errorf("PR head SHA 无效")
	}
	owner, repo := pr.owner, pr.repo
	if metadata.Head.Repo != nil && metadata.Head.Repo.FullName != "" {
		owner, repo, err = reviewSourceFullName(metadata.Head.Repo.FullName)
		if err != nil {
			return nil, err
		}
	}

	var totalBytes int
	failedFiles := 0
	for _, filePath := range uniquePaths {
		fileURL := reviewSourceURL(base, "repos", owner, repo, "contents") +
			"/" + reviewSourceEscapedPath(filePath) + "?ref=" + url.QueryEscape(metadata.Head.SHA)
		body, err := reviewSourceGET(ctx, client, fileURL, token, reviewSourceResponseBytes)
		if err != nil {
			failedFiles++
			continue
		}
		var file reviewSourceFile
		if err := json.Unmarshal(body, &file); err != nil {
			failedFiles++
			continue
		}
		if file.Type != "file" || file.Encoding != "base64" || file.Size > reviewSourceMaxFileBytes {
			failedFiles++
			continue
		}
		content, err := base64.StdEncoding.DecodeString(file.Content)
		if err != nil {
			failedFiles++
			continue
		}
		if len(content) > reviewSourceMaxFileBytes || totalBytes+len(content) > reviewSourceMaxTotalBytes {
			failedFiles++
			continue
		}
		if !utf8.Valid(content) || strings.IndexByte(string(content), 0) >= 0 {
			failedFiles++
			continue
		}
		totalBytes += len(content)
		files[filePath] = redactReviewInput(string(content))
	}
	if failedFiles > 0 {
		return files, fmt.Errorf("%d 个源文件未能读取或超过安全限制", failedFiles)
	}
	return files, nil
}

func parseReviewGitHubPR(source string) (reviewGitHubPR, bool) {
	u, err := url.Parse(strings.TrimSpace(source))
	if err != nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" {
		return reviewGitHubPR{}, false
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	switch strings.ToLower(u.Host) {
	case "github.com":
		if len(parts) < 4 || len(parts) > 5 || parts[2] != "pull" {
			return reviewGitHubPR{}, false
		}
		if len(parts) == 5 && parts[4] != "files" {
			return reviewGitHubPR{}, false
		}
		number := strings.TrimSuffix(strings.TrimSuffix(parts[3], ".diff"), ".patch")
		return reviewGitHubPRParts(parts[0], parts[1], number)
	case "api.github.com":
		if len(parts) != 5 || parts[0] != "repos" || parts[3] != "pulls" {
			return reviewGitHubPR{}, false
		}
		return reviewGitHubPRParts(parts[1], parts[2], parts[4])
	default:
		return reviewGitHubPR{}, false
	}
}

func reviewGitHubPRParts(owner, repo, number string) (reviewGitHubPR, bool) {
	n, err := strconv.ParseUint(number, 10, 64)
	if err != nil || n == 0 || !reviewGitHubNamePattern.MatchString(owner) ||
		!reviewGitHubNamePattern.MatchString(repo) {
		return reviewGitHubPR{}, false
	}
	return reviewGitHubPR{owner: owner, repo: repo, number: number}, true
}

func reviewSourceAPIBase(raw string) (*url.URL, bool, error) {
	if strings.TrimSpace(raw) == "" {
		raw = "https://api.github.com"
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" ||
		(u.Path != "" && u.Path != "/") {
		return nil, false, fmt.Errorf("GitHub API base 无效")
	}
	if u.Scheme == "https" && strings.EqualFold(u.Host, "api.github.com") {
		return u, true, nil
	}
	loopback := u.Hostname() == "127.0.0.1" || u.Hostname() == "::1"
	if u.Scheme == "http" && loopback && u.Port() != "" {
		return u, false, nil
	}
	return nil, false, fmt.Errorf("GitHub API base 必须是 https://api.github.com")
}

func reviewSourcePaths(paths []string) ([]string, error) {
	unique := make([]string, 0, min(len(paths), reviewSourceMaxFiles))
	seen := make(map[string]bool, len(paths))
	for _, filePath := range paths {
		if !reviewSourceValidPath(filePath) {
			return nil, fmt.Errorf("无效的仓库相对路径 %q", filePath)
		}
		if seen[filePath] {
			continue
		}
		seen[filePath] = true
		unique = append(unique, filePath)
		if len(unique) == reviewSourceMaxFiles {
			break
		}
	}
	return unique, nil
}

func reviewSourceValidPath(filePath string) bool {
	if filePath == "" || len(filePath) > 512 || strings.ContainsAny(filePath, "\\\x00\r\n") ||
		strings.HasPrefix(filePath, "/") || strings.Contains(filePath, ":") || path.Clean(filePath) != filePath {
		return false
	}
	for _, segment := range strings.Split(filePath, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

func reviewSourceFullName(fullName string) (string, string, error) {
	parts := strings.Split(fullName, "/")
	if len(parts) != 2 || !reviewGitHubNamePattern.MatchString(parts[0]) ||
		!reviewGitHubNamePattern.MatchString(parts[1]) {
		return "", "", fmt.Errorf("PR head 仓库名称无效")
	}
	return parts[0], parts[1], nil
}

func reviewSourceURL(base *url.URL, parts ...string) string {
	escaped := make([]string, 0, len(parts))
	for _, part := range parts {
		escaped = append(escaped, url.PathEscape(part))
	}
	return strings.TrimSuffix(base.String(), "/") + "/" + strings.Join(escaped, "/")
}

func reviewSourceEscapedPath(filePath string) string {
	parts := strings.Split(filePath, "/")
	for index, part := range parts {
		parts[index] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
}

func reviewSourceGET(
	ctx context.Context,
	client *http.Client,
	requestURL string,
	token string,
	maxBytes int,
) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("构造 GitHub 请求: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", userAgent)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GitHub 请求失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("GitHub 返回 HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(maxBytes+1)))
	if err != nil {
		return nil, fmt.Errorf("读取 GitHub 响应: %w", err)
	}
	if len(body) > maxBytes {
		return nil, fmt.Errorf("GitHub 响应超出大小限制")
	}
	return body, nil
}

// findingSourceExcerpt provides a bounded head-snapshot excerpt with the
// changed line first, its enclosing context, and related definitions/callers.
func findingSourceExcerpt(files map[string]string, finding ReviewFinding, maxChars int) string {
	content, ok := files[finding.File]
	if !ok || finding.Line <= 0 {
		return ""
	}
	lines := strings.Split(content, "\n")
	if finding.Line > len(lines) {
		return ""
	}
	firstEvidenceLine := strings.SplitN(finding.Evidence, "\n", 2)[0]
	if strings.TrimSpace(lines[finding.Line-1]) != strings.TrimSpace(firstEvidenceLine) {
		return ""
	}
	if maxChars <= 0 {
		maxChars = reviewSourceDefaultExcerpt
	}
	maxChars = min(maxChars, reviewSourceMaxExcerpt)
	var excerpt strings.Builder
	target := finding.Line - 1
	header := fmt.Sprintf("file: %s\nchanged line %d: %s\n", finding.File, finding.Line, reviewSourceLine(lines[target]))
	excerpt.WriteString(reviewSourceTrim(header, maxChars))
	if excerpt.Len() >= maxChars {
		return redactReviewInput(excerpt.String())
	}

	contextBudget := maxChars * 3 / 4
	contextBudget = max(contextBudget, excerpt.Len())
	for radius := reviewSourceContextLines; radius >= 0; radius-- {
		nearby := reviewSourceWindow(lines, target, radius)
		if excerpt.Len()+len(nearby) > contextBudget {
			continue
		}
		excerpt.WriteString(nearby)
		break
	}

	symbols := reviewSourceSymbols(lines, target, finding.Evidence)
	if len(symbols) == 0 {
		return redactReviewInput(excerpt.String())
	}
	paths := make([]string, 0, len(files))
	for filePath := range files {
		paths = append(paths, filePath)
	}
	slices.Sort(paths)
	definitions, references := reviewSourceRelated(files, paths, finding, symbols)
	for _, block := range append(definitions, references...) {
		if excerpt.Len()+len(block) > maxChars {
			continue
		}
		excerpt.WriteString(block)
	}
	return redactReviewInput(excerpt.String())
}

func reviewSourceLine(line string) string {
	const maxLineChars = 320
	if len(line) > maxLineChars {
		return line[:maxLineChars] + " ..."
	}
	return line
}

func reviewSourceTrim(s string, maxChars int) string {
	if len(s) <= maxChars {
		return s
	}
	return s[:maxChars]
}

func reviewSourceWindow(lines []string, target, radius int) string {
	start := max(0, target-radius)
	end := min(len(lines), target+radius+1)
	var block strings.Builder
	block.WriteString("nearby head source:\n")
	for index := start; index < end; index++ {
		fmt.Fprintf(&block, "%d | %s\n", index+1, reviewSourceLine(lines[index]))
	}
	return block.String()
}

func reviewSourceSymbols(lines []string, target int, evidence string) []string {
	symbols := make([]string, 0, 6)
	addCalls := func(line string) {
		for _, match := range reviewSymbolCallPattern.FindAllStringSubmatch(line, -1) {
			name := match[1]
			if reviewSourceUsefulSymbol(name) && !slices.Contains(symbols, name) {
				symbols = append(symbols, name)
			}
		}
	}
	addCalls(lines[target])
	for index := target; index >= max(0, target-300); index-- {
		if match := reviewSourceDefinition(lines[index]); match != "" {
			if reviewSourceUsefulSymbol(match) && !slices.Contains(symbols, match) {
				symbols = append(symbols, match)
			}
			break
		}
	}
	addCalls(evidence)
	return symbols[:min(len(symbols), 6)]
}

func reviewSourceUsefulSymbol(name string) bool {
	switch name {
	case "if", "for", "while", "with", "return", "await", "print", "str", "len", "int", "list", "dict", "set":
		return false
	default:
		return len(name) >= 3
	}
}

func reviewSourceDefinition(line string) string {
	if match := reviewPythonDefinition.FindStringSubmatch(line); len(match) == 2 {
		return match[1]
	}
	if match := reviewGoDefinition.FindStringSubmatch(line); len(match) == 2 {
		return match[1]
	}
	return ""
}

func reviewSourceRelated(
	files map[string]string,
	paths []string,
	finding ReviewFinding,
	symbols []string,
) ([]string, []string) {
	definitions := make([]string, 0, 4)
	references := make([]string, 0, 4)
	for _, filePath := range paths {
		lines := strings.Split(files[filePath], "\n")
		for index, line := range lines {
			if filePath == finding.File && index >= finding.Line-1-reviewSourceContextLines &&
				index <= finding.Line-1+reviewSourceContextLines {
				continue
			}
			for _, symbol := range symbols {
				if !reviewSourceHasIdentifier(line, symbol) {
					continue
				}
				block := reviewSourceRelatedBlock(filePath, lines, index, symbol)
				if reviewSourceDefinition(line) == symbol {
					if len(definitions) < 4 {
						definitions = append(definitions, block)
					}
				} else if len(references) < 4 {
					references = append(references, block)
				}
				break
			}
			if len(definitions) >= 4 && len(references) >= 4 {
				return definitions, references
			}
		}
	}
	return definitions, references
}

func reviewSourceHasIdentifier(line, symbol string) bool {
	for offset := 0; offset < len(line); {
		index := strings.Index(line[offset:], symbol)
		if index < 0 {
			return false
		}
		index += offset
		before := index == 0 || !reviewSourceIdentifierByte(line[index-1])
		afterIndex := index + len(symbol)
		after := afterIndex == len(line) || !reviewSourceIdentifierByte(line[afterIndex])
		if before && after {
			return true
		}
		offset = index + len(symbol)
	}
	return false
}

func reviewSourceIdentifierByte(char byte) bool {
	return char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' ||
		char >= '0' && char <= '9' || char == '_'
}

func reviewSourceRelatedBlock(filePath string, lines []string, index int, symbol string) string {
	start := max(0, index-1)
	end := min(len(lines), index+3)
	var block strings.Builder
	fmt.Fprintf(&block, "related %s at %s:%d:\n", symbol, filePath, index+1)
	for lineIndex := start; lineIndex < end; lineIndex++ {
		fmt.Fprintf(&block, "%d | %s\n", lineIndex+1, reviewSourceLine(lines[lineIndex]))
	}
	return block.String()
}
