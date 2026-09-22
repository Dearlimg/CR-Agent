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
	return &Service{Store: store, Config: cfg, Loop: &AgentLoop{Registry: registry, Plan: []LoopStep{{Tool: "diff_reader", Reason: "读取并脱敏 diff"}}, MaxSteps: 3}}
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
		resolved, diff, err := fetchDiff(ctx, req.Source)
		if err != nil {
			j.Status = "failed"
			j.Error = err.Error()
			j.Trace = append(j.Trace, model.TraceEvent{ID: id(req.Source), Tool: "diff_fetcher", Input: req.Source, Output: err.Error(), At: time.Now(), Phase: "action"})
			_ = s.Store.Save(j)
			return
		}
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
	reply, tokens, err := reviewWithDeepSeek(ctx, s.Config, req.Diff)
	if err != nil {
		j.Trace = append(j.Trace, model.TraceEvent{ID: id(err.Error()), Tool: "review-fallback", Input: "diff summary", Output: err.Error(), At: time.Now(), Phase: "reasoning"})
		j.Comments = []model.ReviewComment{{File: "diff", Line: 1, Severity: "info", Confidence: "low", Body: "DeepSeek 调用失败：" + err.Error(), TraceID: readID}}
	} else {
		traceID := id(reply)
		j.Trace = append(j.Trace, model.TraceEvent{ID: traceID, Tool: "deepseek-review", Input: "prompt diff summary", Output: "DeepSeek 审查完成", ModelReply: reply, At: time.Now(), Phase: "reasoning"})
		j.SpentCents = tokens / 1000
		if j.SpentCents < 1 {
			j.SpentCents = 1
		}
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
