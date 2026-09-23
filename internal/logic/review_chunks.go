package logic

import (
	"fmt"
	"strings"
)

const (
	reviewDiffChunkCharLimit = 28000
	maxReviewDiffChunks      = 24
)

// splitReviewDiff keeps complete file sections and hunks together. A single
// hunk larger than the limit fails closed instead of silently dropping lines.
func splitReviewDiff(diff string, limit int) ([]string, error) {
	if limit <= 0 {
		limit = reviewDiffChunkCharLimit
	}
	if strings.TrimSpace(diff) == "" {
		return []string{""}, nil
	}

	sections := splitDiffSections(diff)
	if len(sections) == 0 {
		if len(diff) > limit {
			return nil, fmt.Errorf("diff has no file boundaries and exceeds the review chunk limit")
		}
		return []string{diff}, nil
	}

	packets := make([]string, 0, len(sections))
	for _, section := range sections {
		parts, err := splitOversizedDiffSection(section, limit)
		if err != nil {
			return nil, err
		}
		packets = append(packets, parts...)
	}

	chunks := make([]string, 0, len(packets))
	var current strings.Builder
	for _, packet := range packets {
		if current.Len() > 0 && current.Len()+len(packet) > limit {
			chunks = append(chunks, current.String())
			current.Reset()
		}
		if len(packet) > limit {
			return nil, fmt.Errorf("one review diff section exceeds the review chunk limit")
		}
		current.WriteString(packet)
	}
	if current.Len() > 0 {
		chunks = append(chunks, current.String())
	}
	if len(chunks) > maxReviewDiffChunks {
		return nil, fmt.Errorf("diff requires %d review chunks; limit is %d", len(chunks), maxReviewDiffChunks)
	}
	return chunks, nil
}

func splitDiffSections(diff string) []string {
	lines := strings.SplitAfter(diff, "\n")
	sections := make([]string, 0)
	var current strings.Builder
	for _, line := range lines {
		if strings.HasPrefix(line, "diff --git ") && current.Len() > 0 {
			sections = append(sections, current.String())
			current.Reset()
		}
		current.WriteString(line)
	}
	if current.Len() > 0 {
		sections = append(sections, current.String())
	}
	return sections
}

func splitOversizedDiffSection(section string, limit int) ([]string, error) {
	if len(section) <= limit {
		return []string{section}, nil
	}

	lines := strings.SplitAfter(section, "\n")
	firstHunk := -1
	for index, line := range lines {
		if strings.HasPrefix(line, "@@ ") {
			firstHunk = index
			break
		}
	}
	if firstHunk < 0 {
		return nil, fmt.Errorf("large diff section has no splittable hunks")
	}

	header := strings.Join(lines[:firstHunk], "")
	if len(header) >= limit {
		return nil, fmt.Errorf("diff file header exceeds the review chunk limit")
	}

	hunkStarts := make([]int, 0)
	for index := firstHunk; index < len(lines); index++ {
		if strings.HasPrefix(lines[index], "@@ ") {
			hunkStarts = append(hunkStarts, index)
		}
	}

	parts := make([]string, 0)
	var current strings.Builder
	current.WriteString(header)
	for position, start := range hunkStarts {
		end := len(lines)
		if position+1 < len(hunkStarts) {
			end = hunkStarts[position+1]
		}
		hunk := strings.Join(lines[start:end], "")
		if len(header)+len(hunk) > limit {
			return nil, fmt.Errorf("one diff hunk exceeds the review chunk limit")
		}
		if current.Len()+len(hunk) > limit {
			parts = append(parts, current.String())
			current.Reset()
			current.WriteString(header)
		}
		current.WriteString(hunk)
	}
	if current.Len() > len(header) {
		parts = append(parts, current.String())
	}
	return parts, nil
}
