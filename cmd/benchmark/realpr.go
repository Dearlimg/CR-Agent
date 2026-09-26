package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// realReference is one curated human review comment used as a reference answer.
// Status records the version-alignment audit: whether the flagged issue still
// exists in the final diff snapshot that the system reviews.
type realReference struct {
	File      string `json:"file"`
	Line      int    `json:"line"`
	Body      string `json:"body"`
	CID       int64  `json:"cid,omitempty"`
	Author    string `json:"author,omitempty"`
	Dimension string `json:"dimension"`
	Kind      string `json:"kind"`
	Status    string `json:"status,omitempty"`
	AuditNote string `json:"audit_note,omitempty"`
	AuditedAt string `json:"audited_at,omitempty"`
	// OriginalCommitID pins the PR commit the comment was written against.
	OriginalCommitID string `json:"original_commit_id,omitempty"`
}

// Reference audit statuses. "valid" references form the core recall
// denominator; "fixed_in_snapshot" references are paired negatives (the
// snapshot must not contain the flagged issue, and a finding re-flagging it
// counts as a false positive); "suggestion" references are non-defect
// improvement notes scored on a separate channel; "unverifiable" references
// cannot be judged from the diff alone and are excluded from all metrics.
const (
	refStatusValid            = "valid"
	refStatusFixedInSnapshot  = "fixed_in_snapshot"
	refStatusSuggestion       = "suggestion"
	refStatusUnverifiable     = "unverifiable"
)

var validRefStatuses = map[string]bool{
	refStatusValid:           true,
	refStatusFixedInSnapshot: true,
	refStatusSuggestion:      true,
	refStatusUnverifiable:    true,
}

// refChannel normalizes legacy (pre-audit) references to the core channel so
// saved reports remain rescorable.
func refChannel(ref realReference) string {
	if ref.Status == "" || !validRefStatuses[ref.Status] {
		return refStatusValid
	}
	return ref.Status
}

// realCase is one real-PR benchmark case in benchmarks/real_pr_v1.json.
// Negative controls have no reference comments. DiffSHA256 pins the snapshot:
// any edit to the stored diff file invalidates the audited references.
type realCase struct {
	ID                string          `json:"id"`
	PrURL             string          `json:"pr_url"`
	Title             string          `json:"title,omitempty"`
	Language          string          `json:"language,omitempty"`
	Scenario          string          `json:"scenario"`
	DiffFile          string          `json:"diff_file"`
	DiffSHA256        string          `json:"diff_sha256,omitempty"`
	HeadSHA           string          `json:"head_sha,omitempty"`
	MergeBaseSHA      string          `json:"merge_base_sha,omitempty"`
	ReferenceComments []realReference `json:"reference_comments"`
	Notes             string          `json:"notes,omitempty"`
}

var (
	validScenarios  = map[string]bool{"feature": true, "bugfix": true, "refactor": true, "performance": true, "negative": true}
	validDimensions = map[string]bool{"correctness": true, "security": true, "performance": true, "readability": true, "maintainability": true, "design": true, "testing": true, "compatibility": true}
	validKinds      = map[string]bool{"defect": true, "suggestion": true, "design": true, "test": true}
)

// isRealManifest reports whether the manifest at path uses the real-PR schema.
func isRealManifest(path string) bool {
	content, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var probe []struct {
		PrURL string `json:"pr_url"`
	}
	return json.Unmarshal(content, &probe) == nil && len(probe) > 0 && probe[0].PrURL != ""
}

// isRealReport reports whether a saved run report is a real-PR benchmark run.
func isRealReport(path string) bool {
	content, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var probe struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(content, &probe) != nil {
		return false
	}
	return probe.Version == realReportVersion
}

// loadRealCases reads and validates benchmarks/real_pr_v1.json. Reference
// anchors must point at added lines of the stored diff snapshot.
func loadRealCases(manifestPath string) ([]realCase, error) {
	content, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("读取 real-PR manifest: %w", err)
	}
	var cases []realCase
	if err := json.Unmarshal(content, &cases); err != nil {
		return nil, fmt.Errorf("解析 real-PR manifest: %w", err)
	}
	if len(cases) == 0 {
		return nil, errors.New("real-PR manifest 为空")
	}
	baseDir := filepath.Dir(manifestPath)
	seen := map[string]bool{}
	for _, sample := range cases {
		if sample.ID == "" || seen[sample.ID] {
			return nil, fmt.Errorf("real-PR 样本 ID 缺失或重复: %q", sample.ID)
		}
		seen[sample.ID] = true
		if sample.PrURL == "" {
			return nil, fmt.Errorf("样本 %s 缺少 pr_url", sample.ID)
		}
		parsed, err := url.Parse(sample.PrURL)
		if err != nil || (parsed.Host != "github.com" && parsed.Host != "gitlab.com") {
			return nil, fmt.Errorf("样本 %s 的 pr_url 必须指向 github.com 或 gitlab.com: %s", sample.ID, sample.PrURL)
		}
		if !validScenarios[sample.Scenario] {
			return nil, fmt.Errorf("样本 %s 的 scenario 无效: %q", sample.ID, sample.Scenario)
		}
		if sample.DiffFile == "" {
			return nil, fmt.Errorf("样本 %s 缺少 diff_file", sample.ID)
		}
		diffPath := filepath.Join(baseDir, filepath.FromSlash(sample.DiffFile))
		diffContent, err := os.ReadFile(diffPath)
		if err != nil {
			return nil, fmt.Errorf("样本 %s 的 diff 快照缺失: %w", sample.ID, err)
		}
		if sample.DiffSHA256 == "" {
			return nil, fmt.Errorf("样本 %s 缺少 diff_sha256；运行 benchmarks/curation/refaudit -mode apply 固定快照", sample.ID)
		}
		digest := sha256.Sum256(diffContent)
		if actual := hex.EncodeToString(digest[:]); actual != sample.DiffSHA256 {
			return nil, fmt.Errorf("样本 %s 的 diff 快照与 diff_sha256 不符：审核标注基于旧快照，禁止直接修改 %s", sample.ID, sample.DiffFile)
		}
		added := changedLines(string(diffContent))
		var problems []string
		for _, ref := range sample.ReferenceComments {
			if ref.File == "" || ref.Line <= 0 {
				problems = append(problems, fmt.Sprintf("缺少 file/line: %+v", ref))
				continue
			}
			if !added[ref.File][ref.Line] {
				problems = append(problems, fmt.Sprintf("锚点 %s:%d 不在 diff 新增行上（cid=%d）", ref.File, ref.Line, ref.CID))
				continue
			}
			if strings.TrimSpace(ref.Body) == "" {
				problems = append(problems, fmt.Sprintf("正文为空（cid=%d）", ref.CID))
			}
			if !validDimensions[ref.Dimension] {
				problems = append(problems, fmt.Sprintf("dimension 无效: %q（cid=%d）", ref.Dimension, ref.CID))
			}
			if !validKinds[ref.Kind] {
				problems = append(problems, fmt.Sprintf("kind 无效: %q（cid=%d）", ref.Kind, ref.CID))
			}
			if !validRefStatuses[ref.Status] {
				problems = append(problems, fmt.Sprintf("status 无效: %q（cid=%d）；须为 valid/fixed_in_snapshot/suggestion/unverifiable", ref.Status, ref.CID))
			}
			if ref.Status == refStatusFixedInSnapshot && strings.TrimSpace(ref.AuditNote) == "" {
				problems = append(problems, fmt.Sprintf("fixed_in_snapshot 参考缺少 audit_note（cid=%d）", ref.CID))
			}
		}
		if len(problems) > 0 {
			return nil, fmt.Errorf("样本 %s 有 %d 个参考评论问题:\n  %s", sample.ID, len(problems), strings.Join(problems, "\n  "))
		}
	}
	return cases, nil
}

// diffHunk is one parsed hunk of a unified diff, tracking new-side line numbers.
type diffHunk struct {
	file       string
	newStart   int
	lines      []diffLine
	addedCount int
}

type diffLine struct {
	kind    byte // '+', '-', ' ', or meta
	newLine int
	text    string
}

var (
	hunkHeaderPattern = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,\d+)? @@`)
	diffHeaderPattern = regexp.MustCompile(`^diff --git a/(.+) b/(.+)$`)
)

// parseDiffHunks splits a unified diff into per-hunk structures.
func parseDiffHunks(diff string) []diffHunk {
	var hunks []diffHunk
	file := ""
	newLine := 0
	var current *diffHunk
	for _, raw := range strings.Split(diff, "\n") {
		if match := diffHeaderPattern.FindStringSubmatch(raw); len(match) == 3 {
			file = match[2]
			current = nil
			continue
		}
		if match := hunkHeaderPattern.FindStringSubmatch(raw); len(match) == 2 {
			start, _ := strconv.Atoi(match[1])
			hunks = append(hunks, diffHunk{file: file, newStart: start})
			current = &hunks[len(hunks)-1]
			newLine = start
			continue
		}
		if current == nil {
			continue
		}
		switch {
		case strings.HasPrefix(raw, "+"):
			current.lines = append(current.lines, diffLine{kind: '+', newLine: newLine, text: raw[1:]})
			current.addedCount++
			newLine++
		case strings.HasPrefix(raw, "-"):
			current.lines = append(current.lines, diffLine{kind: '-', text: raw[1:]})
		case strings.HasPrefix(raw, " "):
			current.lines = append(current.lines, diffLine{kind: ' ', newLine: newLine, text: raw[1:]})
			newLine++
		default:
			// "\ No newline at end of file" and stray metadata lines.
		}
	}
	return hunks
}

// extractAnchorContext renders the hunks covering the given (file, line)
// anchors, trimmed to a window spanning all anchors in each hunk. The output is
// bounded so it can be embedded in a judge prompt.
func extractAnchorContext(diff string, anchors map[string]map[int]bool, maxChars int) string {
	hunks := parseDiffHunks(diff)
	var builder strings.Builder
	used := 0
	for _, hunk := range hunks {
		lo, hi := -1, -1
		for index, line := range hunk.lines {
			if line.kind == '+' && anchors[hunk.file][line.newLine] {
				if lo < 0 || index < lo {
					lo = index
				}
				if index > hi {
					hi = index
				}
			}
		}
		if lo < 0 {
			continue
		}
		lo -= 8
		if lo < 0 {
			lo = 0
		}
		hi += 8
		if hi > len(hunk.lines) {
			hi = len(hunk.lines)
		}
		var hunkBuilder strings.Builder
		fmt.Fprintf(&hunkBuilder, "--- %s @@ +%d\n", hunk.file, hunk.newStart)
		for index := lo; index < hi; index++ {
			line := hunk.lines[index]
			switch line.kind {
			case '+':
				fmt.Fprintf(&hunkBuilder, "+%d %s\n", line.newLine, line.text)
			case '-':
				fmt.Fprintf(&hunkBuilder, "-   %s\n", line.text)
			case ' ':
				fmt.Fprintf(&hunkBuilder, " %d %s\n", line.newLine, line.text)
			}
		}
		if used+hunkBuilder.Len() > maxChars {
			break
		}
		used += hunkBuilder.Len()
		builder.WriteString(hunkBuilder.String())
	}
	return builder.String()
}
