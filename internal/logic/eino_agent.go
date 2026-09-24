package logic

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
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
		APIKey: cfg.DeepSeekAPIKey, Model: reviewModelName(cfg),
		BaseURL: strings.TrimRight(cfg.DeepSeekBaseURL, "/") + "/v1",
	})
	if err != nil {
		return "", err
	}
	harness := newReviewHarness()
	harness.Compactor = NewContextCompactor(cfg)
	outputBudget := cfg.ModelMaxOutputTokens
	if outputBudget <= 0 {
		outputBudget = defaultModelMaxOutputTokens
	}
	if isReviewPrompt(ctx) {
		reviewBudget := cfg.ReviewMaxOutputTokens
		if reviewBudget <= 0 {
			reviewBudget = defaultReviewMaxOutputTokens
		}
		if outputBudget > reviewBudget {
			outputBudget = reviewBudget
		}
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
					return observedBudgetedModelRequest(ctx, "goal_evaluator", modelRound, attempt, budget, estimateModelInputTokens(input, nil), func(allowedTokens int) (*schema.Message, error) {
						return chat.Generate(ctx, input, modeloptions.WithMaxTokens(allowedTokens))
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
				return observedBudgetedModelRequest(ctx, "deepseek_model", modelRound, attempt, budget, estimateModelInputTokens(input, tools), func(allowedTokens int) (*schema.Message, error) {
					return bound.Generate(ctx, input, modelRequestOptions(ctx, allowedTokens)...)
				})
			})
		})
	}
	return harness.Run(ctx, prompt)
}

func modelRequestOptions(ctx context.Context, maxTokens int) []modeloptions.Option {
	options := []modeloptions.Option{modeloptions.WithMaxTokens(maxTokens)}
	if isReviewPrompt(ctx) {
		options = append(options, openai.WithExtraFields(map[string]any{
			"thinking":         map[string]string{"type": "enabled"},
			"reasoning_effort": "high",
		}))
	}
	return options
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
	return observedBudgetedModelRequest(ctx, name, round, retryCount, budget, inputFramingTokenReserve, func(int) (*schema.Message, error) {
		return generate()
	})
}

func observedBudgetedModelRequest(
	ctx context.Context,
	name string,
	round int,
	retryCount int,
	requestedOutputTokens int,
	estimatedInputTokens int,
	generate func(int) (*schema.Message, error),
) (*schema.Message, error) {
	meter := reviewBudgetFrom(ctx)
	reservation, reserveErr := meter.reserve(estimatedInputTokens, requestedOutputTokens)
	outputTokenLimit := requestedOutputTokens
	if reserveErr == nil && meter != nil {
		outputTokenLimit = reservation.outputTokens
	}
	var span *TraceSpan
	if recorder := traceRecorderFrom(ctx); recorder != nil {
		span = recorder.Start(
			"model_request",
			name,
			"inference",
			fmt.Sprintf("requested_max_tokens=%d allowed_max_tokens=%d estimated_input_tokens=%d", requestedOutputTokens, outputTokenLimit, estimatedInputTokens),
			traceParentFrom(ctx),
		)
	}
	if reserveErr != nil {
		endModelRequestTrace(span, TraceResult{
			Status: "budget_exhausted", Output: "审查预算不足，已阻止模型请求", Round: round,
			RetryCount: retryCount, Origin: "model",
		})
		_ = meter.persist()
		return nil, reserveErr
	}
	reply, err := generate(outputTokenLimit)
	result := TraceResult{
		Status:     "succeeded",
		Output:     "模型请求完成",
		Origin:     "model",
		Round:      round,
		RetryCount: retryCount,
	}
	if err != nil || reply == nil {
		result.Status = "failed"
		result.Output = "模型请求失败，按预留额度计入预算"
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
	modelName, inputPrice, outputPrice := meter.tracePricing()
	result.Model = modelName
	result.InputPriceYuanPerMillion = inputPrice
	result.OutputPriceYuanPerMillion = outputPrice
	if meter != nil {
		usageKnown := reply != nil && reply.ResponseMeta != nil && reply.ResponseMeta.Usage != nil
		result.CostMicros = meter.settle(reservation, result.InputTokens, result.OutputTokens, usageKnown)
		result.EstimatedCost = !usageKnown
	}
	endModelRequestTrace(span, result)
	if err := meter.persist(); err != nil {
		return nil, fmt.Errorf("模型预算记录失败，已停止后续调用")
	}
	if err != nil {
		return nil, err
	}
	if reply == nil {
		return nil, fmt.Errorf("模型返回空响应")
	}
	if meter != nil && result.CostMicros > 0 && meter.isOverLimit() {
		return nil, fmt.Errorf("审查预算已达到上限，已停止后续模型调用")
	}
	return reply, nil
}

func endModelRequestTrace(span *TraceSpan, result TraceResult) {
	if span != nil {
		span.End(result)
	}
}

func estimateModelInputTokens(messages []*schema.Message, tools []*schema.ToolInfo) int {
	// Treat each input byte as a token and add protocol/tool framing overhead.
	// This intentionally overestimates common text/code requests before reserving cost.
	bytes := int64(inputFramingTokenReserve + len(messages)*64 + len(tools)*128)
	for _, message := range messages {
		if message == nil {
			continue
		}
		bytes += int64(len(message.Role) + len(message.Content) + len(message.ReasoningContent) + 64)
		for _, call := range message.ToolCalls {
			bytes += int64(len(call.ID) + len(call.Type) + len(call.Function.Name) + len(call.Function.Arguments) + 48)
		}
	}
	for _, tool := range tools {
		if tool == nil {
			continue
		}
		bytes += int64(len(tool.Name) + len(tool.Desc) + 128)
		if tool.ParamsOneOf != nil {
			encoded, err := json.Marshal(tool.ParamsOneOf)
			if err == nil {
				bytes += int64(len(encoded))
			}
		}
	}
	if bytes > int64(math.MaxInt) {
		return math.MaxInt
	}
	return int(bytes)
}

func reviewModelName(cfg Config) string {
	if strings.TrimSpace(cfg.DeepSeekModel) == "" {
		return "deepseek-v4-pro"
	}
	return cfg.DeepSeekModel
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
