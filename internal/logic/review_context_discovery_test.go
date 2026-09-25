package logic

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReviewContextDiscoversThenReadsPinnedUnchangedCode(t *testing.T) {
	const sha = "3c4a85732eb696f8e03442de8b0ebb02a7a3ae4c"
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Query().Get("ref") != sha || r.Header.Get("Authorization") != "" {
			t.Errorf("unpinned request or leaked credential: %s", r.URL)
		}
		switch r.URL.Path {
		case "/repos/base/repo/contents/":
			_, _ = w.Write([]byte(`[{"type":"dir","path":"internal"}]`))
		case "/repos/base/repo/contents/internal/orders":
			_, _ = w.Write([]byte(`[{"type":"file","path":"internal/orders/store.go"},{"type":"symlink","path":"internal/orders/link"},{"type":"file","path":"../escape"}]`))
		case "/repos/base/repo/contents/internal/orders/store.go":
			writeReviewSourceFile(w, "package orders\n// Debit can succeed before acknowledgement is lost.\nfunc Debit() error { return ErrTimeout }\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	base, _, err := reviewSourceAPIBase(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := &reviewSourceSnapshot{
		base: base, owner: "base", repo: "repo", headSHA: sha,
		client: server.Client(), files: map[string]string{},
	}
	policy := DefaultPermissionPolicy()
	for _, directory := range []string{".", "internal/orders"} {
		result, err := runReviewContextTool(context.Background(), snapshot, policy, map[string]any{"directory": directory})
		if err != nil || !strings.Contains(result, "internal") || strings.Contains(result, "escape") || strings.Contains(result, "symlink") {
			t.Fatalf("directory=%s result=%s err=%v", directory, result, err)
		}
	}
	result, err := runReviewContextTool(context.Background(), snapshot, policy, map[string]any{
		"file": "internal/orders/store.go", "start_line": float64(2), "end_line": float64(3),
	})
	if err != nil || !strings.Contains(result, "acknowledgement is lost") || !strings.Contains(result, sha) {
		t.Fatalf("source=%s err=%v", result, err)
	}
	if requests != 3 {
		t.Fatalf("requests=%d", requests)
	}
	for _, args := range []map[string]any{
		{"directory": "../private"}, {"directory": ""},
		{"directory": ".", "file": "store.go"}, {},
	} {
		if _, err := runReviewContextTool(context.Background(), snapshot, policy, args); err == nil {
			t.Errorf("accepted invalid arguments: %#v", args)
		}
	}
	policy.grants[PermissionNetworkFetch] = PermissionDeny
	if _, err := runReviewContextTool(context.Background(), snapshot, policy, map[string]any{"directory": "."}); err == nil {
		t.Fatal("directory browsing bypassed network permission")
	}
	if requests != 3 {
		t.Fatal("invalid or denied request accessed network")
	}
}
