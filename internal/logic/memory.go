package logic

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
)

type MemoryType string

const (
	MemoryTypeUser      MemoryType = "user"
	MemoryTypeFeedback  MemoryType = "feedback"
	MemoryTypeProject   MemoryType = "project"
	MemoryTypeReference MemoryType = "reference"
)

type MemoryRecord struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Type        MemoryType `json:"type"`
	Body        string     `json:"body"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type MemoryCandidate struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Type        MemoryType `json:"type"`
	Body        string     `json:"body"`
	Scope       string     `json:"scope"`
}

// MemoryStore keeps cross-review knowledge as one Markdown document per record.
// It stores only validated persistent information, never complete transcripts.
type MemoryStore struct {
	root          string
	maxRecall     int
	maxChars      int
	consolidateAt int
}

func NewMemoryStore(cfg Config) *MemoryStore {
	root := cfg.MemoryDir
	if root == "" {
		root = ".memory"
	}
	maxRecall := cfg.MemoryMaxRecall
	if maxRecall <= 0 {
		maxRecall = 5
	}
	maxChars := cfg.MemoryMaxChars
	if maxChars <= 0 {
		maxChars = 6000
	}
	consolidateAt := cfg.MemoryConsolidateAt
	if consolidateAt <= 0 {
		consolidateAt = 10
	}
	return &MemoryStore{
		root:          root,
		maxRecall:     maxRecall,
		maxChars:      maxChars,
		consolidateAt: consolidateAt,
	}
}

func (s *MemoryStore) Ensure() error {
	return os.MkdirAll(s.root, 0755)
}

func (s *MemoryStore) List() ([]MemoryRecord, error) {
	if err := s.Ensure(); err != nil {
		return nil, fmt.Errorf("create memory directory: %w", err)
	}
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return nil, fmt.Errorf("read memory directory: %w", err)
	}
	records := []MemoryRecord{}
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == "MEMORY.md" || filepath.Ext(entry.Name()) != ".md" {
			continue
		}
		content, err := os.ReadFile(filepath.Join(s.root, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("read memory %q: %w", entry.Name(), err)
		}
		record, ok := parseMemoryDocument(string(content))
		if ok {
			records = append(records, record)
		}
	}
	sort.Slice(records, func(i, j int) bool {
		return records[i].UpdatedAt.After(records[j].UpdatedAt)
	})
	return records, nil
}

func (s *MemoryStore) Save(candidate MemoryCandidate) (MemoryRecord, bool, error) {
	if !shouldStoreMemory(candidate) {
		return MemoryRecord{}, false, nil
	}
	if err := s.Ensure(); err != nil {
		return MemoryRecord{}, false, fmt.Errorf("create memory directory: %w", err)
	}
	records, err := s.List()
	if err != nil {
		return MemoryRecord{}, false, err
	}
	record := MemoryRecord{
		Name:        memorySlug(candidate.Name),
		Description: redact(strings.TrimSpace(candidate.Description)),
		Type:        candidate.Type,
		Body:        redact(strings.TrimSpace(candidate.Body)),
		UpdatedAt:   time.Now().UTC(),
	}
	for _, existing := range records {
		if existing.Name == record.Name || memorySimilarity(existing, record) >= 0.85 {
			return existing, false, nil
		}
	}
	if err := s.writeRecord(record); err != nil {
		return MemoryRecord{}, false, err
	}
	if err := s.rebuildIndex(); err != nil {
		return MemoryRecord{}, false, err
	}
	if len(records)+1 >= s.consolidateAt {
		if err := s.Consolidate(); err != nil {
			return MemoryRecord{}, false, err
		}
	}
	return record, true, nil
}

func (s *MemoryStore) Recall(query string) ([]MemoryRecord, error) {
	records, err := s.List()
	if err != nil {
		return nil, err
	}
	type scoredMemory struct {
		record MemoryRecord
		score  int
	}
	queryTerms := memoryTerms(query)
	scored := make([]scoredMemory, 0, len(records))
	for _, record := range records {
		score := overlapScore(queryTerms, memoryTerms(record.Name+" "+record.Description+" "+record.Body))
		if score > 0 {
			scored = append(scored, scoredMemory{record: record, score: score})
		}
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score == scored[j].score {
			return scored[i].record.UpdatedAt.After(scored[j].record.UpdatedAt)
		}
		return scored[i].score > scored[j].score
	})
	selected := []MemoryRecord{}
	usedChars := 0
	for _, item := range scored {
		if len(selected) >= s.maxRecall {
			break
		}
		entryChars := charCount(item.record.Description) + charCount(item.record.Body)
		if usedChars+entryChars > s.maxChars && len(selected) > 0 {
			continue
		}
		selected = append(selected, item.record)
		usedChars += entryChars
	}
	return selected, nil
}

// Consolidate removes only duplicate records and rebuilds the index. It leaves
// distinct or conflicting records intact so current work can resolve conflicts.
func (s *MemoryStore) Consolidate() error {
	records, err := s.List()
	if err != nil {
		return err
	}
	snapshot, err := s.snapshotDocuments()
	if err != nil {
		return err
	}
	duplicates := map[string]bool{}
	for i, record := range records {
		if duplicates[record.Name] {
			continue
		}
		for _, other := range records[i+1:] {
			if record.Type == other.Type && memorySimilarity(record, other) >= 0.9 {
				duplicates[other.Name] = true
			}
		}
	}
	for name := range duplicates {
		path := filepath.Join(s.root, name+".md")
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return s.restoreSnapshot(snapshot, fmt.Errorf("remove duplicate memory %q: %w", name, err))
		}
	}
	if err := s.rebuildIndex(); err != nil {
		return s.restoreSnapshot(snapshot, err)
	}
	return nil
}

func (s *MemoryStore) snapshotDocuments() (map[string][]byte, error) {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return nil, fmt.Errorf("list memory snapshot: %w", err)
	}
	snapshot := map[string][]byte{}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
			continue
		}
		content, err := os.ReadFile(filepath.Join(s.root, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("read memory snapshot %q: %w", entry.Name(), err)
		}
		snapshot[entry.Name()] = content
	}
	return snapshot, nil
}

func (s *MemoryStore) restoreSnapshot(snapshot map[string][]byte, cause error) error {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return fmt.Errorf("restore memory snapshot after %v: list files: %w", cause, err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
			continue
		}
		if err := os.Remove(filepath.Join(s.root, entry.Name())); err != nil {
			return fmt.Errorf("restore memory snapshot after %v: remove %q: %w", cause, entry.Name(), err)
		}
	}
	for name, content := range snapshot {
		if err := os.WriteFile(filepath.Join(s.root, name), content, 0600); err != nil {
			return fmt.Errorf("restore memory snapshot after %v: write %q: %w", cause, name, err)
		}
	}
	return fmt.Errorf("memory consolidation rolled back: %w", cause)
}

func (s *MemoryStore) writeRecord(record MemoryRecord) error {
	path := filepath.Join(s.root, record.Name+".md")
	content := fmt.Sprintf("---\nname: %s\ndescription: %s\ntype: %s\nupdated_at: %s\n---\n\n%s\n", record.Name, record.Description, record.Type, record.UpdatedAt.Format(time.RFC3339), record.Body)
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		return fmt.Errorf("write memory %q: %w", record.Name, err)
	}
	return nil
}

func (s *MemoryStore) rebuildIndex() error {
	records, err := s.List()
	if err != nil {
		return err
	}
	lines := []string{"# Memory Index", ""}
	for _, record := range records {
		lines = append(lines, fmt.Sprintf("- %s | type=%s | %s", record.Name, record.Type, record.Description))
	}
	path := filepath.Join(s.root, "MEMORY.md")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0600); err != nil {
		return fmt.Errorf("write memory index: %w", err)
	}
	return nil
}

func shouldStoreMemory(candidate MemoryCandidate) bool {
	if strings.ToLower(strings.TrimSpace(candidate.Scope)) != "persistent" {
		return false
	}
	if !validMemoryType(candidate.Type) || memorySlug(candidate.Name) == "" {
		return false
	}
	if strings.TrimSpace(candidate.Description) == "" || strings.TrimSpace(candidate.Body) == "" {
		return false
	}
	if strings.Contains(redact(candidate.Description+"\n"+candidate.Body), "[REDACTED]") {
		return false
	}
	combined := strings.ToLower(candidate.Name + " " + candidate.Description + " " + candidate.Body)
	temporaryTerms := []string{"this session", "current task", "本次会话", "当前任务", "这次任务", "临时"}
	for _, term := range temporaryTerms {
		if strings.Contains(combined, term) {
			return false
		}
	}
	return true
}

func parseMemoryCandidates(raw string) []MemoryCandidate {
	clean := strings.TrimSpace(strings.Trim(raw, "`"))
	candidates := []MemoryCandidate{}
	if json.Unmarshal([]byte(clean), &candidates) == nil {
		return candidates
	}
	start, end := strings.Index(clean, "["), strings.LastIndex(clean, "]")
	if start >= 0 && end > start {
		_ = json.Unmarshal([]byte(clean[start:end+1]), &candidates)
	}
	return candidates
}

func validMemoryType(memoryType MemoryType) bool {
	switch memoryType {
	case MemoryTypeUser, MemoryTypeFeedback, MemoryTypeProject, MemoryTypeReference:
		return true
	default:
		return false
	}
}

func parseMemoryDocument(content string) (MemoryRecord, bool) {
	if !strings.HasPrefix(content, "---\n") {
		return MemoryRecord{}, false
	}
	end := strings.Index(content[4:], "\n---")
	if end < 0 {
		return MemoryRecord{}, false
	}
	values := map[string]string{}
	for _, line := range strings.Split(content[4:end+4], "\n") {
		key, value, ok := strings.Cut(line, ":")
		if ok {
			values[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	updatedAt, err := time.Parse(time.RFC3339, values["updated_at"])
	if err != nil || !validMemoryType(MemoryType(values["type"])) {
		return MemoryRecord{}, false
	}
	record := MemoryRecord{
		Name:        memorySlug(values["name"]),
		Description: values["description"],
		Type:        MemoryType(values["type"]),
		Body:        strings.TrimSpace(content[end+8:]),
		UpdatedAt:   updatedAt,
	}
	return record, record.Name != "" && record.Description != "" && record.Body != ""
}

func renderMemories(records []MemoryRecord) string {
	if len(records) == 0 {
		return "(none)"
	}
	parts := make([]string, 0, len(records))
	for _, record := range records {
		parts = append(parts, fmt.Sprintf("- [%s] %s: %s\n%s", record.Type, record.Name, record.Description, record.Body))
	}
	return strings.Join(parts, "\n\n")
}

func memorySimilarity(left, right MemoryRecord) float64 {
	leftTerms := memoryTerms(left.Description + " " + left.Body)
	rightTerms := memoryTerms(right.Description + " " + right.Body)
	if len(leftTerms) == 0 || len(rightTerms) == 0 {
		return 0
	}
	intersection := overlapScore(leftTerms, rightTerms)
	return float64(intersection) / float64(len(leftTerms)+len(rightTerms)-intersection)
}

func memoryTerms(value string) map[string]bool {
	terms := map[string]bool{}
	for _, token := range strings.FieldsFunc(strings.ToLower(value), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	}) {
		if charCount(token) >= 2 {
			terms[token] = true
		}
	}
	return terms
}

func overlapScore(left, right map[string]bool) int {
	score := 0
	for term := range left {
		if right[term] {
			score++
		}
	}
	return score
}

func memorySlug(value string) string {
	var builder strings.Builder
	previousDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(value)) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			builder.WriteRune(r)
			previousDash = false
			continue
		}
		if builder.Len() > 0 && !previousDash {
			builder.WriteByte('-')
			previousDash = true
		}
	}
	return strings.Trim(builder.String(), "-")
}
