package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"CR-Agent/internal/model"
)

// judgeRefVerdict records whether a system finding covers one reference comment.
type judgeRefVerdict struct {
	Index     int    `json:"index"`
	Verdict   string `json:"verdict"` // covered | partial | missed
	CoveredBy []int  `json:"covered_by,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

// judgeFindingVerdict classifies one system finding against the references.
type judgeFindingVerdict struct {
	Index    int    `json:"index"`
	Verdict  string `json:"verdict"` // matched_reference | valid_new_issue | nitpick | invalid | out_of_scope
	Reason   string `json:"reason,omitempty"`
}

// judgePairScore rates one matched (reference, finding) pair on 1-5 scales.
type judgePairScore struct {
	ReferenceIndex int    `json:"reference_index"`
	FindingIndex   int    `json:"finding_index"`
	Accuracy       int    `json:"accuracy"`
	Relevance      int    `json:"relevance"`
	Usefulness     int    `json:"usefulness"`
	Reason         string `json:"reason,omitempty"`
}

type judgeOutput struct {
	ReferenceVerdicts []judgeRefVerdict     `json:"reference_verdicts"`
	FindingVerdicts   []judgeFindingVerdict `json:"finding_verdicts"`
	PairScores        []judgePairScore      `json:"pair_scores"`
	Raw               string                `json:"-"`
	ParseError        string                `json:"-"`
}

const judgeSystemPrompt = `You are an expert code-review evaluation judge. You compare the findings of an automated code-review system against human review comments written on the same pull request.

You receive:
1. "reference_comments": real human review comments, each anchored to a file and line, with the diff context needed to understand them.
2. "system_findings": findings produced by the automated reviewer, each with file, line and body.
3. Diff context excerpts around the relevant lines.

Rules:
- Every reference_comment carries a "status" from a manual audit of whether the flagged issue still exists in this diff snapshot:
  - "valid": the problem still exists in the snapshot. Judge covered/partial/missed as usual.
  - "fixed_in_snapshot": the requested change is ALREADY present in the snapshot, so the flagged issue does NOT exist here. Verdict "re_flagged" (with covered_by) if some finding asserts this already-fixed issue as a defect; verdict "quiet" if no finding does. Never use covered/partial/missed for these.
  - "suggestion": a non-defect improvement note (style/refactor/docs/tests). Judge covered/partial/missed as usual; these are scored on a separate channel.
  - "unverifiable": cannot be judged from the diff. Verdict "skipped".
- A "valid" or "suggestion" reference is "covered" only if some finding addresses the SAME underlying issue at essentially the same location (same file, line within a few lines, same root cause). Surface wording may differ.
- Use "partial" when the finding touches the same area but misses the core point of the reference comment.
- For findings that do NOT match any reference, judge whether the finding is a genuine, actionable issue worth leaving as a review comment on this diff: valid_new_issue (a real problem the humans missed or did not comment on), nitpick (trivial style/preference), invalid (factually wrong or misreads the code), out_of_scope (about unrelated context, cannot anchor to the diff, or duplicates another finding). A finding that re-asserts an issue already fixed in the snapshot ("fixed_in_snapshot" reference) must be classified invalid.
- For every covered reference, score the matched system finding 1-5:
  accuracy: is the technical claim about the code correct?
  relevance: does it address what the human comment actually pointed at?
  usefulness: would the suggestion help the author improve the code?
- Judge based ONLY on the provided evidence. Do not invent code that is not shown.
- Respond with ONLY a JSON object, no markdown fences, of the form:
{"reference_verdicts":[{"index":0,"verdict":"covered","covered_by":[1],"reason":"..."}],"finding_verdicts":[{"index":0,"verdict":"valid_new_issue","reason":"..."}],"pair_scores":[{"reference_index":0,"finding_index":1,"accuracy":4,"relevance":5,"usefulness":3,"reason":"..."}]}
Verdicts: covered | partial | missed | re_flagged | quiet | skipped.
Every reference comment and every system finding MUST appear exactly once in the respective arrays.`

type judgeClient struct {
	baseURL   string
	apiKey    string
	model     string
	maxTokens int
	http      *http.Client
}

func newJudgeClient(baseURL, apiKey, model string, maxTokens int) *judgeClient {
	return &judgeClient{
		baseURL:   strings.TrimRight(baseURL, "/"),
		apiKey:    apiKey,
		model:     model,
		maxTokens: maxTokens,
		http:      &http.Client{Timeout: 5 * time.Minute},
	}
}

func (j *judgeClient) call(system, user string) (string, error) {
	payload := map[string]any{
		"model": j.model,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
		"temperature": 0,
		"max_tokens":  j.maxTokens,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequest(http.MethodPost, j.baseURL+"/chat/completions", bytes.NewReader(encoded))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if j.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+j.apiKey)
	}
	resp, err := j.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var decoded struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	body := new(bytes.Buffer)
	if _, err := body.ReadFrom(resp.Body); err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("judge HTTP %d: %s", resp.StatusCode, truncateString(body.String(), 300))
	}
	if err := json.Unmarshal(body.Bytes(), &decoded); err != nil {
		return "", fmt.Errorf("judge 响应解析失败: %w", err)
	}
	if decoded.Error != nil {
		return "", errors.New("judge 端点错误: " + decoded.Error.Message)
	}
	if len(decoded.Choices) == 0 {
		return "", errors.New("judge 响应没有 choices")
	}
	return decoded.Choices[0].Message.Content, nil
}

func truncateString(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// runJudge compares system findings against reference comments for one case.
func runJudge(client *judgeClient, sample realCase, diff string, findings []model.ReviewComment) judgeOutput {
	type refPayload struct {
		Index  int    `json:"index"`
		File   string `json:"file"`
		Line   int    `json:"line"`
		Body   string `json:"body"`
		Kind   string `json:"kind"`
		Status string `json:"status"`
	}
	type findingPayload struct {
		Index    int    `json:"index"`
		File     string `json:"file"`
		Line     int    `json:"line"`
		Severity string `json:"severity"`
		Body     string `json:"body"`
	}
	refs := make([]refPayload, 0, len(sample.ReferenceComments))
	anchors := map[string]map[int]bool{}
	for index, ref := range sample.ReferenceComments {
		refs = append(refs, refPayload{Index: index, File: ref.File, Line: ref.Line, Body: clip(ref.Body, 1200), Kind: ref.Kind, Status: refChannel(ref)})
		if anchors[ref.File] == nil {
			anchors[ref.File] = map[int]bool{}
		}
		anchors[ref.File][ref.Line] = true
	}
	incs := make([]findingPayload, 0, len(findings))
	for index, finding := range findings {
		incs = append(incs, findingPayload{Index: index, File: finding.File, Line: finding.Line, Severity: finding.Severity, Body: clip(finding.Body, 1200)})
		if anchors[finding.File] == nil {
			anchors[finding.File] = map[int]bool{}
		}
		anchors[finding.File][finding.Line] = true
	}
	out := judgeOutput{}
	if len(refs) == 0 && len(incs) == 0 {
		out.ParseError = "nothing to judge"
		return out
	}
	context := extractAnchorContext(diff, anchors, 24000)
	userPayload := map[string]any{
		"pr_title":           sample.Title,
		"scenario":           sample.Scenario,
		"reference_comments": refs,
		"system_findings":    incs,
		"diff_context":       context,
	}
	encoded, err := json.MarshalIndent(userPayload, "", "  ")
	if err != nil {
		out.ParseError = err.Error()
		return out
	}
	raw, err := client.call(judgeSystemPrompt, string(encoded))
	if err != nil {
		out.ParseError = err.Error()
		return out
	}
	out.Raw = raw
	if err := parseJudgeJSON(raw, &out); err != nil {
		// One retry: models sometimes wrap JSON in fences despite instructions.
		retryRaw, retryErr := client.call(judgeSystemPrompt+"\nYour previous reply was not valid JSON. Return ONLY the JSON object.", string(encoded))
		if retryErr != nil {
			out.ParseError = err.Error()
			return out
		}
		out.Raw = retryRaw
		if err2 := parseJudgeJSON(retryRaw, &out); err2 != nil {
			out.ParseError = err2.Error()
		}
	}
	return out
}

func parseJudgeJSON(raw string, out *judgeOutput) error {
	clean := strings.TrimSpace(raw)
	clean = strings.TrimPrefix(clean, "```json")
	clean = strings.TrimPrefix(clean, "```")
	clean = strings.TrimSuffix(strings.TrimSpace(clean), "```")
	if start := strings.Index(clean, "{"); start > 0 {
		clean = clean[start:]
	}
	if end := strings.LastIndex(clean, "}"); end >= 0 {
		clean = clean[:end+1]
	}
	var decoded judgeOutput
	if err := json.Unmarshal([]byte(clean), &decoded); err != nil {
		return fmt.Errorf("judge JSON 解析失败: %w", err)
	}
	out.ReferenceVerdicts = decoded.ReferenceVerdicts
	out.FindingVerdicts = decoded.FindingVerdicts
	out.PairScores = decoded.PairScores
	out.ParseError = ""
	return nil
}

func clip(s string, n int) string {
	runes := []rune(strings.TrimSpace(s))
	if len(runes) <= n {
		return string(runes)
	}
	return string(runes[:n]) + "…"
}
