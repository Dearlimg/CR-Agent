package logic

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/cloudwego/eino-ext/components/model/openai"
	modeloptions "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

type reviewModelProvider struct {
	chat                *openai.ChatModel
	pricing             reviewModelPricing
	maxOutputTokens     int
	useDeepSeekThinking bool
}

type reviewModelRouter struct {
	mu       sync.RWMutex
	primary  *reviewModelProvider
	fallback *reviewModelProvider
	active   *reviewModelProvider
}

type reviewModelRequest struct {
	traceName   string
	round       int
	budget      int
	inputTokens int
	retryCount  *int
	messages    []*schema.Message
	tools       []*schema.ToolInfo
}

func newReviewModelRouter(ctx context.Context, cfg Config) (*reviewModelRouter, error) {
	primary, err := openai.NewChatModel(ctx, &openai.ChatModelConfig{
		APIKey:  cfg.DeepSeekAPIKey,
		Model:   reviewModelName(cfg),
		BaseURL: strings.TrimRight(cfg.DeepSeekBaseURL, "/") + "/v1",
	})
	if err != nil {
		return nil, err
	}

	primaryPricing := reviewModelPricing{
		modelName:   reviewModelName(cfg),
		inputPrice:  cfg.InputPriceYuanPerMillion,
		outputPrice: cfg.OutputPriceYuanPerMillion,
	}
	if primaryPricing.inputPrice <= 0 {
		primaryPricing.inputPrice = defaultInputPrice
	}
	if primaryPricing.outputPrice <= 0 {
		primaryPricing.outputPrice = defaultOutputPrice
	}
	primaryProvider := &reviewModelProvider{
		chat:                primary,
		pricing:             primaryPricing,
		maxOutputTokens:     deepSeekMaxOutputTokens,
		useDeepSeekThinking: true,
	}
	router := &reviewModelRouter{
		primary: primaryProvider,
		active:  primaryProvider,
	}

	if !fallbackModelConfigured(cfg) {
		return router, nil
	}
	if err := validateFallbackModelConfig(cfg); err != nil {
		return nil, err
	}
	fallback, err := openai.NewChatModel(ctx, &openai.ChatModelConfig{
		APIKey:  cfg.ReviewFallbackAPIKey,
		Model:   cfg.ReviewFallbackModel,
		BaseURL: cfg.ReviewFallbackBaseURL + "/v1",
	})
	if err != nil {
		return nil, fmt.Errorf("初始化备用模型失败: %w", err)
	}
	router.fallback = &reviewModelProvider{
		chat:            fallback,
		maxOutputTokens: cfg.ModelMaxOutputTokens,
		pricing: reviewModelPricing{
			modelName:   cfg.ReviewFallbackModel,
			inputPrice:  cfg.ReviewFallbackInputPriceYuanPerMillion,
			outputPrice: cfg.ReviewFallbackOutputPriceYuanPerMillion,
		},
	}
	return router, nil
}

func fallbackModelConfigured(cfg Config) bool {
	return strings.TrimSpace(cfg.ReviewFallbackAPIKey) != "" ||
		strings.TrimSpace(cfg.ReviewFallbackBaseURL) != "" ||
		strings.TrimSpace(cfg.ReviewFallbackModel) != "" ||
		cfg.ReviewFallbackInputPriceYuanPerMillion > 0 ||
		cfg.ReviewFallbackOutputPriceYuanPerMillion > 0
}

func validateFallbackModelConfig(cfg Config) error {
	missing := []string{}
	if strings.TrimSpace(cfg.ReviewFallbackAPIKey) == "" {
		missing = append(missing, "REVIEW_FALLBACK_API_KEY")
	}
	if strings.TrimSpace(cfg.ReviewFallbackBaseURL) == "" {
		missing = append(missing, "REVIEW_FALLBACK_BASE_URL")
	}
	if strings.TrimSpace(cfg.ReviewFallbackModel) == "" {
		missing = append(missing, "REVIEW_FALLBACK_MODEL")
	}
	if cfg.ReviewFallbackInputPriceYuanPerMillion <= 0 {
		missing = append(missing, "REVIEW_FALLBACK_INPUT_PRICE_YUAN_PER_MILLION")
	}
	if cfg.ReviewFallbackOutputPriceYuanPerMillion <= 0 {
		missing = append(missing, "REVIEW_FALLBACK_OUTPUT_PRICE_YUAN_PER_MILLION")
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("备用模型配置不完整，缺少: %s", strings.Join(missing, ", "))
}

func (r *reviewModelRouter) activeProvider() *reviewModelProvider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.active
}

func (r *reviewModelRouter) fallbackAfterFailure(
	provider *reviewModelProvider,
	err error,
) *reviewModelProvider {
	if provider != r.primary || r.fallback == nil || !isFailoverEligibleModelError(err) {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.active == r.primary {
		r.active = r.fallback
	}
	if r.active == r.fallback {
		return r.fallback
	}
	return nil
}

func generateReviewModelRequest(
	ctx context.Context,
	cfg Config,
	router *reviewModelRouter,
	request reviewModelRequest,
) (*schema.Message, error) {
	callProvider := func(provider *reviewModelProvider) (*schema.Message, error) {
		outputTokens := request.budget
		if provider.maxOutputTokens > 0 && outputTokens > provider.maxOutputTokens {
			outputTokens = provider.maxOutputTokens
		}
		var bound modeloptions.ToolCallingChatModel = provider.chat
		if len(request.tools) > 0 {
			var err error
			bound, err = provider.chat.WithTools(request.tools)
			if err != nil {
				return nil, err
			}
		}
		retryCount := request.retryCount
		if retryCount == nil {
			attempt := 0
			retryCount = &attempt
		}
		return retryHarnessInference(ctx, cfg, func() (*schema.Message, error) {
			attempt := *retryCount
			*retryCount = attempt + 1
			providerMessages := messagesForModelProvider(request.messages, provider)
			return observedBudgetedModelRequest(
				ctx,
				request.traceName,
				request.round,
				attempt,
				outputTokens,
				request.inputTokens,
				provider.pricing,
				func(allowedTokens int) (*schema.Message, error) {
					return bound.Generate(ctx, providerMessages, modelRequestOptions(ctx, allowedTokens, provider.useDeepSeekThinking)...)
				},
			)
		})
	}

	provider := router.activeProvider()
	response, err := callProvider(provider)
	if err == nil {
		return response, nil
	}
	fallback := router.fallbackAfterFailure(provider, err)
	if fallback == nil {
		return nil, describeModelProviderFailure(err, provider, false)
	}
	recordModelFailover(ctx, provider, fallback, err)

	response, fallbackErr := callProvider(fallback)
	if fallbackErr != nil {
		return nil, fmt.Errorf(
			"主模型 %q 触发%s，已切换备用模型 %q，但备用模型也失败: %w",
			provider.pricing.modelName,
			modelFailureLabel(err),
			fallback.pricing.modelName,
			describeModelProviderFailure(fallbackErr, fallback, true),
		)
	}
	return response, nil
}

func messagesForModelProvider(messages []*schema.Message, provider *reviewModelProvider) []*schema.Message {
	if provider.useDeepSeekThinking {
		return messages
	}
	providerMessages := make([]*schema.Message, len(messages))
	for index, message := range messages {
		if message == nil {
			continue
		}
		copy := *message
		copy.ReasoningContent = ""
		providerMessages[index] = &copy
	}
	return providerMessages
}

func recordModelFailover(
	ctx context.Context,
	primary *reviewModelProvider,
	fallback *reviewModelProvider,
	err error,
) {
	recorder := traceRecorderFrom(ctx)
	if recorder == nil {
		return
	}
	span := recorder.Start(
		"model_failover",
		"model_router",
		"inference",
		"trigger="+modelFailureLabel(err),
		traceParentFrom(ctx),
	)
	span.End(TraceResult{
		Status: "succeeded",
		Output: fmt.Sprintf("主模型 %q 失败，已切换备用模型 %q", primary.pricing.modelName, fallback.pricing.modelName),
		Model:  fallback.pricing.modelName,
		Origin: "orchestrator",
	})
}

func describeModelProviderFailure(err error, provider *reviewModelProvider, isFallback bool) error {
	if err == nil || !isInsufficientBalanceModelError(err) {
		return err
	}
	if isFallback {
		return fmt.Errorf("%w；备用服务商账户余额不足，请补充该账户余额后重试", err)
	}
	return fmt.Errorf("%w；%s API 账户余额不足，请充值后重试，或配置 REVIEW_FALLBACK_* 备用模型", err, provider.pricing.modelName)
}

func modelFailureLabel(err error) string {
	switch modelFailureKind(err) {
	case "insufficient_balance":
		return "账户余额不足"
	case "authentication":
		return "认证失败"
	case "rate_limit":
		return "请求限流"
	case "upstream_unavailable":
		return "服务暂不可用"
	case "network":
		return "网络故障"
	default:
		return "模型调用失败"
	}
}

func isInsufficientBalanceModelError(err error) bool {
	return modelFailureKind(err) == "insufficient_balance"
}

func isProviderRejectedBeforeInference(err error) bool {
	switch modelFailureKind(err) {
	case "invalid_request", "authentication", "insufficient_balance", "rate_limit":
		return true
	default:
		return false
	}
}

func isFailoverEligibleModelError(err error) bool {
	switch modelFailureKind(err) {
	case "insufficient_balance", "authentication", "rate_limit", "upstream_unavailable", "network":
		return true
	default:
		return false
	}
}

func modelFailureKind(err error) string {
	if err == nil {
		return ""
	}
	message := strings.ToLower(err.Error())
	switch {
	case containsAny(message, "status code: 402", "http 402", "insufficient balance", "insufficient_balance"):
		return "insufficient_balance"
	case containsAny(message, "status code: 401", "http 401", "unauthorized", "authentication fails"):
		return "authentication"
	case containsAny(message, "status code: 429", "http 429", "rate limit", "too many requests"):
		return "rate_limit"
	case containsAny(message, "status code: 400", "http 400", "status code: 422", "http 422"):
		return "invalid_request"
	case containsAny(
		message,
		"status code: 500", "http 500", "status code: 502", "http 502",
		"status code: 503", "http 503", "status code: 504", "http 504",
		"status code: 529", "http 529", "temporarily unavailable",
	):
		return "upstream_unavailable"
	case containsAny(
		message,
		"eof", "connection reset", "connection closed", "connection refused",
		"reset by peer", "broken pipe", "timeout", "timed out",
	):
		return "network"
	default:
		return ""
	}
}

func containsAny(value string, markers ...string) bool {
	for _, marker := range markers {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}
