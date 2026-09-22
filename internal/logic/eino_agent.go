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
	if condition, ok := ctx.Value(goalConditionKey{}).(string); ok && strings.TrimSpace(condition) != "" {
		controller, err := NewGoalController(condition, PromptGoalEvaluator{Generate: func(ctx context.Context, prompt string) (string, error) {
			reply, err := retryHarnessInference(ctx, cfg, func() (*schema.Message, error) {
				return chat.Generate(ctx, []*schema.Message{{Role: schema.User, Content: prompt}}, modeloptions.WithMaxTokens(1024))
			})
			if err != nil {
				return "", err
			}
			if reply == nil {
				return "", fmt.Errorf("goal evaluator 返回空响应")
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
		bound, err := chat.WithTools(tools)
		if err != nil {
			return nil, err
		}
		for _, budget := range []int{4096, 8192} {
			reply, err := retryHarnessInference(ctx, cfg, func() (*schema.Message, error) {
				return bound.Generate(ctx, messages, modeloptions.WithMaxTokens(budget))
			})
			if err != nil {
				return nil, err
			}
			if reply == nil {
				return nil, fmt.Errorf("模型返回空响应")
			}
			if reply.ResponseMeta == nil || reply.ResponseMeta.FinishReason != "length" {
				return reply, nil
			}
		}
		return nil, fmt.Errorf("模型输出达到 token 上限，拒绝执行不完整工具调用")
	}
	return harness.Run(ctx, prompt)
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
		"http 500",
		"http 502",
		"http 503",
		"http 504",
		"http 529",
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}
