package logic

import (
	"strings"
	"testing"
)

func TestSplitReviewDiffPacksWholeFiles(t *testing.T) {
	diff := "diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -0,0 +1 @@\n+package a\n" +
		"diff --git a/b.go b/b.go\n--- a/b.go\n+++ b/b.go\n@@ -0,0 +1 @@\n+package b\n"

	chunks, err := splitReviewDiff(diff, len(diff)-1)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 2 {
		t.Fatalf("got %d chunks, want 2", len(chunks))
	}
	for index, chunk := range chunks {
		if strings.Count(chunk, "diff --git ") != 1 {
			t.Fatalf("chunk %d split or combined file boundaries: %q", index, chunk)
		}
		if !strings.Contains(chunk, "@@ -0,0 +1 @@") {
			t.Fatalf("chunk %d lost hunk header: %q", index, chunk)
		}
	}
}

func TestSplitReviewDiffSplitsBetweenHunksWithFileHeader(t *testing.T) {
	header := "diff --git a/a.go b/a.go\nindex 1..2 100644\n--- a/a.go\n+++ b/a.go\n"
	firstHunk := "@@ -0,0 +1,2 @@\n+package a\n+var A = 1\n"
	secondHunk := "@@ -10,0 +13,2 @@\n+func B() {}\n+func C() {}\n"
	limit := len(header) + max(len(firstHunk), len(secondHunk))
	chunks, err := splitReviewDiff(header+firstHunk+secondHunk, limit)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 2 {
		t.Fatalf("got %d chunks, want 2", len(chunks))
	}
	for index, chunk := range chunks {
		if !strings.HasPrefix(chunk, header) || strings.Count(chunk, "@@ ") != 1 {
			t.Fatalf("chunk %d must repeat file metadata and keep one whole hunk: %q", index, chunk)
		}
	}
	if !strings.Contains(chunks[0], "+var A = 1") || !strings.Contains(chunks[1], "+func C() {}") {
		t.Fatalf("chunks lost changed lines: %#v", chunks)
	}
}

func TestSplitReviewDiffRejectsOversizedSingleHunk(t *testing.T) {
	diff := "diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -0,0 +1 @@\n+01234567890123456789\n"
	if _, err := splitReviewDiff(diff, 32); err == nil {
		t.Fatal("expected oversized hunk to fail")
	}
}
