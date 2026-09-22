package logic

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	defaultPreviewChars     = 2000
	defaultRecentResults    = 3
	minimumCompactableChars = 120
)

type ContextRole string

const (
	ContextRoleUser          ContextRole = "user"
	ContextRoleAssistant     ContextRole = "assistant"
	ContextRoleToolUse       ContextRole = "tool_use"
	ContextRoleToolResult    ContextRole = "tool_result"
	ContextRoleArchiveMarker ContextRole = "archive_marker"
	ContextRoleSummary       ContextRole = "summary"
)

// ContextMessage is the model-visible representation of a review-stage message.
// ToolUseID keeps a tool call and its result pair recognizable after compaction.
type ContextMessage struct {
	Role      ContextRole `json:"role"`
	ToolUseID string      `json:"tool_use_id,omitempty"`
	Content   string      `json:"content"`
}

type CompactionEvent struct {
	Stage       string
	Message     string
	ArchivePath string
}

type CompactionResult struct {
	Messages []ContextMessage
	Events   []CompactionEvent
}

type ContextSummarizer func(context.Context, string, string) (string, error)

type CompactRequest struct {
	Messages      []ContextMessage
	ActiveRequest string
	Summarize     ContextSummarizer
}

// ContextCompactor applies low-loss transformations before asking a model for a
// summary. Persisted content is redacted before it is written to disk.
type ContextCompactor struct {
	ToolResultBudget     int
	LargeResultCharLimit int
	ContextCharLimit     int
	MaxMessages          int
	OutputDir            string
	TranscriptDir        string
}

func NewContextCompactor(cfg Config) *ContextCompactor {
	return &ContextCompactor{
		ToolResultBudget:     cfg.ToolResultBudget,
		LargeResultCharLimit: cfg.LargeResultCharLimit,
		ContextCharLimit:     cfg.ContextCharLimit,
		MaxMessages:          cfg.ContextMaxMessages,
		OutputDir:            cfg.ContextOutputDir,
		TranscriptDir:        cfg.ContextTranscriptDir,
	}
}

func (c *ContextCompactor) Prepare(ctx context.Context, req CompactRequest) (CompactionResult, error) {
	messages := append([]ContextMessage{}, req.Messages...)
	result := CompactionResult{Messages: messages, Events: []CompactionEvent{}}

	if err := c.applyToolResultBudget(&result); err != nil {
		return CompactionResult{}, err
	}
	if err := c.applySnipCompact(&result); err != nil {
		return CompactionResult{}, err
	}
	if estimateContextChars(result.Messages) <= c.ContextCharLimit {
		return result, nil
	}
	if err := c.applyMicroCompact(&result); err != nil {
		return CompactionResult{}, err
	}
	if estimateContextChars(result.Messages) > c.ContextCharLimit {
		if err := c.fitToolResults(&result); err != nil {
			return CompactionResult{}, err
		}
	}
	if estimateContextChars(result.Messages) <= c.ContextCharLimit {
		return result, nil
	}
	return c.compactHistory(ctx, result, req)
}

func (c *ContextCompactor) applyToolResultBudget(result *CompactionResult) error {
	total := toolResultChars(result.Messages)
	if total <= c.ToolResultBudget {
		return nil
	}
	indexes := toolResultIndexes(result.Messages)
	sort.Slice(indexes, func(i, j int) bool {
		return charCount(result.Messages[indexes[i]].Content) > charCount(result.Messages[indexes[j]].Content)
	})
	for _, index := range indexes {
		if total <= c.ToolResultBudget {
			break
		}
		content := result.Messages[index].Content
		if charCount(content) <= c.LargeResultCharLimit {
			continue
		}
		path, err := c.saveToolResult(result.Messages[index].ToolUseID, content)
		if err != nil {
			return err
		}
		result.Messages[index].Content = savedResultMarker(path, content, defaultPreviewChars)
		result.Events = append(result.Events, CompactionEvent{
			Stage:       "tool_result_budget",
			Message:     "large tool result persisted with preview",
			ArchivePath: path,
		})
		total = toolResultChars(result.Messages)
	}
	return nil
}

func (c *ContextCompactor) applySnipCompact(result *CompactionResult) error {
	maxMessages := max(4, c.MaxMessages)
	if len(result.Messages) <= maxMessages {
		return nil
	}
	headEnd := min(3, len(result.Messages))
	for headEnd < len(result.Messages) && isToolPair(result.Messages[headEnd-1], result.Messages[headEnd]) {
		headEnd++
	}
	tailCount := max(0, maxMessages-headEnd-1)
	tailStart := len(result.Messages) - tailCount
	if tailStart < headEnd {
		tailStart = headEnd
	}
	for tailStart > 0 && tailStart < len(result.Messages) && isToolPair(result.Messages[tailStart-1], result.Messages[tailStart]) {
		tailStart--
	}
	path, err := c.saveTranscript(result.Messages)
	if err != nil {
		return err
	}
	archived := tailStart - headEnd
	marker := ContextMessage{
		Role:    ContextRoleArchiveMarker,
		Content: fmt.Sprintf("[%d messages archived at %s]", archived, path),
	}
	result.Messages = append(append(append([]ContextMessage{}, result.Messages[:headEnd]...), marker), result.Messages[tailStart:]...)
	result.Events = append(result.Events, CompactionEvent{
		Stage:       "snip_compact",
		Message:     fmt.Sprintf("archived %d old messages", archived),
		ArchivePath: path,
	})
	return nil
}

func (c *ContextCompactor) applyMicroCompact(result *CompactionResult) error {
	target := c.ContextCharLimit * 80 / 100
	indexes := toolResultIndexes(result.Messages)
	keepFrom := max(0, len(indexes)-defaultRecentResults)
	for _, index := range indexes[:keepFrom] {
		if estimateContextChars(result.Messages) <= target {
			break
		}
		content := result.Messages[index].Content
		if charCount(content) <= minimumCompactableChars || isPersistedResultMarker(content) {
			continue
		}
		path, err := c.saveToolResult(result.Messages[index].ToolUseID, content)
		if err != nil {
			return err
		}
		result.Messages[index].Content = fmt.Sprintf("[Earlier tool result saved at %s]", path)
		result.Events = append(result.Events, CompactionEvent{
			Stage:       "micro_compact",
			Message:     "earlier tool result replaced with recoverable reference",
			ArchivePath: path,
		})
	}
	return nil
}

func (c *ContextCompactor) fitToolResults(result *CompactionResult) error {
	target := c.ContextCharLimit * 80 / 100
	indexes := toolResultIndexes(result.Messages)
	sort.Slice(indexes, func(i, j int) bool {
		return charCount(result.Messages[indexes[i]].Content) > charCount(result.Messages[indexes[j]].Content)
	})
	for _, index := range indexes {
		if estimateContextChars(result.Messages) <= target {
			break
		}
		content := result.Messages[index].Content
		if isPersistedResultMarker(content) {
			continue
		}
		path, err := c.saveToolResult(result.Messages[index].ToolUseID, content)
		if err != nil {
			return err
		}
		result.Messages[index].Content = savedResultMarker(path, content, 1000)
		result.Events = append(result.Events, CompactionEvent{
			Stage:       "fit_tool_results",
			Message:     "tool result persisted to fit context budget",
			ArchivePath: path,
		})
	}
	return nil
}

func (c *ContextCompactor) compactHistory(ctx context.Context, result CompactionResult, req CompactRequest) (CompactionResult, error) {
	path, err := c.saveTranscript(result.Messages)
	if err != nil {
		return CompactionResult{}, err
	}
	summary := fallbackContextSummary(result.Messages, path)
	if req.Summarize != nil {
		modelSummary, summarizeErr := req.Summarize(ctx, req.ActiveRequest, renderContextMessages(result.Messages))
		if summarizeErr == nil && strings.TrimSpace(modelSummary) != "" {
			summary = modelSummary
		} else if summarizeErr != nil {
			result.Events = append(result.Events, CompactionEvent{Stage: "compact_history_fallback", Message: "model summary unavailable; used deterministic summary", ArchivePath: path})
		}
	}
	summaryLimit := max(120, c.ContextCharLimit/2)
	summary = truncateRunes(summary, summaryLimit)
	activeRequest := truncateRunes(req.ActiveRequest, summaryLimit)
	result.Messages = []ContextMessage{{
		Role:    ContextRoleSummary,
		Content: fmt.Sprintf("[Compacted]\nCurrent user request:\n%s\n\nConversation summary:\n%s\n\nFull transcript: %s", activeRequest, summary, path),
	}}
	result.Events = append(result.Events, CompactionEvent{Stage: "compact_history", Message: "history replaced with summary", ArchivePath: path})
	return result, nil
}

func (c *ContextCompactor) saveToolResult(toolUseID, content string) (string, error) {
	if err := os.MkdirAll(c.OutputDir, 0755); err != nil {
		return "", fmt.Errorf("create tool result directory: %w", err)
	}
	name := toolUseID
	if name == "" {
		hash := sha256.Sum256([]byte(content))
		name = hex.EncodeToString(hash[:])[:16]
	}
	path := filepath.Join(c.OutputDir, name+".txt")
	if err := os.WriteFile(path, []byte(redact(content)), 0600); err != nil {
		return "", fmt.Errorf("save tool result: %w", err)
	}
	return path, nil
}

func (c *ContextCompactor) saveTranscript(messages []ContextMessage) (string, error) {
	if err := os.MkdirAll(c.TranscriptDir, 0755); err != nil {
		return "", fmt.Errorf("create transcript directory: %w", err)
	}
	redacted := make([]ContextMessage, 0, len(messages))
	for _, message := range messages {
		message.Content = redact(message.Content)
		redacted = append(redacted, message)
	}
	content, err := json.MarshalIndent(redacted, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode transcript: %w", err)
	}
	path := filepath.Join(c.TranscriptDir, fmt.Sprintf("review-%d.json", time.Now().UnixNano()))
	if err := os.WriteFile(path, content, 0600); err != nil {
		return "", fmt.Errorf("save transcript: %w", err)
	}
	return path, nil
}

func toolResultIndexes(messages []ContextMessage) []int {
	indexes := []int{}
	for index, message := range messages {
		if message.Role == ContextRoleToolResult {
			indexes = append(indexes, index)
		}
	}
	return indexes
}

func toolResultChars(messages []ContextMessage) int {
	total := 0
	for _, index := range toolResultIndexes(messages) {
		total += charCount(messages[index].Content)
	}
	return total
}

func estimateContextChars(messages []ContextMessage) int {
	content, _ := json.Marshal(messages)
	return utf8.RuneCount(content)
}

func renderContextMessages(messages []ContextMessage) string {
	parts := make([]string, 0, len(messages))
	for _, message := range messages {
		parts = append(parts, fmt.Sprintf("[%s]\n%s", message.Role, message.Content))
	}
	return strings.Join(parts, "\n\n")
}

func fallbackContextSummary(messages []ContextMessage, transcriptPath string) string {
	return fmt.Sprintf("已归档 %d 条上下文消息。完整、已脱敏记录位于 %s；后续审查应基于当前用户请求和可恢复引用继续。", len(messages), transcriptPath)
}

func savedResultMarker(path, content string, previewLimit int) string {
	return fmt.Sprintf("[Large tool result saved at %s]\nPreview:\n%s", path, truncateRunes(content, previewLimit))
}

func isPersistedResultMarker(content string) bool {
	return strings.HasPrefix(content, "[Large tool result saved at ") || strings.HasPrefix(content, "[Earlier tool result saved at ")
}

func isToolPair(previous, next ContextMessage) bool {
	return previous.Role == ContextRoleToolUse && next.Role == ContextRoleToolResult && previous.ToolUseID != "" && previous.ToolUseID == next.ToolUseID
}

func charCount(value string) int {
	return utf8.RuneCountInString(value)
}

func truncateRunes(value string, limit int) string {
	if charCount(value) <= limit {
		return value
	}
	return string([]rune(value)[:limit]) + "\n[truncated]"
}
