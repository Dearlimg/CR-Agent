package logic

import (
	"CR-Agent/internal/dao"
	"CR-Agent/internal/model"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

const reviewFixture = "diff --git a/go.mod b/go.mod\n" +
	"index 1111111..2222222 100644\n--- a/go.mod\n+++ b/go.mod\n" +
	"@@ -1 +1,2 @@\n module example.com/demo\n+require example.com/dependency v1.2.3\n" +
	"diff --git a/main.go b/main.go\nindex 3333333..4444444 100644\n" +
	"--- a/main.go\n+++ b/main.go\n@@ -1,2 +1,4 @@\n package main\n panic(\"old context\")\n" +
	"+// added comment\n+api_key = \"definitely-fake-value-123\"\n"

func TestPreflightScansRawDiffAndSharesOnlyRedactedEvidence(t *testing.T) {
	artifacts, err := NewPreflightCache().Run(context.Background(), "repo/pr/1", reviewFixture)
	if err != nil {
		t.Fatal(err)
	}
	if len(artifacts.SecretFindings) != 1 || artifacts.SecretFindings[0].Line != 4 {
		t.Fatalf("secret findings=%#v", artifacts.SecretFindings)
	}
	if len(artifacts.DependencyFiles) != 1 || artifacts.DependencyFiles[0] != "go.mod" {
		t.Fatalf("dependency files=%#v", artifacts.DependencyFiles)
	}
	if artifacts.AddedLines != 3 {
		t.Fatalf("diff added lines=%d, want 3", artifacts.AddedLines)
	}
	for _, exposed := range []string{
		artifacts.SanitizedDiff, artifacts.PromptSummary(), artifacts.TraceSummary(),
	} {
		if strings.Contains(exposed, "definitely-fake-value-123") {
			t.Fatalf("secret value entered shared output: %q", exposed)
		}
	}
	for _, check := range artifacts.Checks {
		if check.Name == "static_check" && !strings.Contains(check.Message, "提示=0") {
			t.Fatalf("context line was treated as added code: %#v", check)
		}
	}
}

func TestRedactionPreservesPatchStructureAndModelJSON(t *testing.T) {
	diff := "diff --git a/secret.go b/secret.go\n+++ b/secret.go\n" +
		"@@ -0,0 +1 @@\n+api-key=definitely-fake-value-123\n"
	sanitized := sanitizeDiff(diff)
	if !strings.Contains(sanitized, "diff --git a/secret.go b/secret.go") ||
		!strings.Contains(sanitized, "+[REDACTED]") ||
		strings.Contains(sanitized, "definitely-fake-value-123") {
		t.Fatalf("sanitized diff=%q", sanitized)
	}
	reply := `[{"file":"secret.go","line":1,"severity":"high","confidence":"low","body":"hardcoded api-key=definitely-fake-value-123","suggestion":"remove it"}]`
	safe := sanitizeModelReply(reply)
	if strings.Contains(safe, "definitely-fake-value-123") || len(parseFindings(safe)) != 1 {
		t.Fatalf("sanitized reply=%q", safe)
	}
	findings, err := scanRawDiff(context.Background(), "api_key=definitely-fake-value-123")
	if err != nil || len(findings) != 1 {
		t.Fatalf("unstructured input findings=%#v err=%v", findings, err)
	}
}

func TestNewGoFileReceivesRealSyntaxAndFormatChecks(t *testing.T) {
	diff := "diff --git a/new.go b/new.go\nnew file mode 100644\n" +
		"--- /dev/null\n+++ b/new.go\n@@ -0,0 +1,2 @@\n+package main\n+func f(){ }\n"
	artifacts, err := NewPreflightCache().Run(context.Background(), "inline", diff)
	if err != nil {
		t.Fatal(err)
	}
	statuses := map[string]string{}
	for _, check := range artifacts.Checks {
		statuses[check.Name] = check.Status
	}
	if statuses["syntax_check"] != "passed" || statuses["format_check"] != "failed" {
		t.Fatalf("check statuses=%#v", statuses)
	}
}

func TestMalformedModelFindingsAreNotReportedAsNoIssues(t *testing.T) {
	if _, err := parseFindingsStrict("analysis complete"); err == nil {
		t.Fatal("malformed model output must be reported as incomplete")
	}
	if findings, err := parseFindingsStrict("[]"); err != nil || len(findings) != 0 {
		t.Fatalf("empty JSON array findings=%#v err=%v", findings, err)
	}
}

func TestPreflightCacheReusesMetadataAndInvalidatesChangedDiff(t *testing.T) {
	cache := NewPreflightCache()
	var wg sync.WaitGroup
	results := make(chan bool, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			artifacts, err := cache.Run(context.Background(), "repo/pr/1", reviewFixture)
			if err != nil {
				t.Errorf("preflight: %v", err)
				return
			}
			results <- artifacts.CacheHit
		}()
	}
	wg.Wait()
	close(results)
	misses := 0
	for hit := range results {
		if !hit {
			misses++
		}
	}
	if misses != 1 {
		t.Fatalf("cache misses=%d, want 1", misses)
	}
	changed, err := cache.Run(context.Background(), "repo/pr/1", reviewFixture+"\n+extra")
	if err != nil || changed.CacheHit {
		t.Fatalf("changed diff cache hit=%v err=%v", changed.CacheHit, err)
	}
}

func TestPreflightRunsAsOneOrchestratorTool(t *testing.T) {
	service := NewService(dao.NewJobStore(t.TempDir()), Config{})
	job := &model.ReviewJob{
		ID: "preflight-job", Source: "repo/pr/1", Trace: []model.TraceEvent{},
		Todos: []model.TodoItem{{Content: "preflight", Status: "pending", Order: 0}},
	}
	recorder := newTraceRecorder(job)
	artifacts := &ReviewArtifacts{}
	if err := service.Loop.Run(context.Background(), ToolInput{
		Job: job, Diff: reviewFixture, Tracer: recorder, Artifacts: artifacts,
	}); err != nil {
		t.Fatal(err)
	}
	recorder.Flush()
	if len(job.Trace) != 1 || job.Trace[0].Tool != "preflight_analysis" || job.Trace[0].Origin != "orchestrator" {
		t.Fatalf("unexpected trace=%#v", job.Trace)
	}
	if strings.Contains(job.Trace[0].Output, "definitely-fake-value-123") {
		t.Fatal("trace contains credential value")
	}
}

func TestReviewRoutingAndFindingVerification(t *testing.T) {
	artifacts, err := NewPreflightCache().Run(context.Background(), "repo/pr/1", reviewFixture)
	if err != nil {
		t.Fatal(err)
	}
	if chooseReviewMode(artifacts) != "specialists" {
		t.Fatal("dependency plus source change should use specialists")
	}
	artifacts.DependencyFiles = []string{}
	if chooseReviewMode(artifacts) != "single" {
		t.Fatal("small source change should use one agent")
	}
	findings := []ReviewFinding{
		{File: "main.go", Line: 3, Severity: "HIGH", Confidence: "HIGH", Body: "具体问题"},
		{File: "main.go", Line: 3, Severity: "HIGH", Confidence: "HIGH", Body: "具体问题"},
		{File: "main.go", Line: 2, Severity: "high", Confidence: "high", Body: "上下文行不是新增"},
		{File: "unknown.go", Line: 1, Severity: "high", Confidence: "high", Body: "错误文件"},
	}
	comments := verifiedComments(findings, artifacts, "trace")
	if len(comments) != 1 || comments[0].Line != 3 || comments[0].Severity != "high" {
		t.Fatalf("verified comments=%#v", comments)
	}
}

func TestSmallReviewUsesOneModelTurnAndSharedPreflight(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var request struct {
			Tools []struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode model request: %v", err)
		}
		for _, tool := range request.Tools {
			switch tool.Function.Name {
			case "parse_diff", "get_changed_lines", "syntax_check", "format_check", "secret_scan", "dependency_diff":
				t.Errorf("deterministic tool exposed to model: %s", tool.Function.Name)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "test", "object": "chat.completion",
			"choices": []any{map[string]any{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": `[ {"file":"main.go","line":2,"severity":"medium","confidence":"low","body":"示例问题","suggestion":"修复"} ]`,
				},
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
		ID: "one-agent", Source: "inline", Trace: []model.TraceEvent{},
		Comments: []model.ReviewComment{}, Todos: []model.TodoItem{
			{Order: 0, Status: "pending"}, {Order: 1, Status: "pending"}, {Order: 2, Status: "pending"},
		},
	}
	diff := "diff --git a/main.go b/main.go\n--- a/main.go\n+++ b/main.go\n" +
		"@@ -1 +1,2 @@\n package main\n+func f() {}\n"
	service.run(context.Background(), job, model.ReviewRequest{Diff: diff})
	if job.Status != "completed" {
		t.Fatalf("status=%q error=%q", job.Status, job.Error)
	}
	if calls.Load() != 1 {
		t.Fatalf("model calls=%d, want 1", calls.Load())
	}
	if len(job.Comments) != 1 || job.Comments[0].File != "main.go" || job.Comments[0].Line != 2 {
		t.Fatalf("comments=%#v", job.Comments)
	}
	preflightCalls := 0
	for _, event := range job.Trace {
		if event.Tool == "preflight_analysis" {
			preflightCalls++
		}
	}
	if preflightCalls != 1 {
		t.Fatalf("preflight calls=%d, want 1", preflightCalls)
	}
}

func TestLargeReviewUsesSpecialistsAndOneSynthesis(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "test", "object": "chat.completion",
			"choices": []any{map[string]any{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": "[]"},
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
		ID: "specialist-review", Source: "inline", Trace: []model.TraceEvent{},
		Comments: []model.ReviewComment{}, Todos: []model.TodoItem{
			{Order: 0, Status: "pending"}, {Order: 1, Status: "pending"}, {Order: 2, Status: "pending"},
		},
		TeamEvents: []model.TeamEvent{},
	}
	var diff strings.Builder
	diff.WriteString("diff --git a/main.go b/main.go\n--- a/main.go\n+++ b/main.go\n@@ -0,0 +1,300 @@\n")
	for index := range 300 {
		fmt.Fprintf(&diff, "+// added line %d\n", index)
	}
	service.run(context.Background(), job, model.ReviewRequest{Diff: diff.String()})
	if job.Status != "completed" || calls.Load() != 4 {
		t.Fatalf("status=%q model calls=%d error=%q", job.Status, calls.Load(), job.Error)
	}
	preflightCalls := 0
	for _, event := range job.Trace {
		if event.Tool == "preflight_analysis" {
			preflightCalls++
		}
	}
	if preflightCalls != 1 || len(job.TeamEvents) != 6 {
		t.Fatalf("preflight calls=%d team events=%d", preflightCalls, len(job.TeamEvents))
	}
}
