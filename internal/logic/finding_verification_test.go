package logic

import (
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
