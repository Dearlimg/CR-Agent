package logic

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/flow/agent/react"
	"github.com/cloudwego/eino/schema"
)

// EinoReviewAgent exposes the review model through Eino's ReAct agent.
// Tool execution remains owned by our harness so every call is persisted in trace.
func EinoReviewAgent(ctx context.Context, cfg Config, prompt string) (string, error) {
	if strings.TrimSpace(cfg.DeepSeekAPIKey) == "" {
		return "", fmt.Errorf("DEEPSEEK_API_KEY 未配置")
	}
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
		result, err := runEinoReviewAgent(ctx, cfg, prompt)
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
			return "", ctx.Err()
		case <-timer.C:
		}
	}
	return "", fmt.Errorf("模型调用失败（已重试 %d 次）: %w", retries, lastErr)
}

func runEinoReviewAgent(ctx context.Context, cfg Config, prompt string) (string, error) {
	model, err := openai.NewChatModel(ctx, &openai.ChatModelConfig{APIKey: cfg.DeepSeekAPIKey, Model: "deepseek-chat", BaseURL: strings.TrimRight(cfg.DeepSeekBaseURL, "/") + "/v1"})
	if err != nil {
		return "", err
	}
	agent, err := react.NewAgent(ctx, &react.AgentConfig{ToolCallingModel: model, MaxStep: 4})
	if err != nil {
		return "", err
	}
	msg, err := agent.Generate(ctx, []*schema.Message{{Role: schema.User, Content: prompt}})
	if err != nil {
		return "", err
	}
	return msg.Content, nil
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
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}
