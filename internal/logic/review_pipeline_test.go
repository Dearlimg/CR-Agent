package logic

import (
	"CR-Agent/internal/dao"
	"CR-Agent/internal/model"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestReviewPipelineReanchorsAndRecordsThreeWayVerdict(t *testing.T) {
	for _, test := range []struct {
		verdict  string
		status   string
		outcome  string
		comments int
	}{
		{verdict: findingConfirmed, status: "completed", outcome: "completed_with_findings", comments: 1},
		{verdict: findingRejected, status: "completed", outcome: "completed_no_findings", comments: 0},
		{verdict: findingInconclusive, status: "completed_with_warnings", outcome: "incomplete", comments: 0},
	} {
		t.Run(test.verdict, func(t *testing.T) {
			var calls atomic.Int32
			var verificationPrompt string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				call := calls.Add(1)
				if call == 2 {
					var request struct {
						Messages []struct {
							Content string `json:"content"`
						} `json:"messages"`
					}
					if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
						t.Errorf("decode verification request: %v", err)
					}
					for _, message := range request.Messages {
						verificationPrompt += message.Content
					}
				}
				reply := `[{"file":"a.py","line":10,"severity":"medium","confidence":"medium","body":"bug","evidence":"bug()","trigger":"call","impact":"fails","suggestion":"fix"}]`
				if call == 2 {
					reply = `{"verdict":"` + test.verdict + `","reason":"检查源码后判断"}`
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"choices": []any{map[string]any{
						"message":       map[string]any{"role": "assistant", "content": reply},
						"finish_reason": "stop",
					}},
				})
			}))
			defer server.Close()
			root := t.TempDir()
			service := NewService(dao.NewJobStore(filepath.Join(root, "jobs")), Config{
				SkillsDir: "../../skills", MemoryDir: filepath.Join(root, "memory"),
				TasksDir: filepath.Join(root, "tasks"), BackgroundTasksDir: filepath.Join(root, "background"),
				TeamMailboxDir: filepath.Join(root, "team"), CronFile: filepath.Join(root, "cron.json"),
				DeepSeekAPIKey: "test-only", DeepSeekBaseURL: server.URL,
			})
			job := &model.ReviewJob{
				ID: "wrong-line", Source: "inline", Trace: []model.TraceEvent{},
				Comments: []model.ReviewComment{}, Todos: []model.TodoItem{
					{Order: 0, Status: "pending"}, {Order: 1, Status: "pending"}, {Order: 2, Status: "pending"},
				},
			}
			diff := "diff --git a/a.py b/a.py\n--- a/a.py\n+++ b/a.py\n" +
				"@@ -0,0 +10,2 @@\n+placeholder()\n+bug()\n"
			service.run(context.Background(), job, model.ReviewRequest{Diff: diff})
			if job.Status != test.status || job.ReviewOutcome != test.outcome || len(job.Comments) != test.comments {
				t.Fatalf("status=%q outcome=%q comments=%d error=%q", job.Status, job.ReviewOutcome, len(job.Comments), job.Error)
			}
			if calls.Load() != 2 || !strings.Contains(verificationPrompt, `"line":11`) ||
				!strings.Contains(verificationPrompt, "11 +bug()") {
				t.Fatalf("verification did not use corrected line: calls=%d prompt=%q", calls.Load(), verificationPrompt)
			}
			if test.comments == 1 && job.Comments[0].Line != 11 {
				t.Fatalf("published comment still uses hallucinated line: %#v", job.Comments[0])
			}
			if test.verdict == findingInconclusive {
				if !strings.Contains(job.Error, "缺少判定") ||
					!strings.Contains(reviewCheckMessage(job.ReviewScope, "finding_verification"), "证据待定=1") {
					t.Fatalf("inconclusive verdict was not surfaced: error=%q scope=%#v", job.Error, job.ReviewScope)
				}
			}
		})
	}
}

func TestReviewPipelineUsesPinnedSourceForSemanticVerification(t *testing.T) {
	const headSHA = "3c4a85732eb696f8e03442de8b0ebb02a7a3ae4c"
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/base/repo/pulls/1":
			_, _ = w.Write([]byte(`{"head":{"sha":"` + headSHA + `","repo":{"full_name":"base/repo"}}}`))
		case "/repos/base/repo/contents/a.py":
			writePipelineSourceFile(w, "class Client:\n    def run(self):\n        return service.fetch()\n")
		case "/repos/base/repo/contents/service.py":
			writePipelineSourceFile(w, "def fetch():\n    return 1\n")
		default:
			http.NotFound(w, r)
		}
		if strings.Contains(r.URL.Path, "/contents/") && r.URL.Query().Get("ref") != headSHA {
			t.Errorf("source ref=%q, want PR head", r.URL.Query().Get("ref"))
		}
	}))
	defer api.Close()
	var calls atomic.Int32
	var verificationPrompt string
	modelAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := calls.Add(1)
		reply := `[{"file":"a.py","line":3,"severity":"medium","confidence":"medium","body":"bug","evidence":"return service.fetch()","trigger":"call","impact":"fails","suggestion":"fix"}]`
		if call == 2 {
			var request struct {
				Messages []struct {
					Content string `json:"content"`
				} `json:"messages"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Errorf("decode verification request: %v", err)
			}
			for _, message := range request.Messages {
				verificationPrompt += message.Content
			}
			reply = `{"verdict":"confirmed","reason":"源码定义可见"}`
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{
				"message":       map[string]any{"role": "assistant", "content": reply},
				"finish_reason": "stop",
			}},
		})
	}))
	defer modelAPI.Close()
	root := t.TempDir()
	service := NewService(dao.NewJobStore(filepath.Join(root, "jobs")), Config{
		SkillsDir: "../../skills", MemoryDir: filepath.Join(root, "memory"),
		TasksDir: filepath.Join(root, "tasks"), BackgroundTasksDir: filepath.Join(root, "background"),
		TeamMailboxDir: filepath.Join(root, "team"), CronFile: filepath.Join(root, "cron.json"),
		DeepSeekAPIKey: "test-only", DeepSeekBaseURL: modelAPI.URL, GitHubAPIBase: api.URL,
	})
	job := &model.ReviewJob{
		ID: "pinned-source", Source: "https://api.github.com/repos/base/repo/pulls/1",
		Trace: []model.TraceEvent{}, Comments: []model.ReviewComment{},
		Todos: []model.TodoItem{
			{Order: 0, Status: "pending"}, {Order: 1, Status: "pending"}, {Order: 2, Status: "pending"},
		},
	}
	diff := "diff --git a/a.py b/a.py\n--- a/a.py\n+++ b/a.py\n" +
		"@@ -0,0 +1,3 @@\n+class Client:\n+    def run(self):\n+        return service.fetch()\n" +
		"diff --git a/service.py b/service.py\n--- a/service.py\n+++ b/service.py\n" +
		"@@ -0,0 +1,2 @@\n+def fetch():\n+    return 1\n"
	service.run(context.Background(), job, model.ReviewRequest{Diff: diff})
	if job.Status != "completed" || len(job.Comments) != 1 || calls.Load() != 2 {
		t.Fatalf("status=%q comments=%d model calls=%d error=%q", job.Status, len(job.Comments), calls.Load(), job.Error)
	}
	if !strings.Contains(verificationPrompt, "related fetch at service.py:1") ||
		!strings.Contains(verificationPrompt, "def fetch():") {
		t.Fatalf("verification omitted related pinned source: %q", verificationPrompt)
	}
}

func writePipelineSourceFile(w http.ResponseWriter, content string) {
	_ = json.NewEncoder(w).Encode(map[string]any{
		"type": "file", "encoding": "base64", "size": len(content),
		"content": base64.StdEncoding.EncodeToString([]byte(content)),
	})
}

func reviewCheckMessage(scope model.ReviewScope, name string) string {
	for _, check := range scope.Checks {
		if check.Name == name {
			return check.Message
		}
	}
	return ""
}
