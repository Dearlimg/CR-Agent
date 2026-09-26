package logic

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReviewSpecialistRepairsMalformedCandidateWithoutResendingDiff(t *testing.T) {
	valid := `[{"file":"a.go","line":7,"severity":"medium","confidence":"high",` +
		`"body":"会返回错误","evidence":"return err","trigger":"调用失败",` +
		`"impact":"请求失败","suggestion":"处理错误"}]`
	malformed := strings.TrimSuffix(valid, "]")
	call := 0
	ctx := context.WithValue(context.Background(), harnessSetupKey{}, func(*ReviewHarness) {})
	result := runReviewSpecialist(ctx, specialistRunRequest{
		Config: Config{},
		Agent:  ReviewSubagent{Name: "correctness", Focus: "正确性"},
		Diff:   "diff-only-marker",
		Infer: func(ctx context.Context, _ Config, prompt string) (string, error) {
			call++
			switch call {
			case 1:
				if !strings.Contains(prompt, "diff-only-marker") || ctx.Value(harnessSetupKey{}) == nil {
					t.Fatal("first review lost its diff or tools")
				}
				return malformed, nil
			case 2:
				if strings.Contains(prompt, "diff-only-marker") || !strings.Contains(prompt, malformed) {
					t.Fatal("repair did not use only the malformed report")
				}
				setup, ok := ctx.Value(harnessSetupKey{}).(func(*ReviewHarness))
				if !ok {
					t.Fatal("repair must install a no-tools harness")
				}
				harness := &ReviewHarness{tools: NewToolRegistry()}
				harness.tools.MustRegister(ToolDefinition{
					ToolMetadata: ToolMetadata{Name: "danger", Description: "danger", Permission: PermissionReadDiff},
					Run:          func(context.Context, ToolInput) (ToolResult, error) { return ToolResult{}, nil },
				})
				setup(harness)
				if len(harness.tools.List()) != 0 {
					t.Fatal("repair harness still exposes review tools")
				}
				return valid, nil
			default:
				t.Fatal("unexpected model call")
				return "", nil
			}
		},
	})
	findings, parseErr := parseFindingsStrict(result.Summary)
	if result.Error != nil || parseErr != nil || len(findings) != 1 || call != 2 {
		t.Fatalf("result=%#v calls=%d", result, call)
	}
}

func TestReviewSpecialistDoesNotTurnMalformedCandidateIntoNoFindings(t *testing.T) {
	malformed := `[{"file":"a.go","line":7,`
	for _, repair := range []struct {
		name  string
		reply string
		err   error
	}{
		{name: "empty repair", reply: "[]"},
		{name: "invalid repair", reply: "still invalid"},
		{name: "failed repair", err: errors.New("request failed")},
	} {
		t.Run(repair.name, func(t *testing.T) {
			call := 0
			result := runReviewSpecialist(context.Background(), specialistRunRequest{
				Agent: ReviewSubagent{Name: "correctness"},
				Diff:  "diff",
				Infer: func(context.Context, Config, string) (string, error) {
					call++
					if call == 1 {
						return malformed, nil
					}
					return repair.reply, repair.err
				},
			})
			if !errors.Is(result.Error, errIncompleteReview) || result.Summary != "[]" || call != 2 {
				t.Fatalf("result=%#v calls=%d", result, call)
			}
		})
	}
}

func TestReviewSpecialistRepairsIncompleteFindingSchema(t *testing.T) {
	incomplete := `[{"file":"a.go","line":7,"severity":"medium","confidence":"high","body":"bug"}]`
	complete := `[{"file":"a.go","line":7,"severity":"medium","confidence":"high",` +
		`"body":"bug","evidence":"return err","trigger":"call fails",` +
		`"impact":"request fails","suggestion":"handle error"}]`
	calls := 0
	result := runReviewSpecialist(context.Background(), specialistRunRequest{
		Agent: ReviewSubagent{Name: "correctness"},
		Diff:  "diff",
		Infer: func(_ context.Context, _ Config, prompt string) (string, error) {
			calls++
			if calls == 1 {
				return incomplete, nil
			}
			if !strings.Contains(prompt, "evidence") {
				t.Fatal("schema repair prompt did not include the required fields")
			}
			return complete, nil
		},
	})
	findings, err := parseFindingsStrict(result.Summary)
	if result.Error != nil || err != nil || len(findings) != 1 || calls != 2 {
		t.Fatalf("result=%#v findings=%#v err=%v calls=%d", result, findings, err, calls)
	}
}

func TestReviewAgentAcceptsValidRepairFromProviderWithoutToolCall(t *testing.T) {
	valid := `[{"file":"a.go","line":7,"severity":"medium","confidence":"high",` +
		`"body":"会返回错误","evidence":"return err","trigger":"调用失败",` +
		`"impact":"请求失败","suggestion":"处理错误"}]`
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
			t.Error(err)
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		if len(request.Tools) != 1 || request.Tools[0].Function.Name != parseReviewFindingsJSONTool {
			t.Errorf("request %d JSON validation tools=%#v", calls, request.Tools)
		}
		content := strings.TrimSuffix(valid, "]")
		if calls == 2 {
			content = valid
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "repair-test", "object": "chat.completion",
			"choices": []any{map[string]any{
				"index": 0, "message": map[string]any{"role": "assistant", "content": content},
				"finish_reason": "stop",
			}},
		})
	}))
	defer server.Close()

	result := RunReviewAgent(context.Background(), Config{
		DeepSeekAPIKey: "test-only", DeepSeekBaseURL: server.URL,
	}, "diff", ReviewPromptContext{})
	findings, err := parseFindingsStrict(result.Summary)
	if result.Error != nil || err != nil || len(findings) != 1 || calls != 2 {
		t.Fatalf("result=%#v findings=%#v parseErr=%v calls=%d", result, findings, err, calls)
	}
}

func TestReviewSpecialistAcceptsEmptyValidReportWithoutRepair(t *testing.T) {
	call := 0
	result := runReviewSpecialist(context.Background(), specialistRunRequest{
		Agent: ReviewSubagent{Name: "security"},
		Diff:  "diff",
		Infer: func(context.Context, Config, string) (string, error) {
			call++
			return "[]", nil
		},
	})
	if result.Error != nil || result.Summary != "[]" || call != 1 {
		t.Fatalf("result=%#v calls=%d", result, call)
	}
}

func TestReviewSpecialistSubmitsCompleteLargeDiffOnce(t *testing.T) {
	diff := strings.Builder{}
	writeFile := func(name, marker string) {
		diff.WriteString("diff --git a/" + name + " b/" + name + "\n")
		diff.WriteString("--- a/" + name + "\n+++ b/" + name + "\n")
		diff.WriteString("@@ -0,0 +1,1500 @@\n")
		diff.WriteString("+" + marker + "\n")
		for range 1499 {
			diff.WriteString("+var value = 1\n")
		}
	}
	writeFile("a.go", "FIRST_FILE_MARKER")
	writeFile("b.go", "SECOND_FILE_MARKER")

	calls := 0
	result := runReviewSpecialist(context.Background(), specialistRunRequest{
		Agent: ReviewSubagent{Name: "correctness", Focus: "correctness"},
		Diff:  diff.String(),
		Infer: func(_ context.Context, _ Config, prompt string) (string, error) {
			calls++
			if !strings.Contains(prompt, "FIRST_FILE_MARKER") ||
				!strings.Contains(prompt, "SECOND_FILE_MARKER") ||
				!strings.Contains(prompt, diff.String()) {
				t.Fatal("完整 diff 未在同一次审查请求中提交")
			}
			return `[ {"file":"a.go","line":1,"severity":"high","confidence":"high","body":"issue","evidence":"FIRST_FILE_MARKER","trigger":"call","impact":"bad","suggestion":"fix"} ]`, nil
		},
	})
	findings, err := parseFindingsStrict(result.Summary)
	if result.Error != nil || err != nil || len(findings) != 1 || calls != 1 {
		t.Fatalf("result=%#v findings=%#v parseErr=%v calls=%d", result, findings, err, calls)
	}
	if findings[0].File != "a.go" || findings[0].Evidence != "FIRST_FILE_MARKER" {
		t.Fatalf("aggregated finding=%#v", findings[0])
	}
}
