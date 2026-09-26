package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"CR-Agent/internal/dao"
	"CR-Agent/internal/logic"
	"CR-Agent/internal/model"
)

// realCaseResult holds everything recorded for one real-PR case.
type realCaseResult struct {
	ID             string                `json:"id"`
	Scenario       string                `json:"scenario"`
	Language       string                `json:"language,omitempty"`
	PrURL          string                `json:"pr_url"`
	Title          string                `json:"title,omitempty"`
	Status         string                `json:"status"`
	DurationMs     int64                 `json:"duration_ms"`
	References     []realReference       `json:"references"`
	Findings       []model.ReviewComment `json:"findings"`
	RefVerdicts    []judgeRefVerdict     `json:"reference_verdicts"`
	FindingVerdict []judgeFindingVerdict `json:"finding_verdicts"`
	PairScores     []judgePairScore      `json:"pair_scores"`
	JudgeRaw       string                `json:"-"`
	JudgeError     string                `json:"judge_error,omitempty"`
	CoveredRefs    int                   `json:"covered_refs"`
	PartialRefs    int                   `json:"partial_refs"`
	MissedRefs     int                   `json:"missed_refs"`
	CoverageScore  float64               `json:"coverage_score"`
	CoreRefs       int                   `json:"core_refs"`
	FixedRefs      int                   `json:"fixed_refs"`
	FixedHits      int                   `json:"fixed_issue_reflags"`
	SuggestionRefs int                   `json:"suggestion_refs"`
	SuggestionCovered float64            `json:"suggestion_covered"`
	SuggestionMissed  int                `json:"suggestion_missed"`
	SkippedRefs   int                   `json:"skipped_refs"`
	Matched        int                   `json:"matched_findings"`
	Novel          int                   `json:"novel_findings"`
	FalsePositives int                   `json:"false_positive_findings"`
	Nitpicks       int                   `json:"nitpick_findings"`
	Unjudged       int                   `json:"unjudged_findings"`
	ValidLines     int                   `json:"valid_lines"`
	InvalidLines   int                   `json:"invalid_lines"`
	TraceEvents    int                   `json:"trace_events"`
	CompleteTrace  int                   `json:"complete_trace_events"`
	ModelJSONValid bool                  `json:"model_json_valid"`
	CanaryLeaked   bool                  `json:"redaction_canary_leaked"`
	ModelReplies   []string              `json:"model_replies,omitempty"`
	Error          string                `json:"error,omitempty"`
	Usage          proxyUsage            `json:"model_usage"`
}

// realRunReport is the aggregate report for a real-PR benchmark run.
type realRunReport struct {
	Version               string           `json:"version"`
	Manifest              string           `json:"manifest"`
	Route                 string           `json:"route"`
	StartedAt             time.Time        `json:"started_at"`
	FinishedAt            time.Time        `json:"finished_at"`
	Model                 string           `json:"model"`
	JudgeModel            string           `json:"judge_model"`
	CasesRequested        int              `json:"cases_requested"`
	CasesCompleted        int              `json:"cases_completed"`
	MaxForwardedCalls     int              `json:"max_forwarded_calls"`
	MaxOutputTokens       int              `json:"max_output_tokens_per_call"`
	Results               []realCaseResult `json:"results"`
	TotalRefs             int              `json:"total_reference_comments"`
	CoreRefs              int              `json:"core_reference_comments"`
	CoveredRefs           int              `json:"covered_reference_comments"`
	PartialRefs           int              `json:"partial_reference_comments"`
	MissedRefs            int              `json:"missed_reference_comments"`
	SuggestionRefs        int              `json:"suggestion_reference_comments"`
	SuggestionCovered     float64          `json:"suggestion_covered_weight"`
	SuggestionMissed      int              `json:"suggestion_missed"`
	SuggestionCoverage    float64          `json:"suggestion_coverage"`
	FixedRefs             int              `json:"fixed_in_snapshot_refs"`
	FixedHits             int              `json:"fixed_issue_reflags"`
	SkippedRefs           int              `json:"skipped_refs"`
	TotalFindings         int              `json:"total_findings"`
	MatchedFindings       int              `json:"matched_findings"`
	NovelFindings         int              `json:"novel_findings"`
	FalsePositiveFindings int              `json:"false_positive_findings"`
	NitpickFindings       int              `json:"nitpick_findings"`
	UnjudgedFindings      int              `json:"unjudged_findings"`
	ValidLines            int              `json:"valid_lines"`
	InvalidLines          int              `json:"invalid_lines"`
	TraceEvents           int              `json:"trace_events"`
	CompleteTrace         int              `json:"complete_trace_events"`
	ForwardedRequests     int              `json:"forwarded_requests"`
	RejectedRequests      int              `json:"budget_rejections"`
	ReportedTokens        int64            `json:"reported_tokens"`
	ResponsesWithoutUsage int              `json:"responses_without_usage"`
	PairCount             int              `json:"scored_pairs"`
	Recall                float64          `json:"reference_coverage"`
	Precision             float64          `json:"finding_precision"`
	ValidLineRate         float64          `json:"valid_line_rate"`
	TraceCompleteness     float64          `json:"trace_completeness"`
	MeanAccuracy          float64          `json:"mean_accuracy"`
	MeanRelevance         float64          `json:"mean_relevance"`
	MeanUsefulness        float64          `json:"mean_usefulness"`
	RedactionCanaryHit    bool             `json:"redaction_canary_leaked"`
	DimensionCoverage     map[string]*dimCoverage  `json:"dimension_coverage"`
	ScenarioMetrics       map[string]*scenarioMetric `json:"scenario_metrics"`
}

type dimCoverage struct {
	Total    int     `json:"references"`
	Covered  float64 `json:"covered_weight"` // partial counts 0.5
	Recall   float64 `json:"recall"`
}

type scenarioMetric struct {
	Cases         int     `json:"cases"`
	Refs          int     `json:"references"`
	CoveredWeight float64 `json:"covered_weight"`
	Recall        float64 `json:"recall"`
	Findings      int     `json:"findings"`
	Matched       int     `json:"matched"`
	Novel         int     `json:"novel"`
	FalsePos      int     `json:"false_positives"`
	Precision     float64 `json:"precision"`
}

const realReportVersion = "real-pr-v1"

// selectRealCases applies --case-ids / --limit to the manifest order.
func selectRealCases(cases []realCase, options benchmarkOptions) ([]realCase, error) {
	if strings.TrimSpace(options.CaseIDs) != "" {
		byID := make(map[string]realCase, len(cases))
		for _, sample := range cases {
			byID[sample.ID] = sample
		}
		selected := make([]realCase, 0)
		seen := map[string]bool{}
		for _, id := range strings.Split(options.CaseIDs, ",") {
			id = strings.TrimSpace(id)
			sample, ok := byID[id]
			if !ok || seen[id] {
				return nil, fmt.Errorf("real-PR 样本 ID %q 不存在或重复", id)
			}
			seen[id] = true
			selected = append(selected, sample)
		}
		return selected, nil
	}
	if options.Limit > 0 && options.Limit < len(cases) {
		return cases[:options.Limit], nil
	}
	return cases, nil
}

// runReal executes the real-PR benchmark end to end.
func runReal(manifestPath string, options benchmarkOptions, judgeMaxTokens int) error {
	if options.MaxCalls <= 0 || options.MaxOutputTokens <= 0 || options.CaseTimeout <= 0 {
		return errors.New("max-calls, max-output-tokens 和 case-timeout 必须大于 0")
	}
	cfg, err := logic.LoadConfig()
	if err != nil {
		return err
	}
	if strings.TrimSpace(cfg.DeepSeekAPIKey) == "" {
		return errors.New("未配置 DEEPSEEK_API_KEY；benchmark 不会自动读取或输出密钥")
	}
	cases, err := loadRealCases(manifestPath)
	if err != nil {
		return err
	}
	cases, err = selectRealCases(cases, options)
	if err != nil {
		return err
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

	root, err := os.MkdirTemp("", "cr-agent-benchmark-real-")
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
	judge := newJudgeClient("http://"+listener.Addr().String(), cfg.DeepSeekAPIKey, modelName, judgeMaxTokens)

	report := realRunReport{
		Version:           realReportVersion,
		Manifest:          manifestPath,
		Route:             "real-pr",
		StartedAt:         time.Now().UTC(),
		Model:             modelName + " via configured endpoint",
		JudgeModel:        modelName,
		CasesRequested:    len(cases),
		MaxForwardedCalls: options.MaxCalls,
		MaxOutputTokens:   options.MaxOutputTokens,
		Results:           []realCaseResult{},
		DimensionCoverage: map[string]*dimCoverage{},
		ScenarioMetrics:   map[string]*scenarioMetric{},
	}

	baseDir := filepath.Dir(manifestPath)
	for _, sample := range cases {
		diffContent, err := os.ReadFile(filepath.Join(baseDir, filepath.FromSlash(sample.DiffFile)))
		if err != nil {
			return fmt.Errorf("样本 %s 的 diff 快照读取失败: %w", sample.ID, err)
		}
		caseRoot := filepath.Join(root, "case-"+sample.ID)
		caseCfg := isolatedCaseConfig(cfg, caseRoot)
		store := dao.NewJobStore(filepath.Join(caseRoot, "jobs"))
		service := logic.NewService(store, caseCfg)
		before := proxyState.snapshot()
		result := runRealCase(service, sample, string(diffContent), judge, options.CaseTimeout)
		service.Stop()
		result.Usage = diffUsage(before, proxyState.snapshot())
		report.Results = append(report.Results, result)
		if result.Status == "completed" || result.Status == "completed_with_warnings" {
			report.CasesCompleted++
		}
		if after := proxyState.snapshot(); after.Rejected > 0 || after.Forwarded >= options.MaxCalls {
			break
		}
	}

	finalizeRealReport(&report)
	redactConfiguredKeyReal(&report, cfg.DeepSeekAPIKey)
	if err := writeRealReport(report); err != nil {
		return err
	}
	printRealSummary(report)
	return nil
}

// rescoreRealReport recomputes aggregate metrics of a saved real-PR run from
// its stored judge verdicts against the current manifest labels.
func rescoreRealReport(path string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("读取 real-PR 运行结果: %w", err)
	}
	var report realRunReport
	if err := json.Unmarshal(content, &report); err != nil {
		return fmt.Errorf("解析 real-PR 运行结果: %w", err)
	}
	if report.Version != realReportVersion {
		return fmt.Errorf("报告版本不是 %s", realReportVersion)
	}
	cases, err := loadRealCases(report.Manifest)
	if err != nil {
		return err
	}
	byID := make(map[string]realCase, len(cases))
	for _, sample := range cases {
		byID[sample.ID] = sample
	}
	for index := range report.Results {
		result := &report.Results[index]
		sample, ok := byID[result.ID]
		if !ok {
			return fmt.Errorf("结果中的样本 %q 不在当前 manifest", result.ID)
		}
		result.References = sample.ReferenceComments
		result.CoveredRefs, result.PartialRefs, result.MissedRefs = 0, 0, len(result.References)
		result.Matched, result.Novel, result.FalsePositives, result.Nitpicks = 0, 0, 0, 0
		result.CoverageScore = 0
		applyJudgeVerdicts(result)
	}
	finalizeRealReport(&report)
	if err := writeRealReport(report); err != nil {
		return err
	}
	printRealSummary(report)
	return nil
}

// runRealCase reviews one real PR through the standard service and judges it.
func runRealCase(service *logic.Service, sample realCase, diffContent string, judge *judgeClient, timeout time.Duration) realCaseResult {
	result := realCaseResult{
		ID: sample.ID, Scenario: sample.Scenario, Language: sample.Language,
		PrURL: sample.PrURL, Title: sample.Title, Status: "failed",
		References: sample.ReferenceComments,
		Findings:   []model.ReviewComment{},
		MissedRefs: len(sample.ReferenceComments),
	}
	started := time.Now()
	job, err := service.Create(model.ReviewRequest{Source: sample.PrURL})
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
				result.ModelReplies = collectModelReplies(completed.Trace)
				result.ValidLines, result.InvalidLines = countValidLines(diffContent, result.Findings)
				if result.Status != "completed" && result.Status != "completed_with_warnings" {
					result.Error = completed.Error
				}
			}
			break
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
	result.DurationMs = time.Since(started).Milliseconds()

	// Judge only completed reviews that produced a usable diff snapshot.
	if result.Status == "completed" || result.Status == "completed_with_warnings" {
		output := runJudge(judge, sample, diffContent, result.Findings)
		result.RefVerdicts = output.ReferenceVerdicts
		result.FindingVerdict = output.FindingVerdicts
		result.PairScores = output.PairScores
		result.JudgeRaw = clip(output.Raw, 8000)
		result.JudgeError = output.ParseError
		applyJudgeVerdicts(&result)
	}
	return result
}

// applyJudgeVerdicts derives coverage and finding classifications from judge
// output. Coverage is computed over the core channel (status "valid") only;
// suggestion-channel matches feed the separate suggestion metrics, and a
// re-flagged fixed_in_snapshot reference converts its covering findings into
// false positives. Judge verdicts redefine the counters (the constructor
// placeholder assumes every reference missed without a judge run); references
// without any verdict count as missed in their channel.
func applyJudgeVerdicts(result *realCaseResult) {
	result.CoveredRefs, result.PartialRefs, result.MissedRefs = 0, 0, 0
	result.SuggestionCovered, result.SuggestionMissed = 0, 0
	result.FixedHits = 0
	result.CoreRefs, result.FixedRefs, result.SuggestionRefs, result.SkippedRefs = 0, 0, 0, 0
	for _, ref := range result.References {
		switch refChannel(ref) {
		case refStatusValid:
			result.CoreRefs++
		case refStatusFixedInSnapshot:
			result.FixedRefs++
		case refStatusSuggestion:
			result.SuggestionRefs++
		case refStatusUnverifiable:
			result.SkippedRefs++
		}
	}
	total := len(result.References)
	channelOf := func(index int) string {
		if index < 0 || index >= total {
			return ""
		}
		return refChannel(result.References[index])
	}
	coverage := 0.0
	seen := map[int]bool{}
	for _, verdict := range result.RefVerdicts {
		if verdict.Index < 0 || verdict.Index >= total {
			continue
		}
		seen[verdict.Index] = true
		switch channelOf(verdict.Index) {
		case refStatusValid:
			switch verdict.Verdict {
			case "covered":
				result.CoveredRefs++
				coverage += 1.0
			case "partial":
				result.PartialRefs++
				coverage += 0.5
			case "missed":
				result.MissedRefs++
			}
		case refStatusFixedInSnapshot:
			if verdict.Verdict == "re_flagged" {
				result.FixedHits++
			}
		case refStatusSuggestion:
			switch verdict.Verdict {
			case "covered":
				result.SuggestionCovered += 1.0
			case "partial":
				result.SuggestionCovered += 0.5
			case "missed":
				result.SuggestionMissed++
			}
		case refStatusUnverifiable:
		}
	}
	for index := range result.References {
		if seen[index] {
			continue
		}
		switch channelOf(index) {
		case refStatusValid:
			result.MissedRefs++
		case refStatusSuggestion:
			result.SuggestionMissed++
		}
	}
	result.CoverageScore = coverage
	matched := map[int]bool{}
	for _, verdict := range result.RefVerdicts {
		if verdict.Index < 0 || verdict.Index >= total {
			continue
		}
		channel := channelOf(verdict.Index)
		isFixedReflag := channel == refStatusFixedInSnapshot && verdict.Verdict == "re_flagged"
		if (channel == refStatusValid || channel == refStatusSuggestion) && (verdict.Verdict == "covered" || verdict.Verdict == "partial") {
			for _, findingIndex := range verdict.CoveredBy {
				matched[findingIndex] = true
			}
		}
		if isFixedReflag {
			// Findings re-asserting an already-fixed issue are false positives
			// regardless of the per-finding verdict.
			for _, findingIndex := range verdict.CoveredBy {
				matched[findingIndex] = false
			}
		}
	}
	// A failed judge must never fabricate verdicts: findings stay unjudged
	// instead of defaulting to false positives, and unseen core references
	// count as missed (conservative).
	if result.JudgeError != "" {
		result.Unjudged = len(result.Findings)
		return
	}
	for index := range result.Findings {
		if matched[index] {
			result.Matched++
			continue
		}
		verdict := "out_of_scope"
		if index < len(result.FindingVerdict) {
			verdict = result.FindingVerdict[index].Verdict
		}
		switch verdict {
		case "valid_new_issue":
			result.Novel++
		case "nitpick":
			result.Nitpicks++
		case "matched_reference":
			result.Matched++
		default:
			result.FalsePositives++
		}
	}
}

// finalizeRealReport aggregates per-case results into report-level metrics.
// Reference coverage is over the core channel only (audited "valid"
// references); suggestion coverage is reported separately, and fixed-issue
// re-flags are tracked as false-positive precursors. All accumulators are
// reset first so rescoring a saved report cannot double-count its stale
// report-level values.
func finalizeRealReport(report *realRunReport) {
	report.TotalRefs, report.CoreRefs, report.FixedRefs = 0, 0, 0
	report.SuggestionRefs, report.SkippedRefs = 0, 0
	report.CoveredRefs, report.PartialRefs, report.MissedRefs = 0, 0, 0
	report.SuggestionCovered, report.SuggestionMissed = 0, 0
	report.FixedHits = 0
	report.TotalFindings = 0
	report.MatchedFindings, report.NovelFindings = 0, 0
	report.FalsePositiveFindings, report.NitpickFindings = 0, 0
	report.UnjudgedFindings = 0
	report.ValidLines, report.InvalidLines = 0, 0
	report.TraceEvents, report.CompleteTrace = 0, 0
	report.PairCount = 0
	report.ForwardedRequests, report.RejectedRequests = 0, 0
	report.ReportedTokens, report.ResponsesWithoutUsage = 0, 0
	report.RedactionCanaryHit = false
	report.DimensionCoverage = map[string]*dimCoverage{}
	report.ScenarioMetrics = map[string]*scenarioMetric{}
	accuracySum, relevanceSum, usefulnessSum := 0.0, 0.0, 0.0
	for _, result := range report.Results {
		report.TotalRefs += len(result.References)
		report.CoreRefs += result.CoreRefs
		report.FixedRefs += result.FixedRefs
		report.SuggestionRefs += result.SuggestionRefs
		report.SkippedRefs += result.SkippedRefs
		report.CoveredRefs += result.CoveredRefs
		report.PartialRefs += result.PartialRefs
		report.MissedRefs += result.MissedRefs
		report.SuggestionCovered += result.SuggestionCovered
		report.SuggestionMissed += result.SuggestionMissed
		report.FixedHits += result.FixedHits
		report.TotalFindings += len(result.Findings)
		report.MatchedFindings += result.Matched
		report.NovelFindings += result.Novel
		report.FalsePositiveFindings += result.FalsePositives
		report.NitpickFindings += result.Nitpicks
		report.UnjudgedFindings += result.Unjudged
		report.ValidLines += result.ValidLines
		report.InvalidLines += result.InvalidLines
		report.TraceEvents += result.TraceEvents
		report.CompleteTrace += result.CompleteTrace
		if result.CanaryLeaked {
			report.RedactionCanaryHit = true
		}
		for _, pair := range result.PairScores {
			report.PairCount++
			accuracySum += float64(pair.Accuracy)
			relevanceSum += float64(pair.Relevance)
			usefulnessSum += float64(pair.Usefulness)
		}
		if result.Usage.Forwarded > 0 {
			report.ForwardedRequests += result.Usage.Forwarded
			report.RejectedRequests += result.Usage.Rejected
			report.ReportedTokens += result.Usage.Tokens
			report.ResponsesWithoutUsage += result.Usage.Unknown
		}
		// Per-scenario and per-dimension aggregation.
		scenario := result.Scenario
		if report.ScenarioMetrics[scenario] == nil {
			report.ScenarioMetrics[scenario] = &scenarioMetric{}
		}
		metric := report.ScenarioMetrics[scenario]
		metric.Cases++
		metric.Refs += len(result.References)
		metric.CoveredWeight += result.CoverageScore
		metric.Findings += len(result.Findings)
		metric.Matched += result.Matched
		metric.Novel += result.Novel
		metric.FalsePos += result.FalsePositives
		for index, ref := range result.References {
			if refChannel(ref) != refStatusValid {
				continue
			}
			if report.DimensionCoverage[ref.Dimension] == nil {
				report.DimensionCoverage[ref.Dimension] = &dimCoverage{}
			}
			coverage := report.DimensionCoverage[ref.Dimension]
			coverage.Total++
			switch {
			case index < len(result.RefVerdicts) && result.RefVerdicts[index].Verdict == "covered":
				coverage.Covered += 1.0
			case index < len(result.RefVerdicts) && result.RefVerdicts[index].Verdict == "partial":
				coverage.Covered += 0.5
			}
		}
	}
	report.Recall = ratioFloat(float64(report.CoveredRefs)+0.5*float64(report.PartialRefs), float64(report.CoreRefs))
	report.SuggestionCoverage = ratioFloat(report.SuggestionCovered, float64(report.SuggestionRefs))
	// Precision excludes unjudged findings: a failed judge must not deflate it.
	report.Precision = ratio(report.MatchedFindings+report.NovelFindings, report.TotalFindings-report.UnjudgedFindings)
	report.ValidLineRate = ratio(report.ValidLines, report.ValidLines+report.InvalidLines)
	report.TraceCompleteness = ratio(report.CompleteTrace, report.TraceEvents)
	if report.PairCount > 0 {
		report.MeanAccuracy = accuracySum / float64(report.PairCount)
		report.MeanRelevance = relevanceSum / float64(report.PairCount)
		report.MeanUsefulness = usefulnessSum / float64(report.PairCount)
	}
	for _, metric := range report.ScenarioMetrics {
		metric.Recall = ratioFloat(metric.CoveredWeight, float64(metric.Refs))
		metric.Precision = ratio(metric.Matched+metric.Novel, metric.Findings)
	}
	for _, coverage := range report.DimensionCoverage {
		coverage.Recall = ratioFloat(coverage.Covered, float64(coverage.Total))
	}
	report.FinishedAt = time.Now().UTC()
}

// collectModelReplies extracts non-empty model replies from the trace for audit.
func collectModelReplies(events []model.TraceEvent) []string {
	var replies []string
	for _, event := range events {
		if strings.TrimSpace(event.ModelReply) != "" {
			replies = append(replies, clip(event.ModelReply, 2000))
		}
	}
	return replies
}

func redactConfiguredKeyReal(report *realRunReport, apiKey string) {
	if report == nil || apiKey == "" {
		return
	}
	for index := range report.Results {
		result := &report.Results[index]
		result.Error = strings.ReplaceAll(result.Error, apiKey, "[REDACTED]")
		result.JudgeRaw = strings.ReplaceAll(result.JudgeRaw, apiKey, "[REDACTED]")
		result.JudgeError = strings.ReplaceAll(result.JudgeError, apiKey, "[REDACTED]")
		for findingIndex := range result.Findings {
			result.Findings[findingIndex].Body = strings.ReplaceAll(result.Findings[findingIndex].Body, apiKey, "[REDACTED]")
		}
	}
}

func ratioFloat(numerator, denominator float64) float64 {
	if denominator == 0 {
		return 0
	}
	return numerator / denominator
}

func writeRealReport(report realRunReport) error {
	if err := os.MkdirAll("benchmarks/results", 0o755); err != nil {
		return err
	}
	name := "run-" + report.StartedAt.Format("20060102T150405Z") + "-realpr"
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join("benchmarks/results", name+".json"), encoded, 0o600); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join("benchmarks/results", name+".md"), []byte(renderRealMarkdown(report)), 0o600)
}

func renderRealMarkdown(report realRunReport) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "# CR-Agent Real-PR Code Review Benchmark\n\n")
	fmt.Fprintf(&builder, "- Manifest: `%s`\n- Started: %s\n- Model: %s (judge: %s)\n- Cases completed: %d/%d\n- Wall time: %d ms\n- Forwarded model requests: %d (cap %d)\n- Model-reported tokens: %s\n\n",
		report.Manifest, report.StartedAt.Format(time.RFC3339), report.Model, report.JudgeModel,
		report.CasesCompleted, report.CasesRequested,
		report.FinishedAt.Sub(report.StartedAt).Milliseconds(),
		report.ForwardedRequests, report.MaxForwardedCalls,
		tokenSummary(proxyUsage{Tokens: report.ReportedTokens, Unknown: report.ResponsesWithoutUsage}))
	fmt.Fprintf(&builder, "| Reference coverage | Finding precision | Valid line rate | Trace completeness | Accuracy | Relevance | Usefulness |\n")
	fmt.Fprintf(&builder, "|---:|---:|---:|---:|---:|---:|---:|\n")
	fmt.Fprintf(&builder, "| %.1f%% | %.1f%% | %.1f%% | %.1f%% | %.2f | %.2f | %.2f |\n\n",
		100*report.Recall, 100*report.Precision, 100*report.ValidLineRate, 100*report.TraceCompleteness,
		report.MeanAccuracy, report.MeanRelevance, report.MeanUsefulness)
	fmt.Fprintf(&builder, "Refs: %d total — core %d (%d covered, %d partial, %d missed), suggestion %d (coverage %.1f%%), fixed_in_snapshot %d (re-flagged %d), skipped %d. Findings: %d total (%d matched, %d valid-new, %d false-positive, %d nitpick, %d unjudged). Canary leaked: **%t**.\n\n",
		report.TotalRefs,
		report.CoreRefs, report.CoveredRefs, report.PartialRefs, report.MissedRefs,
		report.SuggestionRefs, 100*report.SuggestionCoverage,
		report.FixedRefs, report.FixedHits,
		report.SkippedRefs,
		report.TotalFindings, report.MatchedFindings, report.NovelFindings, report.FalsePositiveFindings, report.NitpickFindings, report.UnjudgedFindings,
		report.RedactionCanaryHit)
	if len(report.ScenarioMetrics) > 0 {
		scenarios := make([]string, 0, len(report.ScenarioMetrics))
		for name := range report.ScenarioMetrics {
			scenarios = append(scenarios, name)
		}
		sort.Strings(scenarios)
		fmt.Fprintf(&builder, "## Per scenario\n\n| Scenario | Cases | Refs | Coverage | Findings | Precision |\n|---|---:|---:|---:|---:|---:|\n")
		for _, name := range scenarios {
			metric := report.ScenarioMetrics[name]
			fmt.Fprintf(&builder, "| %s | %d | %d | %.1f%% | %d | %.1f%% |\n",
				name, metric.Cases, metric.Refs, 100*metric.Recall, metric.Findings, 100*metric.Precision)
		}
		builder.WriteString("\n")
	}
	if len(report.DimensionCoverage) > 0 {
		dimensions := make([]string, 0, len(report.DimensionCoverage))
		for name := range report.DimensionCoverage {
			dimensions = append(dimensions, name)
		}
		sort.Strings(dimensions)
		fmt.Fprintf(&builder, "## Per reference dimension\n\n| Dimension | Refs | Coverage |\n|---|---:|---:|\n")
		for _, name := range dimensions {
			coverage := report.DimensionCoverage[name]
			fmt.Fprintf(&builder, "| %s | %d | %.1f%% |\n", name, coverage.Total, 100*coverage.Recall)
		}
		builder.WriteString("\n")
	}
	fmt.Fprintf(&builder, "## Per case\n\n| Case | Scenario | Status | Core cov/part/miss | Sugg cov | Fixed reflag | Findings m/n/fp | Line ok | Judge err |\n")
	fmt.Fprintf(&builder, "|---|---|---|---|---|---|---|---|---|\n")
	for _, result := range report.Results {
		fmt.Fprintf(&builder, "| `%s` | %s | %s | %d/%d/%d | %.1f/%d | %d/%d | %d/%d/%d | %d/%d | %s |\n",
			result.ID, result.Scenario, result.Status,
			result.CoveredRefs, result.PartialRefs, result.MissedRefs,
			result.SuggestionCovered, result.SuggestionMissed,
			result.FixedHits, result.FixedRefs,
			result.Matched, result.Novel, result.FalsePositives,
			result.ValidLines, result.InvalidLines,
			result.JudgeError)
	}
	builder.WriteString("\nCore reference coverage counts partial matches as 0.5 over audited \"valid\" references only; \"fixed_in_snapshot\" references are paired negatives whose re-flags count as false positives; suggestion references are scored on a separate channel. Finding precision counts matched findings plus judge-confirmed novel issues as true positives. Quality scores are 1-5 from the LLM judge over matched pairs. Judge verdicts are recorded per case for manual audit.\n")
	return builder.String()
}

func printRealSummary(report realRunReport) {
	fmt.Printf("CR-Agent Real-PR Benchmark %s manifest=%s\n", report.Version, report.Manifest)
	fmt.Printf("cases: %d/%d completed\n", report.CasesCompleted, report.CasesRequested)
	fmt.Printf("reference_coverage=%.1f%% (core %d/%d refs; suggestion %.1f%% of %d; fixed_in_snapshot %d with %d re-flags) finding_precision=%.1f%% (%d/%d judged findings, %d unjudged)\n",
		100*report.Recall, report.CoveredRefs, report.CoreRefs, 100*report.SuggestionCoverage, report.SuggestionRefs, report.FixedRefs, report.FixedHits,
		100*report.Precision, report.MatchedFindings+report.NovelFindings, report.TotalFindings-report.UnjudgedFindings, report.UnjudgedFindings)
	fmt.Printf("valid_line=%.1f%% trace=%.1f%% accuracy=%.2f relevance=%.2f usefulness=%.2f canary_leaked=%t\n",
		100*report.ValidLineRate, 100*report.TraceCompleteness, report.MeanAccuracy, report.MeanRelevance, report.MeanUsefulness, report.RedactionCanaryHit)
	fmt.Printf("model_requests=%d/%d rejected=%d reported_tokens=%s\n", report.ForwardedRequests, report.MaxForwardedCalls, report.RejectedRequests, tokenSummary(proxyUsage{Tokens: report.ReportedTokens, Unknown: report.ResponsesWithoutUsage}))
	for _, result := range report.Results {
		fmt.Printf("case=%s scenario=%s status=%s refs=%d/%d/%d findings=%d/%d/%d duration_ms=%d\n",
			result.ID, result.Scenario, result.Status, result.CoveredRefs, result.PartialRefs, result.MissedRefs,
			result.Matched, result.Novel, result.FalsePositives, result.DurationMs)
	}
	fmt.Printf("artifacts: benchmarks/results/run-%s-realpr.{json,md}\n", report.StartedAt.Format("20060102T150405Z"))
}
