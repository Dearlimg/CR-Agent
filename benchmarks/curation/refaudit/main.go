// Command refaudit supports the version-alignment audit of the real-PR
// benchmark: each human reference comment was written against an intermediate
// PR state, but the benchmark reviews the final merged diff, so some
// references target issues that are already fixed in the snapshot.
//
// Modes:
//
//	go run ./benchmarks/curation/refaudit -mode cards
//	  Generate benchmarks/curation/audit/cards.{md,json}: per-reference
//	  evidence cards pairing the comment-time diff hunk with the final
//	  snapshot region around the anchor, plus backtick-token presence hints.
//	  No network access.
//
//	go run ./benchmarks/curation/refaudit -mode fetch
//	  Fetch per-case commit metadata (head sha, merge base, and per-comment
//	  commit ids) from the GitHub API into audit/commit-meta.json. Respects
//	  GITHUB_TOKEN when set; stops early when the rate limit runs low.
//
//	go run ./benchmarks/curation/refaudit -mode apply
//	  Merge audit/decisions.json (manual audit verdicts) and
//	  audit/commit-meta.json into benchmarks/real_pr_v1.json, adding
//	  diff_sha256 per case and status/audit_note/audited_at/
//	  original_commit_id per reference. Fails unless every reference has a
//	  decision.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	manifestPath = "benchmarks/real_pr_v1.json"
	auditDir     = "benchmarks/curation/audit"
)

// manifest types mirror the benchmark loader; only fields this tool reads or
// writes are declared. Reference anchors must stay untouched.
type reference struct {
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
	// OriginalCommitID pins the commit the comment was written against.
	OriginalCommitID string `json:"original_commit_id,omitempty"`
}

type sample struct {
	ID                string      `json:"id"`
	PrURL             string      `json:"pr_url"`
	Title             string      `json:"title,omitempty"`
	Language          string      `json:"language,omitempty"`
	Scenario          string      `json:"scenario"`
	DiffFile          string      `json:"diff_file"`
	DiffSHA256        string      `json:"diff_sha256,omitempty"`
	HeadSHA           string      `json:"head_sha,omitempty"`
	MergeBaseSHA      string      `json:"merge_base_sha,omitempty"`
	ReferenceComments []reference `json:"reference_comments"`
	Notes             string      `json:"notes,omitempty"`
}

// curationComment is the subset of harvest's normalized comment files needed
// for the evidence cards.
type curationComment struct {
	ID       int64  `json:"id"`
	Path     string `json:"path"`
	Line     int    `json:"line"`
	DiffHunk string `json:"diff_hunk"`
}

type commitMeta struct {
	HeadSHA      string                      `json:"head_sha"`
	MergeBaseSHA string                      `json:"merge_base_sha"`
	Comments     map[string]commentCommitRef `json:"comments"`
}

type commentCommitRef struct {
	CommitID         string `json:"commit_id"`
	OriginalCommitID string `json:"original_commit_id"`
}

// decision is one manual audit verdict for a reference comment.
type decision struct {
	Status string `json:"status"`
	Note   string `json:"note"`
}

var validStatuses = map[string]bool{
	"valid":             true,
	"fixed_in_snapshot": true,
	"suggestion":        true,
	"unverifiable":      true,
}

func main() {
	mode := flag.String("mode", "cards", "cards | fetch | apply")
	flag.Parse()

	switch *mode {
	case "cards":
		if err := runCards(); err != nil {
			fail(err)
		}
	case "fetch":
		if err := runFetch(); err != nil {
			fail(err)
		}
	case "apply":
		if err := runApply(); err != nil {
			fail(err)
		}
	default:
		fail(fmt.Errorf("未知 mode %q", *mode))
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "refaudit:", err)
	os.Exit(1)
}

func loadManifest() ([]sample, error) {
	content, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("读取 manifest: %w", err)
	}
	var cases []sample
	if err := json.Unmarshal(content, &cases); err != nil {
		return nil, fmt.Errorf("解析 manifest: %w", err)
	}
	return cases, nil
}

func loadComments(caseID string) (map[int64]curationComment, error) {
	path := filepath.Join("benchmarks/curation", caseID+"-comments.json")
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[int64]curationComment{}, nil
		}
		return nil, err
	}
	var comments []curationComment
	if err := json.Unmarshal(content, &comments); err != nil {
		return nil, fmt.Errorf("解析 %s: %w", path, err)
	}
	byID := make(map[int64]curationComment, len(comments))
	for _, comment := range comments {
		byID[comment.ID] = comment
	}
	return byID, nil
}

// ---------------------------------------------------------------------------
// cards mode
// ---------------------------------------------------------------------------

type card struct {
	Case       string   `json:"case"`
	Index      int      `json:"index"`
	CID        int64    `json:"cid"`
	Author     string   `json:"author"`
	Kind       string   `json:"kind"`
	Dimension  string   `json:"dimension"`
	Anchor     string   `json:"anchor"`
	Body       string   `json:"body"`
	HunkAtComment string `json:"diff_hunk_at_comment"`
	FinalContext  string `json:"final_snapshot_context"`
	Tokens     []tokenHint `json:"token_hints"`
}

type tokenHint struct {
	Token        string `json:"token"`
	InFinalAdded bool   `json:"in_final_added_lines"`
	InCommentHunk bool  `json:"in_comment_hunk"`
}

func runCards() error {
	cases, err := loadManifest()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(auditDir, 0o755); err != nil {
		return err
	}
	var cards []card
	for _, sample := range cases {
		diffBytes, err := os.ReadFile(filepath.Join("benchmarks", filepath.FromSlash(sample.DiffFile)))
		if err != nil {
			return fmt.Errorf("样本 %s: %w", sample.ID, err)
		}
		diff := string(diffBytes)
		added := addedLineText(diff)
		comments, err := loadComments(sample.ID)
		if err != nil {
			return fmt.Errorf("样本 %s: %w", sample.ID, err)
		}
		for index, ref := range sample.ReferenceComments {
			c := card{
				Case: sample.ID, Index: index, CID: ref.CID, Author: ref.Author,
				Kind: ref.Kind, Dimension: ref.Dimension,
				Anchor: fmt.Sprintf("%s:%d", ref.File, ref.Line),
				Body:   ref.Body,
			}
			if comment, ok := comments[ref.CID]; ok {
				c.HunkAtComment = clipText(comment.DiffHunk, 1800)
			} else {
				c.HunkAtComment = "(curation 评论文件中未找到该 cid)"
			}
			c.FinalContext = anchorContext(diff, ref.File, ref.Line, 8)
			for _, token := range backtickTokens(ref.Body) {
				c.Tokens = append(c.Tokens, tokenHint{
					Token:        token,
					InFinalAdded: added[ref.File][token],
					InCommentHunk: strings.Contains(c.HunkAtComment, token),
				})
			}
			cards = append(cards, c)
		}
	}
	if err := writeJSON(filepath.Join(auditDir, "cards.json"), cards); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(auditDir, "cards.md"), []byte(renderCards(cards)), 0o600); err != nil {
		return err
	}
	fmt.Printf("cards: %d references from %d cases -> %s/cards.{md,json}\n", len(cards), len(cases), auditDir)
	return nil
}

func renderCards(cards []card) string {
	var builder strings.Builder
	builder.WriteString("# Real-PR 参考评论版本对齐审核工作表\n\n")
	builder.WriteString("对每条参考回答一个问题：评论抱怨的代码在“评论时 hunk”里存在，在“最终快照”里是否仍然如此？\n\n")
	builder.WriteString("status 取值：valid（仍成立的缺陷类）| suggestion（仍成立的非缺陷建议）| fixed_in_snapshot（要求的修改已在最终 diff 中）| unverifiable（diff 内无法验证）\n\n")
	for _, c := range cards {
		fmt.Fprintf(&builder, "## %s #%d (cid=%d) %s [%s/%s]\n\n", c.Case, c.Index, c.CID, c.Anchor, c.Kind, c.Dimension)
		fmt.Fprintf(&builder, "- status: \n- audit_note: \n\n")
		fmt.Fprintf(&builder, "**评论**：\n\n> %s\n\n", strings.ReplaceAll(c.Body, "\n", "\n> "))
		if len(c.Tokens) > 0 {
			builder.WriteString("**反引号符号提示**（最终 diff 新增行 / 评论时 hunk 中是否出现）：\n\n")
			for _, hint := range c.Tokens {
				fmt.Fprintf(&builder, "- `%s`：final_added=%t, comment_hunk=%t\n", hint.Token, hint.InFinalAdded, hint.InCommentHunk)
			}
			builder.WriteString("\n")
		}
		builder.WriteString("**评论时 diff hunk**：\n\n```diff\n" + c.HunkAtComment + "\n```\n\n")
		builder.WriteString("**最终快照锚点区**：\n\n```diff\n" + c.FinalContext + "\n```\n\n")
		builder.WriteString("---\n\n")
	}
	return builder.String()
}

// backtickTokens extracts `code` spans from a comment body: they carry the
// concrete APIs/symbols a comment asks to add, which is the main staleness
// signal (the demanded change often already appears verbatim in the snapshot).
func backtickTokens(body string) []string {
	seen := map[string]bool{}
	var tokens []string
	for _, match := range backtickPattern.FindAllStringSubmatch(body, -1) {
		token := strings.TrimSpace(match[1])
		if token == "" || len(token) > 80 || seen[token] {
			continue
		}
		seen[token] = true
		tokens = append(tokens, token)
	}
	return tokens
}

var backtickPattern = regexp.MustCompile("`([^`\\n]+)`")

// ---------------------------------------------------------------------------
// fetch mode
// ---------------------------------------------------------------------------

type fetchTarget struct {
	ID     string
	Repo   string
	Number int
}

func runFetch() error {
	cases, err := loadManifest()
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 30 * time.Second}
	metas := map[string]commitMeta{}
	for _, sample := range cases {
		target, err := parsePRURL(sample.PrURL)
		if err != nil {
			return fmt.Errorf("样本 %s: %w", sample.ID, err)
		}
		remaining, err := fetchRateRemaining(client)
		if err == nil && remaining >= 0 && remaining < 8 {
			return fmt.Errorf("API 配额剩余 %d，中止以保留配额；稍后重跑 -mode fetch 会跳过已完成样本", remaining)
		}
		meta, err := fetchCaseMeta(client, target)
		if err != nil {
			return fmt.Errorf("样本 %s: %w", sample.ID, err)
		}
		metas[sample.ID] = meta
		fmt.Printf("[%s] head=%s merge_base=%s comments=%d\n", sample.ID, short(meta.HeadSHA), short(meta.MergeBaseSHA), len(meta.Comments))
	}
	if err := os.MkdirAll(auditDir, 0o755); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(auditDir, "commit-meta.json"), metas); err != nil {
		return err
	}
	fmt.Printf("commit meta -> %s/commit-meta.json (%d cases)\n", auditDir, len(metas))
	return nil
}

func parsePRURL(raw string) (fetchTarget, error) {
	parts := strings.Split(strings.Trim(raw, "/"), "/")
	if len(parts) < 5 {
		return fetchTarget{}, fmt.Errorf("无法解析 PR URL: %s", raw)
	}
	number, err := strconv.Atoi(parts[len(parts)-1])
	if err != nil {
		return fetchTarget{}, fmt.Errorf("PR URL 编号无效: %s", raw)
	}
	return fetchTarget{ID: "", Repo: parts[len(parts)-4] + "/" + parts[len(parts)-3], Number: number}, nil
}

func apiGet(client *http.Client, path string) ([]byte, error) {
	url := "https://api.github.com" + path
	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	if token := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: HTTP %d: %s", path, response.StatusCode, truncate(string(body), 200))
	}
	return body, nil
}

func fetchRateRemaining(client *http.Client) (int, error) {
	body, err := apiGet(client, "/rate_limit")
	if err != nil {
		return -1, err
	}
	var decoded struct {
		Resources struct {
			Core struct {
				Remaining int `json:"remaining"`
			} `json:"core"`
		} `json:"resources"`
	}
	if json.Unmarshal(body, &decoded) != nil {
		return -1, fmt.Errorf("rate_limit 响应解析失败")
	}
	return decoded.Resources.Core.Remaining, nil
}

type prPayload struct {
	Head struct {
		SHA string `json:"sha"`
	} `json:"head"`
	Base struct {
		SHA string `json:"sha"`
	} `json:"base"`
}

func fetchCaseMeta(client *http.Client, target fetchTarget) (commitMeta, error) {
	meta := commitMeta{Comments: map[string]commentCommitRef{}}
	body, err := apiGet(client, fmt.Sprintf("/repos/%s/pulls/%d", target.Repo, target.Number))
	if err != nil {
		return meta, err
	}
	var pr prPayload
	if err := json.Unmarshal(body, &pr); err != nil {
		return meta, fmt.Errorf("PR 响应解析失败: %w", err)
	}
	meta.HeadSHA = pr.Head.SHA
	meta.MergeBaseSHA = pr.Base.SHA

	// First PR commit's parent is the merge base; the PR base.sha field is the
	// branch tip at API time and must not be used as the diff base.
	commitsBody, err := apiGet(client, fmt.Sprintf("/repos/%s/pulls/%d/commits?per_page=1", target.Repo, target.Number))
	if err == nil {
		var commits []struct {
			Parents []struct {
				SHA string `json:"sha"`
			} `json:"parents"`
		}
		if json.Unmarshal(commitsBody, &commits) == nil && len(commits) > 0 && len(commits[0].Parents) > 0 {
			meta.MergeBaseSHA = commits[0].Parents[0].SHA
		}
	}

	page := 1
	for {
		body, err := apiGet(client, fmt.Sprintf("/repos/%s/pulls/%d/comments?per_page=100&page=%d", target.Repo, target.Number, page))
		if err != nil {
			return meta, err
		}
		var comments []struct {
			ID               int64  `json:"id"`
			CommitID         string `json:"commit_id"`
			OriginalCommitID string `json:"original_commit_id"`
		}
		if err := json.Unmarshal(body, &comments); err != nil {
			return meta, fmt.Errorf("评论响应解析失败: %w", err)
		}
		if len(comments) == 0 {
			break
		}
		for _, comment := range comments {
			meta.Comments[strconv.FormatInt(comment.ID, 10)] = commentCommitRef{
				CommitID:         comment.CommitID,
				OriginalCommitID: comment.OriginalCommitID,
			}
		}
		if len(comments) < 100 {
			break
		}
		page++
	}
	return meta, nil
}

func short(sha string) string {
	if len(sha) > 10 {
		return sha[:10]
	}
	return sha
}

// ---------------------------------------------------------------------------
// apply mode
// ---------------------------------------------------------------------------

func runApply() error {
	cases, err := loadManifest()
	if err != nil {
		return err
	}
	var decisions map[string]map[string]decision
	if err := readOptionalJSON(filepath.Join(auditDir, "decisions.json"), &decisions); err != nil {
		return err
	}
	var metas map[string]commitMeta
	if err := readOptionalJSON(filepath.Join(auditDir, "commit-meta.json"), &metas); err != nil {
		return err
	}

	missing := 0
	counts := map[string]int{}
	for caseIndex := range cases {
		sample := &cases[caseIndex]
		diffBytes, err := os.ReadFile(filepath.Join("benchmarks", filepath.FromSlash(sample.DiffFile)))
		if err != nil {
			return fmt.Errorf("样本 %s: %w", sample.ID, err)
		}
		digest := sha256.Sum256(diffBytes)
		sample.DiffSHA256 = hex.EncodeToString(digest[:])
		if meta, ok := metas[sample.ID]; ok {
			sample.HeadSHA = meta.HeadSHA
			sample.MergeBaseSHA = meta.MergeBaseSHA
		}
		for refIndex := range sample.ReferenceComments {
			ref := &sample.ReferenceComments[refIndex]
			key := strconv.FormatInt(ref.CID, 10)
			if meta, ok := metas[sample.ID]; ok {
				if commitRef, ok := meta.Comments[key]; ok && commitRef.OriginalCommitID != "" {
					ref.OriginalCommitID = commitRef.OriginalCommitID
				}
			}
			byCase := decisions[sample.ID]
			decision, ok := byCase[key]
			if !ok {
				fmt.Fprintf(os.Stderr, "缺少审核决定: %s cid=%d\n", sample.ID, ref.CID)
				missing++
				continue
			}
			if !validStatuses[decision.Status] {
				return fmt.Errorf("%s cid=%d 的 status 无效: %q", sample.ID, ref.CID, decision.Status)
			}
			if decision.Status == "fixed_in_snapshot" && strings.TrimSpace(decision.Note) == "" {
				return fmt.Errorf("%s cid=%d 标记 fixed_in_snapshot 但缺少 audit_note（需指出最终 diff 中对应修复位置）", sample.ID, ref.CID)
			}
			ref.Status = decision.Status
			ref.AuditNote = decision.Note
			ref.AuditedAt = time.Now().UTC().Format("2006-01-02")
			counts[decision.Status]++
		}
	}
	if missing > 0 {
		return fmt.Errorf("共有 %d 条参考缺少审核决定，未写入 manifest", missing)
	}
	encoded, err := json.MarshalIndent(cases, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(manifestPath, encoded, 0o600); err != nil {
		return err
	}
	fmt.Printf("manifest 已更新: %s\n", manifestPath)
	fmt.Printf("diff_sha256 已固定 %d 个快照；状态分布:\n", len(cases))
	for _, status := range []string{"valid", "suggestion", "fixed_in_snapshot", "unverifiable"} {
		fmt.Printf("  %-18s %d\n", status, counts[status])
	}
	return nil
}

func readOptionalJSON(path string, target any) error {
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return json.Unmarshal(content, target)
}

func writeJSON(path string, value any) error {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, encoded, 0o600)
}

// ---------------------------------------------------------------------------
// diff parsing (mirror of the benchmark's diff handling, kept standalone)
// ---------------------------------------------------------------------------

var (
	cardDiffHeader = regexp.MustCompile(`^diff --git a/(.+) b/(.+)$`)
	cardHunkHeader = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,\d+)? @@`)
)

type snapLine struct {
	kind    byte
	newLine int
	text    string
}

type snapHunk struct {
	file  string
	lines []snapLine
}

func parseSnapDiff(diff string) []snapHunk {
	var hunks []snapHunk
	file := ""
	newLine := 0
	var current *snapHunk
	for _, raw := range strings.Split(diff, "\n") {
		raw = strings.TrimSuffix(raw, "\r")
		if match := cardDiffHeader.FindStringSubmatch(raw); len(match) == 3 {
			file = match[2]
			current = nil
			continue
		}
		if match := cardHunkHeader.FindStringSubmatch(raw); len(match) == 2 {
			start, _ := strconv.Atoi(match[1])
			hunks = append(hunks, snapHunk{file: file})
			current = &hunks[len(hunks)-1]
			newLine = start
			continue
		}
		if current == nil {
			continue
		}
		switch {
		case strings.HasPrefix(raw, "+"):
			current.lines = append(current.lines, snapLine{kind: '+', newLine: newLine, text: raw[1:]})
			newLine++
		case strings.HasPrefix(raw, "-"):
			current.lines = append(current.lines, snapLine{kind: '-', text: raw[1:]})
		case strings.HasPrefix(raw, " "):
			current.lines = append(current.lines, snapLine{kind: ' ', newLine: newLine, text: raw[1:]})
			newLine++
		}
	}
	return hunks
}

// addedLineText maps file -> added-line text set, for token presence hints.
func addedLineText(diff string) map[string]map[string]bool {
	presence := map[string]map[string]bool{}
	for _, hunk := range parseSnapDiff(diff) {
		if presence[hunk.file] == nil {
			presence[hunk.file] = map[string]bool{}
		}
		for _, line := range hunk.lines {
			if line.kind == '+' {
				presence[hunk.file][line.text] = true
			}
		}
	}
	return presence
}

// anchorContext renders the hunk covering (file, line) with contextWidth lines
// on each side, in the same annotated style as the judge prompt context.
func anchorContext(diff, file string, line, contextWidth int) string {
	var builder strings.Builder
	for _, hunk := range parseSnapDiff(diff) {
		if hunk.file != file {
			continue
		}
		anchor := -1
		for index, snapLine := range hunk.lines {
			if snapLine.kind == '+' && snapLine.newLine == line {
				anchor = index
				break
			}
		}
		if anchor < 0 {
			continue
		}
		lo := anchor - contextWidth
		if lo < 0 {
			lo = 0
		}
		hi := anchor + contextWidth + 1
		if hi > len(hunk.lines) {
			hi = len(hunk.lines)
		}
		fmt.Fprintf(&builder, "--- %s @@\n", file)
		for index := lo; index < hi; index++ {
			snapLine := hunk.lines[index]
			switch snapLine.kind {
			case '+':
				fmt.Fprintf(&builder, "+%d %s\n", snapLine.newLine, snapLine.text)
			case '-':
				fmt.Fprintf(&builder, "-   %s\n", snapLine.text)
			case ' ':
				fmt.Fprintf(&builder, " %d %s\n", snapLine.newLine, snapLine.text)
			}
		}
	}
	if builder.Len() == 0 {
		return "(锚点未落在最终 diff 的新增行上)"
	}
	return builder.String()
}

func clipText(text string, limit int) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit]) + " …(截断)"
}

func truncate(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	return text[:limit]
}
