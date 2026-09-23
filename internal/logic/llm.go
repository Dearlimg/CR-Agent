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
	findings, err := decodeFindingsArray(clean)
	if err == nil {
		return findings, nil
	}
	if unwrapped, ok := unwrapJSONCodeFence(clean); ok {
		findings, err = decodeFindingsArray(unwrapped)
		if err == nil {
			return findings, nil
		}
	}

	for _, candidate := range extractJSONArrays(clean) {
		findings, err = decodeFindingsArray(candidate)
		if err == nil && len(findings) > 0 {
			return findings, nil
		}
		findings, err = decodeFindingArrayMembers(candidate)
		if err == nil && len(findings) > 0 {
			return findings, nil
		}
	}
	return nil, fmt.Errorf("%w：模型输出不是有效 finding JSON 数组", errIncompleteReview)
}

func unwrapJSONCodeFence(raw string) (string, bool) {
	if !strings.HasPrefix(raw, "```") {
		return "", false
	}
	lineEnd := strings.IndexByte(raw, '\n')
	if lineEnd < 0 || !strings.HasSuffix(raw, "```") {
		return "", false
	}
	language := strings.TrimSpace(strings.TrimPrefix(raw[:lineEnd], "```"))
	if language != "" && !strings.EqualFold(language, "json") {
		return "", false
	}
	return strings.TrimSpace(strings.TrimSuffix(raw[lineEnd+1:], "```")), true
}

func decodeFindingsArray(raw string) ([]ReviewFinding, error) {
	var findings []ReviewFinding
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&findings); err != nil || findings == nil {
		return nil, errors.New("invalid finding array")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, errors.New("trailing data after finding array")
	}
	for index, finding := range findings {
		if err := validateFindingShape(finding); err != nil {
			return nil, fmt.Errorf("%w：finding %d %v", errIncompleteReview, index+1, err)
		}
	}
	return findings, nil
}

// extractJSONArrays finds complete arrays embedded in common model wrappers,
// such as Markdown fences or a short preamble. Brackets inside strings are
// ignored so finding evidence cannot terminate extraction early.
func extractJSONArrays(raw string) []string {
	arrays := []string{}
	inString := false
	escaped := false
	start := -1
	depth := 0
	for index, char := range raw {
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if char == '\\' {
				escaped = true
				continue
			}
			if char == '"' {
				inString = false
			}
			continue
		}
		if char == '"' {
			inString = true
			continue
		}
		switch char {
		case '[':
			if depth == 0 {
				start = index
			}
			depth++
		case ']':
			if depth == 0 {
				continue
			}
			depth--
			if depth == 0 && start >= 0 {
				arrays = append(arrays, raw[start:index+1])
				start = -1
			}
		}
	}
	return arrays
}

// decodeFindingArrayMembers tolerates separator mistakes such as a trailing
// comma while still requiring a closed array of complete, schema-valid objects.
func decodeFindingArrayMembers(raw string) ([]ReviewFinding, error) {
	if len(raw) < 2 || raw[0] != '[' || raw[len(raw)-1] != ']' {
		return nil, errors.New("incomplete finding array")
	}
	content := raw[1 : len(raw)-1]
	members := []string{}
	start := 0
	objectDepth := 0
	arrayDepth := 0
	inString := false
	escaped := false
	for index, char := range content {
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if char == '\\' {
				escaped = true
				continue
			}
			if char == '"' {
				inString = false
			}
			continue
		}
		switch char {
		case '"':
			inString = true
		case '{':
			objectDepth++
		case '}':
			objectDepth--
			if objectDepth < 0 {
				return nil, errors.New("unexpected object close")
			}
		case '[':
			arrayDepth++
		case ']':
			arrayDepth--
			if arrayDepth < 0 {
				return nil, errors.New("unexpected array close")
			}
		case ',':
			if objectDepth == 0 && arrayDepth == 0 {
				members = append(members, strings.TrimSpace(content[start:index]))
				start = index + 1
			}
		}
	}
	if inString || objectDepth != 0 || arrayDepth != 0 {
		return nil, errors.New("incomplete finding object")
	}
	last := strings.TrimSpace(content[start:])
	if last != "" {
		members = append(members, last)
	}
	if len(members) == 0 {
		return nil, errors.New("empty finding array")
	}

	findings := make([]ReviewFinding, 0, len(members))
	for index, member := range members {
		var finding ReviewFinding
		decoder := json.NewDecoder(strings.NewReader(member))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&finding); err != nil {
			return nil, fmt.Errorf("invalid finding %d: %w", index+1, err)
		}
		if err := decoder.Decode(&struct{}{}); err != io.EOF {
			return nil, fmt.Errorf("trailing data in finding %d", index+1)
		}
		if err := validateFindingShape(finding); err != nil {
			return nil, fmt.Errorf("%w：finding %d %v", errIncompleteReview, index+1, err)
		}
		findings = append(findings, finding)
	}
	return findings, nil
}

func validateFindingShape(finding ReviewFinding) error {
	missing := []string{}
	if strings.TrimSpace(finding.File) == "" {
		missing = append(missing, "file")
	}
	if finding.Line <= 0 {
		missing = append(missing, "line")
	}
	if !validFindingLevel(finding.Severity) {
		missing = append(missing, "severity")
	}
	if !validFindingLevel(finding.Confidence) {
		missing = append(missing, "confidence")
	}
	for _, field := range []struct {
		name  string
		value string
	}{
		{name: "body", value: finding.Body},
		{name: "evidence", value: finding.Evidence},
		{name: "trigger", value: finding.Trigger},
		{name: "impact", value: finding.Impact},
		{name: "suggestion", value: finding.Suggestion},
	} {
		if strings.TrimSpace(field.value) == "" {
			missing = append(missing, field.name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("缺少必需字段：%s", strings.Join(missing, ", "))
	}
	return nil
}

func validFindingLevel(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "high", "medium", "low":
		return true
	default:
		return false
	}
}

// sanitizeModelReply keeps valid finding JSON parseable while removing values
// from every user-facing field before traces or team mailboxes persist it.
func sanitizeModelReply(raw string) string {
	findings := parseFindings(raw)
	if findings == nil {
		return redactReviewInput(raw)
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
		return redactReviewInput(raw)
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
