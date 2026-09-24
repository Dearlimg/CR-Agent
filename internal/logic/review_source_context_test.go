package logic

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReviewSourceSnapshotPinsHeadAndSuppliesRelatedDefinition(t *testing.T) {
	const headSHA = "3c4a85732eb696f8e03442de8b0ebb02a7a3ae4c"
	const fakeSecret = "not-a-real-secret-123"
	requested := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = append(requested, r.URL.RequestURI())
		if r.Header.Get("Authorization") != "" {
			t.Error("token was sent to non-GitHub test host")
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/base/repo/pulls/195":
			_, _ = w.Write([]byte(`{"head":{"sha":"` + headSHA + `","repo":{"full_name":"fork/repo"}}}`))
		case "/repos/fork/repo/contents/pkg/agent.py":
			writeReviewSourceFile(w, "class Agent:\n    def get_app(self, app_code):\n        return app_service.sync_app_detail(app_code)\n")
		case "/repos/fork/repo/contents/pkg/service.py":
			writeReviewSourceFile(w, "class AppService:\n    def sync_app_detail(self, app_code):\n        password = \""+fakeSecret+"\"\n        return self.dao.get(app_code)\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	files, err := loadReviewSourceSnapshot(
		context.Background(),
		"https://api.github.com/repos/base/repo/pulls/195",
		Config{GitHubAPIBase: server.URL, GitHubToken: "test-only-token"},
		[]string{"pkg/agent.py", "pkg/service.py"},
	)
	if err != nil || len(files) != 2 {
		t.Fatalf("files=%d err=%v", len(files), err)
	}
	for _, request := range requested[1:] {
		if !strings.Contains(request, "ref="+headSHA) || !strings.HasPrefix(request, "/repos/fork/repo/contents/") {
			t.Errorf("source request not pinned to fork head: %q", request)
		}
	}
	if strings.Contains(files["pkg/service.py"], fakeSecret) {
		t.Fatal("credential value entered source snapshot")
	}
	finding := ReviewFinding{
		File: "pkg/agent.py", Line: 3,
		Evidence: "        return app_service.sync_app_detail(app_code)",
	}
	excerpt := findingSourceExcerpt(files, finding, 6000)
	if !strings.Contains(excerpt, "def sync_app_detail") ||
		!strings.Contains(excerpt, "pkg/service.py:2") || strings.Contains(excerpt, fakeSecret) {
		t.Fatalf("related definition or redaction missing: %q", excerpt)
	}
	finding.Evidence = "wrong snapshot line"
	if excerpt := findingSourceExcerpt(files, finding, 6000); excerpt != "" {
		t.Fatalf("stale snapshot should not be presented: %q", excerpt)
	}
}

func TestReviewSourceSnapshotRejectsUnsafePathsAndRedirects(t *testing.T) {
	if _, _, err := reviewSourceAPIBase("https://example.com"); err == nil {
		t.Fatal("repository token could be forwarded to a non-GitHub API host")
	}
	for _, filePath := range []string{"../secret", "/root/file", "dir/../file", "C:/file", "a\\b"} {
		if reviewSourceValidPath(filePath) {
			t.Errorf("unsafe path accepted: %q", filePath)
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", "https://example.com/other")
		w.WriteHeader(http.StatusFound)
	}))
	defer server.Close()
	_, err := loadReviewSourceSnapshot(
		context.Background(), "https://github.com/base/repo/pull/195",
		Config{GitHubAPIBase: server.URL}, []string{"pkg/file.go"},
	)
	if err == nil || !strings.Contains(err.Error(), "302") {
		t.Fatalf("redirect should fail without following: %v", err)
	}
	if files, err := loadReviewSourceSnapshot(context.Background(), "inline", Config{}, []string{"a.go"}); err != nil || files != nil {
		t.Fatalf("inline review should skip remote source: files=%v err=%v", files, err)
	}
}

func TestReviewSourceSnapshotKeepsUsableFilesWhenOneIsOversized(t *testing.T) {
	const headSHA = "3c4a85732eb696f8e03442de8b0ebb02a7a3ae4c"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/base/repo/pulls/1":
			_, _ = w.Write([]byte(`{"head":{"sha":"` + headSHA + `"}}`))
		case "/repos/base/repo/contents/big.go":
			writeReviewSourceFile(w, strings.Repeat("x", reviewSourceMaxFileBytes+1))
		case "/repos/base/repo/contents/small.go":
			writeReviewSourceFile(w, "package small\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	files, err := loadReviewSourceSnapshot(
		context.Background(), "https://api.github.com/repos/base/repo/pulls/1",
		Config{GitHubAPIBase: server.URL}, []string{"big.go", "small.go"},
	)
	if err == nil || len(files) != 1 || files["small.go"] != "package small\n" {
		t.Fatalf("partial source context was lost or size limit ignored: files=%d err=%v", len(files), err)
	}
}

func TestReviewSourcePathsAreBounded(t *testing.T) {
	paths := make([]string, 0, reviewSourceMaxFiles+1)
	for i := range reviewSourceMaxFiles + 1 {
		paths = append(paths, "pkg/file"+string(rune('a'+i))+".go")
	}
	selected, err := reviewSourcePaths(paths)
	if err != nil || len(selected) != reviewSourceMaxFiles {
		t.Fatalf("selected=%d err=%v", len(selected), err)
	}
}

func writeReviewSourceFile(w http.ResponseWriter, content string) {
	_ = json.NewEncoder(w).Encode(map[string]any{
		"type": "file", "encoding": "base64", "size": len(content),
		"content": base64.StdEncoding.EncodeToString([]byte(content)),
	})
}
