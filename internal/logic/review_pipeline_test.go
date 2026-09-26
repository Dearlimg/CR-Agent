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
	"time"
)

func TestReviewPipelineReanchorsAndRecordsFourWayVerdict(t *testing.T) {
	for _, test := range []struct {
		verdict    string
		status     string
		outcome    string
		comments   int
		secretScan bool
	}{
		{verdict: findingConfirmed, status: "completed", outcome: "completed_with_findings", comments: 1},
		{verdict: findingConfirmed, status: "completed", outcome: "completed_with_findings", comments: 1, secretScan: true},
		{verdict: findingRejected, status: "completed", outcome: "completed_no_findings", comments: 0},
		{verdict: findingPlausible, status: "completed_with_warnings", outcome: "incomplete", comments: 0},
		{verdict: findingInconclusive, status: "completed_with_warnings", outcome: "incomplete", comments: 0},
	} {
		name := string(test.verdict)
		if test.secretScan {
			name += "_with_secret_scan_hit"
		}
		t.Run(name, func(t *testing.T) {
			var calls atomic.Int32
			wantCalls := int32(2)
			if test.verdict == findingConfirmed {
				wantCalls++ // High confidence confirmed findings also trigger memory extraction.
			}
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
					reply = testFindingVerdictJSON(test.verdict, "检查源码后判断")
				}
				if call > 2 {
					reply = "[]"
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
			if test.secretScan {
				diff += "diff --git a/config.txt b/config.txt\n--- /dev/null\n+++ b/config.txt\n" +
					"@@ -0,0 +1 @@\n+api_key = \"definitely-fake-value-123\"\n"
			}
			service.run(context.Background(), job, model.ReviewRequest{Diff: diff})
			if job.Status != test.status || job.ReviewOutcome != test.outcome || len(job.Comments) != test.comments {
				t.Fatalf("status=%q outcome=%q comments=%d error=%q", job.Status, job.ReviewOutcome, len(job.Comments), job.Error)
			}
			if calls.Load() != wantCalls || !strings.Contains(verificationPrompt, `"line":11`) ||
				!strings.Contains(verificationPrompt, "11 +bug()") {
				t.Fatalf("verification did not use corrected line: calls=%d prompt=%q", calls.Load(), verificationPrompt)
			}
			if test.comments == 1 && job.Comments[0].Line != 11 {
				t.Fatalf("published comment still uses hallucinated line: %#v", job.Comments[0])
			}
			checkpoint, err := loadReviewCheckpoint(job)
			if err != nil || len(checkpoint.VerificationResults) != 1 {
				t.Fatalf("verification checkpoint lost: checkpoint=%#v err=%v", checkpoint, err)
			}
			saved := checkpoint.VerificationResults[0]
			if saved.Verdict != test.verdict || saved.Assessment == nil || saved.TraceID == "" {
				t.Fatalf("structured verdict not persisted: %#v", saved)
			}
			if saved.Assessment.Reason != "检查源码后判断" || saved.Assessment.SupportingEvidence == "" {
				t.Fatalf("assessment details lost: %#v", saved.Assessment)
			}
			if test.verdict == findingConfirmed && job.Comments[0].Confidence != saved.Assessment.Confidence {
				t.Fatalf("verifier confidence not applied: %#v", job.Comments[0])
			}
			if test.verdict == findingPlausible {
				if !strings.Contains(job.Error, "前提待核实") || len(saved.Assessment.Assumptions) == 0 {
					t.Fatalf("conditional risk lost its pending premise: error=%q assessment=%#v", job.Error, saved.Assessment)
				}
				if !strings.Contains(reviewCheckMessage(job.ReviewScope, "finding_verification"), "有根据待核实=1") {
					t.Fatalf("plausible verdict was collapsed into another category: %#v", job.ReviewScope)
				}
			}
			if test.secretScan {
				if job.Error != "" || reviewCheckStatus(job.ReviewScope, "secret_scan") != "found" ||
					reviewCheckStatus(job.ReviewScope, "finding_verification") != "passed" {
					t.Fatalf("secret scan changed review completion: error=%q scope=%#v", job.Error, job.ReviewScope)
				}
			}
			if test.verdict == findingInconclusive {
				if !strings.Contains(job.Error, "缺少判定") ||
					!strings.Contains(reviewCheckMessage(job.ReviewScope, "finding_verification"), "证据待定=1") {
					t.Fatalf("inconclusive verdict was not surfaced: error=%q scope=%#v", job.Error, job.ReviewScope)
				}
			}
			job.Comments = []model.ReviewComment{}
			service.run(context.Background(), job, model.ReviewRequest{Diff: diff})
			if calls.Load() != wantCalls || len(job.Comments) != test.comments {
				t.Fatalf("resume reran verification or changed publication: calls=%d comments=%#v", calls.Load(), job.Comments)
			}
			if test.comments == 1 {
				comment := job.Comments[0]
				if comment.VerificationReason != saved.Assessment.Reason || comment.TraceID != saved.TraceID {
					t.Fatalf("resume lost assessment metadata: %#v", comment)
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
			reply = testFindingVerdictJSON(findingConfirmed, "源码定义可见")
		}
		if call > 2 {
			reply = "[]"
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
	if job.Status != "completed" || len(job.Comments) != 1 || calls.Load() != 3 {
		t.Fatalf("status=%q comments=%d model calls=%d error=%q", job.Status, len(job.Comments), calls.Load(), job.Error)
	}
	if !strings.Contains(verificationPrompt, "related fetch at service.py:1") ||
		!strings.Contains(verificationPrompt, "def fetch():") {
		t.Fatalf("verification omitted related pinned source: %q", verificationPrompt)
	}
}

func TestReviewResumeContinuesAtUnfinishedFindingAndRestoresBudget(t *testing.T) {
	var calls atomic.Int32
	modelAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := calls.Add(1)
		reply := testFindingVerdictJSON(findingConfirmed, "复核确认")
		if call > 1 {
			reply = "[]"
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{
				"message": map[string]any{
					"role": "assistant", "content": reply,
				},
				"finish_reason": "stop",
			}},
		})
	}))
	defer modelAPI.Close()

	root := t.TempDir()
	storeDir := filepath.Join(root, "jobs")
	checkpoint := newReviewCheckpoint(model.ReviewRequest{Source: "inline"})
	checkpoint.Stage = reviewStageVerification
	checkpoint.Diff = "diff --git a/a.py b/a.py\n--- a/a.py\n+++ b/a.py\n@@ -0,0 +1,2 @@\n+bug_a()\n+bug_b()\n"
	checkpoint.Artifacts = ReviewArtifacts{
		Files:      []ChangedFile{{Path: "a.py", AddedLines: []int{1, 2}}},
		AddedLines: 2, Checks: []PreflightCheck{},
	}
	checkpoint.Candidates = []ReviewFinding{
		{File: "a.py", Line: 1, Severity: "medium", Confidence: "medium", Body: "bug a", Evidence: "bug_a()", Trigger: "call", Impact: "fails", Suggestion: "fix"},
		{File: "a.py", Line: 2, Severity: "medium", Confidence: "medium", Body: "bug b", Evidence: "bug_b()", Trigger: "call", Impact: "fails", Suggestion: "fix"},
	}
	checkpoint.ModelTraceID = "first-pass-trace"
	checkpoint.VerificationCursor = 1
	checkpoint.VerificationResults = []checkpointVerification{{
		Finding: ReviewFinding{
			File: "a.py", Line: 1, Severity: "medium", Confidence: "medium", Body: "bug a",
			Evidence: "bug_a()", Trigger: "call", Impact: "fails", Suggestion: "fix",
			VerificationStatus: "second_pass_review_passed", VerificationReason: "already checked",
		},
		Verdict: string(findingConfirmed),
	}}
	job := &model.ReviewJob{
		ID: "resume-fixture", Status: "running", Source: "inline", BudgetYuan: 10,
		BudgetMicros: 10_000_000, SpentMicros: 1200, ReservedMicros: 500,
		Comments: []model.ReviewComment{}, Trace: []model.TraceEvent{}, Todos: []model.TodoItem{},
	}
	if err := persistReviewCheckpoint(job, checkpoint); err != nil {
		t.Fatal(err)
	}
	if err := dao.NewJobStore(storeDir).Save(job); err != nil {
		t.Fatal(err)
	}

	store := dao.NewJobStore(storeDir)
	service := NewService(store, Config{
		SkillsDir: "../../skills", MemoryDir: filepath.Join(root, "memory"),
		TasksDir: filepath.Join(root, "tasks"), BackgroundTasksDir: filepath.Join(root, "background"),
		TeamMailboxDir: filepath.Join(root, "team"), CronFile: filepath.Join(root, "cron.json"),
		DeepSeekAPIKey: "test-only", DeepSeekBaseURL: modelAPI.URL,
	})
	if _, err := service.ResumeReview(job.ID); err != nil {
		t.Fatalf("resume review: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		updated, found := store.Get(job.ID)
		if found && (updated.Status == "completed" || updated.Status == "completed_with_warnings" || updated.Status == "failed") {
			if updated.Status != "completed" || len(updated.Comments) != 2 {
				t.Fatalf("resume status=%q comments=%d error=%q", updated.Status, len(updated.Comments), updated.Error)
			}
			if calls.Load() != 2 {
				t.Fatalf("model calls=%d, want one unfinished finding and one memory extraction", calls.Load())
			}
			if updated.SpentMicros < 1700 || updated.ReservedMicros != 0 {
				t.Fatalf("budget spent=%d reserved=%d; interrupted reservation was not restored", updated.SpentMicros, updated.ReservedMicros)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("resumed review did not reach a terminal state")
}

func TestServiceStartResumesPersistedFinalizingReview(t *testing.T) {
	root := t.TempDir()
	store := dao.NewJobStore(filepath.Join(root, "jobs"))
	checkpoint := newReviewCheckpoint(model.ReviewRequest{Source: "inline"})
	checkpoint.Stage = reviewStageFinalizing
	job := &model.ReviewJob{
		ID: "startup-resume-fixture", Status: "running", Source: "inline",
		BudgetYuan: 10, BudgetMicros: 10_000_000,
		Comments: []model.ReviewComment{}, Trace: []model.TraceEvent{}, Todos: []model.TodoItem{},
	}
	if err := persistReviewCheckpoint(job, checkpoint); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(job); err != nil {
		t.Fatal(err)
	}
	store = dao.NewJobStore(filepath.Join(root, "jobs"))
	service := NewService(store, Config{
		SkillsDir: "../../skills", MemoryDir: filepath.Join(root, "memory"),
		TasksDir: filepath.Join(root, "tasks"), BackgroundTasksDir: filepath.Join(root, "background"),
		TeamMailboxDir: filepath.Join(root, "team"), CronFile: filepath.Join(root, "cron.json"),
	})
	if err := service.Start(); err != nil {
		t.Fatalf("start with recoverable review: %v", err)
	}
	defer service.Stop()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		updated, found := store.Get(job.ID)
		if found && updated.Status == "completed" {
			if updated.ReviewOutcome != "completed_no_findings" {
				t.Fatalf("review outcome=%q, want completed_no_findings", updated.ReviewOutcome)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("service startup did not resume the finalizing review")
}

func TestReviewCheckpointRedactsRequestSecrets(t *testing.T) {
	checkpoint := newReviewCheckpoint(model.ReviewRequest{
		Source:      "inline",
		Diff:        "raw diff must not be persisted",
		MemoryQuery: `api_key="checkpoint-query-secret"`,
		Goal:        `token="checkpoint-goal-secret"`,
	})
	if checkpoint.Request.Diff != "" {
		t.Fatalf("raw diff was retained in checkpoint request: %q", checkpoint.Request.Diff)
	}
	encoded, err := json.Marshal(checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"checkpoint-query-secret", "checkpoint-goal-secret", "raw diff must not be persisted"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("checkpoint contains sensitive or duplicate request data %q: %s", secret, encoded)
		}
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

func reviewCheckStatus(scope model.ReviewScope, name string) string {
	for _, check := range scope.Checks {
		if check.Name == name {
			return check.Status
		}
	}
	return ""
}
