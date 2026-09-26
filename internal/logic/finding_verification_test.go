package logic

import (
	"CR-Agent/internal/model"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFindingDiffExcerptKeepsTargetEvidenceAndLocalContext(t *testing.T) {
	diff := "diff --git a/internal/api.go b/internal/api.go\n--- a/internal/api.go\n+++ b/internal/api.go\n" +
		"@@ -1,5 +1,6 @@\n package api\n import \"log\"\n+log.Printf(\"password=%s\", password)\n return nil\n }\n"
	excerpt, err := findingDiffExcerpt(diff, "internal/api.go", 3, 1, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(excerpt, "file: internal/api.go") ||
		!strings.Contains(excerpt, "3 +log.Printf(\"password=%s\", password)") ||
		!strings.Contains(excerpt, "2  import") || !strings.Contains(excerpt, "4  return nil") {
		t.Fatalf("excerpt lost target evidence or neighboring lines: %q", excerpt)
	}
	if _, err := findingDiffExcerpt(diff, "internal/other.go", 3, 1, 1000); err == nil {
		t.Fatal("expected a missing file to fail excerpt construction")
	}
}

func TestFindingDiffExcerptRejectsOverlongEvidence(t *testing.T) {
	diff := "diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -0,0 +1 @@\n+" + strings.Repeat("x", 100) + "\n"
	if _, err := findingDiffExcerpt(diff, "a.go", 1, 0, 24); err == nil {
		t.Fatal("expected the excerpt limit to reject an overlong evidence line")
	}
}

func TestFindingDiffExcerptIncludesNearbyImpact(t *testing.T) {
	var diff strings.Builder
	diff.WriteString("diff --git a/service.py b/service.py\n+++ b/service.py\n@@ -0,0 +639,13 @@\n")
	for line := 639; line <= 651; line++ {
		if line == 651 {
			diff.WriteString("+app_resp.total_page = total_count // query.page_size\n")
			continue
		}
		diff.WriteString("+other_code()\n")
	}
	excerpt, err := findingDiffExcerpt(
		diff.String(), "service.py", 639,
		findingVerificationContextLines, findingVerificationDiffLimit,
	)
	if err != nil || !strings.Contains(excerpt, "651 +app_resp.total_page") {
		t.Fatalf("nearby impact missing: excerpt=%q err=%v", excerpt, err)
	}
}

func TestFindingVerifierDistinguishesMissingContextFromRejection(t *testing.T) {
	for _, verdict := range []string{findingConfirmed, findingPlausible, findingRejected, findingInconclusive} {
		t.Run(verdict, func(t *testing.T) {
			var prompt string
			var system, user string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request struct {
					Messages []struct {
						Role    string `json:"role"`
						Content string `json:"content"`
					} `json:"messages"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Errorf("decode request: %v", err)
				}
				for _, message := range request.Messages {
					prompt += message.Content
					switch message.Role {
					case "system":
						system += message.Content
					case "user":
						user += message.Content
					}
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"choices": []any{map[string]any{
						"message": map[string]any{
							"role": "assistant", "content": testFindingVerdictJSON(verdict, "代码证据判定"),
						},
						"finish_reason": "stop",
					}},
				})
			}))
			defer server.Close()
			diff := "diff --git a/a.py b/a.py\n+++ b/a.py\n@@ -0,0 +10 @@\n+return service.fetch()\n"
			job := &model.ReviewJob{ID: "verify-test", Trace: []model.TraceEvent{}}
			got, _, err := verifyFindingIndependently(context.Background(), findingVerificationRequest{
				Config: Config{DeepSeekBaseURL: server.URL, DeepSeekAPIKey: "test-only"},
				Diff:   diff, Finding: ReviewFinding{File: "a.py", Line: 10, Evidence: "return service.fetch()"},
				SourceExcerpt: "file: service.py\n22 def fetch(): return None",
				Recorder:      newTraceRecorder(job),
			})
			if err != nil || got.Verdict != verdict {
				t.Fatalf("verdict=%#v err=%v, want %q", got, err, verdict)
			}
			if got.SupportingEvidence == "" || got.Counterevidence == "" || got.Confidence == "" {
				t.Fatalf("structured assessment lost: %#v", got)
			}
			if !strings.Contains(prompt, "service.py") || !strings.Contains(prompt, "10 +return service.fetch()") {
				t.Fatalf("verification prompt lacks code context: %q", prompt)
			}
			if !strings.Contains(system, "inconclusive") || strings.Contains(system, "service.py") ||
				!strings.Contains(user, "service.py") {
				t.Fatalf("verification roles mixed instructions and source: system=%q user=%q", system, user)
			}
		})
	}
}

func TestFindingVerifierRejectsOldBooleanVerdict(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{
				"message":       map[string]any{"role": "assistant", "content": `{"is_real":false,"reason":"缺少上下文"}`},
				"finish_reason": "stop",
			}},
		})
	}))
	defer server.Close()
	_, _, err := verifyFindingIndependently(context.Background(), findingVerificationRequest{
		Config:   Config{DeepSeekBaseURL: server.URL, DeepSeekAPIKey: "test-only"},
		Diff:     "diff --git a/a.go b/a.go\n+++ b/a.go\n@@ -0,0 +1 @@\n+bug()\n",
		Finding:  ReviewFinding{File: "a.go", Line: 1, Evidence: "bug()"},
		Recorder: newTraceRecorder(&model.ReviewJob{ID: "old-verdict", Trace: []model.TraceEvent{}}),
	})
	if !errors.Is(err, errIncompleteReview) {
		t.Fatalf("old boolean verdict should be incomplete, got %v", err)
	}
}

func TestFindingVerifierRepairsInvalidJSONThroughValidationTool(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var request struct {
			Tools []struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		message := map[string]any{
			"role": "assistant", "content": testFindingVerdictJSON(findingConfirmed, "有明确触发路径") + "\n额外解释",
		}
		finishReason := "stop"
		if calls == 2 {
			if len(request.Tools) != 1 || request.Tools[0].Function.Name != parseFindingVerdictJSONTool {
				t.Errorf("repair tools=%#v", request.Tools)
			}
			message["content"] = ""
			message["tool_calls"] = []any{map[string]any{
				"id": "validate-verdict", "type": "function",
				"function": map[string]any{
					"name": parseFindingVerdictJSONTool,
					"arguments": jsonString(map[string]string{
						"json": testFindingVerdictJSON(findingConfirmed, "有明确触发路径"),
					}),
				},
			}}
			finishReason = "tool_calls"
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": message, "finish_reason": finishReason}},
		})
	}))
	defer server.Close()

	diff := "diff --git a/a.go b/a.go\n+++ b/a.go\n@@ -0,0 +1 @@\n+bug()\n"
	job := &model.ReviewJob{ID: "verdict-json-repair", Trace: []model.TraceEvent{}}
	recorder := newTraceRecorder(job)
	got, _, err := verifyFindingIndependently(withTraceRecorder(context.Background(), recorder), findingVerificationRequest{
		Config:   Config{DeepSeekBaseURL: server.URL, DeepSeekAPIKey: "test-only"},
		Diff:     diff,
		Finding:  ReviewFinding{File: "a.go", Line: 1, Evidence: "bug()"},
		Recorder: recorder,
	})
	if err != nil || got.Verdict != findingConfirmed || got.Reason != "有明确触发路径" || calls != 3 {
		t.Fatalf("verdict=%#v calls=%d err=%v", got, calls, err)
	}
	recorder.Flush()
	foundToolTrace := false
	for _, trace := range job.Trace {
		if trace.Tool == parseFindingVerdictJSONTool {
			foundToolTrace = trace.Status == "succeeded" && strings.Contains(trace.Input, "有明确触发路径")
		}
	}
	if !foundToolTrace {
		t.Fatalf("JSON validation tool input/output not traced: %#v", job.Trace)
	}
}

func testFindingVerdictJSON(verdict, reason string) string {
	assessment := findingVerdict{
		Verdict: verdict, Reason: reason,
		SupportingEvidence: "候选行的调用和返回值可见",
		Counterevidence:    "已检查可见代码中的前置校验；不可见调用方尚未核实",
		Assumptions:        []string{},
		Confidence:         "medium",
	}
	switch verdict {
	case findingConfirmed:
		assessment.Confidence = "high"
	case findingPlausible:
		assessment.Assumptions = []string{"确认调用方是否允许该输入；读取调用方校验逻辑"}
	case findingInconclusive:
		assessment.Assumptions = []string{"缺少返回类型定义，读取定义后判断该操作是否可能失败"}
		assessment.Confidence = "low"
	case findingRejected:
		assessment.Confidence = "low"
	}
	return jsonString(assessment)
}

func TestFindingVerdictRequiresStructuredAssessment(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{name: "missing support", mutate: func(value map[string]any) { delete(value, "supporting_evidence") }},
		{name: "missing counterevidence", mutate: func(value map[string]any) { delete(value, "counterevidence") }},
		{name: "blank reason", mutate: func(value map[string]any) { value["reason"] = " " }},
		{name: "missing confidence", mutate: func(value map[string]any) { delete(value, "confidence") }},
		{name: "invalid confidence", mutate: func(value map[string]any) { value["confidence"] = "certain" }},
		{name: "missing assumptions", mutate: func(value map[string]any) { delete(value, "assumptions") }},
		{name: "null assumptions", mutate: func(value map[string]any) { value["assumptions"] = nil }},
		{name: "blank premise", mutate: func(value map[string]any) { value["assumptions"] = []string{" "} }},
		{name: "plausible without premise", mutate: func(value map[string]any) { value["assumptions"] = []string{} }},
		{name: "plausible high confidence", mutate: func(value map[string]any) { value["confidence"] = "high" }},
		{name: "confirmed with premise", mutate: func(value map[string]any) { value["verdict"] = findingConfirmed }},
		{name: "unknown verdict", mutate: func(value map[string]any) { value["verdict"] = "maybe" }},
		{name: "unknown field", mutate: func(value map[string]any) { value["is_real"] = true }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value := map[string]any{}
			if err := json.Unmarshal([]byte(testFindingVerdictJSON(findingPlausible, "条件性风险")), &value); err != nil {
				t.Fatal(err)
			}
			test.mutate(value)
			if _, err := findingVerdictJSONToolSpec().Validate(jsonString(value)); err == nil {
				t.Fatalf("invalid structured verdict accepted: %#v", value)
			}
		})
	}
	for _, confidence := range []string{"medium", "low"} {
		var verdict findingVerdict
		if err := parseFindingVerdict(testFindingVerdictJSON(findingPlausible, "条件性风险"), &verdict); err != nil {
			t.Fatal(err)
		}
		verdict.Confidence = confidence
		normalized, err := findingVerdictJSONToolSpec().Validate(jsonString(verdict))
		if err != nil || !strings.Contains(normalized, confidence) {
			t.Fatalf("actionable %s-confidence risk rejected: %v", confidence, err)
		}
	}
}

func TestFindingVerdictDoesNotReusePreviousAssessment(t *testing.T) {
	var verdict findingVerdict
	if err := parseFindingVerdict(testFindingVerdictJSON(findingConfirmed, "已确认"), &verdict); err != nil {
		t.Fatal(err)
	}
	if err := parseFindingVerdict(`{"verdict":"confirmed","reason":"缺少新协议字段"}`, &verdict); err == nil {
		t.Fatal("incomplete response inherited fields from a previous assessment")
	}
}

func TestFindingVerdictRedactsEveryAssessmentField(t *testing.T) {
	text := "api_key=definitely-fake-value-123"
	verdict := redactFindingVerdict(findingVerdict{
		Verdict: findingPlausible, Reason: text, SupportingEvidence: text,
		Counterevidence: text, Assumptions: []string{text}, Confidence: "low",
	})
	for _, value := range []string{
		verdict.Reason, verdict.SupportingEvidence, verdict.Counterevidence, verdict.Assumptions[0],
	} {
		if strings.Contains(value, "definitely-fake-value-123") || !strings.Contains(value, "[REDACTED]") {
			t.Fatalf("assessment field was not redacted: %q", value)
		}
	}
}
