package logic

import (
	"context"
	"strings"
	"testing"
)

func TestMCPClientRegisterAndCallKeepsErrorsAtBoundary(t *testing.T) {
	client := NewMCPClient()
	if err := client.Register(
		[]MCPTool{{Name: "search", Description: "search", InputSchema: objectSchema("query")}},
		map[string]MCPHandler{
			"search": func(_ context.Context, args map[string]any) (string, error) {
				query, ok := args["query"].(string)
				if !ok {
					return "", context.Canceled
				}
				return query, nil
			},
		},
	); err != nil {
		t.Fatal(err)
	}
	output, err := client.CallTool(context.Background(), "search", map[string]any{"query": "hooks"})
	if err != nil || output != "hooks" {
		t.Fatalf("output=%q err=%v", output, err)
	}
	_, err = client.CallTool(context.Background(), "missing", nil)
	if err == nil || !strings.Contains(err.Error(), "unknown tool") {
		t.Fatalf("unknown tool err=%v", err)
	}
}

func TestMCPManagerConnectAssemblesPrefixedToolsAndPolicy(t *testing.T) {
	manager := NewMCPManager(DefaultMCPHostPolicy())
	if err := manager.RegisterServer("docs.one", func() (*MCPClient, error) {
		client := NewMCPClient()
		err := client.Register(
			[]MCPTool{
				{Name: "get.version", Description: "version"},
				{Name: "trigger", Description: "trigger"},
			},
			map[string]MCPHandler{
				"get.version": func(_ context.Context, _ map[string]any) (string, error) { return "v1", nil },
				"trigger":     func(_ context.Context, _ map[string]any) (string, error) { return "queued", nil },
			},
		)
		return client, err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Connect(context.Background(), "docs.one"); err != nil {
		t.Fatal(err)
	}
	pool, err := manager.AssembleToolPool()
	if err != nil || len(pool) != 2 {
		t.Fatalf("pool=%#v err=%v", pool, err)
	}
	if pool[0].Name != "mcp__docs_one__get_version" {
		t.Fatalf("pool[0]=%#v", pool[0])
	}
	if pool[0].Permission != PermissionRepositoryExec || pool[1].Permission != PermissionRepositoryExec {
		t.Fatalf("permissions=%#v", pool)
	}
	output, err := manager.Connect(context.Background(), "docs.one")
	if err != nil || !strings.Contains(output, "already connected") {
		t.Fatalf("reconnect output=%q err=%v", output, err)
	}
}

func TestMCPConnectToolRegistersDiscoveredTools(t *testing.T) {
	manager := NewMCPManager(DefaultMCPHostPolicy())
	if err := manager.RegisterServer("docs", newDocsMCPServer); err != nil {
		t.Fatal(err)
	}
	registry := NewToolRegistry()
	manager.RegisterConnectTool(registry)
	connect, ok := registry.Get("connect_mcp")
	if !ok {
		t.Fatal("connect_mcp was not registered")
	}
	result, err := connect.Run(context.Background(), ToolInput{Args: map[string]any{"name": "docs"}})
	if err != nil || !strings.Contains(result.Output, "connected") {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	search, ok := registry.Get("mcp__docs__search")
	if !ok {
		t.Fatal("discovered docs search was not registered")
	}
	result, err = search.Run(context.Background(), ToolInput{Args: map[string]any{"query": "agent hooks"}})
	if err != nil || !strings.Contains(result.Output, "agent hooks") {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestMCPToolNameCollisionAfterNormalization(t *testing.T) {
	manager := NewMCPManager(NewMCPHostPolicy(map[MCPToolKey]PermissionDecision{
		{Server: "docs.one"}: PermissionAllow,
		{Server: "docs_one"}: PermissionAllow,
	}))
	for _, server := range []string{"docs.one", "docs_one"} {
		server := server
		if err := manager.RegisterServer(server, func() (*MCPClient, error) {
			client := NewMCPClient()
			return client, client.Register(
				[]MCPTool{{Name: "search"}},
				map[string]MCPHandler{"search": func(_ context.Context, _ map[string]any) (string, error) { return "", nil }},
			)
		}); err != nil {
			t.Fatal(err)
		}
		_, err := manager.Connect(context.Background(), server)
		if server == "docs.one" && err != nil {
			t.Fatal(err)
		}
		if server == "docs_one" && (err == nil || !strings.Contains(err.Error(), "collision")) {
			t.Fatalf("collision err=%v", err)
		}
	}
	if pool, err := manager.AssembleToolPool(); err != nil || len(pool) != 1 {
		t.Fatalf("failed connect poisoned pool: %#v %v", pool, err)
	}
}
