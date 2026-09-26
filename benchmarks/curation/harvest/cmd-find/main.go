// find mode: locate added lines matching a text pattern in a stored diff.
// Usage: go run ./benchmarks/curation/harvest/cmd-find <case-id> <substring>
package main

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

var (
	diffHeaderRe = regexp.MustCompile(`^diff --git a/(.+) b/(.+)$`)
	hunkHeaderRe = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,\d+)? @@`)
)

func main() {
	id := os.Args[1]
	pattern := strings.ToLower(os.Args[2])
	raw, err := os.ReadFile("benchmarks/real_pr_cases/" + id + ".diff")
	if err != nil {
		raw, err = os.ReadFile("benchmarks/curation/" + id + ".diff")
		if err != nil {
			panic(err)
		}
	}
	file := ""
	newLine := 0
	for _, line := range strings.Split(string(raw), "\n") {
		if match := diffHeaderRe.FindStringSubmatch(line); match != nil {
			file = match[2]
			continue
		}
		if match := hunkHeaderRe.FindStringSubmatch(line); match != nil {
			n, _ := strconv.Atoi(match[1])
			newLine = n
			continue
		}
		if strings.HasPrefix(line, "index ") || strings.HasPrefix(line, "--- ") || strings.HasPrefix(line, "+++ ") || strings.HasPrefix(line, "\\") {
			continue
		}
		if strings.HasPrefix(line, "+") {
			text := strings.ToLower(line[1:])
			if strings.Contains(text, pattern) {
				fmt.Printf("%s:%d | %s\n", file, newLine, strings.TrimSpace(line[1:]))
			}
			newLine++
			continue
		}
		if strings.HasPrefix(line, " ") {
			newLine++
		}
	}
}
