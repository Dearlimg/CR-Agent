package main

import (
	"CR-Agent/internal/logic"
	"CR-Agent/internal/model"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestValidateReviewRoute(t *testing.T) {
	for _, route := range []string{"auto", "single", "specialists"} {
		if err := validateReviewRoute(route); err != nil {
			t.Errorf("validateReviewRoute(%q): %v", route, err)
		}
	}
	if err := validateReviewRoute("unknown"); err == nil {
		t.Fatal("unknown route should be rejected")
	}
}

func TestRenderMarkdownIncludesPerCaseUsage(t *testing.T) {
	report := runReport{
		Version:   "code-review-v1",
		Route:     "specialists",
		StartedAt: time.Date(2026, 9, 23, 0, 0, 0, 0, time.UTC),
		Results: []caseResult{{
			ID: "nil-dereference", Status: "completed", TruePositives: 1,
			DurationMs: 1234, Usage: proxyUsage{Forwarded: 3, Unknown: 3},
		}},
	}
	markdown := renderMarkdown(report)
	for _, want := range []string{
		"- Route: `specialists`",
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
	base := logic.Config{MemoryDir: "shared", ReviewModeOverride: "specialists"}
	first := isolatedCaseConfig(base, "case-1")
	second := isolatedCaseConfig(base, "case-2")
	if first.MemoryDir == second.MemoryDir || first.TasksDir == second.TasksDir || first.TeamMailboxDir == second.TeamMailboxDir {
		t.Fatal("benchmark cases share persistent state")
	}
	if base.MemoryDir != "shared" || first.ReviewModeOverride != "specialists" || second.ReviewModeOverride != "specialists" {
		t.Fatal("case isolation changed the base configuration or route")
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
