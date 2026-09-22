package logic

import (
	"context"
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

func RunReviewSubagents(ctx context.Context, cfg Config, diff string, promptContext ReviewPromptContext) []SubagentResult {
	agents := ReviewSpecialists()
	results := make([]SubagentResult, len(agents))
	var wg sync.WaitGroup
	for i, agent := range agents {
		wg.Add(1)
		go func(index int, a ReviewSubagent) {
			defer wg.Done()
			results[index] = RunReviewSpecialist(ctx, cfg, a, diff, promptContext)
		}(i, agent)
	}
	wg.Wait()
	return results
}

func ReviewSpecialists() []ReviewSubagent {
	return []ReviewSubagent{
		{Name: "correctness", Focus: "只审查逻辑正确性、边界条件、错误处理和回归风险"},
		{Name: "security", Focus: "只审查密钥、认证、注入、SSRF、权限和敏感数据风险"},
		{Name: "dependency", Focus: "只审查依赖变更、兼容性、可维护性和测试缺口"},
	}
}

func RunReviewSpecialist(ctx context.Context, cfg Config, agent ReviewSubagent, diff string, promptContext ReviewPromptContext) SubagentResult {
	started := time.Now()
	prompt := BuildReviewSubagentPrompt(agent.Focus, promptContext, redact(diff))
	summary, err := EinoReviewAgent(ctx, cfg, prompt)
	return SubagentResult{Name: agent.Name, Summary: summary, Error: err, DurationMs: time.Since(started).Milliseconds()}
}
