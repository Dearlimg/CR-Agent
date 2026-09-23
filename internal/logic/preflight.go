package logic

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go/format"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const preflightVersion = "review-preflight-v1"

var hunkPattern = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,\d+)? @@`)
var credentialPattern = regexp.MustCompile(`(?i)(?:api[_-]?key|secret|password|token|authorization)\s*[:=]\s*["']?[A-Za-z0-9_\-/+=]{8,}`)
var providerTokenPattern = regexp.MustCompile(`(?:sk-[A-Za-z0-9_-]{20,}|gh[pousr]_[A-Za-z0-9_]{20,})`)

type ChangedFile struct {
	Path       string `json:"path"`
	AddedLines []int  `json:"added_lines"`
	New        bool   `json:"new"`
}

type PreflightCheck struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

type SecretFinding struct {
	Rule string `json:"rule"`
	File string `json:"file"`
	Line int    `json:"line"`
}

// ReviewArtifacts only exposes derived metadata and a redacted diff to agents.
// Raw code and matched credentials never enter the shared cache or prompt summary.
type ReviewArtifacts struct {
	DiffDigest      string           `json:"diff_digest"`
	Files           []ChangedFile    `json:"files"`
	AddedLines      int              `json:"diff_added_lines"`
	DependencyFiles []string         `json:"dependency_files"`
	Checks          []PreflightCheck `json:"checks"`
	SecretFindings  []SecretFinding  `json:"secret_findings"`
	SanitizedDiff   string           `json:"-"`
	CacheHit        bool             `json:"-"`
}

func (a ReviewArtifacts) PromptSummary() string {
	// The diff already contains hunk line numbers. Repeating every added line
	// and verbose check message can cost more tokens than the useful evidence.
	safe := struct {
		Files           []string          `json:"files"`
		AddedLines      int               `json:"diff_added_lines"`
		DependencyFiles []string          `json:"dependency_files"`
		Checks          map[string]string `json:"checks"`
		SecretFindings  []SecretFinding   `json:"secret_findings"`
	}{
		Files:           make([]string, len(a.Files)),
		AddedLines:      a.AddedLines,
		DependencyFiles: make([]string, len(a.DependencyFiles)),
		Checks:          make(map[string]string, len(a.Checks)),
		SecretFindings:  make([]SecretFinding, len(a.SecretFindings)),
	}
	for index, file := range a.Files {
		safe.Files[index] = redactFindingText(file.Path)
	}
	for index, file := range a.DependencyFiles {
		safe.DependencyFiles[index] = redactFindingText(file)
	}
	for _, check := range a.Checks {
		safe.Checks[check.Name] = check.Status
	}
	for index, finding := range a.SecretFindings {
		safe.SecretFindings[index] = SecretFinding{
			Rule: finding.Rule, File: redactFindingText(finding.File), Line: finding.Line,
		}
	}
	content, _ := json.Marshal(safe)
	return string(content)
}

func (a ReviewArtifacts) TraceSummary() string {
	return fmt.Sprintf(
		"files=%d diff_added_lines=%d dependency_files=%d secret_findings=%d analysis_cache_hit=%t",
		len(a.Files), a.AddedLines, len(a.DependencyFiles), len(a.SecretFindings), a.CacheHit,
	)
}

type preflightAnalysis struct {
	Files           []ChangedFile
	AddedLines      int
	DependencyFiles []string
	Checks          []PreflightCheck
}

type preflightEntry struct {
	analysis  preflightAnalysis
	createdAt time.Time
}

type preflightFlight struct {
	done     chan struct{}
	analysis preflightAnalysis
}

type PreflightCache struct {
	mu       sync.Mutex
	entries  map[string]preflightEntry
	inflight map[string]*preflightFlight
	order    []string
}

func NewPreflightCache() *PreflightCache {
	return &PreflightCache{
		entries:  map[string]preflightEntry{},
		inflight: map[string]*preflightFlight{},
		order:    []string{},
	}
}

func (c *PreflightCache) Analyze(ctx context.Context, source, diff string) (preflightAnalysis, bool, error) {
	keyBytes := sha256.Sum256([]byte(preflightVersion + "\x00" + source + "\x00" + diff))
	key := hex.EncodeToString(keyBytes[:])
	c.mu.Lock()
	if entry, ok := c.entries[key]; ok && time.Since(entry.createdAt) < 30*time.Minute {
		c.mu.Unlock()
		return clonePreflightAnalysis(entry.analysis), true, nil
	}
	if flight, ok := c.inflight[key]; ok {
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			return preflightAnalysis{}, false, ctx.Err()
		case <-flight.done:
			return clonePreflightAnalysis(flight.analysis), true, nil
		}
	}
	flight := &preflightFlight{done: make(chan struct{})}
	c.inflight[key] = flight
	c.mu.Unlock()

	analysis := analyzeDiff(diff)
	c.mu.Lock()
	flight.analysis = analysis
	c.entries[key] = preflightEntry{analysis: analysis, createdAt: time.Now()}
	for index, existing := range c.order {
		if existing == key {
			c.order = append(c.order[:index], c.order[index+1:]...)
			break
		}
	}
	c.order = append(c.order, key)
	if len(c.order) > 64 {
		delete(c.entries, c.order[0])
		c.order = c.order[1:]
	}
	delete(c.inflight, key)
	close(flight.done)
	c.mu.Unlock()
	return clonePreflightAnalysis(analysis), false, nil
}

func clonePreflightAnalysis(in preflightAnalysis) preflightAnalysis {
	out := preflightAnalysis{
		Files:           make([]ChangedFile, len(in.Files)),
		AddedLines:      in.AddedLines,
		DependencyFiles: append([]string{}, in.DependencyFiles...),
		Checks:          append([]PreflightCheck{}, in.Checks...),
	}
	for index, file := range in.Files {
		out.Files[index] = ChangedFile{
			Path:       file.Path,
			AddedLines: append([]int{}, file.AddedLines...),
			New:        file.New,
		}
	}
	return out
}

func (c *PreflightCache) Run(ctx context.Context, source, diff string) (ReviewArtifacts, error) {
	analysis, cacheHit, err := c.Analyze(ctx, source, diff)
	if err != nil {
		return ReviewArtifacts{}, err
	}
	// Scan the original bytes on every review. The cache never stores code or secret values.
	secrets, err := scanRawDiff(ctx, diff)
	if err != nil {
		return ReviewArtifacts{}, fmt.Errorf("密钥扫描失败: %w", err)
	}
	scanStatus := "passed"
	if len(secrets) > 0 {
		scanStatus = "found"
	}
	analysis.Checks = append(analysis.Checks, PreflightCheck{
		Name: "secret_scan", Status: scanStatus,
		Message: fmt.Sprintf("原始 diff 新增行疑似密钥命中=%d；未记录密钥值", len(secrets)),
	})
	digest := sha256.Sum256([]byte(diff))
	return ReviewArtifacts{
		DiffDigest:      hex.EncodeToString(digest[:]),
		Files:           analysis.Files,
		AddedLines:      analysis.AddedLines,
		DependencyFiles: analysis.DependencyFiles,
		Checks:          analysis.Checks,
		SecretFindings:  secrets,
		SanitizedDiff:   sanitizeDiff(diff),
		CacheHit:        cacheHit,
	}, nil
}

func analyzeDiff(diff string) preflightAnalysis {
	analysis := preflightAnalysis{
		Files:           []ChangedFile{},
		DependencyFiles: []string{},
		Checks:          []PreflightCheck{},
	}
	var current *ChangedFile
	var nextLine int
	var conflictCount, staticHints, formatFailures, syntaxFailures, completeGoFiles, partialGoFiles int
	newFileSources := map[string]*strings.Builder{}
	if !strings.Contains(diff, "diff --git ") {
		analysis.Files = append(analysis.Files, ChangedFile{Path: "diff", AddedLines: []int{}})
		current = &analysis.Files[0]
		nextLine = 1
	}
	for _, line := range strings.Split(diff, "\n") {
		if strings.HasPrefix(line, "diff --git ") {
			file := ChangedFile{Path: diffFilePath(line), AddedLines: []int{}}
			analysis.Files = append(analysis.Files, file)
			current = &analysis.Files[len(analysis.Files)-1]
			nextLine = 0
			if isDependencyFile(file.Path) {
				analysis.DependencyFiles = append(analysis.DependencyFiles, file.Path)
			}
			continue
		}
		if current == nil {
			continue
		}
		if strings.HasPrefix(line, "new file mode ") {
			current.New = true
			continue
		}
		if match := hunkPattern.FindStringSubmatch(line); len(match) == 2 {
			nextLine, _ = strconv.Atoi(match[1])
			continue
		}
		if nextLine == 0 {
			continue
		}
		if strings.HasPrefix(line, "+") {
			content := strings.TrimPrefix(line, "+")
			current.AddedLines = append(current.AddedLines, nextLine)
			analysis.AddedLines++
			if strings.HasPrefix(content, "<<<<<<<") || strings.HasPrefix(content, "=======") || strings.HasPrefix(content, ">>>>>>>") {
				conflictCount++
			}
			if strings.Contains(content, "TODO") || strings.Contains(content, "panic(") {
				staticHints++
			}
			if current.New && strings.HasSuffix(current.Path, ".go") {
				builder := newFileSources[current.Path]
				if builder == nil {
					builder = &strings.Builder{}
					newFileSources[current.Path] = builder
				}
				builder.WriteString(content)
				builder.WriteByte('\n')
			}
			nextLine++
			continue
		}
		if strings.HasPrefix(line, " ") {
			nextLine++
		}
	}
	for _, file := range analysis.Files {
		if !strings.HasSuffix(file.Path, ".go") {
			continue
		}
		if !file.New {
			partialGoFiles++
			continue
		}
		completeGoFiles++
		builder := newFileSources[file.Path]
		if builder == nil {
			syntaxFailures++
			continue
		}
		content := []byte(builder.String())
		formatted, err := format.Source(content)
		if err != nil {
			syntaxFailures++
			continue
		}
		if string(formatted) != string(content) {
			formatFailures++
		}
	}
	staticStatus := "passed"
	if staticHints > 0 {
		staticStatus = "hint"
	}
	analysis.Checks = append(analysis.Checks,
		PreflightCheck{Name: "conflict_marker_check", Status: passOrFail(conflictCount), Message: fmt.Sprintf("新增行冲突标记=%d", conflictCount)},
		PreflightCheck{Name: "static_check", Status: staticStatus, Message: fmt.Sprintf("新增行 TODO/panic 字符串提示=%d；仅供人工判断", staticHints)},
	)
	goStatus := "not_run"
	if completeGoFiles > 0 && partialGoFiles == 0 {
		goStatus = "passed"
	}
	if syntaxFailures > 0 {
		goStatus = "failed"
	}
	analysis.Checks = append(analysis.Checks, PreflightCheck{
		Name: "syntax_check", Status: goStatus,
		Message: fmt.Sprintf("新建 Go 文件解析=%d，解析失败=%d，修改文件未做完整语法检查=%d", completeGoFiles, syntaxFailures, partialGoFiles),
	})
	formatStatus := goStatus
	if formatFailures > 0 {
		formatStatus = "failed"
	}
	analysis.Checks = append(analysis.Checks, PreflightCheck{
		Name: "format_check", Status: formatStatus,
		Message: fmt.Sprintf("新建 Go 文件 gofmt 检查=%d，需格式化=%d，修改文件未检查=%d", completeGoFiles, formatFailures, partialGoFiles),
	})
	return analysis
}

func passOrFail(count int) string {
	if count > 0 {
		return "failed"
	}
	return "passed"
}

func diffFilePath(line string) string {
	match := filePattern.FindStringSubmatch(line)
	if len(match) == 3 {
		return redactFindingText(match[2])
	}
	return redactFindingText(strings.TrimPrefix(line, "diff --git "))
}

func isDependencyFile(name string) bool {
	switch path.Base(name) {
	case "go.mod", "go.sum", "package.json", "package-lock.json", "pnpm-lock.yaml", "yarn.lock",
		"requirements.txt", "poetry.lock", "pyproject.toml", "Cargo.toml", "Cargo.lock":
		return true
	default:
		return false
	}
}

func scanRawDiff(ctx context.Context, diff string) ([]SecretFinding, error) {
	findings := []SecretFinding{}
	if !strings.Contains(diff, "@@ -") {
		for index, line := range strings.Split(diff, "\n") {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "--- ") {
				continue
			}
			content := strings.TrimPrefix(line, "+")
			if credentialPattern.MatchString(content) {
				findings = append(findings, SecretFinding{Rule: "credential_assignment", File: "diff", Line: index + 1})
			}
			if providerTokenPattern.MatchString(content) {
				findings = append(findings, SecretFinding{Rule: "provider_token", File: "diff", Line: index + 1})
			}
		}
		return findings, nil
	}
	var file string
	var nextLine int
	for _, line := range strings.Split(diff, "\n") {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if strings.HasPrefix(line, "diff --git ") {
			file = diffFilePath(line)
			nextLine = 0
			continue
		}
		if match := hunkPattern.FindStringSubmatch(line); len(match) == 2 {
			nextLine, _ = strconv.Atoi(match[1])
			continue
		}
		if nextLine == 0 {
			continue
		}
		if strings.HasPrefix(line, "+") {
			content := strings.TrimPrefix(line, "+")
			if credentialPattern.MatchString(content) {
				findings = append(findings, SecretFinding{Rule: "credential_assignment", File: file, Line: nextLine})
			}
			if providerTokenPattern.MatchString(content) {
				findings = append(findings, SecretFinding{Rule: "provider_token", File: file, Line: nextLine})
			}
			nextLine++
			continue
		}
		if strings.HasPrefix(line, " ") {
			nextLine++
		}
	}
	return findings, nil
}

// sanitizeDiff keeps patch headers and line prefixes intact so agents can
// still locate changed lines after credential-bearing content is removed.
func sanitizeDiff(diff string) string {
	lines := strings.Split(diff, "\n")
	for index, line := range lines {
		if strings.HasPrefix(line, "diff --git ") || strings.HasPrefix(line, "--- ") ||
			strings.HasPrefix(line, "+++ ") || strings.HasPrefix(line, "@@ ") {
			lines[index] = redactFindingText(line)
			continue
		}
		if len(line) > 0 && (line[0] == '+' || line[0] == '-' || line[0] == ' ') {
			lines[index] = line[:1] + redact(line[1:])
			continue
		}
		lines[index] = redact(line)
	}
	return strings.Join(lines, "\n")
}

func chooseReviewMode(artifacts ReviewArtifacts) string {
	if len(artifacts.SecretFindings) > 0 {
		return "specialists"
	}
	productionFiles := 0
	directories := map[string]bool{}
	for _, file := range artifacts.Files {
		if !isReviewProductionFile(file.Path) {
			continue
		}
		productionFiles++
		directories[path.Dir(file.Path)] = true
		if isSensitiveReviewPath(file.Path) {
			return "specialists"
		}
	}
	if len(artifacts.DependencyFiles) > 0 && (productionFiles > 0 || len(artifacts.DependencyFiles) > 1) {
		return "specialists"
	}
	if productionFiles >= 6 || (productionFiles >= 4 && len(directories) >= 2) {
		return "specialists"
	}
	if productionFiles > 0 && hasSensitiveAddedCode(artifacts.SanitizedDiff) {
		return "specialists"
	}
	return "single"
}

func isReviewProductionFile(name string) bool {
	if isDependencyFile(name) {
		return false
	}
	lower := strings.ToLower(name)
	base := path.Base(lower)
	for _, segment := range strings.Split(lower, "/") {
		if segment == "test" || segment == "tests" || segment == "testdata" || segment == "docs" {
			return false
		}
	}
	if strings.HasSuffix(base, "_test.go") || strings.Contains(base, ".test.") || strings.Contains(base, ".spec.") {
		return false
	}
	switch path.Ext(base) {
	case ".md", ".mdx", ".txt", ".rst", ".png", ".jpg", ".jpeg", ".svg", ".gif", ".lock":
		return false
	default:
		return true
	}
}

func isSensitiveReviewPath(name string) bool {
	segments := strings.FieldsFunc(strings.ToLower(name), func(r rune) bool {
		return r == '/' || r == '_' || r == '-' || r == '.'
	})
	for _, segment := range segments {
		switch segment {
		case "auth", "authentication", "authorization", "security", "crypto", "permission",
			"permissions", "secrets", "migration", "migrations", "database", "db":
			return true
		}
	}
	return false
}

var sensitiveAddedCodePattern = regexp.MustCompile(
	`(?i)\b(?:authorization|authentication|password|credentials?|permissions?|jwt|csrf|transaction|rollback|execcontext|idempotency|lease)\b`,
)

func hasSensitiveAddedCode(diff string) bool {
	productionFile := !strings.Contains(diff, "diff --git ")
	for _, line := range strings.Split(diff, "\n") {
		if strings.HasPrefix(line, "diff --git ") {
			productionFile = isReviewProductionFile(diffFilePath(line))
			continue
		}
		if !productionFile || !strings.HasPrefix(line, "+") || strings.HasPrefix(line, "+++") {
			continue
		}
		content := strings.TrimSpace(line[1:])
		if strings.Contains(content, "[REDACTED]") {
			return true
		}
		if strings.HasPrefix(content, "//") || strings.HasPrefix(content, "#") {
			continue
		}
		if sensitiveAddedCodePattern.MatchString(content) {
			return true
		}
	}
	return false
}
