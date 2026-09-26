// Command harvest fetches GitHub PR diffs and review comments for real-PR
// benchmark curation. It is idempotent: existing complete artifacts are kept,
// so rerunning after an API rate-limit reset finishes the remaining cases.
//
// Usage:
//
//	go run ./benchmarks/curation/harvest -out benchmarks/curation
//
// Artifacts per case <id>:
//   - <id>.diff            merged PR diff (github.com, no API quota)
//   - <id>-comments.json   normalized review-comment array (API, paginated)
//   - <id>-digest.md       anchored, substantive comments for manual curation
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type target struct {
	ID     string
	Repo   string
	Number int
}

// Single-page cases first so a partial quota still completes whole files.
var targets = []target{
	{"ts-57465", "microsoft/TypeScript", 57465},
	{"node-41749", "nodejs/node", 41749},
	{"gin-2632", "gin-gonic/gin", 2632},
	{"gin-2767", "gin-gonic/gin", 2767},
	{"pandas-34473", "pandas-dev/pandas", 34473},
	{"gin-1026", "gin-gonic/gin", 1026},
	{"gin-4224", "gin-gonic/gin", 4224},
	{"etcd-2009", "etcd-io/etcd", 2009},
	{"redis-9323", "redis/redis", 9323},
	{"tokio-4652", "tokio-rs/tokio", 4652},
	{"tokio-6001", "tokio-rs/tokio", 6001},
	{"redis-14017", "redis/redis", 14017},
	{"pandas-27237", "pandas-dev/pandas", 27237},
	{"pandas-22862", "pandas-dev/pandas", 22862},
	{"redis-9788", "redis/redis", 9788},
}

type comment struct {
	ID               int64  `json:"id"`
	User             *struct {
		Login string `json:"login"`
	} `json:"user"`
	Body             string `json:"body"`
	Path             string `json:"path"`
	Line             *int   `json:"line"`
	OriginalLine     *int   `json:"original_line"`
	Side             string `json:"side"`
	SubjectType      string `json:"subject_type"`
	Position         *int   `json:"position"`
	OriginalPosition *int   `json:"original_position"`
	InReplyToID      *int64 `json:"in_reply_to_id"`
	DiffHunk         string `json:"diff_hunk"`
	HTMLURL          string `json:"html_url"`
	CreatedAt        string `json:"created_at"`
}

var client = &http.Client{Timeout: 60 * time.Second}
var rateRemaining = 60

func get(url string) ([]byte, error) {
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "cr-agent-benchmark-curation")
		req.Header.Set("Accept", "application/vnd.github+json")
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		if v := resp.Header.Get("X-RateLimit-Remaining"); v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				rateRemaining = n
			}
		}
		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			return nil, err
		}
		if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
			if attempt < 2 {
				wait := 30 * time.Second
				if ra := resp.Header.Get("Retry-After"); ra != "" {
					if n, err := strconv.Atoi(ra); err == nil {
						wait = time.Duration(n) * time.Second
					}
				}
				fmt.Printf("    %d from %s, retrying in %s\n", resp.StatusCode, url, wait)
				time.Sleep(wait)
				continue
			}
			return nil, fmt.Errorf("HTTP %d for %s: %s", resp.StatusCode, url, truncate(body, 200))
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("HTTP %d for %s: %s", resp.StatusCode, url, truncate(body, 200))
		}
		return body, nil
	}
}

func truncate(b []byte, n int) string {
	s := strings.TrimSpace(string(b))
	if len(s) > n {
		return s[:n]
	}
	return s
}

func main() {
	out := flag.String("out", "benchmarks/curation", "output directory")
	flag.Parse()
	if err := os.MkdirAll(*out, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "mkdir:", err)
		os.Exit(1)
	}
	stopped := false
	for _, t := range targets {
		if stopped {
			break
		}
		diffPath := filepath.Join(*out, t.ID+".diff")
		if !fileNonEmpty(diffPath) {
			body, err := get("https://github.com/" + t.Repo + "/pull/" + strconv.Itoa(t.Number) + ".diff")
			if err != nil {
				fmt.Printf("[%s] diff fetch failed: %v\n", t.ID, err)
			} else if err := os.WriteFile(diffPath, body, 0o644); err != nil {
				fmt.Printf("[%s] diff write failed: %v\n", t.ID, err)
			} else {
				fmt.Printf("[%s] diff %d bytes\n", t.ID, len(body))
			}
		}
		commentsPath := filepath.Join(*out, t.ID+"-comments.json")
		if !hasComments(commentsPath) {
			var all []comment
			for page := 1; ; page++ {
				if rateRemaining < 3 {
					fmt.Printf("rate limit low (%d left); rerun after reset to finish %s\n", rateRemaining, t.ID)
					stopped = true
					break
				}
				url := fmt.Sprintf("https://api.github.com/repos/%s/pulls/%d/comments?per_page=100&page=%d", t.Repo, t.Number, page)
				body, err := get(url)
				if err != nil {
					fmt.Printf("[%s] comments fetch failed: %v\n", t.ID, err)
					all = nil
					break
				}
				var items []comment
				if err := json.Unmarshal(body, &items); err != nil {
					fmt.Printf("[%s] comments parse failed: %v\n", t.ID, err)
					all = nil
					break
				}
				all = append(all, items...)
				if len(items) < 100 {
					break
				}
				time.Sleep(500 * time.Millisecond)
			}
			if all != nil {
				encoded, err := json.MarshalIndent(all, "", "  ")
				if err == nil {
					err = os.WriteFile(commentsPath, encoded, 0o644)
				}
				if err != nil {
					fmt.Printf("[%s] comments write failed: %v\n", t.ID, err)
				} else {
					fmt.Printf("[%s] %d comments\n", t.ID, len(all))
				}
			}
		}
	}
	writeDigests(*out)
	fmt.Println("harvest done")
}

func fileNonEmpty(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Size() > 0
}

func hasComments(path string) bool {
	body, err := os.ReadFile(path)
	if err != nil || len(strings.TrimSpace(string(body))) == 0 {
		return false
	}
	var items []comment
	if err := json.Unmarshal(body, &items); err != nil {
		return false
	}
	return len(items) > 0
}

func writeDigests(out string) {
	entries, err := os.ReadDir(out)
	if err != nil {
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, "-comments.json") {
			continue
		}
		id := strings.TrimSuffix(name, "-comments.json")
		body, err := os.ReadFile(filepath.Join(out, name))
		if err != nil {
			continue
		}
		var comments []comment
		if err := json.Unmarshal(body, &comments); err != nil || len(comments) == 0 {
			continue
		}
		// Old PRs expose only a diff position; resolve file/line from the
		// stored diff snapshot so those comments can be curated too.
		diffBody, err := os.ReadFile(filepath.Join(out, id+".diff"))
		if err == nil {
			resolved := resolvePositions(comments, string(diffBody))
			fmt.Printf("[%s] position-resolved anchors: %d\n", id, resolved)
		}
		writeDigest(out, id, comments)
	}
}

// resolvePositions recovers missing anchors for old comments by matching the
// comment's stored diff_hunk content against the final merged diff. Positions
// alone are unreliable (they refer to earlier PR iterations), so we require a
// unique text match instead. Returns how many anchors were resolved.
func resolvePositions(comments []comment, diff string) int {
	// added[file][lineText] -> sorted line numbers with that exact text.
	added := map[string]map[string][]int{}
	file := ""
	newLine := 0
	for _, raw := range strings.Split(diff, "\n") {
		if match := diffHeaderRe.FindStringSubmatch(raw); match != nil {
			file = match[2]
			continue
		}
		if strings.HasPrefix(raw, "@@ ") {
			if match := hunkHeaderRe.FindStringSubmatch(raw); match != nil {
				n, _ := strconv.Atoi(match[1])
				newLine = n
			}
			continue
		}
		if strings.HasPrefix(raw, "index ") || strings.HasPrefix(raw, "--- ") || strings.HasPrefix(raw, "+++ ") || strings.HasPrefix(raw, "\\") {
			continue
		}
		switch {
		case strings.HasPrefix(raw, "+"):
			text := raw[1:]
			if added[file] == nil {
				added[file] = map[string][]int{}
			}
			added[file][text] = append(added[file][text], newLine)
			newLine++
		case strings.HasPrefix(raw, "-"):
		case strings.HasPrefix(raw, " "):
			newLine++
		}
	}
	resolved := 0
	for index := range comments {
		c := &comments[index]
		if c.OriginalLine != nil || c.Line != nil {
			continue
		}
		// Try each '+' line of the hunk, oldest context last; the anchor is
		// usually the last added line of the stored hunk context.
		var candidates []int
		for _, hunkLine := range plusLinesOfHunk(c.DiffHunk) {
			lines := added[c.Path][hunkLine]
			if len(lines) == 1 {
				candidates = append(candidates, lines[0])
			}
		}
		if len(candidates) == 0 {
			continue
		}
		// The anchor is the first uniquely-matched added line that also
		// appears within a small window of the others (same hunk region).
		line := candidates[0]
		for _, other := range candidates {
			if abs(other-line) > 0 && abs(other-line) < 6 {
				line = other
				break
			}
		}
		c.OriginalLine = &line
		c.Line = &line
		resolved++
	}
	return resolved
}

// plusLinesOfHunk extracts the added-line texts from a stored diff hunk.
func plusLinesOfHunk(hunk string) []string {
	var out []string
	for _, raw := range strings.Split(hunk, "\n") {
		if strings.HasPrefix(raw, "+") && !strings.HasPrefix(raw, "+++") {
			text := strings.TrimRight(raw[1:], "\r")
			if strings.TrimSpace(text) != "" {
				out = append(out, text)
			}
		}
	}
	return out
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

var (
	diffHeaderRe = regexp.MustCompile(`^diff --git a/(.+) b/(.+)$`)
	hunkHeaderRe = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,\d+)? @@`)
)

func writeDigest(out, id string, comments []comment) {
	type row struct {
		idx    int
		cid    int64
		author string
		path   string
		line   int
		side   string
		subj   string
		reply  bool
		body   string
	}
	var rows []row
	for idx, c := range comments {
		anchored := c.Position != nil || c.OriginalPosition != nil
		if !anchored || len(c.Body) < 40 {
			continue
		}
		line := 0
		if c.OriginalLine != nil {
			line = *c.OriginalLine
		} else if c.Line != nil {
			line = *c.Line
		}
		author := ""
		if c.User != nil {
			author = c.User.Login
		}
		rows = append(rows, row{idx: idx, cid: c.ID, author: author, path: c.Path, line: line,
			side: c.Side, subj: c.SubjectType, reply: c.InReplyToID != nil, body: c.Body})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].path != rows[j].path {
			return rows[i].path < rows[j].path
		}
		if rows[i].line != rows[j].line {
			return rows[i].line < rows[j].line
		}
		return rows[i].idx < rows[j].idx
	})
	var b strings.Builder
	fmt.Fprintf(&b, "# digest %s : %d anchored candidates (of %d total)\n", id, len(rows), len(comments))
	for _, r := range rows {
		fmt.Fprintf(&b, "\n## [%d] %s:%d author=%s reply=%t subj=%s side=%s cid=%d\n%s\n", r.idx, r.path, r.line, r.author, r.reply, r.subj, r.side, r.cid, r.body)
	}
	digestPath := filepath.Join(out, id+"-digest.md")
	if err := os.WriteFile(digestPath, []byte(b.String()), 0o644); err != nil {
		fmt.Printf("[%s] digest write failed: %v\n", id, err)
		return
	}
	fmt.Printf("[%s] digest rows=%d\n", id, len(rows))
}
