package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"CR-Agent/internal/logic"
	"CR-Agent/internal/model"
)

// remoteJob mirrors the GET /api/reviews/:id response of a deployed service.
type remoteJob struct {
	ID       string                `json:"id"`
	Status   string                `json:"status"`
	Error    string                `json:"error"`
	Source   string                `json:"source"`
	Comments []model.ReviewComment `json:"comments"`
	Trace    []model.TraceEvent    `json:"trace"`
}

// runRealFromRemote evaluates saved job results fetched from a deployed
// service: it scores them against the manifest references with the LLM judge
// and writes the standard real-PR report.
func runRealFromRemote(remoteDir string, judgeMaxTokens int) error {
	cases, err := loadRealCases("benchmarks/real_pr_v1.json")
	if err != nil {
		return err
	}
	cfg, err := logic.LoadConfig()
	if err != nil {
		return err
	}
	if strings.TrimSpace(cfg.DeepSeekAPIKey) == "" {
		return errors.New("未配置 DEEPSEEK_API_KEY；裁判需要调用模型")
	}
	modelName := cfg.DeepSeekModel
	if strings.TrimSpace(modelName) == "" {
		modelName = "deepseek-flash"
	}
	judge := newJudgeClient(strings.TrimRight(cfg.DeepSeekBaseURL, "/"), cfg.DeepSeekAPIKey, modelName, judgeMaxTokens)
	baseDir := filepath.Dir("benchmarks/real_pr_v1.json")

	report := realRunReport{
		Version:           realReportVersion,
		Manifest:          "benchmarks/real_pr_v1.json",
		Route:             "real-pr-remote",
		Model:             modelName + " (deployed service)",
		JudgeModel:        modelName,
		CasesRequested:    len(cases),
		Results:           []realCaseResult{},
		DimensionCoverage: map[string]*dimCoverage{},
		ScenarioMetrics:   map[string]*scenarioMetric{},
	}
	report.StartedAt = time.Now().UTC()

	for _, sample := range cases {
		diffContent, err := os.ReadFile(filepath.Join(baseDir, filepath.FromSlash(sample.DiffFile)))
		if err != nil {
			return fmt.Errorf("样本 %s 的 diff 快照读取失败: %w", sample.ID, err)
		}
		result := realCaseResult{
			ID: sample.ID, Scenario: sample.Scenario, Language: sample.Language,
			PrURL: sample.PrURL, Title: sample.Title, Status: "failed",
			References: sample.ReferenceComments,
			Findings:   []model.ReviewComment{},
			MissedRefs: len(sample.ReferenceComments),
		}
		jobPath := filepath.Join(remoteDir, sample.ID+".json")
		jobBody, err := os.ReadFile(jobPath)
		if err != nil {
			result.Error = "缺少线上 job 结果: " + err.Error()
			report.Results = append(report.Results, result)
			continue
		}
		var job remoteJob
		jobBody = bytes.TrimPrefix(jobBody, []byte{0xEF, 0xBB, 0xBF})
		if err := json.Unmarshal(jobBody, &job); err != nil {
			result.Error = "线上 job 结果解析失败: " + err.Error()
			report.Results = append(report.Results, result)
			continue
		}
		result.Status = job.Status
		result.Findings = actionableFindings(job.Comments)
		result.TraceEvents = len(job.Trace)
		result.CompleteTrace = completeTraceCount(job.Trace)
		result.ModelJSONValid = modelReplyValid(job.Trace)
		result.ModelReplies = collectModelReplies(job.Trace)
		result.ValidLines, result.InvalidLines = countValidLines(string(diffContent), result.Findings)
		if job.Error != "" && job.Status != "completed" && job.Status != "completed_with_warnings" {
			result.Error = job.Error
		}
		output := runJudge(judge, sample, string(diffContent), result.Findings)
		result.RefVerdicts = output.ReferenceVerdicts
		result.FindingVerdict = output.FindingVerdicts
		result.PairScores = output.PairScores
		result.JudgeRaw = clip(output.Raw, 8000)
		result.JudgeError = output.ParseError
		applyJudgeVerdicts(&result)
		report.Results = append(report.Results, result)
		if result.Status == "completed" || result.Status == "completed_with_warnings" {
			report.CasesCompleted++
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
