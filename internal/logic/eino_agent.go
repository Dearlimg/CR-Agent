package logic

import (
	"context"
	"fmt"
	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/flow/agent/react"
	"github.com/cloudwego/eino/schema"
	"strings"
)

// EinoReviewAgent exposes the review model through Eino's ReAct agent.
// Tool execution remains owned by our harness so every call is persisted in trace.
func EinoReviewAgent(ctx context.Context, cfg Config, prompt string) (string, error) {
	if strings.TrimSpace(cfg.DeepSeekAPIKey) == "" {
		return "", fmt.Errorf("DEEPSEEK_API_KEY 未配置")
	}
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
