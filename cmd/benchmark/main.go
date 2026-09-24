package main

import (
	"CR-Agent/internal/dao"
	"CR-Agent/internal/logic"
	"CR-Agent/internal/model"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type expectedFinding struct {
	File        string     `json:"file"`
	Line        int        `json:"line"`
	MatchGroups [][]string `json:"match_groups"`
}

type testCase struct {
	ID       string            `json:"id"`
	Category string            `json:"category"`
	Diff     string            `json:"diff"`
	Expected []expectedFinding `json:"expected"`
}

type proxyUsage struct {
	Forwarded int   `json:"forwarded_requests"`
	Rejected  int   `json:"budget_rejections"`
	Tokens    int64 `json:"reported_tokens"`
	Unknown   int   `json:"responses_without_usage"`
}

type caseResult struct {
	ID             string                `json:"id"`
	Category       string                `json:"category"`
	Status         string                `json:"status"`
	DurationMs     int64                 `json:"duration_ms"`
	Expected       []expectedFinding     `json:"expected"`
	Findings       []model.ReviewComment `json:"findings"`
	TruePositives  int                   `json:"true_positives"`
	FalsePositives int                   `json:"false_positives"`
	FalseNegatives int                   `json:"false_negatives"`
	ValidLines     int                   `json:"valid_changed_lines"`
	InvalidLines   int                   `json:"invalid_changed_lines"`
	TraceEvents    int                   `json:"trace_events"`
	CompleteTrace  int                   `json:"complete_trace_events"`
	ModelJSONValid bool                  `json:"model_json_valid"`
	CanaryLeaked   bool                  `json:"redaction_canary_leaked"`
	Error          string                `json:"error,omitempty"`
	Usage          proxyUsage            `json:"model_usage"`
}

type runReport struct {
	Version               string       `json:"benchmark_version"`
	Route                 string       `json:"route"`
	StartedAt             time.Time    `json:"started_at"`
	FinishedAt            time.Time    `json:"finished_at"`
	Model                 string       `json:"model"`
	CasesRequested        int          `json:"cases_requested"`
	CasesCompleted        int          `json:"cases_completed"`
	MaxForwardedCalls     int          `json:"max_forwarded_calls"`
	MaxOutputTokens       int          `json:"max_output_tokens_per_call"`
	Results               []caseResult `json:"results"`
	TruePositives         int          `json:"true_positives"`
	FalsePositives        int          `json:"false_positives"`
	FalseNegatives        int          `json:"false_negatives"`
	ValidLines            int          `json:"valid_changed_lines"`
	InvalidLines          int          `json:"invalid_changed_lines"`
	TraceEvents           int          `json:"trace_events"`
	CompleteTrace         int          `json:"complete_trace_events"`
	ForwardedRequests     int          `json:"forwarded_requests"`
	RejectedRequests      int          `json:"budget_rejections"`
	ReportedTokens        int64        `json:"reported_tokens"`
	ResponsesWithoutUsage int          `json:"responses_without_usage"`
	Precision             float64      `json:"precision"`
	Recall                float64      `json:"recall"`
	F1                    float64      `json:"f1"`
	ValidLineRate         float64      `json:"valid_line_rate"`
	TraceCompleteness     float64      `json:"trace_completeness"`
	RedactionCanaryHit    bool         `json:"redaction_canary_leaked"`
}

type guardedProxy struct {
	mu           sync.Mutex
	forwarded    int
	rejected     int
	maxRequests  int
	maxTokens    int
	maxBodyBytes int64
	totalTokens  int64
	unknownUsage int
	proxy        *httputil.ReverseProxy
}

type benchmarkOptions struct {
	Limit           int
	MaxCalls        int
	MaxOutputTokens int
	CaseTimeout     time.Duration
	CaseIDs         string
}

func main() {
	limit := flag.Int("limit", 6, "number of benchmark cases to run; cases are taken in manifest order")
	caseIDs := flag.String("case-ids", "", "comma-separated sample IDs; overrides --limit")
	maxCalls := flag.Int("max-calls", 48, "hard cap on requests forwarded to the configured model endpoint")
	maxTokens := flag.Int("max-output-tokens", 1024, "maximum output tokens allowed per model request")
	caseTimeout := flag.Duration("case-timeout", 2*time.Minute, "time allowed for one review case")
	validateOnly := flag.Bool("validate-only", false, "validate the local fixture and scoring labels without calling a model")
	scorePath := flag.String("score-report", "", "re-score a saved run against the current labels without calling a model")
	flag.Parse()

	if *scorePath != "" {
		if err := rescoreReport(*scorePath); err != nil {
			fmt.Fprintln(os.Stderr, "benchmark:", err)
			os.Exit(1)
		}
		return
	}
	if *validateOnly {
		cases, err := loadCases("benchmarks/code_review_v1.json")
		if err != nil {
			fmt.Fprintln(os.Stderr, "benchmark:", err)
			os.Exit(1)
		}
		positive, negative := 0, 0
		for _, sample := range cases {
			if len(sample.Expected) == 0 {
				negative++
			} else {
				positive++
			}
		}
		fmt.Printf("validated %d cases (%d positive, %d negative)\n", len(cases), positive, negative)
		return
	}
	if err := run(benchmarkOptions{
		Limit:           *limit,
		MaxCalls:        *maxCalls,
		MaxOutputTokens: *maxTokens,
		CaseTimeout:     *caseTimeout,
		CaseIDs:         *caseIDs,
	}); err != nil {
		fmt.Fprintln(os.Stderr, "benchmark:", err)
		os.Exit(1)
	}
}

// validateRecordedRoute keeps reports from earlier benchmark runs readable.
func validateRecordedRoute(route string) error {
	switch route {
	case "auto", "single", "specialists":
		return nil
	default:
		return fmt.Errorf("benchmark 记录中的审查路线无效: %q", route)
	}
}

func run(options benchmarkOptions) error {
	if options.Limit <= 0 || options.MaxCalls <= 0 || options.MaxOutputTokens <= 0 || options.CaseTimeout <= 0 {
		return errors.New("limit, max-calls, max-output-tokens 和 case-timeout 必须大于 0")
	}
	cfg := logic.LoadConfig()
	if strings.TrimSpace(cfg.DeepSeekAPIKey) == "" {
		return errors.New("未配置 DEEPSEEK_API_KEY；benchmark 不会自动读取或输出密钥")
	}

	cases, err := loadCases("benchmarks/code_review_v1.json")
	if err != nil {
		return err
	}
	if strings.TrimSpace(options.CaseIDs) != "" {
		byID := make(map[string]testCase, len(cases))
		for _, sample := range cases {
			byID[sample.ID] = sample
		}
		selected := make([]testCase, 0)
		seen := map[string]bool{}
		for _, id := range strings.Split(options.CaseIDs, ",") {
			id = strings.TrimSpace(id)
			sample, ok := byID[id]
			if !ok || seen[id] {
				return fmt.Errorf("样本 ID %q 不存在或重复", id)
			}
			seen[id] = true
			selected = append(selected, sample)
		}
		cases = selected
	} else {
		if options.Limit > len(cases) {
			options.Limit = len(cases)
		}
		cases = cases[:options.Limit]
	}

	upstream, err := url.Parse(strings.TrimRight(cfg.DeepSeekBaseURL, "/"))
	if err != nil || upstream.Host == "" || (upstream.Scheme != "https" && upstream.Scheme != "http") {
		return errors.New("DEEPSEEK_BASE_URL 必须是有效的 http(s) 地址")
	}
	proxyState := newGuardedProxy(upstream, options.MaxCalls, options.MaxOutputTokens, 48*1024)
	proxyServer := &http.Server{Addr: "127.0.0.1:0", Handler: proxyState}
	listener, err := netListenLoopback()
	if err != nil {
		return fmt.Errorf("启动本地模型预算代理: %w", err)
	}
	defer proxyServer.Close()
	go func() { _ = proxyServer.Serve(listener) }()

	root, err := os.MkdirTemp("", "cr-agent-benchmark-")
	if err != nil {
		return fmt.Errorf("创建隔离运行目录: %w", err)
	}
	defer removeBenchmarkTemp(root)
	cfg.PersistenceMode = "file"
	cfg.DeepSeekBaseURL = "http://" + listener.Addr().String()
	cfg.SkillsDir = "skills"

	modelName := cfg.DeepSeekModel
	if strings.TrimSpace(modelName) == "" {
		modelName = "deepseek-flash"
	}
	report := runReport{
		Version:           "code-review-v1",
		Route:             "single",
		StartedAt:         time.Now().UTC(),
		Model:             modelName + " via configured endpoint",
		CasesRequested:    len(cases),
		MaxForwardedCalls: options.MaxCalls,
		MaxOutputTokens:   options.MaxOutputTokens,
		Results:           []caseResult{},
	}
	for index, sample := range cases {
		caseRoot := filepath.Join(root, fmt.Sprintf("case-%02d", index))
		caseCfg := isolatedCaseConfig(cfg, caseRoot)
		store := dao.NewJobStore(filepath.Join(caseRoot, "jobs"))
		service := logic.NewService(store, caseCfg)
		before := proxyState.snapshot()
		result := runCase(service, sample, options.CaseTimeout)
		service.Stop()
		after := proxyState.snapshot()
		result.Usage = diffUsage(before, after)
		report.Results = append(report.Results, result)
		if result.Status == "completed" || result.Status == "completed_with_warnings" {
			report.CasesCompleted++
		}
		report.TruePositives += result.TruePositives
		report.FalsePositives += result.FalsePositives
		report.FalseNegatives += result.FalseNegatives
		report.ValidLines += result.ValidLines
		report.InvalidLines += result.InvalidLines
		report.TraceEvents += result.TraceEvents
		report.CompleteTrace += result.CompleteTrace
		if after.Rejected > 0 || after.Forwarded >= options.MaxCalls {
			break
		}
	}

	report.FinishedAt = time.Now().UTC()
	usage := proxyState.snapshot()
	report.ForwardedRequests = usage.Forwarded
	report.RejectedRequests = usage.Rejected
	report.ReportedTokens = usage.Tokens
	report.ResponsesWithoutUsage = usage.Unknown
	report.Precision = ratio(report.TruePositives, report.TruePositives+report.FalsePositives)
	report.Recall = ratio(report.TruePositives, report.TruePositives+report.FalseNegatives)
	report.F1 = harmonic(report.Precision, report.Recall)
	report.ValidLineRate = ratio(report.ValidLines, report.ValidLines+report.InvalidLines)
	report.TraceCompleteness = ratio(report.CompleteTrace, report.TraceEvents)
	for _, result := range report.Results {
		report.RedactionCanaryHit = report.RedactionCanaryHit || result.CanaryLeaked
	}
	redactConfiguredKey(&report, cfg.DeepSeekAPIKey)
	if err := writeReport(report); err != nil {
		return err
	}
	printSummary(report)
	return nil
}

func isolatedCaseConfig(base logic.Config, root string) logic.Config {
	base.TeamMailboxDir = filepath.Join(root, "team")
	base.TasksDir = filepath.Join(root, "tasks")
	base.BackgroundTasksDir = filepath.Join(root, "background")
	base.MemoryDir = filepath.Join(root, "memory")
	base.WorkflowDir = filepath.Join(root, "workflows")
	base.CronFile = filepath.Join(root, "cron.json")
	base.ContextOutputDir = filepath.Join(root, "tool-results")
	base.ContextTranscriptDir = filepath.Join(root, "transcripts")
	return base
}

func netListenLoopback() (net.Listener, error) {
	return net.Listen("tcp", "127.0.0.1:0")
}

func loadCases(path string) ([]testCase, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取 benchmark 样本: %w", err)
	}
	var cases []testCase
	if err := json.Unmarshal(content, &cases); err != nil {
		return nil, fmt.Errorf("解析 benchmark 样本: %w", err)
	}
	if len(cases) == 0 {
		return nil, errors.New("benchmark 样本为空")
	}
	seen := map[string]bool{}
	for _, sample := range cases {
		if sample.ID == "" || sample.Diff == "" || seen[sample.ID] {
			return nil, fmt.Errorf("样本 ID 缺失、diff 为空或 ID 重复: %q", sample.ID)
		}
		seen[sample.ID] = true
		addedLines := changedLines(sample.Diff)
		for _, expected := range sample.Expected {
			if expected.File == "" || expected.Line <= 0 || !addedLines[expected.File][expected.Line] || len(expected.MatchGroups) == 0 {
				return nil, fmt.Errorf("样本 %q 的标注行 %s:%d 必须位于新增行，检测到=%v", sample.ID, expected.File, expected.Line, addedLines[expected.File])
			}
			for _, group := range expected.MatchGroups {
				if len(group) == 0 {
					return nil, fmt.Errorf("样本 %q 存在空的匹配词组", sample.ID)
				}
			}
		}
	}
	return cases, nil
}

func rescoreReport(path string) error {
	cases, err := loadCases("benchmarks/code_review_v1.json")
	if err != nil {
		return err
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("读取已有 benchmark 结果: %w", err)
	}
	var report runReport
	if err := json.Unmarshal(content, &report); err != nil {
		return fmt.Errorf("解析已有 benchmark 结果: %w", err)
	}
	if report.Route != "" {
		if err := validateRecordedRoute(report.Route); err != nil {
			return fmt.Errorf("已有 benchmark 结果的路线无效: %w", err)
		}
	}
	byID := make(map[string]testCase, len(cases))
	for _, sample := range cases {
		byID[sample.ID] = sample
	}
	report.TruePositives = 0
	report.FalsePositives = 0
	report.FalseNegatives = 0
	report.ValidLines = 0
	report.InvalidLines = 0
	report.TraceEvents = 0
	report.CompleteTrace = 0
	report.CasesCompleted = 0
	report.ForwardedRequests = 0
	report.RejectedRequests = 0
	report.ReportedTokens = 0
	report.ResponsesWithoutUsage = 0
	for index := range report.Results {
		result := &report.Results[index]
		sample, ok := byID[result.ID]
		if !ok {
			return fmt.Errorf("结果中的样本 %q 不在当前 manifest", result.ID)
		}
		result.Expected = sample.Expected
		result.TruePositives, result.FalsePositives, result.FalseNegatives = score(sample, result.Findings)
		result.ValidLines, result.InvalidLines = countValidLines(sample.Diff, result.Findings)
		report.ForwardedRequests += result.Usage.Forwarded
		report.RejectedRequests += result.Usage.Rejected
		report.ReportedTokens += result.Usage.Tokens
		report.ResponsesWithoutUsage += result.Usage.Unknown
		if result.Status == "completed" || result.Status == "completed_with_warnings" {
			report.CasesCompleted++
		}
		report.TruePositives += result.TruePositives
		report.FalsePositives += result.FalsePositives
		report.FalseNegatives += result.FalseNegatives
		report.ValidLines += result.ValidLines
		report.InvalidLines += result.InvalidLines
		report.TraceEvents += result.TraceEvents
		report.CompleteTrace += result.CompleteTrace
	}
	report.Precision = ratio(report.TruePositives, report.TruePositives+report.FalsePositives)
	report.Recall = ratio(report.TruePositives, report.TruePositives+report.FalseNegatives)
	report.F1 = harmonic(report.Precision, report.Recall)
	report.ValidLineRate = ratio(report.ValidLines, report.ValidLines+report.InvalidLines)
	report.TraceCompleteness = ratio(report.CompleteTrace, report.TraceEvents)
	if err := writeReport(report); err != nil {
		return err
	}
	printSummary(report)
	return nil
}

func runCase(service *logic.Service, sample testCase, timeout time.Duration) caseResult {
	result := caseResult{
		ID: sample.ID, Category: sample.Category, Status: "failed",
		Expected: sample.Expected, Findings: []model.ReviewComment{},
		FalseNegatives: len(sample.Expected),
	}
	started := time.Now()
	job, err := service.Create(model.ReviewRequest{Source: "benchmark://" + sample.ID, Diff: sample.Diff})
	if err != nil {
		result.Error = err.Error()
		result.DurationMs = time.Since(started).Milliseconds()
		return result
	}
	deadline := time.NewTimer(timeout)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer deadline.Stop()
	defer ticker.Stop()
	for {
		background, err := service.Background.Get(job.BackgroundTaskID)
		if err != nil {
			result.Error = err.Error()
			result.DurationMs = time.Since(started).Milliseconds()
			return result
		}
		if background.Status == model.BackgroundTaskCompleted || background.Status == model.BackgroundTaskFailed || background.Status == model.BackgroundTaskCancelled {
			completed, exists := service.Store.Get(job.ID)
			if exists {
				result.Status = completed.Status
				result.Findings = actionableFindings(completed.Comments)
				result.TraceEvents = len(completed.Trace)
				result.CompleteTrace = completeTraceCount(completed.Trace)
				result.ModelJSONValid = modelReplyValid(completed.Trace)
				result.CanaryLeaked = traceContains(completed.Trace, "BENCHMARK_FAKE_VALUE_NOT_A_CREDENTIAL_0001")
				result.TruePositives, result.FalsePositives, result.FalseNegatives = score(sample, result.Findings)
				result.ValidLines, result.InvalidLines = countValidLines(sample.Diff, result.Findings)
				if result.Status != "completed" && result.Status != "completed_with_warnings" {
					result.Error = completed.Error
				}
			}
			result.DurationMs = time.Since(started).Milliseconds()
			return result
		}
		select {
		case <-deadline.C:
			_, _ = service.Background.Cancel(job.BackgroundTaskID)
			result.Error = "单个样本超过时限，后台任务已取消"
			result.DurationMs = time.Since(started).Milliseconds()
			return result
		case <-ticker.C:
		}
	}
}

func actionableFindings(comments []model.ReviewComment) []model.ReviewComment {
	findings := make([]model.ReviewComment, 0, len(comments))
	for _, comment := range comments {
		if comment.Severity == "info" || strings.Contains(comment.Body, "未发现需要评论的问题") {
			continue
		}
		findings = append(findings, comment)
	}
	return findings
}

func score(sample testCase, findings []model.ReviewComment) (tp, fp, fn int) {
	matched := make([]bool, len(findings))
	for _, expected := range sample.Expected {
		found := false
		for index, finding := range findings {
			if matched[index] || !sameFile(expected.File, finding.File) || abs(expected.Line-finding.Line) > 2 {
				continue
			}
			if !matchesGroups(expected.MatchGroups, finding.Body) {
				continue
			}
			matched[index] = true
			found = true
			break
		}
		if found {
			tp++
		} else {
			fn++
		}
	}
	for _, isMatched := range matched {
		if !isMatched {
			fp++
		}
	}
	return tp, fp, fn
}

func matchesGroups(groups [][]string, body string) bool {
	text := strings.ToLower(body)
	for _, group := range groups {
		groupMatched := false
		for _, term := range group {
			if strings.Contains(text, strings.ToLower(term)) {
				groupMatched = true
				break
			}
		}
		if !groupMatched {
			return false
		}
	}
	return true
}

func countValidLines(diff string, findings []model.ReviewComment) (valid, invalid int) {
	changed := changedLines(diff)
	for _, finding := range findings {
		if changed[finding.File][finding.Line] {
			valid++
		} else {
			invalid++
		}
	}
	return valid, invalid
}

func changedLines(diff string) map[string]map[int]bool {
	changed := map[string]map[int]bool{}
	file := ""
	line := 0
	for _, raw := range strings.Split(diff, "\n") {
		if match := diffFilePattern.FindStringSubmatch(raw); len(match) == 3 {
			file = match[2]
			if changed[file] == nil {
				changed[file] = map[int]bool{}
			}
			continue
		}
		if strings.HasPrefix(raw, "@@") {
			match := hunkPattern.FindStringSubmatch(raw)
			if len(match) == 2 {
				line, _ = strconv.Atoi(match[1])
			}
			continue
		}
		if strings.HasPrefix(raw, "+++") || strings.HasPrefix(raw, "---") || strings.HasPrefix(raw, "\\") {
			continue
		}
		if strings.HasPrefix(raw, "+") {
			changed[file][line] = true
			line++
			continue
		}
		if strings.HasPrefix(raw, " ") {
			line++
		}
	}
	return changed
}

var (
	diffFilePattern = regexp.MustCompile(`^diff --git a/(.+) b/(.+)$`)
	hunkPattern     = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,\d+)? @@`)
)

func modelReplyValid(events []model.TraceEvent) bool {
	for _, event := range events {
		isReviewOutput := event.Tool == "review_agent" || event.Tool == "deepseek-review"
		if !isReviewOutput || strings.TrimSpace(event.ModelReply) == "" {
			continue
		}
		content := strings.TrimSpace(event.ModelReply)
		content = strings.TrimPrefix(content, "```json")
		content = strings.TrimPrefix(content, "```")
		content = strings.TrimSuffix(strings.TrimSpace(content), "```")
		var findings []json.RawMessage
		return json.Unmarshal([]byte(strings.TrimSpace(content)), &findings) == nil
	}
	return false
}

func completeTraceCount(events []model.TraceEvent) int {
	count := 0
	for _, event := range events {
		if !event.StartedAt.IsZero() && event.EndedAt != nil && event.Status != "" && !event.EndedAt.Before(event.StartedAt) {
			count++
		}
	}
	return count
}

func traceContains(events []model.TraceEvent, value string) bool {
	if value == "" {
		return false
	}
	content, _ := json.Marshal(events)
	return strings.Contains(string(content), value)
}

func sameFile(expected, actual string) bool {
	return strings.TrimPrefix(filepath.ToSlash(expected), "./") == strings.TrimPrefix(filepath.ToSlash(actual), "./")
}

func diffUsage(before, after proxyUsage) proxyUsage {
	return proxyUsage{
		Forwarded: after.Forwarded - before.Forwarded,
		Rejected:  after.Rejected - before.Rejected,
		Tokens:    after.Tokens - before.Tokens,
		Unknown:   after.Unknown - before.Unknown,
	}
}

func (p *guardedProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "only model POST requests are allowed", http.StatusMethodNotAllowed)
		return
	}
	content, err := io.ReadAll(io.LimitReader(r.Body, p.maxBodyBytes+1))
	if err != nil || int64(len(content)) > p.maxBodyBytes {
		http.Error(w, "benchmark request body limit exceeded", http.StatusRequestEntityTooLarge)
		return
	}
	var payload map[string]any
	if err := json.Unmarshal(content, &payload); err != nil {
		http.Error(w, "invalid model request JSON", http.StatusBadRequest)
		return
	}
	if current, ok := payload["max_tokens"].(float64); !ok || current > float64(p.maxTokens) {
		payload["max_tokens"] = p.maxTokens
	}
	content, err = json.Marshal(payload)
	if err != nil {
		http.Error(w, "invalid model request", http.StatusBadRequest)
		return
	}
	r.Body = io.NopCloser(strings.NewReader(string(content)))
	r.ContentLength = int64(len(content))
	r.Header.Set("Content-Length", fmt.Sprint(len(content)))
	p.mu.Lock()
	if p.forwarded >= p.maxRequests {
		p.rejected++
		p.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":{"message":"benchmark model-call budget reached","type":"rate_limit_error"}}`)
		return
	}
	p.forwarded++
	p.mu.Unlock()
	p.proxy.ServeHTTP(w, r)
}

func newGuardedProxy(target *url.URL, maxRequests, maxTokens int, maxBodyBytes int64) *guardedProxy {
	state := &guardedProxy{maxRequests: maxRequests, maxTokens: maxTokens, maxBodyBytes: maxBodyBytes}
	proxy := httputil.NewSingleHostReverseProxy(target)
	baseDirector := proxy.Director
	proxy.Director = func(request *http.Request) {
		baseDirector(request)
		request.Host = target.Host
	}
	proxy.ModifyResponse = func(response *http.Response) error {
		content, err := io.ReadAll(response.Body)
		if err != nil {
			return err
		}
		_ = response.Body.Close()
		response.Body = io.NopCloser(strings.NewReader(string(content)))
		response.ContentLength = int64(len(content))
		response.Header.Set("Content-Length", fmt.Sprint(len(content)))
		var decoded struct {
			Usage struct {
				TotalTokens int64 `json:"total_tokens"`
			} `json:"usage"`
		}
		state.mu.Lock()
		if json.Unmarshal(content, &decoded) == nil && decoded.Usage.TotalTokens > 0 {
			state.totalTokens += decoded.Usage.TotalTokens
		} else {
			state.unknownUsage++
		}
		state.mu.Unlock()
		return nil
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, err error) {
		state.mu.Lock()
		state.unknownUsage++
		state.mu.Unlock()
		http.Error(w, "model endpoint request failed", http.StatusBadGateway)
	}
	state.proxy = proxy
	return state
}

func (p *guardedProxy) snapshot() proxyUsage {
	p.mu.Lock()
	defer p.mu.Unlock()
	return proxyUsage{Forwarded: p.forwarded, Rejected: p.rejected, Tokens: p.totalTokens, Unknown: p.unknownUsage}
}

func writeReport(report runReport) error {
	if err := os.MkdirAll("benchmarks/results", 0755); err != nil {
		return err
	}
	name := reportFileStem(report)
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join("benchmarks/results", name+".json"), encoded, 0600); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join("benchmarks/results", name+".md"), []byte(renderMarkdown(report)), 0600)
}

func reportFileStem(report runReport) string {
	return "run-" + report.StartedAt.Format("20060102T150405Z") + "-" + reportRoute(report)
}

func reportRoute(report runReport) string {
	if report.Route == "" {
		return "single"
	}
	return report.Route
}

func tokenSummary(usage proxyUsage) string {
	if usage.Unknown == 0 {
		return strconv.FormatInt(usage.Tokens, 10)
	}
	if usage.Tokens == 0 {
		return fmt.Sprintf("unknown (%d responses)", usage.Unknown)
	}
	return fmt.Sprintf("%d known + %d unknown", usage.Tokens, usage.Unknown)
}

func renderMarkdown(report runReport) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "# CR-Agent Code Review Benchmark\n\n")
	fmt.Fprintf(
		&builder,
		"- Benchmark: `%s`\n- Route: `%s`\n- Started: %s\n- Model: %s\n- Cases completed: %d/%d\n- Total wall time: %d ms\n- Forwarded model requests: %d (cap %d)\n- Model-reported tokens: %s\n\n",
		report.Version,
		reportRoute(report),
		report.StartedAt.Format(time.RFC3339),
		report.Model,
		report.CasesCompleted,
		report.CasesRequested,
		report.FinishedAt.Sub(report.StartedAt).Milliseconds(),
		report.ForwardedRequests,
		report.MaxForwardedCalls,
		tokenSummary(proxyUsage{Tokens: report.ReportedTokens, Unknown: report.ResponsesWithoutUsage}),
	)
	fmt.Fprintf(&builder, "| Precision | Recall | F1 | Valid changed-line rate | Trace completeness |\n|---:|---:|---:|---:|---:|\n")
	fmt.Fprintf(&builder, "| %.1f%% | %.1f%% | %.1f%% | %.1f%% | %.1f%% |\n\n",
		100*report.Precision, 100*report.Recall, 100*report.F1, 100*report.ValidLineRate, 100*report.TraceCompleteness)
	fmt.Fprintf(&builder, "TP=%d, FP=%d, FN=%d. Redaction canary leaked: **%t**.\n\n", report.TruePositives, report.FalsePositives, report.FalseNegatives, report.RedactionCanaryHit)
	fmt.Fprintf(&builder, "| Case | Category | Status | TP | FP | FN | Line valid/invalid | Duration | Model requests | Reported tokens |\n|---|---|---|---:|---:|---:|---:|---:|---:|---:|\n")
	for _, result := range report.Results {
		fmt.Fprintf(
			&builder,
			"| `%s` | %s | %s | %d | %d | %d | %d/%d | %d ms | %d | %s |\n",
			result.ID,
			result.Category,
			result.Status,
			result.TruePositives,
			result.FalsePositives,
			result.FalseNegatives,
			result.ValidLines,
			result.InvalidLines,
			result.DurationMs,
			result.Usage.Forwarded,
			tokenSummary(result.Usage),
		)
	}
	fmt.Fprintf(&builder, "\nScoring uses fixture-specific phrase groups plus changed-file and ±2-line localization. It is a pilot heuristic, not an independent semantic judge. Failed cases count their missing expected issues as false negatives; compare completion rate alongside precision and recall.\n")
	return builder.String()
}

func printSummary(report runReport) {
	fmt.Printf("CR-Agent Code Review Benchmark %s route=%s\n", report.Version, reportRoute(report))
	fmt.Printf("cases: %d/%d completed; TP=%d FP=%d FN=%d\n", report.CasesCompleted, report.CasesRequested, report.TruePositives, report.FalsePositives, report.FalseNegatives)
	fmt.Printf("wall_ms=%d\n", report.FinishedAt.Sub(report.StartedAt).Milliseconds())
	fmt.Printf("precision=%.1f%% recall=%.1f%% F1=%.1f%% valid_line=%.1f%% trace=%.1f%%\n", 100*report.Precision, 100*report.Recall, 100*report.F1, 100*report.ValidLineRate, 100*report.TraceCompleteness)
	fmt.Printf("model_requests=%d/%d rejected=%d reported_tokens=%s redaction_canary_leaked=%t\n", report.ForwardedRequests, report.MaxForwardedCalls, report.RejectedRequests, tokenSummary(proxyUsage{Tokens: report.ReportedTokens, Unknown: report.ResponsesWithoutUsage}), report.RedactionCanaryHit)
	for _, result := range report.Results {
		fmt.Printf(
			"case=%s status=%s TP=%d FP=%d FN=%d duration_ms=%d model_requests=%d reported_tokens=%s\n",
			result.ID,
			result.Status,
			result.TruePositives,
			result.FalsePositives,
			result.FalseNegatives,
			result.DurationMs,
			result.Usage.Forwarded,
			tokenSummary(result.Usage),
		)
	}
	fmt.Printf("artifacts: benchmarks/results/%s.{json,md}\n", reportFileStem(report))
}

func redactConfiguredKey(report *runReport, apiKey string) {
	if report == nil || apiKey == "" {
		return
	}
	for index := range report.Results {
		result := &report.Results[index]
		result.Error = strings.ReplaceAll(result.Error, apiKey, "[REDACTED]")
		for findingIndex := range result.Findings {
			result.Findings[findingIndex].Body = strings.ReplaceAll(result.Findings[findingIndex].Body, apiKey, "[REDACTED]")
		}
	}
}

func ratio(numerator, denominator int) float64 {
	if denominator == 0 {
		return 0
	}
	return float64(numerator) / float64(denominator)
}

func harmonic(precision, recall float64) float64 {
	if precision+recall == 0 {
		return 0
	}
	return 2 * precision * recall / (precision + recall)
}

func abs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func removeBenchmarkTemp(path string) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return
	}
	absTemp, err := filepath.Abs(os.TempDir())
	if err != nil {
		return
	}
	if filepath.Dir(absPath) == absTemp && strings.HasPrefix(filepath.Base(absPath), "cr-agent-benchmark-") {
		_ = os.RemoveAll(absPath)
	}
}
