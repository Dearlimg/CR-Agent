package logic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
type ReviewFinding struct {
	File                string `json:"file"`
	Line                int    `json:"line"`
	Severity            string `json:"severity"`
	Confidence          string `json:"confidence"`
	Body                string `json:"body"`
	Evidence            string `json:"evidence"`
	Trigger             string `json:"trigger"`
	Impact              string `json:"impact"`
	Suggestion          string `json:"suggestion"`
	VerificationStatus  string `json:"-"`
	VerificationReason  string `json:"-"`
	VerificationTraceID string `json:"-"`
}

var errIncompleteReview = errors.New("审查未完成")

func parseFindings(raw string) []ReviewFinding {
	findings, _ := parseFindingsStrict(raw)
	return findings
}

func parseFindingsStrict(raw string) ([]ReviewFinding, error) {
	clean := strings.TrimSpace(raw)
	var fs []ReviewFinding
	decoder := json.NewDecoder(strings.NewReader(clean))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&fs); err == nil && fs != nil {
		if err := decoder.Decode(&struct{}{}); err == io.EOF {
			return fs, nil
		}
	}
	return nil, fmt.Errorf("%w：模型输出不是有效 finding JSON 数组", errIncompleteReview)
}

// sanitizeModelReply keeps valid finding JSON parseable while removing values
// from every user-facing field before traces or team mailboxes persist it.
func sanitizeModelReply(raw string) string {
	findings := parseFindings(raw)
	if findings == nil {
		return redact(raw)
	}
	for index := range findings {
		findings[index].File = redactFindingText(findings[index].File)
		findings[index].Body = redactFindingText(findings[index].Body)
		findings[index].Evidence = redactFindingText(findings[index].Evidence)
		findings[index].Trigger = redactFindingText(findings[index].Trigger)
		findings[index].Impact = redactFindingText(findings[index].Impact)
		findings[index].Suggestion = redactFindingText(findings[index].Suggestion)
	}
	encoded, err := json.Marshal(findings)
	if err != nil {
		return redact(raw)
	}
	return string(encoded)
}

func redactFindingText(value string) string {
	value = credentialPattern.ReplaceAllString(value, "[REDACTED]")
	value = providerTokenPattern.ReplaceAllString(value, "[REDACTED]")
	return value
}

func reviewWithDeepSeek(ctx context.Context, cfg Config, diff string) (string, int, error) {
	if strings.TrimSpace(cfg.DeepSeekAPIKey) == "" {
		return "", 0, fmt.Errorf("DEEPSEEK_API_KEY 未配置")
	}
	prompt := "你是资深代码审查员。只基于下面 git diff，严格输出 JSON 数组，不要 Markdown。每项字段为 file(string), line(number), severity(high/medium/low), confidence(high/medium/low), body(string), evidence(string), trigger(string), impact(string), suggestion(string)。evidence 必须逐字引用该行变更代码；trigger 描述可复现条件；impact 描述具体后果；suggestion 给出最小修复。找出真实 bug、兼容性、异常处理和安全风险；没有问题输出 []。不要编造不可见上下文。\n\n" + redact(diff)
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
