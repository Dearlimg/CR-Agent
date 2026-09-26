package main

import (
	"CR-Agent/internal/logic"
	"CR-Agent/internal/model"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestValidateRecordedRoute(t *testing.T) {
	for _, route := range []string{"auto", "single", "specialists"} {
		if err := validateRecordedRoute(route); err != nil {
			t.Errorf("validateRecordedRoute(%q): %v", route, err)
		}
	}
	if err := validateRecordedRoute("unknown"); err == nil {
		t.Fatal("unknown recorded route should be rejected")
	}
}

func TestRenderMarkdownIncludesPerCaseUsage(t *testing.T) {
	report := runReport{
		Version:   "code-review-v1",
		Route:     "single",
		StartedAt: time.Date(2026, 9, 23, 0, 0, 0, 0, time.UTC),
		Results: []caseResult{{
			ID: "nil-dereference", Status: "completed", TruePositives: 1,
			DurationMs: 1234, Usage: proxyUsage{Forwarded: 3, Unknown: 3},
		}},
	}
	markdown := renderMarkdown(report)
	for _, want := range []string{
		"- Route: `single`",
		"| Model requests | Reported tokens |",
		"| `nil-dereference` |",
		"1234 ms | 3 | unknown (3 responses)",
	} {
		if !strings.Contains(markdown, want) {
			t.Errorf("report missing %q", want)
		}
	}
}

func TestIsolatedCaseConfigSeparatesMemoryAndState(t *testing.T) {
	base := logic.Config{MemoryDir: "shared"}
	first := isolatedCaseConfig(base, "case-1")
	second := isolatedCaseConfig(base, "case-2")
	if first.MemoryDir == second.MemoryDir || first.TasksDir == second.TasksDir || first.TeamMailboxDir == second.TeamMailboxDir {
		t.Fatal("benchmark cases share persistent state")
	}
	if base.MemoryDir != "shared" {
		t.Fatal("case isolation changed the base configuration")
	}
}

func TestScoreMatchesOnlyGroundedFinding(t *testing.T) {
	sample := testCase{
		Expected: []expectedFinding{{
			File: "internal/a.go", Line: 12,
			MatchGroups: [][]string{{"panic", "崩溃"}, {"nil", "空指针"}},
		}},
	}
	predictions := []model.ReviewComment{
		{File: "internal/a.go", Line: 13, Body: "空指针会导致 panic"},
		{File: "internal/a.go", Line: 30, Body: "不相关的问题"},
	}
	tp, fp, fn := score(sample, predictions)
	if tp != 1 || fp != 1 || fn != 0 {
		t.Fatalf("score=(%d,%d,%d), want (1,1,0)", tp, fp, fn)
	}
}

func TestApplyJudgeVerdictsSeparatesAuditChannels(t *testing.T) {
	result := &realCaseResult{
		References: []realReference{
			{File: "a.go", Line: 10, Kind: "defect", Status: "valid"},
			{File: "b.go", Line: 20, Kind: "defect", Status: "fixed_in_snapshot"},
			{File: "c.go", Line: 30, Kind: "suggestion", Status: "suggestion"},
			{File: "d.go", Line: 40, Kind: "design", Status: "unverifiable"},
		},
		Findings: []model.ReviewComment{
			{File: "a.go", Line: 11, Body: "covers the valid defect"},
			{File: "b.go", Line: 21, Body: "re-flags the already-fixed issue"},
			{File: "c.go", Line: 31, Body: "matches the suggestion"},
			{File: "zz.go", Line: 99, Body: "unrelated valid issue"},
		},
		RefVerdicts: []judgeRefVerdict{
			{Index: 0, Verdict: "covered", CoveredBy: []int{0}},
			{Index: 1, Verdict: "re_flagged", CoveredBy: []int{1}},
			{Index: 2, Verdict: "covered", CoveredBy: []int{2}},
			{Index: 3, Verdict: "skipped"},
		},
		FindingVerdict: []judgeFindingVerdict{
			{Index: 0, Verdict: "matched_reference"},
			{Index: 1, Verdict: "invalid"},
			{Index: 2, Verdict: "matched_reference"},
			{Index: 3, Verdict: "valid_new_issue"},
		},
	}
	applyJudgeVerdicts(result)
	if result.CoveredRefs != 1 || result.PartialRefs != 0 || result.MissedRefs != 0 {
		t.Errorf("core coverage=(%d,%d,%d), want (1,0,0)", result.CoveredRefs, result.PartialRefs, result.MissedRefs)
	}
	if result.FixedHits != 1 || result.FixedRefs != 1 {
		t.Errorf("fixed refs/hits=(%d,%d), want (1,1)", result.FixedRefs, result.FixedHits)
	}
	if result.SuggestionCovered != 1.0 {
		t.Errorf("suggestion covered=%.1f, want 1.0", result.SuggestionCovered)
	}
	if result.SkippedRefs != 1 {
		t.Errorf("skipped=%d, want 1", result.SkippedRefs)
	}
	// Finding 0 matched the valid reference; finding 1 re-flagged a fixed
	// issue and must be a false positive despite being anchorable; finding 2
	// matched the suggestion channel; finding 3 is a judge-confirmed novel issue.
	if result.Matched != 2 || result.Novel != 1 || result.FalsePositives != 1 {
		t.Errorf("findings matched/novel/fp=(%d,%d,%d), want (2,1,1)", result.Matched, result.Novel, result.FalsePositives)
	}
	if result.CoverageScore != 1.0 {
		t.Errorf("coverage score=%.1f, want 1.0", result.CoverageScore)
	}
}

func TestApplyJudgeVerdictsQuietOnFixedIssue(t *testing.T) {
	result := &realCaseResult{
		References: []realReference{
			{File: "b.go", Line: 20, Kind: "defect", Status: "fixed_in_snapshot"},
		},
		Findings: []model.ReviewComment{},
		RefVerdicts: []judgeRefVerdict{
			{Index: 0, Verdict: "quiet"},
		},
	}
	applyJudgeVerdicts(result)
	if result.FixedHits != 0 {
		t.Errorf("fixed hits=%d, want 0 (system stayed quiet)", result.FixedHits)
	}
}

func TestFinalizeRealReportCoreDenominator(t *testing.T) {
	result := realCaseResult{
		References: []realReference{
			{Status: "valid"},
			{Status: "suggestion"},
			{Status: "fixed_in_snapshot"},
		},
		RefVerdicts: []judgeRefVerdict{
			{Index: 0, Verdict: "partial"},
			{Index: 1, Verdict: "covered"},
			{Index: 2, Verdict: "quiet"},
		},
	}
	applyJudgeVerdicts(&result)
	report := &realRunReport{
		DimensionCoverage: map[string]*dimCoverage{},
		ScenarioMetrics:   map[string]*scenarioMetric{},
		Results:           []realCaseResult{result},
	}
	finalizeRealReport(report)
	if report.CoreRefs != 1 || report.SuggestionRefs != 1 || report.FixedRefs != 1 {
		t.Fatalf("channel refs core/sugg/fixed=(%d,%d,%d), want (1,1,1)", report.CoreRefs, report.SuggestionRefs, report.FixedRefs)
	}
	if report.Recall != 0.5 {
		t.Errorf("core recall=%.2f, want 0.5 (partial counts 0.5 of the single core ref)", report.Recall)
	}
	if report.SuggestionCoverage != 1.0 {
		t.Errorf("suggestion coverage=%.2f, want 1.0", report.SuggestionCoverage)
	}
}

func TestRefChannelDefaultsLegacyToValid(t *testing.T) {
	if got := refChannel(realReference{Status: ""}); got != "valid" {
		t.Errorf("legacy reference channel=%q, want valid", got)
	}
	if got := refChannel(realReference{Status: "bogus"}); got != "valid" {
		t.Errorf("bogus status channel=%q, want valid", got)
	}
	if got := refChannel(realReference{Status: "fixed_in_snapshot"}); got != "fixed_in_snapshot" {
		t.Errorf("fixed channel=%q, want fixed_in_snapshot", got)
	}
}

func TestLoadRealCasesRejectsTamperedSnapshot(t *testing.T) {
	dir := t.TempDir()
	diff := "diff --git a/x.go b/x.go\n--- a/x.go\n+++ b/x.go\n@@ -1,2 +1,3 @@\n line one\n+line two\n"
	diffPath := filepath.Join(dir, "x.diff")
	if err := os.WriteFile(diffPath, []byte(diff), 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(diff))
	base := realCase{
		ID: "case-1", PrURL: "https://github.com/o/r/pull/1", Scenario: "bugfix",
		DiffFile: "x.diff", DiffSHA256: hex.EncodeToString(digest[:]),
		ReferenceComments: []realReference{{
			File: "x.go", Line: 2, Body: "body", Dimension: "correctness", Kind: "defect", Status: "valid",
		}},
	}
	manifest := []realCase{base}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(dir, "manifest.json")
	if err := os.WriteFile(manifestPath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadRealCases(manifestPath); err != nil {
		t.Fatalf("valid manifest rejected: %v", err)
	}

	// Tampering with the snapshot must fail validation: the audit refers to
	// the pinned content.
	if err := os.WriteFile(diffPath, []byte(diff+"\n+line three\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadRealCases(manifestPath); err == nil {
		t.Fatal("tampered snapshot accepted")
	}

	// Missing or invalid status must fail validation.
	if err := os.WriteFile(diffPath, []byte(diff), 0o600); err != nil {
		t.Fatal(err)
	}
	noStatus := base
	noStatus.ReferenceComments[0].Status = ""
	encoded, _ = json.Marshal([]realCase{noStatus})
	if err := os.WriteFile(manifestPath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadRealCases(manifestPath); err == nil {
		t.Fatal("reference without audit status accepted")
	}
}

func TestGuardedProxyCapsRequestsAndOutputTokens(t *testing.T) {
	upstreamCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls++
		if r.Header.Get("Authorization") != "Bearer test-only" {
			t.Errorf("authorization header was not forwarded")
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode upstream request: %v", err)
		}
		if payload["max_tokens"] != float64(16) {
			t.Errorf("max_tokens=%v, want 16", payload["max_tokens"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"[]"}}],"usage":{"total_tokens":7}}`))
	}))
	defer upstream.Close()

	target, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	guard := newGuardedProxy(target, 1, 16, 1024)
	proxy := httptest.NewServer(guard)
	defer proxy.Close()

	request, err := http.NewRequest(http.MethodPost, proxy.URL+"/v1/chat/completions", strings.NewReader(`{"max_tokens":4096,"messages":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer test-only")
	first, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	first.Body.Close()
	if first.StatusCode != http.StatusOK {
		t.Fatalf("first request status=%d", first.StatusCode)
	}

	request, err = http.NewRequest(http.MethodPost, proxy.URL+"/v1/chat/completions", strings.NewReader(`{"messages":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	second, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	second.Body.Close()
	if second.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("second request status=%d, want %d", second.StatusCode, http.StatusTooManyRequests)
	}
	if upstreamCalls != 1 {
		t.Fatalf("upstream calls=%d, want 1", upstreamCalls)
	}
	usage := guard.snapshot()
	if usage.Forwarded != 1 || usage.Rejected != 1 || usage.Tokens != 7 {
		t.Fatalf("proxy usage=%+v", usage)
	}
}
