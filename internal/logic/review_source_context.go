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
	"sync"
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
	reviewIdentifierPattern = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)
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

type reviewSourceSnapshotRequest struct {
	Source                 string
	Config                 Config
	Paths                  []string
	Findings               []ReviewFinding
	PrepareForOnDemandRead bool
}

type reviewSourceSnapshot struct {
	mu        sync.Mutex
	base      *url.URL
	owner     string
	repo      string
	headSHA   string
	token     string
	client    *http.Client
	files     map[string]string
	totalSize int
}

type reviewImportSearch struct {
	Paths    *[]string
	Seen     map[string]bool
	FilePath string
	Content  string
	Symbols  map[string]bool
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
	return loadReviewSourceSnapshotWithFindings(ctx, reviewSourceSnapshotRequest{
		Source: source,
		Config: cfg,
		Paths:  paths,
	})
}

func loadReviewSourceSnapshotWithFindings(
	ctx context.Context,
	request reviewSourceSnapshotRequest,
) (map[string]string, error) {
	snapshot, err := loadReviewSourceReaderWithFindings(ctx, request)
	if snapshot == nil {
		return nil, err
	}
	return snapshot.files, err
}

func loadReviewSourceReaderWithFindings(
	ctx context.Context,
	request reviewSourceSnapshotRequest,
) (*reviewSourceSnapshot, error) {
	source := request.Source
	cfg := request.Config
	paths := request.Paths
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
	if len(uniquePaths) == 0 && !request.PrepareForOnDemandRead {
		return &reviewSourceSnapshot{
			base: base, files: map[string]string{},
		}, nil
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
	snapshot := &reviewSourceSnapshot{
		base: base, owner: owner, repo: repo, headSHA: metadata.Head.SHA,
		token: token, client: client, files: make(map[string]string, len(uniquePaths)),
	}
	fetchFile := func(filePath string) bool {
		_, err := snapshot.fetchPath(ctx, filePath)
		return err == nil
	}

	failedFiles := 0
	for _, filePath := range uniquePaths {
		if !fetchFile(filePath) {
			failedFiles++
		}
	}
	if failedFiles > 0 {
		return snapshot, fmt.Errorf("%d 个源文件未能读取或超过安全限制", failedFiles)
	}

	// Imported source is optional. A missing related module must not invalidate
	// the changed-file snapshot or turn a usable verification into a fetch error.
	attemptsLeft := reviewSourceMaxFiles - len(uniquePaths)
	for depth := 0; depth < 2 && attemptsLeft > 0; depth++ {
		addedFile := false
		for _, filePath := range reviewFindingImportedPaths(snapshot.files, request.Findings, depth) {
			if _, loaded := snapshot.files[filePath]; loaded {
				continue
			}
			if attemptsLeft == 0 {
				break
			}
			attemptsLeft--
			if fetchFile(filePath) {
				addedFile = true
			}
		}
		if !addedFile {
			break
		}
	}
	return snapshot, nil
}

func (s *reviewSourceSnapshot) fetchPath(ctx context.Context, filePath string) (string, error) {
	if !reviewSourceValidPath(filePath) {
		return "", fmt.Errorf("无效的仓库相对路径")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if content, ok := s.files[filePath]; ok {
		return content, nil
	}
	if len(s.files) >= reviewSourceMaxFiles {
		return "", fmt.Errorf("源码上下文文件数达到限制")
	}
	if s.totalSize >= reviewSourceMaxTotalBytes {
		return "", fmt.Errorf("源码上下文总大小达到限制")
	}
	requestCtx, cancel := context.WithTimeout(ctx, reviewSourceRequestTimeout)
	defer cancel()
	fileURL := reviewSourceURL(s.base, "repos", s.owner, s.repo, "contents") +
		"/" + reviewSourceEscapedPath(filePath) + "?ref=" + url.QueryEscape(s.headSHA)
	body, err := reviewSourceGET(requestCtx, s.client, fileURL, s.token, reviewSourceResponseBytes)
	if err != nil {
		return "", fmt.Errorf("无法读取固定提交源码")
	}
	var file reviewSourceFile
	if err := json.Unmarshal(body, &file); err != nil || file.Type != "file" ||
		file.Encoding != "base64" || file.Size < 0 || file.Size > reviewSourceMaxFileBytes {
		return "", fmt.Errorf("固定提交源码格式无效或超过单文件限制")
	}
	content, err := base64.StdEncoding.DecodeString(file.Content)
	if err != nil || len(content) > reviewSourceMaxFileBytes ||
		s.totalSize+len(content) > reviewSourceMaxTotalBytes {
		return "", fmt.Errorf("固定提交源码无效或超过总大小限制")
	}
	if !utf8.Valid(content) || strings.IndexByte(string(content), 0) >= 0 {
		return "", fmt.Errorf("固定提交源码不是可读文本")
	}
	redacted := redactReviewInput(string(content))
	s.totalSize += len(content)
	s.files[filePath] = redacted
	return redacted, nil
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
	paths = append(paths, finding.File)
	for filePath := range files {
		if filePath != finding.File {
			paths = append(paths, filePath)
		}
	}
	slices.Sort(paths[1:])
	definitions, references := reviewSourceRelated(files, paths, finding, symbols)
	for _, block := range append(definitions, references...) {
		if excerpt.Len()+len(block) > maxChars {
			continue
		}
		excerpt.WriteString(block)
	}
	return redactReviewInput(excerpt.String())
}

func reviewFindingImportedPaths(files map[string]string, findings []ReviewFinding, importPass int) []string {
	relatedPaths := make([]string, 0)
	seen := make(map[string]bool)
	if importPass == 0 {
		prioritized := make([]ReviewFinding, 0, len(findings))
		remaining := make([]ReviewFinding, 0, len(findings))
		for _, finding := range findings {
			content, ok := files[finding.File]
			if !ok || finding.Line <= 0 {
				remaining = append(remaining, finding)
				continue
			}
			lines := strings.Split(content, "\n")
			if finding.Line > len(lines) {
				remaining = append(remaining, finding)
				continue
			}
			isTypeRelated := false
			for _, symbol := range reviewSourceSymbols(lines, finding.Line-1, finding.Evidence) {
				if symbol[0] >= 'A' && symbol[0] <= 'Z' {
					isTypeRelated = true
					break
				}
			}
			if isTypeRelated {
				prioritized = append(prioritized, finding)
				continue
			}
			remaining = append(remaining, finding)
		}
		prioritized = append(prioritized, remaining...)
		for _, finding := range prioritized {
			content, ok := files[finding.File]
			if !ok || finding.Line <= 0 {
				continue
			}
			lines := strings.Split(content, "\n")
			if finding.Line > len(lines) {
				continue
			}
			symbols := make(map[string]bool)
			for _, symbol := range reviewSourceSymbols(lines, finding.Line-1, finding.Evidence) {
				symbols[symbol] = true
			}
			reviewAppendImportedPaths(reviewImportSearch{
				Paths:    &relatedPaths,
				Seen:     seen,
				FilePath: finding.File,
				Content:  content,
				Symbols:  symbols,
			})
		}
		return relatedPaths
	}

	symbols := make(map[string]bool)
	for _, finding := range findings {
		content, ok := files[finding.File]
		if !ok || finding.Line <= 0 {
			continue
		}
		lines := strings.Split(content, "\n")
		if finding.Line > len(lines) {
			continue
		}
		for _, symbol := range reviewSourceSymbols(lines, finding.Line-1, finding.Evidence) {
			symbols[symbol] = true
		}
	}
	if len(symbols) == 0 {
		return relatedPaths
	}

	filePaths := make([]string, 0, len(files))
	for filePath := range files {
		filePaths = append(filePaths, filePath)
	}
	slices.Sort(filePaths)
	for _, filePath := range filePaths {
		reviewAppendImportedPaths(reviewImportSearch{
			Paths:    &relatedPaths,
			Seen:     seen,
			FilePath: filePath,
			Content:  files[filePath],
			Symbols:  symbols,
		})
	}
	return relatedPaths
}

func reviewAppendImportedPaths(search reviewImportSearch) {
	lines := strings.Split(search.Content, "\n")
	for index := 0; index < len(lines); index++ {
		module, names, isFromImport, nextLine := reviewPythonImport(lines, index)
		if module == "" {
			continue
		}
		index = nextLine
		if isFromImport {
			matchedNames := make([]string, 0, len(names))
			for _, name := range names {
				if search.Symbols[name] {
					matchedNames = append(matchedNames, name)
				}
			}
			if len(matchedNames) == 0 {
				continue
			}
			reviewAppendUniquePaths(
				search.Paths,
				search.Seen,
				reviewPythonImportCandidates(search.FilePath, module, matchedNames),
			)
			continue
		}
		modules := strings.Split(module, ",")
		for index, name := range names {
			if search.Symbols[name] && index < len(modules) {
				reviewAppendUniquePaths(
					search.Paths,
					search.Seen,
					reviewPythonImportCandidates(search.FilePath, modules[index], nil),
				)
			}
		}
	}
}

func reviewPythonImport(lines []string, index int) (string, []string, bool, int) {
	line := strings.TrimSpace(strings.SplitN(lines[index], "#", 2)[0])
	if strings.HasPrefix(line, "from ") {
		separator := strings.Index(line, " import ")
		if separator < 0 {
			return "", nil, true, index
		}
		module := strings.TrimSpace(strings.TrimPrefix(line[:separator], "from "))
		rest := strings.TrimSpace(line[separator+len(" import "):])
		lastLine := index
		if strings.Contains(rest, "(") && !strings.Contains(rest, ")") {
			for next := index + 1; next < len(lines) && next <= index+12; next++ {
				rest += " " + strings.TrimSpace(strings.SplitN(lines[next], "#", 2)[0])
				lastLine = next
				if strings.Contains(lines[next], ")") {
					break
				}
			}
		}
		if !reviewPythonModuleName(module) {
			return "", nil, true, lastLine
		}
		return module, reviewPythonImportNames(rest), true, lastLine
	}
	if !strings.HasPrefix(line, "import ") {
		return "", nil, false, index
	}
	rest := strings.TrimSpace(strings.TrimPrefix(line, "import "))
	parts := strings.Split(rest, ",")
	modules := make([]string, 0, len(parts))
	names := make([]string, 0, len(parts))
	for _, part := range parts {
		fields := strings.Fields(strings.TrimSpace(part))
		if len(fields) == 0 || !reviewPythonModuleName(fields[0]) {
			continue
		}
		module := fields[0]
		alias := strings.TrimSuffix(module[strings.LastIndex(module, ".")+1:], ",")
		if len(fields) >= 3 && fields[1] == "as" {
			alias = fields[2]
		}
		modules = append(modules, module)
		names = append(names, alias)
	}
	if len(modules) == 0 {
		return "", nil, false, index
	}
	return strings.Join(modules, ","), names, false, index
}

func reviewPythonImportNames(raw string) []string {
	names := make([]string, 0)
	for _, part := range strings.Split(raw, ",") {
		fields := strings.Fields(strings.Trim(part, " \t()"))
		if len(fields) == 0 {
			continue
		}
		name := fields[0]
		if name == "*" {
			names = append(names, name)
			continue
		}
		if !reviewPythonModuleName(name) {
			continue
		}
		if len(fields) >= 3 && fields[1] == "as" {
			names = append(names, name, fields[2])
			continue
		}
		names = append(names, name)
	}
	return names
}

func reviewPythonModuleName(name string) bool {
	if name == "" {
		return false
	}
	for _, char := range name {
		if char == '.' || char == '_' || char >= 'a' && char <= 'z' ||
			char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' {
			continue
		}
		return false
	}
	return true
}

func reviewPythonImportCandidates(sourcePath, module string, importedNames []string) []string {
	module = strings.TrimSpace(module)
	level := 0
	for level < len(module) && module[level] == '.' {
		level++
	}
	moduleParts := []string{}
	if remainder := strings.Trim(module[level:], "."); remainder != "" {
		moduleParts = strings.Split(remainder, ".")
	}
	roots := []string{}
	if level > 0 {
		baseParts := strings.Split(path.Dir(sourcePath), "/")
		for step := 1; step < level && len(baseParts) > 0; step++ {
			baseParts = baseParts[:len(baseParts)-1]
		}
		roots = append(roots, strings.Join(baseParts, "/"))
	} else {
		parts := strings.Split(sourcePath, "/")
		for index, part := range parts[:max(0, len(parts)-1)] {
			if part == "src" {
				roots = append(roots, strings.Join(parts[:index+1], "/"))
			}
		}
		roots = append(roots, "")
	}

	candidates := make([]string, 0)
	seen := make(map[string]bool)
	for _, root := range roots {
		base := path.Join(append([]string{root}, moduleParts...)...)
		if len(moduleParts) > 0 {
			reviewAppendUniquePaths(&candidates, seen, []string{base + ".py", path.Join(base, "__init__.py")})
		} else {
			reviewAppendUniquePaths(&candidates, seen, []string{path.Join(base, "__init__.py")})
		}
		for _, name := range importedNames {
			if name == "*" || !reviewPythonModuleName(name) {
				continue
			}
			child := path.Join(base, name)
			reviewAppendUniquePaths(&candidates, seen, []string{child + ".py", path.Join(child, "__init__.py")})
		}
	}
	return candidates
}

func reviewAppendUniquePaths(paths *[]string, seen map[string]bool, candidates []string) {
	for _, candidate := range candidates {
		if !reviewSourceValidPath(candidate) || seen[candidate] {
			continue
		}
		seen[candidate] = true
		*paths = append(*paths, candidate)
	}
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
	symbols := make([]string, 0, 16)
	addIdentifiers := func(line string) {
		for _, name := range reviewIdentifierPattern.FindAllString(line, -1) {
			if reviewSourceUsefulSymbol(name) && !slices.Contains(symbols, name) {
				symbols = append(symbols, name)
			}
		}
	}
	addCalls := func(line string) {
		for _, match := range reviewSymbolCallPattern.FindAllStringSubmatch(line, -1) {
			name := match[1]
			if reviewSourceUsefulSymbol(name) && !slices.Contains(symbols, name) {
				symbols = append(symbols, name)
			}
		}
	}
	addIdentifiers(evidence)
	addCalls(lines[target])
	addIdentifiers(lines[target])
	for index := target; index >= max(0, target-300); index-- {
		if match := reviewSourceDefinition(lines[index]); match != "" {
			addIdentifiers(lines[index])
			break
		}
	}
	addCalls(evidence)
	return symbols[:min(len(symbols), 16)]
}

func reviewSourceUsefulSymbol(name string) bool {
	switch name {
	case "if", "for", "while", "with", "return", "await", "print", "str", "len", "int", "list", "dict",
		"set", "self", "cls", "None", "True", "False":
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
	for _, symbol := range symbols {
		foundDefinition := false
		foundReference := false
		for _, filePath := range paths {
			lines := strings.Split(files[filePath], "\n")
			for index, line := range lines {
				if filePath == finding.File && index >= finding.Line-1-reviewSourceContextLines &&
					index <= finding.Line-1+reviewSourceContextLines {
					continue
				}
				if !reviewSourceHasIdentifier(line, symbol) {
					continue
				}
				block := reviewSourceRelatedBlock(filePath, lines, index, symbol)
				if reviewSourceDefinition(line) == symbol {
					if !foundDefinition && len(definitions) < 4 {
						definitions = append(definitions, block)
						foundDefinition = true
					}
					continue
				}
				if !foundReference && len(references) < 4 {
					references = append(references, block)
					foundReference = true
				}
				if foundDefinition && foundReference {
					break
				}
			}
			if foundDefinition && foundReference {
				break
			}
		}
		if len(definitions) >= 4 && len(references) >= 4 {
			return definitions, references
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
