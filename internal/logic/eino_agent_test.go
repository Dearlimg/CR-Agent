package logic

import (
	"CR-Agent/internal/model"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestIsRetryableModelError(t *testing.T) {
	for _, test := range []struct {
		name      string
		err       error
		retryable bool
	}{
		{name: "eof", err: errors.New("Post request: EOF"), retryable: true},
		{name: "rate limit", err: errors.New("HTTP 429"), retryable: true},
		{name: "adapter rate limit", err: errors.New("failed to create chat completion: status code: 429"), retryable: true},
		{name: "auth", err: errors.New("HTTP 401"), retryable: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := isRetryableModelError(test.err); got != test.retryable {
				t.Fatalf("retryable=%v, want %v", got, test.retryable)
			}
		})
	}
}

func TestObservedModelRequestRecordsAttemptMetadataAndRedactsProviderErrors(t *testing.T) {
	job := &model.ReviewJob{ID: "model-request", Trace: []model.TraceEvent{}}
	recorder := newTraceRecorder(job)
	ctx := withTraceParent(withTraceRecorder(context.Background(), recorder), "parent-model")
	reply := &schema.Message{
		Role: schema.Assistant,
		ResponseMeta: &schema.ResponseMeta{
			FinishReason: "length",
			Usage: &schema.TokenUsage{
				PromptTokens:     123,
				CompletionTokens: 4096,
			},
		},
	}
	got, err := observedModelRequest(ctx, "deepseek_chat", 2, 1, 4096, func() (*schema.Message, error) {
		return reply, nil
	})
	if err != nil || got != reply {
		t.Fatalf("reply=%#v err=%v", got, err)
	}
	_, err = observedModelRequest(ctx, "deepseek_chat", 2, 2, 4096, func() (*schema.Message, error) {
		return nil, errors.New("secret-token-request-failed")
	})
	if err == nil {
		t.Fatal("expected provider error")
	}
	recorder.Flush()
	if len(job.Trace) != 2 {
		t.Fatalf("trace length=%d, want 2", len(job.Trace))
	}
	first := job.Trace[0]
	if first.Kind != "model_request" || first.ParentID != "parent-model" || first.Round != 2 || first.RetryCount != 1 {
		t.Fatalf("unexpected request identity: %#v", first)
	}
	if first.InputTokens != 123 || first.OutputTokens != 4096 || first.FinishReason != "length" || first.Status != "truncated" {
		t.Fatalf("unexpected provider metadata: %#v", first)
	}
	if first.EndedAt == nil || first.EndedAt.Before(first.StartedAt) || first.DurationMs < 0 {
		t.Fatalf("invalid request timing: %#v", first)
	}
	if job.Trace[1].Status != "failed" || job.Trace[1].RetryCount != 2 {
		t.Fatalf("unexpected failed request: %#v", job.Trace[1])
	}
	for _, event := range job.Trace {
		if strings.Contains(event.Input, "secret-token") || strings.Contains(event.Output, "secret-token") {
			t.Fatalf("request trace leaked provider error: %#v", event)
		}
	}
}

func TestObservedModelRequestRecordsSanitizedInputAndRawReply(t *testing.T) {
	job := &model.ReviewJob{ID: "model-request-input", Trace: []model.TraceEvent{}}
	recorder := newTraceRecorder(job)
	ctx := withTraceRecorder(context.Background(), recorder)
	tool, err := modelJSONToolInfo(goalDecisionJSONToolSpec())
	if err != nil {
		t.Fatalf("create JSON tool info: %v", err)
	}
	request := observedModelCall{
		name:                  "deepseek-v4-pro",
		round:                 3,
		retryCount:            1,
		requestedOutputTokens: 8192,
		estimatedInputTokens:  512,
		pricing:               reviewModelPricing{modelName: "deepseek-v4-pro"},
		messages: []*schema.Message{
			{Role: schema.System, Content: "Return JSON. api_key=\"test-secret-value\""},
			{Role: schema.User, Content: "MALFORMED_REPLY_MARKER: {missing quote"},
		},
		tools: []*schema.ToolInfo{tool},
	}
	reply := &schema.Message{
		Role:    schema.Assistant,
		Content: "MALFORMED_REPLY_MARKER: {missing quote",
		ResponseMeta: &schema.ResponseMeta{
			FinishReason: "stop",
			Usage:        &schema.TokenUsage{PromptTokens: 123, CompletionTokens: 17},
		},
	}
	got, err := observedBudgetedModelRequest(ctx, request, func(int) (*schema.Message, error) {
		return reply, nil
	})
	if err != nil || got != reply {
		t.Fatalf("reply=%#v err=%v", got, err)
	}
	recorder.Flush()
	if len(job.Trace) != 1 {
		t.Fatalf("trace length=%d, want 1", len(job.Trace))
	}
	trace := job.Trace[0]
	if !strings.Contains(trace.Prompt, "MALFORMED_REPLY_MARKER") || !strings.Contains(trace.Prompt, parseGoalDecisionJSONTool) {
		t.Fatalf("trace omitted request messages or tools: %s", trace.Prompt)
	}
	if strings.Contains(trace.Prompt, "test-secret-value") || !strings.Contains(trace.Prompt, "[REDACTED]") {
		t.Fatalf("request trace did not redact credentials: %s", trace.Prompt)
	}
	if !strings.Contains(trace.ModelReply, "MALFORMED_REPLY_MARKER") || !strings.Contains(trace.ModelReply, "finish_reason") {
		t.Fatalf("trace omitted raw model reply metadata: %s", trace.ModelReply)
	}
}

func TestProviderWireRequestRedactsSyntheticCredentials(t *testing.T) {
	secrets := []string{
		"synthetic-private-key-wire-body",
		"synthetic-multiline-config-value",
		"synthetic-uri-password",
		"glpat-ABCDEFGHIJKLMNOPQRSTUVWXYZ123456",
	}
	input := "-----BEGIN PRIVATE KEY-----\n" + secrets[0] + "\n-----END PRIVATE KEY-----\n" +
		"settings:\n  client_secret: " + secrets[1] + "\n" +
		"postgres://user:" + secrets[2] + "@db.example.test/app\n" + secrets[3]
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		for _, secret := range secrets {
			if strings.Contains(string(body), secret) {
				t.Errorf("synthetic credential reached provider HTTP body: %q", secret)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "test", "object": "chat.completion",
			"choices": []any{map[string]any{
				"index": 0, "message": map[string]any{"role": "assistant", "content": "[]"}, "finish_reason": "stop",
			}},
		})
	}))
	defer server.Close()
	cfg := Config{DeepSeekAPIKey: "synthetic-provider-key", DeepSeekBaseURL: server.URL}
	router, err := newReviewModelRouter(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	_, err = generateReviewModelRequest(context.Background(), cfg, router, reviewModelRequest{
		traceName: "wire-redaction", budget: 1024,
		messages: []*schema.Message{{Role: schema.User, Content: input}},
	})
	if err != nil || calls != 1 {
		t.Fatalf("provider calls=%d err=%v", calls, err)
	}
}

func TestGoalEvaluatorRepairsInvalidJSONThroughToolFallback(t *testing.T) {
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
		message := map[string]any{"role": "assistant", "content": "finished"}
		finishReason := "stop"
		if calls > 1 {
			if len(request.Tools) != 1 || request.Tools[0].Function.Name != parseGoalDecisionJSONTool {
				t.Errorf("goal evaluator tools=%#v", request.Tools)
			}
			message["content"] = ""
			arguments := `{"json":"not JSON"}`
			if calls == 3 {
				arguments = `{"json":"{\"ok\":true,\"reason\":\"goal reached\",\"impossible\":false}"}`
			}
			message["tool_calls"] = []any{map[string]any{
				"id": "goal-json", "type": "function",
				"function": map[string]any{"name": parseGoalDecisionJSONTool, "arguments": arguments},
			}}
			finishReason = "tool_calls"
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": message, "finish_reason": finishReason}},
		})
	}))
	defer server.Close()

	ctx := withGoalCondition(context.Background(), "the task is complete")
	answer, err := EinoReviewAgent(ctx, Config{DeepSeekAPIKey: "test-only", DeepSeekBaseURL: server.URL}, "perform task")
	if err != nil || answer != "finished" || calls != 3 {
		t.Fatalf("answer=%q calls=%d err=%v", answer, calls, err)
	}
}

func TestGenerateWithinLengthBudgetDoesNotReplayIncompleteToolCall(t *testing.T) {
	message := &schema.Message{
		Role:         schema.Assistant,
		ToolCalls:    []schema.ToolCall{testToolCall("partial", "diff_fetcher", `{`)},
		ResponseMeta: &schema.ResponseMeta{FinishReason: "length"},
	}
	calls := 0
	_, err := generateWithinLengthBudget([]*schema.Message{{Role: schema.User, Content: "review"}}, 4096,
		func(_ []*schema.Message, budget int) (*schema.Message, error) {
			calls++
			if budget != 4096 {
				t.Fatalf("budget=%d, want 4096", budget)
			}
			return message, nil
		})
	if err == nil || !strings.Contains(err.Error(), "不完整工具调用") || calls != 1 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}

func TestGenerateWithinLengthBudgetRequestsOneConciseReanswer(t *testing.T) {
	original := []*schema.Message{{Role: schema.User, Content: "review"}}
	calls := 0
	reply, err := generateWithinLengthBudget(original, 4096, func(input []*schema.Message, budget int) (*schema.Message, error) {
		calls++
		if budget != 4096 {
			t.Fatalf("budget=%d, want 4096", budget)
		}
		if calls == 1 {
			if len(input) != 1 {
				t.Fatalf("first input length=%d", len(input))
			}
			return &schema.Message{Role: schema.Assistant, Content: "partial", ResponseMeta: &schema.ResponseMeta{FinishReason: "length"}}, nil
		}
		if len(input) != 2 || !strings.Contains(input[1].Content, "精简") {
			t.Fatalf("retry input=%#v", input)
		}
		return &schema.Message{Role: schema.Assistant, Content: "[]", ResponseMeta: &schema.ResponseMeta{FinishReason: "stop"}}, nil
	})
	if err != nil || reply.Content != "[]" || calls != 2 || len(original) != 1 {
		t.Fatalf("reply=%#v err=%v calls=%d original=%d", reply, err, calls, len(original))
	}
}

func TestEinoAgentRecordsWireUsageAcrossLengthRetry(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var request struct {
			MaxTokens int `json:"max_tokens"`
			Messages  []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		wantMaxTokens := 16384
		if calls == 2 {
			wantMaxTokens = 8192
		}
		if request.MaxTokens != wantMaxTokens {
			t.Errorf("max_tokens=%d, want %d", request.MaxTokens, wantMaxTokens)
		}
		if calls == 2 && !strings.Contains(request.Messages[len(request.Messages)-1].Content, "精简") {
			t.Error("length retry did not request concise re-answer")
		}
		finish := "length"
		content := "partial"
		if calls == 2 {
			finish = "stop"
			content = "[]"
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "test", "object": "chat.completion",
			"choices": []any{map[string]any{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": content},
				"finish_reason": finish,
			}},
			"usage": map[string]any{
				"prompt_tokens":     105,
				"completion_tokens": 4096,
				"total_tokens":      4201,
			},
		})
	}))
	defer server.Close()
	job := &model.ReviewJob{ID: "wire-usage", Trace: []model.TraceEvent{}}
	recorder := newTraceRecorder(job)
	ctx := withTraceParent(withTraceRecorder(context.Background(), recorder), "review-parent")
	ctx = context.WithValue(ctx, harnessSetupKey{}, func(*ReviewHarness) {})
	answer, err := EinoReviewAgent(ctx, Config{
		DeepSeekAPIKey:       "test-only",
		DeepSeekBaseURL:      server.URL,
		ModelMaxOutputTokens: 16384,
	}, "review")
	if err != nil || answer != "[]" || calls != 2 {
		t.Fatalf("answer=%q calls=%d err=%v", answer, calls, err)
	}
	recorder.Flush()
	if len(job.Trace) != 2 {
		t.Fatalf("trace length=%d, want 2", len(job.Trace))
	}
	for index, event := range job.Trace {
		if event.Round != 1 || event.RetryCount != index || event.InputTokens != 105 || event.OutputTokens != 4096 {
			t.Fatalf("unexpected request %d metadata: %#v", index, event)
		}
		if event.ParentID != "review-parent" || event.Kind != "model_request" {
			t.Fatalf("unexpected request %d parent: %#v", index, event)
		}
	}
	if job.Trace[0].FinishReason != "length" || job.Trace[1].FinishReason != "stop" {
		t.Fatalf("finish reasons=%q, %q", job.Trace[0].FinishReason, job.Trace[1].FinishReason)
	}
}

func TestEinoAgentRecordsTransportRetryAttempts(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			http.Error(w, "rate limited", http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "test", "object": "chat.completion",
			"choices": []any{map[string]any{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": "[]"},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{
				"prompt_tokens":     15,
				"completion_tokens": 2,
				"total_tokens":      17,
			},
		})
	}))
	defer server.Close()
	job := &model.ReviewJob{ID: "transport-retry", Trace: []model.TraceEvent{}}
	recorder := newTraceRecorder(job)
	ctx := withTraceRecorder(context.Background(), recorder)
	ctx = context.WithValue(ctx, harnessSetupKey{}, func(*ReviewHarness) {})
	answer, err := EinoReviewAgent(ctx, Config{
		DeepSeekAPIKey: "test-only", DeepSeekBaseURL: server.URL,
		ModelMaxRetries: 1, ModelRetryBaseMs: 1,
	}, "review")
	if err != nil || answer != "[]" || calls != 2 {
		t.Fatalf("answer=%q calls=%d err=%v", answer, calls, err)
	}
	recorder.Flush()
	if len(job.Trace) != 2 || job.Trace[0].Status != "failed" || job.Trace[0].RetryCount != 0 {
		t.Fatalf("first attempt=%#v", job.Trace)
	}
	if job.Trace[1].Status != "succeeded" || job.Trace[1].RetryCount != 1 || job.Trace[1].InputTokens != 15 {
		t.Fatalf("second attempt=%#v", job.Trace[1])
	}
}
