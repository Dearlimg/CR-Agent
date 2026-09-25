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
	for _, verdict := range []string{findingConfirmed, findingRejected, findingInconclusive} {
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
						"message":       map[string]any{"role": "assistant", "content": `{"verdict":"` + verdict + `","reason":"代码证据判定"}`},
						"finish_reason": "stop",
					}},
				})
			}))
			defer server.Close()
			diff := "diff --git a/a.py b/a.py\n+++ b/a.py\n@@ -0,0 +10 @@\n+return service.fetch()\n"
			job := &model.ReviewJob{ID: "verify-test", Trace: []model.TraceEvent{}}
			got, _, _, err := verifyFindingIndependently(context.Background(), findingVerificationRequest{
				Config: Config{DeepSeekBaseURL: server.URL, DeepSeekAPIKey: "test-only"},
				Diff:   diff, Finding: ReviewFinding{File: "a.py", Line: 10, Evidence: "return service.fetch()"},
				SourceExcerpt: "file: service.py\n22 def fetch(): return None",
				Recorder:      newTraceRecorder(job),
			})
			if err != nil || got != verdict {
				t.Fatalf("verdict=%q err=%v, want %q", got, err, verdict)
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
	_, _, _, err := verifyFindingIndependently(context.Background(), findingVerificationRequest{
		Config:   Config{DeepSeekBaseURL: server.URL, DeepSeekAPIKey: "test-only"},
		Diff:     "diff --git a/a.go b/a.go\n+++ b/a.go\n@@ -0,0 +1 @@\n+bug()\n",
		Finding:  ReviewFinding{File: "a.go", Line: 1, Evidence: "bug()"},
		Recorder: newTraceRecorder(&model.ReviewJob{ID: "old-verdict", Trace: []model.TraceEvent{}}),
	})
	if !errors.Is(err, errIncompleteReview) {
		t.Fatalf("old boolean verdict should be incomplete, got %v", err)
	}
}
