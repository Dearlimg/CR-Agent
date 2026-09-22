package logic

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestContextCompactorPersistsLargeToolResult(t *testing.T) {
	root := t.TempDir()
	compactor := &ContextCompactor{
		ToolResultBudget:     100,
		LargeResultCharLimit: 50,
		ContextCharLimit:     10000,
		MaxMessages:          50,
		OutputDir:            filepath.Join(root, "outputs"),
		TranscriptDir:        filepath.Join(root, "transcripts"),
	}
	content := strings.Repeat("审查输出", 1500)
	result, err := compactor.Prepare(context.Background(), CompactRequest{
		Messages: []ContextMessage{{Role: ContextRoleToolResult, ToolUseID: "subagent-security", Content: content}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Events) != 1 || result.Events[0].Stage != "tool_result_budget" {
		t.Fatalf("events = %#v", result.Events)
	}
	if strings.Contains(result.Messages[0].Content, content) || !strings.Contains(result.Messages[0].Content, "Large tool result saved at") {
		t.Fatalf("context still contains full result: %q", result.Messages[0].Content)
	}
	if _, err := os.Stat(filepath.Join(root, "outputs", "subagent-security.txt")); err != nil {
		t.Fatalf("persisted output missing: %v", err)
	}
}

func TestContextCompactorKeepsToolPairsAtSnipBoundary(t *testing.T) {
	root := t.TempDir()
	compactor := &ContextCompactor{
		ToolResultBudget:     10000,
		LargeResultCharLimit: 10000,
		ContextCharLimit:     10000,
		MaxMessages:          6,
		OutputDir:            filepath.Join(root, "outputs"),
		TranscriptDir:        filepath.Join(root, "transcripts"),
	}
	messages := []ContextMessage{
		{Role: ContextRoleUser, Content: "request"},
		{Role: ContextRoleAssistant, Content: "plan"},
		{Role: ContextRoleToolUse, ToolUseID: "first", Content: "call first"},
		{Role: ContextRoleToolResult, ToolUseID: "first", Content: "first result"},
		{Role: ContextRoleAssistant, Content: "analysis"},
		{Role: ContextRoleToolUse, ToolUseID: "second", Content: "call second"},
		{Role: ContextRoleToolResult, ToolUseID: "second", Content: "second result"},
		{Role: ContextRoleAssistant, Content: "more analysis"},
		{Role: ContextRoleToolUse, ToolUseID: "third", Content: "call third"},
		{Role: ContextRoleToolResult, ToolUseID: "third", Content: "third result"},
	}
	result, err := compactor.Prepare(context.Background(), CompactRequest{Messages: messages})
	if err != nil {
		t.Fatal(err)
	}
	for index := 1; index < len(result.Messages); index++ {
		if result.Messages[index].Role == ContextRoleToolResult && result.Messages[index-1].Role != ContextRoleToolUse {
			t.Fatalf("orphaned tool result at index %d: %#v", index, result.Messages)
		}
	}
	if !strings.Contains(renderContextMessages(result.Messages), "archived at") {
		t.Fatalf("missing archive marker: %#v", result.Messages)
	}
}

func TestContextCompactorUsesSummaryAndPreservesRequest(t *testing.T) {
	root := t.TempDir()
	compactor := &ContextCompactor{
		ToolResultBudget:     100000,
		LargeResultCharLimit: 100000,
		ContextCharLimit:     500,
		MaxMessages:          50,
		OutputDir:            filepath.Join(root, "outputs"),
		TranscriptDir:        filepath.Join(root, "transcripts"),
	}
	messages := []ContextMessage{}
	for index := range 6 {
		messages = append(messages, ContextMessage{
			Role:      ContextRoleToolResult,
			ToolUseID: "subagent-" + string(rune('a'+index)),
			Content:   strings.Repeat("finding ", 100),
		})
	}
	result, err := compactor.Prepare(context.Background(), CompactRequest{
		Messages:      messages,
		ActiveRequest: "汇总当前审查",
		Summarize: func(_ context.Context, activeRequest, history string) (string, error) {
			if activeRequest != "汇总当前审查" || history == "" {
				t.Fatalf("unexpected summarizer input: %q / %q", activeRequest, history)
			}
			return "已确认一个高置信度问题。", nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Messages) != 1 || result.Messages[0].Role != ContextRoleSummary {
		t.Fatalf("messages = %#v", result.Messages)
	}
	if !strings.Contains(result.Messages[0].Content, "汇总当前审查") || !strings.Contains(result.Messages[0].Content, "已确认一个高置信度问题") {
		t.Fatalf("summary lost request or facts: %q", result.Messages[0].Content)
	}
}
