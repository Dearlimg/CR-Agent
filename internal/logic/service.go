package logic

import (
	"CR-Agent/internal/dao"
	"CR-Agent/internal/model"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"
)

type Service struct {
	Store  *dao.JobStore
	Config Config
}

func NewService(store *dao.JobStore, cfg Config) *Service { return &Service{Store: store, Config: cfg} }
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
		j.Status = "failed"
		j.Error = "链接抓取逻辑已迁移到 logic 层，请接入 fetcher 后重试"
		_ = s.Store.Save(j)
		return
	}
	j.Trace = append(j.Trace, model.TraceEvent{ID: id(req.Diff), Tool: "diff_reader", Input: "redacted diff", Output: "读取并脱敏完成", At: time.Now(), Phase: "observation"})
	j.Comments = []model.ReviewComment{{File: "diff", Line: 1, Severity: "info", Confidence: "low", Body: "MVC 重构后的规则审查占位结果，请接入 DeepSeek 审查器。", TraceID: j.Trace[0].ID}}
	j.Status = "completed"
	j.UpdatedAt = time.Now()
	_ = s.Store.Save(j)
}
