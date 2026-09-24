package logic

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino-ext/components/model/openai"
	modeloptions "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// EinoReviewAgent binds Eino inference to the host-owned model/tool loop.
// Service-provided setup supplies job-scoped tools and trace recording.
func EinoReviewAgent(ctx context.Context, cfg Config, prompt string) (string, error) {
	if strings.TrimSpace(cfg.DeepSeekAPIKey) == "" {
		return "", fmt.Errorf("DEEPSEEK_API_KEY 未配置")
	}
	chat, err := openai.NewChatModel(ctx, &openai.ChatModelConfig{
		APIKey: cfg.DeepSeekAPIKey, Model: "deepseek-chat",
		BaseURL: strings.TrimRight(cfg.DeepSeekBaseURL, "/") + "/v1",
	})
	if err != nil {
		return "", err
	}
	harness := newReviewHarness()
	harness.Compactor = NewContextCompactor(cfg)
	outputBudget := cfg.ModelMaxOutputTokens
	if outputBudget <= 0 {
		outputBudget = 8192
	}
	round := 0
	nextRound := func() int {
		round++
		return round
	}
	if condition, ok := ctx.Value(goalConditionKey{}).(string); ok && strings.TrimSpace(condition) != "" {
		controller, err := NewGoalController(condition, PromptGoalEvaluator{Generate: func(ctx context.Context, prompt string) (string, error) {
			messages := []*schema.Message{{Role: schema.User, Content: prompt}}
			modelRound := nextRound()
			retryCount := 0
			reply, err := generateWithinLengthBudget(messages, 1024, func(input []*schema.Message, budget int) (*schema.Message, error) {
				return retryHarnessInference(ctx, cfg, func() (*schema.Message, error) {
					attempt := retryCount
					retryCount++
					return observedModelRequest(ctx, "goal_evaluator", modelRound, attempt, budget, func() (*schema.Message, error) {
						return chat.Generate(ctx, input, modeloptions.WithMaxTokens(budget))
					})
				})
			})
			if err != nil {
				return "", err
			}
			return reply.Content, nil
		}}, cfg.GoalMaxBlocks)
		if err != nil {
			return "", err
		}
		harness.Goal = controller
	}
	if setup, ok := ctx.Value(harnessSetupKey{}).(func(*ReviewHarness)); ok {
		setup(harness)
	} else {
		// Summaries and memory extraction do not need side-effecting tools.
		harness.tools = map[string]harnessTool{}
	}
	harness.Model = func(ctx context.Context, messages []*schema.Message, tools []*schema.ToolInfo) (*schema.Message, error) {
		var bound modeloptions.ToolCallingChatModel = chat
		if len(tools) > 0 {
			var err error
			bound, err = chat.WithTools(tools)
			if err != nil {
				return nil, err
			}
		}
		modelRound := nextRound()
		retryCount := 0
		return generateWithinLengthBudget(messages, outputBudget, func(input []*schema.Message, budget int) (*schema.Message, error) {
			return retryHarnessInference(ctx, cfg, func() (*schema.Message, error) {
				attempt := retryCount
				retryCount++
				return observedModelRequest(ctx, "deepseek_chat", modelRound, attempt, budget, func() (*schema.Message, error) {
					return bound.Generate(ctx, input, modeloptions.WithMaxTokens(budget))
				})
			})
		})
	}
	return harness.Run(ctx, prompt)
}

// A length-truncated tool call is never sent to the harness for execution.
// A text-only truncation gets one concise re-answer within the same token budget.
func generateWithinLengthBudget(
	messages []*schema.Message,
	budget int,
	generate func([]*schema.Message, int) (*schema.Message, error),
) (*schema.Message, error) {
	reply, err := generate(messages, budget)
	if err != nil {
		return nil, err
	}
	if reply == nil {
		return nil, fmt.Errorf("模型返回空响应")
	}
	if reply.ResponseMeta == nil || reply.ResponseMeta.FinishReason != "length" {
		return reply, nil
	}
	if len(reply.ToolCalls) > 0 {
		return nil, fmt.Errorf("模型输出达到 token 上限，拒绝执行不完整工具调用")
	}

	concise := &schema.Message{
		Role:    schema.User,
		Content: "上一轮输出被长度限制截断。请重新给出完整且精简的回答，只保留任务要求的结果；如需工具，仅调用必要的工具。",
	}
	input := append(append([]*schema.Message{}, messages...), concise)
	reply, err = generate(input, budget)
	if err != nil {
		return nil, err
	}
	if reply == nil {
		return nil, fmt.Errorf("模型返回空响应")
	}
	if reply.ResponseMeta != nil && reply.ResponseMeta.FinishReason == "length" {
		return nil, fmt.Errorf("模型输出达到 token 上限，精简重答仍被截断")
	}
	return reply, nil
}

// A request span contains provider metadata only; prompts, replies and raw
// provider errors must not become durable trace data.
func observedModelRequest(
	ctx context.Context,
	name string,
	round int,
	retryCount int,
	budget int,
	generate func() (*schema.Message, error),
) (*schema.Message, error) {
	var span *TraceSpan
	if recorder := traceRecorderFrom(ctx); recorder != nil {
		span = recorder.Start(
			"model_request",
			name,
			"inference",
			fmt.Sprintf("max_tokens=%d", budget),
			traceParentFrom(ctx),
		)
	}
	reply, err := generate()
	if span == nil {
		return reply, err
	}
	result := TraceResult{
		Status:     "succeeded",
		Output:     "模型请求完成",
		Origin:     "model",
		Round:      round,
		RetryCount: retryCount,
	}
	if err != nil || reply == nil {
		result.Status = "failed"
		result.Output = "模型请求失败"
	} else if reply.ResponseMeta != nil {
		result.FinishReason = reply.ResponseMeta.FinishReason
		if usage := reply.ResponseMeta.Usage; usage != nil {
			result.InputTokens = usage.PromptTokens
			result.OutputTokens = usage.CompletionTokens
		}
		if result.FinishReason == "length" {
			result.Status = "truncated"
			result.Output = "模型输出达到 token 上限"
		}
	}
	span.End(result)
	return reply, err
}

func retryHarnessInference(ctx context.Context, cfg Config, generate func() (*schema.Message, error)) (*schema.Message, error) {
	maxRetries := cfg.ModelMaxRetries
	if maxRetries < 0 {
		maxRetries = 0
	}
	if maxRetries > 5 {
		maxRetries = 5
	}
	baseDelay := time.Duration(cfg.ModelRetryBaseMs) * time.Millisecond
	if baseDelay <= 0 {
		baseDelay = 500 * time.Millisecond
	}
	var lastErr error
	retries := 0
	for attempt := 0; attempt <= maxRetries; attempt++ {
		result, err := generate()
		if err == nil {
			return result, nil
		}
		lastErr = err
		if attempt == maxRetries || !isRetryableModelError(err) {
			break
		}
		retries++
		delay := baseDelay * time.Duration(1<<attempt)
		if delay > 8*time.Second {
			delay = 8 * time.Second
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	return nil, fmt.Errorf("模型调用失败（已重试 %d 次）: %w", retries, lastErr)
}

func isRetryableModelError(err error) bool {
	message := strings.ToLower(err.Error())
	for _, marker := range []string{
		"eof",
		"connection reset",
		"connection closed",
		"connection refused",
		"reset by peer",
		"broken pipe",
		"timeout",
		"temporarily unavailable",
		"http 429",
		"status code: 429",
		"http 500",
		"status code: 500",
		"http 502",
		"status code: 502",
		"http 503",
		"status code: 503",
		"http 504",
		"status code: 504",
		"http 529",
		"status code: 529",
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}
