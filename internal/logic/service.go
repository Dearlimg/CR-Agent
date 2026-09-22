package logic

import (
	"CR-Agent/internal/dao"
	"CR-Agent/internal/model"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

type Service struct {
	Store  dao.Store
	Config Config
	Loop   *AgentLoop
}

func NewService(store dao.Store, cfg Config) *Service {
	registry := NewToolRegistry()
	registry.Register("diff_reader", func(_ context.Context, in ToolInput) (ToolResult, error) {
		return ToolResult{Output: fmt.Sprintf("读取并脱敏完成，diff_bytes=%d", len(in.Diff))}, nil
	})
	registry.Register("static_check", func(_ context.Context, in ToolInput) (ToolResult, error) {
		hits := []string{}
		if strings.Contains(in.Diff, "TODO") {
			hits = append(hits, "TODO")
		}
		if strings.Contains(in.Diff, "panic(") {
			hits = append(hits, "panic")
		}
		return ToolResult{Output: fmt.Sprintf("静态检查完成，命中=%v", hits)}, nil
	})
	record := func(jobID, traceID, tool, status, input, output, callErr string, durationMs int64) error {
		return store.RecordToolCall(jobID, traceID, tool, status, input, output, callErr, durationMs)
	}
	return &Service{Store: store, Config: cfg, Loop: &AgentLoop{Registry: registry, Plan: []LoopStep{{Tool: "diff_reader", Reason: "读取并脱敏 diff"}, {Tool: "static_check", Reason: "执行前置静态检查"}}, MaxSteps: 3, Record: record}}
}
func id(s string) string {
	h := sha256.Sum256([]byte(s + time.Now().String()))
	return hex.EncodeToString(h[:])[:16]
}
func (s *Service) Create(req model.ReviewRequest) (*model.ReviewJob, error) {
	j := &model.ReviewJob{ID: id(req.Source + req.Diff), Status: "queued", Source: req.Source, UpdatedAt: time.Now(), Comments: []model.ReviewComment{}, Trace: []model.TraceEvent{}}
	if err := s.Store.Save(j); err != nil {
		return nil, err
	}
	go s.run(context.Background(), j, req)
	return j, nil
}
func (s *Service) run(ctx context.Context, j *model.ReviewJob, req model.ReviewRequest) {
	j.Status = "running"
	_ = s.Store.Save(j)
	if strings.TrimSpace(req.Diff) == "" {
		fetchStarted := time.Now()
		resolved, diff, err := fetchDiff(ctx, req.Source)
		fetchTraceID := id("diff_fetcher" + j.ID)
		fetchDuration := time.Since(fetchStarted).Milliseconds()
		if err != nil {
			_ = s.Store.RecordToolCall(j.ID, fetchTraceID, "diff_fetcher", "failed", req.Source, "", err.Error(), fetchDuration)
			j.Status = "failed"
			j.Error = err.Error()
			j.Trace = append(j.Trace, model.TraceEvent{ID: id(req.Source), Tool: "diff_fetcher", Input: req.Source, Output: err.Error(), At: time.Now(), Phase: "action"})
			_ = s.Store.Save(j)
			return
		}
		_ = s.Store.RecordToolCall(j.ID, fetchTraceID, "diff_fetcher", "succeeded", req.Source, fmt.Sprintf("diff_bytes=%d", len(diff)), "", fetchDuration)
		j.Source, req.Diff = resolved, diff
	}
	if err := s.Loop.Run(ctx, ToolInput{Job: j, Diff: req.Diff}); err != nil {
		j.Status = "failed"
		j.Error = err.Error()
		_ = s.Store.Save(j)
		return
	}
	readID := ""
	if len(j.Trace) > 0 {
		readID = j.Trace[len(j.Trace)-1].ID
	}
	prompt := "只基于下面 git diff 输出 JSON 数组，字段为 file,line,severity,confidence,body,suggestion；没有问题输出 []。不要编造。\n\n" + redact(req.Diff)
	modelStarted := time.Now()
	reply, err := EinoReviewAgent(ctx, s.Config, prompt)
	modelTraceID := id("deepseek-review" + j.ID)
	modelDuration := time.Since(modelStarted).Milliseconds()
	if err != nil {
		_ = s.Store.RecordToolCall(j.ID, modelTraceID, "deepseek_review", "failed", "prompt diff summary", "", err.Error(), modelDuration)
	} else {
		_ = s.Store.RecordToolCall(j.ID, modelTraceID, "deepseek_review", "succeeded", "prompt diff summary", "DeepSeek 审查完成", "", modelDuration)
	}
	if err != nil {
		j.Trace = append(j.Trace, model.TraceEvent{ID: id(err.Error()), Tool: "review-fallback", Input: "diff summary", Output: err.Error(), At: time.Now(), Phase: "reasoning"})
		j.Comments = []model.ReviewComment{{File: "diff", Line: 1, Severity: "info", Confidence: "low", Body: "DeepSeek 调用失败：" + err.Error(), TraceID: readID}}
	} else {
		traceID := modelTraceID
		j.Trace = append(j.Trace, model.TraceEvent{ID: traceID, Tool: "deepseek-review", Input: "prompt diff summary", Output: "DeepSeek 审查完成", ModelReply: reply, At: time.Now(), Phase: "reasoning"})
		j.SpentCents = 1
		findings := parseFindings(reply)
		j.Comments = []model.ReviewComment{}
		for _, f := range findings {
			body := f.Body
			if f.Suggestion != "" {
				body += "\n建议：" + f.Suggestion
			}
			j.Comments = append(j.Comments, model.ReviewComment{File: f.File, Line: f.Line, Severity: f.Severity, Confidence: f.Confidence, Body: body, TraceID: traceID})
		}
		if len(j.Comments) == 0 {
			j.Comments = []model.ReviewComment{{File: "diff", Line: 1, Severity: "info", Confidence: "high", Body: "未发现需要评论的问题。", TraceID: traceID}}
		}
	}
	j.Status = "completed"
	j.UpdatedAt = time.Now()
	_ = s.Store.Save(j)
}
