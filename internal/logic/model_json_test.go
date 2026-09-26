package logic

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"CR-Agent/internal/model"
	"github.com/cloudwego/eino/schema"
)

func TestModelJSONValidationTools(t *testing.T) {
	t.Run("goal decision", func(t *testing.T) {
		spec := goalDecisionJSONToolSpec()
		if _, err := spec.Validate(`{"ok":true,"reason":"tests passed","impossible":false}`); err != nil {
			t.Fatalf("valid goal decision rejected: %v", err)
		}
		if _, err := spec.Validate(`{"ok":true,"reason":"done","impossible":true}`); err == nil {
			t.Fatal("contradictory goal decision accepted")
		}
		if _, err := spec.Validate(`{"ok":true,"reason":"done","impossible":false,"extra":1}`); err == nil {
			t.Fatal("unknown goal decision field accepted")
		}
	})

	t.Run("workflow schema", func(t *testing.T) {
		spec := workflowJSONToolSpec(map[string]any{
			"type":     "object",
			"required": []string{"ok"},
			"properties": map[string]any{
				"ok": map[string]any{"type": "boolean"},
			},
		})
		if _, err := spec.Validate(`{"ok":true}`); err != nil {
			t.Fatalf("valid workflow output rejected: %v", err)
		}
		if _, err := spec.Validate(`{"ok":"yes"}`); err == nil {
			t.Fatal("workflow output with wrong field type accepted")
		}
	})

	t.Run("memory candidates", func(t *testing.T) {
		valid := "" + `[{"name":"go-style","description":"Go style","type":"project","body":"Use gofmt.","scope":"persistent"}]`
		if _, err := memoryCandidatesJSONToolSpec().Validate(valid); err != nil {
			t.Fatalf("valid memory candidate rejected: %v", err)
		}
		if _, err := memoryCandidatesJSONToolSpec().Validate(`[{"name":"go-style","description":"Go style","type":"unknown","body":"Use gofmt.","scope":"persistent"}]`); err == nil {
			t.Fatal("memory candidate with invalid type accepted")
		}
	})

	t.Run("review findings empty array", func(t *testing.T) {
		harness := newReviewHarness()
		var parsed []ReviewFinding
		called := false
		registerReviewFindingJSONTool(harness, func(findings []ReviewFinding) {
			parsed = findings
			called = true
		}, true)
		var definition ToolDefinition
		for _, candidate := range harness.tools.List() {
			if candidate.Name == parseReviewFindingsJSONTool {
				definition = candidate
				break
			}
		}
		result, err := definition.Run(context.Background(), ToolInput{Args: map[string]any{"json": "[]"}})
		if err != nil || result.Output != "[]" || !called || len(parsed) != 0 {
			t.Fatalf("result=%#v called=%v parsed=%#v err=%v", result, called, parsed, err)
		}
		if _, _, err := normalizeReviewFindingsJSONWithEmpty("[]", false); err == nil {
			t.Fatal("repair tool unexpectedly accepted empty findings")
		}
	})
}

func TestExecuteModelJSONToolCallRecordsNormalizedInputAndOutput(t *testing.T) {
	job := &model.ReviewJob{ID: "json-tool", Trace: []model.TraceEvent{}}
	recorder := newTraceRecorder(job)
	ctx := withTraceParent(withTraceRecorder(context.Background(), recorder), "parent-json")
	spec := goalDecisionJSONToolSpec()
	call := schema.ToolCall{
		ID:   "call-json",
		Type: "function",
		Function: schema.FunctionCall{
			Name:      spec.Name,
			Arguments: `{"json":"{\"ok\":true,\"reason\":\"tests passed\",\"impossible\":false}"}`,
		},
	}
	output, err := executeModelJSONToolCall(ctx, call, spec)
	if err != nil {
		t.Fatalf("execute JSON tool call: %v", err)
	}
	var decision GoalDecision
	if err := json.Unmarshal([]byte(output), &decision); err != nil || !decision.OK {
		t.Fatalf("output=%q decision=%#v err=%v", output, decision, err)
	}
	recorder.Flush()
	if len(job.Trace) != 1 {
		t.Fatalf("trace length=%d, want 1", len(job.Trace))
	}
	trace := job.Trace[0]
	if trace.ParentID != "parent-json" || trace.Tool != spec.Name || trace.Status != "succeeded" {
		t.Fatalf("unexpected tool trace: %#v", trace)
	}
	if !strings.Contains(trace.Input, "tests passed") || trace.Output != output {
		t.Fatalf("tool trace omitted arguments or normalized result: %#v", trace)
	}
}

func TestJSONTraceRedactionHandlesNestedToolArguments(t *testing.T) {
	job := &model.ReviewJob{ID: "json-tool-redaction", Trace: []model.TraceEvent{}}
	recorder := newTraceRecorder(job)
	ctx := withTraceRecorder(context.Background(), recorder)
	spec := modelJSONToolSpec{
		Name:        parseGoalDecisionJSONTool,
		Description: "validate JSON",
		Validate: func(raw string) (string, error) {
			normalized, _, err := normalizeModelJSON(raw, nil)
			return normalized, err
		},
	}
	call := schema.ToolCall{
		ID:   "call-secret",
		Type: "function",
		Function: schema.FunctionCall{
			Name:      spec.Name,
			Arguments: `{"json":"{\"api_key\":\"nested-tool-secret\"}"}`,
		},
	}
	output, err := executeModelJSONToolCall(ctx, call, spec)
	if err != nil {
		t.Fatalf("execute JSON tool call: %v", err)
	}
	recorder.Flush()
	trace := job.Trace[0]
	for label, value := range map[string]string{"input": trace.Input, "output": trace.Output, "returned": output} {
		if strings.Contains(value, "nested-tool-secret") || !strings.Contains(value, "[REDACTED]") {
			t.Errorf("%s redaction failed: %s", label, value)
		}
	}
}
