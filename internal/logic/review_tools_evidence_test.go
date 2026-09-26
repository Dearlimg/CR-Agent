package logic

import (
	"strings"
	"testing"
)

func TestValidateFindingEvidenceReanchorsUniqueAddedBlock(t *testing.T) {
	diff := strings.Join([]string{
		"diff --git a/agent.py b/agent.py",
		"--- a/agent.py",
		"+++ b/agent.py",
		"@@ -0,0 +1277,3 @@",
		"+                            # preparatory step, not task completion",
		"+                            _prev_was_skill_read = _last_tool_name in (",
		"+                                \"view\",",
		"diff --git a/service.py b/service.py",
		"--- a/service.py",
		"+++ b/service.py",
		"@@ -0,0 +596 @@",
		"+            q = session.query(ServeEntity)",
		"@@ -0,0 +639,2 @@",
		"+            offset = (query.page - 1) * query.page_size",
		"+            results = q.offset(offset).limit(query.page_size).all()",
	}, "\n")
	findings := []ReviewFinding{
		completeEvidenceFinding(
			"agent.py", 1277,
			"_prev_was_skill_read = _last_tool_name in (\n\"view\",",
		),
		completeEvidenceFinding(
			"service.py", 596,
			"offset = (query.page - 1) * query.page_size\nresults = q.offset(offset).limit(query.page_size).all()",
		),
	}

	verified, rejected, reasons := validateFindingEvidence(findings, diff)
	if rejected != 0 || len(verified) != len(findings) || len(reasons) != 0 {
		t.Fatalf("verified=%d rejected=%d, want 2 and 0", len(verified), rejected)
	}
	if verified[0].Line != 1278 || verified[1].Line != 639 {
		t.Errorf("corrected lines = %d, %d; want 1278, 639", verified[0].Line, verified[1].Line)
	}
	if verified[0].Evidence != "                            _prev_was_skill_read = _last_tool_name in (\n                                \"view\"," {
		t.Errorf("agent evidence was not replaced with actual added text: %q", verified[0].Evidence)
	}
	if verified[1].Evidence != "            offset = (query.page - 1) * query.page_size\n            results = q.offset(offset).limit(query.page_size).all()" {
		t.Errorf("service evidence was not replaced with actual added text: %q", verified[1].Evidence)
	}
}

func TestValidateFindingEvidencePreservesCorrectDuplicateAnchor(t *testing.T) {
	diff := strings.Join([]string{
		"diff --git a/a.go b/a.go",
		"--- a/a.go",
		"+++ b/a.go",
		"@@ -0,0 +10,4 @@",
		"+different()",
		"+same()",
		"+other()",
		"+same()",
	}, "\n")
	anchored := completeEvidenceFinding("a.go", 11, "same()")

	verified, rejected, reasons := validateFindingEvidence([]ReviewFinding{anchored}, diff)
	if rejected != 0 || len(verified) != 1 || verified[0].Line != 11 || len(reasons) != 0 {
		t.Fatalf("correct anchor should survive duplicate text: verified=%v rejected=%d reasons=%v", verified, rejected, reasons)
	}

	wrongAnchor := completeEvidenceFinding("a.go", 10, "same()")
	verified, rejected, reasons = validateFindingEvidence([]ReviewFinding{wrongAnchor}, diff)
	if rejected != 1 || len(verified) != 0 || reasons[evidenceReasonAmbiguousAnchor] != 1 {
		t.Fatalf("ambiguous relocation should fail: verified=%v rejected=%d reasons=%v", verified, rejected, reasons)
	}
}

func TestValidateFindingEvidenceRejectsNonAddedOrFalseQuotes(t *testing.T) {
	diff := strings.Join([]string{
		"diff --git a/a.go b/a.go",
		"--- a/a.go",
		"+++ b/a.go",
		"@@ -1,3 +1,4 @@",
		"-removed()",
		" context()",
		"+added()",
		" context2()",
		"+added2()",
		"diff --git a/b.go b/b.go",
		"--- a/b.go",
		"+++ b/b.go",
		"@@ -0,0 +1 @@",
		"+onlyOtherFile()",
	}, "\n")
	findings := []ReviewFinding{
		completeEvidenceFinding("a.go", 2, "removed()"),
		completeEvidenceFinding("a.go", 1, "context()"),
		completeEvidenceFinding("a.go", 2, "onlyOtherFile()"),
		completeEvidenceFinding("a.go", 2, "invented()"),
		completeEvidenceFinding("a.go", 2, "added()\nadded2()"),
	}

	verified, rejected, reasons := validateFindingEvidence(findings, diff)
	if rejected != len(findings) || len(verified) != 0 {
		t.Fatalf("non-added or false quotes should fail: verified=%v rejected=%d", verified, rejected)
	}
	if reasons[evidenceReasonTextMismatch] != 4 || reasons[evidenceReasonContextOnly] != 1 {
		t.Fatalf("unexpected rejection breakdown: %v, want text_mismatch=4 context_only=1", reasons)
	}
}

func TestValidateFindingEvidenceClassifiesMissingFieldAndUnknownFile(t *testing.T) {
	diff := strings.Join([]string{
		"diff --git a/a.go b/a.go",
		"--- a/a.go",
		"+++ b/a.go",
		"@@ -0,0 +1,2 @@",
		"+added()",
		"+added2()",
	}, "\n")
	findings := []ReviewFinding{
		completeEvidenceFinding("a.go", 1, "added()"),
		completeEvidenceFinding("b.go", 1, "added()"),
		{File: "a.go", Line: 1, Evidence: "added()", Body: "only body"},
	}
	verified, rejected, reasons := validateFindingEvidence(findings, diff)
	if rejected != 2 || len(verified) != 1 {
		t.Fatalf("verified=%d rejected=%d, want 1 and 2", len(verified), rejected)
	}
	if reasons[evidenceReasonUnknownFile] != 1 || reasons[evidenceReasonMissingField] != 1 {
		t.Fatalf("unexpected breakdown: %v, want unknown_file=1 missing_field=1", reasons)
	}
}

func completeEvidenceFinding(file string, line int, evidence string) ReviewFinding {
	return ReviewFinding{
		File:       file,
		Line:       line,
		Body:       "Problem",
		Evidence:   evidence,
		Trigger:    "Trigger",
		Impact:     "Impact",
		Suggestion: "Suggestion",
	}
}
