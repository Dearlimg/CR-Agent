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

const deepSeekMaxOutputTokens = 393216

// EinoReviewAgent binds Eino inference to the host-owned model/tool loop.
// Service-provided setup supplies job-scoped tools and trace recording.
func EinoReviewAgent(ctx context.Context, cfg Config, prompt string) (string, error) {
	if strings.TrimSpace(cfg.DeepSeekAPIKey) == "" {
		return "", fmt.Errorf("DEEPSEEK_API_KEY 未配置")
	}
	router, err := newReviewModelRouter(ctx, cfg)
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
		outputBudget = deepSeekMaxOutputTokens
	}
	round := 0
	nextRound := func() int {
		round++
		return round
	}
	if condition, ok := ctx.Value(goalConditionKey{}).(string); ok && strings.TrimSpace(condition) != "" {
		goalTool := goalDecisionJSONToolSpec()
		goalToolInfo, err := modelJSONToolInfo(goalTool)
		if err != nil {
			return "", err
		}
		controller, err := NewGoalController(condition, PromptGoalEvaluator{Generate: func(ctx context.Context, prompt string) (string, error) {
			messages := []*schema.Message{{Role: schema.User, Content: prompt}}
			infer := func(input []*schema.Message) (*schema.Message, error) {
				modelRound := nextRound()
				retryCount := 0
				return generateWithinLengthBudget(input, 1024, func(current []*schema.Message, budget int) (*schema.Message, error) {
					return generateReviewModelRequest(ctx, cfg, router, reviewModelRequest{
						retryCount:  &retryCount,
						traceName:   "goal_evaluator",
						round:       modelRound,
						budget:      budget,
						messages:    current,
						tools:       []*schema.ToolInfo{goalToolInfo},
						inputTokens: estimateModelInputTokens(current, []*schema.ToolInfo{goalToolInfo}),
					})
				})
			}
			resolve := func(reply *schema.Message) (string, error) {
				if reply == nil {
					return "", fmt.Errorf("goal evaluator 返回空响应")
				}
				if len(reply.ToolCalls) > 0 {
					if len(reply.ToolCalls) != 1 {
						return "", fmt.Errorf("goal evaluator 请求了多个 JSON 校验工具")
					}
					return executeModelJSONToolCall(ctx, reply.ToolCalls[0], goalTool)
				}
				var decision GoalDecision
				if err := parseGoalDecision(reply.Content, &decision); err != nil {
					return "", err
				}
				return normalizeTypedModelJSON(reply.Content, &decision)
			}
			reply, err := infer(messages)
			if err != nil {
				return "", err
			}
			normalized, resolveErr := resolve(reply)
			if resolveErr == nil {
				return normalized, nil
			}
			messages = append(append([]*schema.Message{}, messages...), modelJSONRepairMessage(reply, resolveErr, goalTool.Name))
			reply, err = infer(messages)
			if err != nil {
				return "", err
			}
			return resolve(reply)
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
		harness.tools = NewToolRegistry()
	}
	if setup, ok := ctx.Value(modelJSONToolSetupKey{}).(func(*ReviewHarness)); ok {
		setup(harness)
	}
	if harness.Record == nil {
		if recorder := traceRecorderFrom(ctx); recorder != nil {
			harness.Record = func(name, callID, status, input, output string, started, ended time.Time, _ int64) {
				recorder.RecordAt(
					"tool", name, "harness", input, traceParentFrom(ctx), started, ended,
					TraceResult{Status: status, Output: output, Origin: "model", ToolCallID: callID},
				)
			}
		}
	}
	harness.Model = func(ctx context.Context, messages []*schema.Message, tools []*schema.ToolInfo) (*schema.Message, error) {
		modelRound := nextRound()
		retryCount := 0
		return generateReviewWithinLengthBudget(messages, outputBudget, tools, func(input []*schema.Message, budget int, allowedTools []*schema.ToolInfo) (*schema.Message, error) {
			return generateReviewModelRequest(ctx, cfg, router, reviewModelRequest{
				retryCount:  &retryCount,
				traceName:   "review_model",
				round:       modelRound,
				budget:      budget,
				messages:    input,
				tools:       allowedTools,
				inputTokens: estimateModelInputTokens(input, allowedTools),
			})
		})
	}
	if envelope, ok := promptEnvelopeFrom(ctx); ok {
		return harness.RunEnvelope(withoutPromptEnvelope(ctx), envelope)
	}
	return harness.Run(ctx, prompt)
}

func modelRequestOptions(ctx context.Context, maxTokens int, useDeepSeekThinking bool) []modeloptions.Option {
	options := []modeloptions.Option{modeloptions.WithMaxTokens(maxTokens)}
	if useDeepSeekThinking && isReviewPrompt(ctx) {
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

func generateReviewWithinLengthBudget(
	messages []*schema.Message,
	budget int,
	tools []*schema.ToolInfo,
	generate func([]*schema.Message, int, []*schema.ToolInfo) (*schema.Message, error),
) (*schema.Message, error) {
	initial := func(input []*schema.Message, tokenBudget int) (*schema.Message, error) {
		return generate(input, tokenBudget, tools)
	}
	fallback := func(input []*schema.Message, tokenBudget int) (*schema.Message, error) {
		if tokenBudget > 8192 {
			tokenBudget = 8192
		}
		return generate(input, tokenBudget, nil)
	}
	return generateWithinLengthBudgetFallback(messages, budget, initial, fallback)
}

func generateWithinLengthBudgetFallback(
	messages []*schema.Message,
	budget int,
	initialGenerate func([]*schema.Message, int) (*schema.Message, error),
	fallbackGenerate func([]*schema.Message, int) (*schema.Message, error),
) (*schema.Message, error) {
	reply, err := initialGenerate(messages, budget)
	if err != nil {
		return nil, err
	}
	if reply == nil {
		return nil, fmt.Errorf("模型返回空响应")
	}
	if reply.ResponseMeta == nil || reply.ResponseMeta.FinishReason != "length" {
		return reply, nil
	}

	for _, maxFindings := range []int{5, 2} {
		concise := &schema.Message{
			Role: schema.User,
			Content: fmt.Sprintf("上一轮输出被长度限制截断。请不要调用工具，直接给出精简的最终结论。最多保留 %d 条证据充分、优先级最高的问题；没有充分证据时输出 []。", maxFindings) +
				"只输出 JSON 数组，每项包含 file、line、severity、confidence、body、evidence、trigger、impact、suggestion 字段。",
		}
		input := append(append([]*schema.Message{}, messages...), concise)
		reply, err = fallbackGenerate(input, budget)
		if err != nil {
			return nil, err
		}
		if reply == nil {
			return nil, fmt.Errorf("模型返回空响应")
		}
		if len(reply.ToolCalls) > 0 {
			return nil, fmt.Errorf("模型在禁止工具的精简重答中仍请求工具调用")
		}
		if reply.ResponseMeta == nil || reply.ResponseMeta.FinishReason != "length" {
			return reply, nil
		}
	}
	return nil, fmt.Errorf("模型输出达到 token 上限，精简重答两轮仍被截断")
}

// Persist sanitized provider input and completion data for model-call diagnosis.
func observedModelRequest(
	ctx context.Context,
	name string,
	round int,
	retryCount int,
	budget int,
	generate func() (*schema.Message, error),
) (*schema.Message, error) {
	return observedBudgetedModelRequest(ctx, observedModelCall{
		name:                  name,
		round:                 round,
		retryCount:            retryCount,
		requestedOutputTokens: budget,
		estimatedInputTokens:  inputFramingTokenReserve,
		pricing:               reviewBudgetFrom(ctx).pricing(),
	}, func(int) (*schema.Message, error) {
		return generate()
	})
}

type observedModelCall struct {
	name                  string
	round                 int
	retryCount            int
	requestedOutputTokens int
	estimatedInputTokens  int
	pricing               reviewModelPricing
	messages              []*schema.Message
	tools                 []*schema.ToolInfo
	thinkingEnabled       bool
}

func observedBudgetedModelRequest(
	ctx context.Context,
	request observedModelCall,
	generate func(int) (*schema.Message, error),
) (*schema.Message, error) {
	meter := reviewBudgetFrom(ctx)
	reservation, reserveErr := meter.reserveForModel(request.estimatedInputTokens, request.requestedOutputTokens, request.pricing)
	outputTokenLimit := request.requestedOutputTokens
	if reserveErr == nil && meter != nil {
		outputTokenLimit = reservation.outputTokens
	}
	tracePrompt := marshalModelRequestForTrace(request, outputTokenLimit)
	var span *TraceSpan
	if recorder := traceRecorderFrom(ctx); recorder != nil {
		span = recorder.Start(
			"model_request",
			request.name,
			"inference",
			fmt.Sprintf(
				"requested_max_tokens=%d allowed_max_tokens=%d estimated_input_tokens=%d",
				request.requestedOutputTokens,
				outputTokenLimit,
				request.estimatedInputTokens,
			),
			traceParentFrom(ctx),
		)
	}
	if reserveErr != nil {
		endModelRequestTrace(span, TraceResult{
			Status: "budget_exhausted", Output: "审查预算不足，已阻止模型请求", Prompt: tracePrompt, Round: request.round,
			RetryCount: request.retryCount, Origin: "model",
		})
		_ = meter.persist()
		return nil, reserveErr
	}
	reply, err := generate(outputTokenLimit)
	result := TraceResult{
		Status:     "succeeded",
		Output:     "模型请求完成",
		Prompt:     tracePrompt,
		ModelReply: marshalModelReplyForTrace(reply),
		Origin:     "model",
		Round:      request.round,
		RetryCount: request.retryCount,
	}
	if err != nil || reply == nil {
		result.Status = "failed"
		result.Output = "模型请求失败"
		if err != nil {
			result.Output += ": " + redact(redactReviewInput(err.Error()))
		} else {
			result.Output += ": 模型返回空响应"
		}
		if isProviderRejectedBeforeInference(err) {
			result.Output = "服务商在模型生成前拒绝请求，未计模型费用"
		}
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
	result.Model = reservation.pricing.modelName
	result.InputPriceYuanPerMillion = reservation.pricing.inputPrice
	result.OutputPriceYuanPerMillion = reservation.pricing.outputPrice
	if meter != nil {
		usageKnown := reply != nil && reply.ResponseMeta != nil && reply.ResponseMeta.Usage != nil
		if err != nil && isProviderRejectedBeforeInference(err) {
			usageKnown = true
		}
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

func marshalModelRequestForTrace(request observedModelCall, allowedTokens int) string {
	type requestTrace struct {
		Model                string             `json:"model"`
		RequestedMaxTokens   int                `json:"requested_max_tokens"`
		AllowedMaxTokens     int                `json:"allowed_max_tokens"`
		EstimatedInputTokens int                `json:"estimated_input_tokens"`
		ThinkingEnabled      bool               `json:"thinking_enabled"`
		ReasoningEffort      string             `json:"reasoning_effort,omitempty"`
		Messages             []*schema.Message  `json:"messages"`
		Tools                []*schema.ToolInfo `json:"tools,omitempty"`
	}
	messages := make([]*schema.Message, 0, len(request.messages))
	for _, message := range request.messages {
		if message == nil {
			continue
		}
		messages = append(messages, modelMessageForTrace(message))
	}
	encoded, err := json.Marshal(requestTrace{
		Model:                request.pricing.modelName,
		RequestedMaxTokens:   request.requestedOutputTokens,
		AllowedMaxTokens:     allowedTokens,
		EstimatedInputTokens: request.estimatedInputTokens,
		ThinkingEnabled:      request.thinkingEnabled,
		ReasoningEffort:      reasoningEffort(request.thinkingEnabled),
		Messages:             messages,
		Tools:                request.tools,
	})
	if err != nil {
		return "trace request serialization failed: " + redactReviewInput(err.Error())
	}
	return redactTraceText(string(encoded))
}

func reasoningEffort(enabled bool) string {
	if enabled {
		return "high"
	}
	return ""
}

func marshalModelReplyForTrace(reply *schema.Message) string {
	if reply == nil {
		return ""
	}
	type responseTrace struct {
		Role         string             `json:"role"`
		Content      string             `json:"content,omitempty"`
		ToolCalls    []schema.ToolCall  `json:"tool_calls,omitempty"`
		FinishReason string             `json:"finish_reason,omitempty"`
		Usage        *schema.TokenUsage `json:"usage,omitempty"`
	}
	tracedReply := modelMessageForTrace(reply)
	response := responseTrace{Role: string(tracedReply.Role), Content: tracedReply.Content, ToolCalls: tracedReply.ToolCalls}
	if reply.ResponseMeta != nil {
		response.FinishReason = reply.ResponseMeta.FinishReason
		response.Usage = reply.ResponseMeta.Usage
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		return "trace response serialization failed: " + redactReviewInput(err.Error())
	}
	return redactTraceText(string(encoded))
}

func modelMessageForTrace(message *schema.Message) *schema.Message {
	if message == nil {
		return nil
	}
	copy := *message
	copy.Content = redactTraceText(message.Content)
	copy.ReasoningContent = ""
	copy.ToolCalls = append([]schema.ToolCall{}, message.ToolCalls...)
	for index := range copy.ToolCalls {
		copy.ToolCalls[index].Function.Arguments = redactTraceText(copy.ToolCalls[index].Function.Arguments)
	}
	return &copy
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
		return "deepseek-flash"
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
