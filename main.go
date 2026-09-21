package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type ReviewRequest struct {
	Source      string `json:"source"`
	Diff        string `json:"diff"`
	BudgetCents int    `json:"budget_cents"`
}
type ReviewComment struct {
	File       string `json:"file"`
	Line       int    `json:"line"`
	Severity   string `json:"severity"`
	Confidence string `json:"confidence"`
	Body       string `json:"body"`
	TraceID    string `json:"trace_id"`
}
type ReviewJob struct {
	ID         string          `json:"id"`
	Status     string          `json:"status"`
	Source     string          `json:"source"`
	Comments   []ReviewComment `json:"comments"`
	Trace      []TraceEvent    `json:"trace"`
	SpentCents int             `json:"spent_cents"`
	UpdatedAt  time.Time       `json:"updated_at"`
	Error      string          `json:"error,omitempty"`
}
type TraceEvent struct {
	ID         string    `json:"id"`
	Tool       string    `json:"tool"`
	Input      string    `json:"input"`
	Output     string    `json:"output"`
	Prompt     string    `json:"prompt,omitempty"`
	ModelReply string    `json:"model_reply,omitempty"`
	At         time.Time `json:"at"`
}

type Tool func(context.Context, string) (string, error)
type ToolRegistry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

func (r *ToolRegistry) Register(name string, t Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[name] = t
}
func (r *ToolRegistry) Run(ctx context.Context, name, input string) (string, error) {
	r.mu.RLock()
	t := r.tools[name]
	r.mu.RUnlock()
	if t == nil {
		return "", os.ErrNotExist
	}
	return t(ctx, input)
}

var registry = ToolRegistry{tools: map[string]Tool{}}
var jobs = struct {
	sync.RWMutex
	m map[string]*ReviewJob
}{m: map[string]*ReviewJob{}}

func saveJob(j *ReviewJob) {
	jobs.Lock()
	jobs.m[j.ID] = j
	jobs.Unlock()
	_ = os.MkdirAll(".checkpoints", 0755)
	_ = os.WriteFile(filepath.Join(".checkpoints", j.ID+".json"), []byte(jsonString(j)), 0600)
}
func newID(s string) string {
	h := sha256.Sum256([]byte(s + time.Now().String()))
	return hex.EncodeToString(h[:])[:16]
}
func runReview(ctx context.Context, j *ReviewJob, req ReviewRequest, cfg Config) {
	j.Status = "running"
	saveJob(j)
	input := redact(req.Diff)
	tid := newID(input)
	ev := TraceEvent{ID: tid, Tool: "diff_reader", Input: input, At: time.Now()}
	ev.Output = "已读取并完成敏感字段脱敏"
	j.Trace = append(j.Trace, ev)
	saveJob(j)
	if strings.TrimSpace(input) == "" {
		j.Status = "failed"
		j.Error = "diff 不能为空，链接抓取需要配置对应平台凭证"
		saveJob(j)
		return
	}
	// 安全默认：仅分析 diff，不执行仓库代码；工具通过 registry 显式注册。
	out, _ := registry.Run(ctx, "static-check", input)
	trace := TraceEvent{ID: newID(out), Tool: "static-check", Input: input, Output: out, At: time.Now()}
	j.Trace = append(j.Trace, trace)
	j.SpentCents = 1
	if strings.Contains(input, "TODO") || strings.Contains(input, "panic(") {
		j.Comments = []ReviewComment{{File: "diff", Line: 1, Severity: "warning", Confidence: "high", Body: "检测到可能需要处理的 TODO 或 panic，请在合并前确认异常路径。", TraceID: trace.ID}}
	} else {
		j.Comments = []ReviewComment{{File: "diff", Line: 1, Severity: "info", Confidence: "reference", Body: "未发现内置静态检查规则命中，建议结合业务语义复核。", TraceID: trace.ID}}
	}
	j.Status = "completed"
	j.UpdatedAt = time.Now()
	saveJob(j)
}
func main() {
	cfg := loadConfig()
	registry.Register("static-check", func(_ context.Context, s string) (string, error) {
		if strings.Contains(s, "TODO") || strings.Contains(s, "panic(") {
			return "命中 TODO/panic 规则", nil
		}
		return "规则检查通过", nil
	})
	r := gin.Default()
	r.StaticFile("/", "web/index.html")
	r.GET("/api/health", func(c *gin.Context) { c.JSON(200, gin.H{"status": "ok"}) })
	r.POST("/api/reviews", func(c *gin.Context) {
		var req ReviewRequest
		if c.ShouldBindJSON(&req) != nil || (strings.TrimSpace(req.Source) == "" && strings.TrimSpace(req.Diff) == "") {
			c.JSON(400, gin.H{"error": "source 或 diff 至少填写一项"})
			return
		}
		if req.BudgetCents <= 0 {
			req.BudgetCents = cfg.BudgetCents
		}
		j := &ReviewJob{ID: newID(req.Source + req.Diff), Status: "queued", Source: req.Source, UpdatedAt: time.Now()}
		saveJob(j)
		go runReview(context.Background(), j, req, cfg)
		c.JSON(202, j)
	})
	r.GET("/api/reviews/:id", func(c *gin.Context) {
		jobs.RLock()
		j := jobs.m[c.Param("id")]
		jobs.RUnlock()
		if j == nil {
			c.JSON(404, gin.H{"error": "review 不存在"})
			return
		}
		c.JSON(200, j)
	})
	r.Run(":" + cfg.Port)
}
