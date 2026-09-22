package logic

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// SubagentResult is deliberately compact: the parent receives the final report
// only, rather than the child model's conversation history.
type SubagentResult struct {
	Name       string
	Summary    string
	Error      error
	DurationMs int64
}

type ReviewSubagent struct {
	Name  string
	Focus string
}

func RunReviewSubagents(ctx context.Context, cfg Config, diff string) []SubagentResult {
	agents := []ReviewSubagent{
		{Name: "correctness", Focus: "只审查逻辑正确性、边界条件、错误处理和回归风险"},
		{Name: "security", Focus: "只审查密钥、认证、注入、SSRF、权限和敏感数据风险"},
		{Name: "dependency", Focus: "只审查依赖变更、兼容性、可维护性和测试缺口"},
	}
	results := make([]SubagentResult, len(agents))
	var wg sync.WaitGroup
	for i, agent := range agents {
		wg.Add(1)
		go func(index int, a ReviewSubagent) {
			defer wg.Done()
			started := time.Now()
			prompt := fmt.Sprintf(`你是 Code Review 子 Agent。%s。
只基于以下 diff 输出 JSON 数组，字段为 file,line,severity,confidence,body,suggestion；没有问题输出 []。
不要编造 diff 外的上下文，不要调用其他 Agent，也不要输出解释性文字。

%s`, a.Focus, redact(diff))
			summary, err := EinoReviewAgent(ctx, cfg, prompt)
			results[index] = SubagentResult{
				Name:       a.Name,
				Summary:    summary,
				Error:      err,
				DurationMs: time.Since(started).Milliseconds(),
			}
		}(i, agent)
	}
	wg.Wait()
	return results
}
