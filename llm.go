package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		TotalTokens int `json:"total_tokens"`
	} `json:"usage"`
}

func reviewWithDeepSeek(ctx context.Context, cfg Config, diff string) (string, int, error) {
	if strings.TrimSpace(cfg.DeepSeekAPIKey) == "" {
		return "", 0, fmt.Errorf("DEEPSEEK_API_KEY 未配置")
	}
	prompt := "你是资深代码审查员。只基于下面 git diff，输出中文审查意见。找出真实 bug、兼容性、异常处理和安全风险；没有问题就输出 NO_FINDINGS。每条意见包含：严重级别(high/medium/low)、置信度(high/medium/low)、原因和建议。不要编造。\n\n" + redact(diff)
	body, _ := json.Marshal(map[string]any{"model": "deepseek-chat", "temperature": 0.1, "messages": []map[string]string{{"role": "system", "content": "你负责严谨的 Code Review。"}, {"role": "user", "content": prompt}}})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(cfg.DeepSeekBaseURL, "/")+"/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+cfg.DeepSeekAPIKey)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", 0, fmt.Errorf("DeepSeek 返回 HTTP %d", resp.StatusCode)
	}
	var out chatResponse
	if err = json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", 0, err
	}
	if len(out.Choices) == 0 {
		return "", out.Usage.TotalTokens, fmt.Errorf("DeepSeek 未返回内容")
	}
	return out.Choices[0].Message.Content, out.Usage.TotalTokens, nil
}
