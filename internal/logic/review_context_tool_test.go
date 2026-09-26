package logic

import (
	"context"
	"testing"
)

func TestReviewContextToolSchemaRequiresSearchOrRange(t *testing.T) {
	registry := NewToolRegistry()
	registry.MustRegister(ToolDefinition{
		ToolMetadata: ToolMetadata{
			Name:        reviewContextToolName,
			Description: reviewContextToolDescription,
			InputSchema: reviewContextToolSchema(),
			Permission:  PermissionRepositoryRead,
		},
		Run: func(context.Context, ToolInput) (ToolResult, error) { return ToolResult{}, nil },
	})
	definition, ok := registry.Get(reviewContextToolName)
	if !ok {
		t.Fatal("review context tool not registered")
	}
	if _, err := definition.ToolMetadata.EinoInfo(); err != nil {
		t.Fatalf("convert source tool schema for model: %v", err)
	}
	for _, test := range []struct {
		name  string
		args  map[string]any
		valid bool
	}{
		{name: "file only", args: map[string]any{"file": "a.go"}},
		{name: "partial range", args: map[string]any{"file": "a.go", "start_line": 1}},
		{name: "directory", args: map[string]any{"directory": "."}, valid: true},
		{name: "query", args: map[string]any{"query": "Symbol"}, valid: true},
		{name: "file query", args: map[string]any{"file": "a.go", "query": "Symbol"}, valid: true},
		{name: "file range", args: map[string]any{"file": "a.go", "start_line": 1, "end_line": 20}, valid: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := definition.ValidateArguments(test.args)
			if (err == nil) != test.valid {
				t.Fatalf("valid=%v, want %v: %v", err == nil, test.valid, err)
			}
		})
	}
}
