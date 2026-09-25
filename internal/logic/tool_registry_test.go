package logic

import (
	"context"
	"testing"
)

func TestToolRegistryValidatesAndPreservesSharedMetadata(t *testing.T) {
	metadata := ToolMetadata{
		Name: "inspect_value", Description: "Inspect a value",
		InputSchema: map[string]any{
			"type": "object", "additionalProperties": false,
			"properties": map[string]any{"value": map[string]any{"type": "string"}},
			"required":   []string{"value"},
		},
		Permission: PermissionReadDiff,
	}
	definition := ToolDefinition{
		ToolMetadata: metadata,
		Run:          func(context.Context, ToolInput) (ToolResult, error) { return ToolResult{}, nil },
	}
	registry := NewToolRegistry()
	if err := registry.Register(definition); err != nil {
		t.Fatalf("register valid tool: %v", err)
	}
	if err := registry.Register(definition); err == nil {
		t.Fatal("duplicate registration was accepted")
	}
	info, err := metadata.EinoInfo()
	if err != nil {
		t.Fatalf("convert metadata to Eino info: %v", err)
	}
	if info.Name != metadata.Name || info.Desc != metadata.Description || info.ParamsOneOf == nil {
		t.Fatalf("tool info=%#v", info)
	}
	jsonSchema, err := info.ParamsOneOf.ToJSONSchema()
	if err != nil || len(jsonSchema.Required) != 1 || jsonSchema.Required[0] != "value" {
		t.Fatalf("converted schema=%#v err=%v", jsonSchema, err)
	}

	loaded, ok := registry.Get(metadata.Name)
	if !ok {
		t.Fatal("registered tool could not be read")
	}
	loadedProperties := loaded.InputSchema["properties"].(map[string]any)
	loadedProperties["value"].(map[string]any)["type"] = "integer"
	stored, _ := registry.Get(metadata.Name)
	storedProperties := stored.InputSchema["properties"].(map[string]any)
	if storedProperties["value"].(map[string]any)["type"] != "string" {
		t.Fatalf("caller mutated the registry schema: %#v", stored.InputSchema)
	}
}

func TestToolRegistryRejectsIncompleteOrInvalidDefinitions(t *testing.T) {
	handler := Tool(func(context.Context, ToolInput) (ToolResult, error) { return ToolResult{}, nil })
	valid := ToolMetadata{
		Name: "sample", Description: "sample tool",
		InputSchema: map[string]any{
			"type": "object", "properties": map[string]any{"value": map[string]any{"type": "string"}},
			"required": []string{"value"},
		},
		Permission: PermissionReadDiff,
	}
	for _, test := range []struct {
		name       string
		metadata   ToolMetadata
		missingRun bool
	}{
		{name: "invalid name", metadata: ToolMetadata{Name: "bad/name", Description: "bad", Permission: PermissionReadDiff}},
		{name: "missing description", metadata: ToolMetadata{Name: "sample", Permission: PermissionReadDiff}},
		{name: "missing permission", metadata: ToolMetadata{Name: "sample", Description: "sample"}},
		{name: "invalid schema root", metadata: ToolMetadata{Name: "sample", Description: "sample", InputSchema: map[string]any{"type": "string"}, Permission: PermissionReadDiff}},
		{name: "required field missing from schema", metadata: ToolMetadata{Name: "sample", Description: "sample", InputSchema: map[string]any{"type": "object", "required": []string{"missing"}}, Permission: PermissionReadDiff}},
		{name: "missing handler", metadata: valid, missingRun: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			registry := NewToolRegistry()
			definition := ToolDefinition{ToolMetadata: test.metadata, Run: handler}
			if test.missingRun {
				definition.Run = nil
			}
			if err := registry.Register(definition); err == nil {
				t.Fatal("invalid tool definition was accepted")
			}
		})
	}
}

func TestToolDefinitionTypeChecksArgumentsBeforeExecution(t *testing.T) {
	definition := ToolDefinition{
		ToolMetadata: ToolMetadata{
			Name: "validate_args", Description: "Validate arguments",
			InputSchema: map[string]any{
				"type": "object", "additionalProperties": false,
				"properties": map[string]any{
					"name":  map[string]any{"type": "string"},
					"count": map[string]any{"type": "integer"},
				},
				"required": []string{"name"},
			},
			Permission: PermissionReadDiff,
		},
		Run: func(context.Context, ToolInput) (ToolResult, error) { return ToolResult{}, nil },
	}
	for _, args := range []map[string]any{
		{"name": "review", "count": float64(3)},
		{"name": "review", "count": float64(3.5)},
		{"name": "review", "unexpected": true},
		{"count": float64(3)},
	} {
		err := definition.ValidateArguments(args)
		if args["count"] == float64(3) && args["unexpected"] == nil && args["name"] == "review" && err != nil {
			t.Fatalf("valid arguments rejected: %v", err)
		}
		if args["name"] != "review" || args["count"] == float64(3.5) || args["unexpected"] != nil {
			if err == nil {
				t.Fatalf("invalid arguments accepted: %#v", args)
			}
		}
	}
}
