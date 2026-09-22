package logic

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// MCPTool describes a tool discovered from an MCP server.
// The schema is intentionally kept as JSON-compatible data so the harness does
// not depend on one particular MCP transport implementation.
type MCPTool struct {
	Name            string         `json:"name"`
	Description     string         `json:"description"`
	InputSchema     map[string]any `json:"input_schema"`
	ReadOnlyHint    bool           `json:"readOnlyHint,omitempty"`
	DestructiveHint bool           `json:"destructiveHint,omitempty"`
}

type MCPHandler func(context.Context, map[string]any) (string, error)

// MCPClient stores tools discovered from one server and their call handlers.
// A client is transport-agnostic: stdio, HTTP and in-process servers can all
// expose the same register/call boundary later.
type MCPClient struct {
	mu       sync.RWMutex
	tools    []MCPTool
	handlers map[string]MCPHandler
}

func NewMCPClient() *MCPClient {
	return &MCPClient{tools: []MCPTool{}, handlers: map[string]MCPHandler{}}
}

func (c *MCPClient) Register(tools []MCPTool, handlers map[string]MCPHandler) error {
	if c == nil {
		return fmt.Errorf("MCP client 为空")
	}
	registered := make([]MCPTool, 0, len(tools))
	registeredHandlers := make(map[string]MCPHandler, len(tools))
	for _, tool := range tools {
		name := strings.TrimSpace(tool.Name)
		if name == "" {
			return fmt.Errorf("MCP 工具名不能为空")
		}
		if _, exists := registeredHandlers[name]; exists {
			return fmt.Errorf("MCP 工具重复注册: %q", name)
		}
		handler, ok := handlers[name]
		if !ok || handler == nil {
			return fmt.Errorf("MCP 工具缺少 handler: %q", name)
		}
		tool.Name = name
		tool.InputSchema = cloneSchema(tool.InputSchema)
		registered = append(registered, tool)
		registeredHandlers[name] = handler
	}
	c.mu.Lock()
	c.tools = registered
	c.handlers = registeredHandlers
	c.mu.Unlock()
	return nil
}

func (c *MCPClient) Tools() []MCPTool {
	if c == nil {
		return []MCPTool{}
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	tools := make([]MCPTool, len(c.tools))
	for i, tool := range c.tools {
		tools[i] = tool
		tools[i].InputSchema = cloneSchema(tool.InputSchema)
	}
	return tools
}

func (c *MCPClient) ListTools(ctx context.Context) ([]MCPTool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	return c.Tools(), nil
}

func (c *MCPClient) CallTool(ctx context.Context, name string, args map[string]any) (output string, err error) {
	if c == nil {
		return "", fmt.Errorf("MCP error: client 为空")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	c.mu.RLock()
	handler, ok := c.handlers[name]
	c.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("MCP error: unknown tool %q", name)
	}
	if args == nil {
		args = map[string]any{}
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			output = ""
			err = fmt.Errorf("MCP error: handler panic: %v", recovered)
		}
	}()
	output, err = handler(ctx, args)
	if err != nil {
		return "", fmt.Errorf("MCP error: %w", err)
	}
	return output, nil
}

type MCPServerFactory func() (*MCPClient, error)

type MCPToolKey struct {
	Server string
	Tool   string
}

// MCPHostPolicy is deliberately owned by the host. Server hints are metadata,
// not authorization. Unknown tools require approval by default.
type MCPHostPolicy struct {
	decisions map[MCPToolKey]PermissionDecision
}

func NewMCPHostPolicy(decisions map[MCPToolKey]PermissionDecision) *MCPHostPolicy {
	copyDecisions := make(map[MCPToolKey]PermissionDecision, len(decisions))
	for key, decision := range decisions {
		copyDecisions[key] = decision
	}
	return &MCPHostPolicy{decisions: copyDecisions}
}

func DefaultMCPHostPolicy() *MCPHostPolicy {
	return NewMCPHostPolicy(map[MCPToolKey]PermissionDecision{
		{Server: "docs", Tool: "search"}:      PermissionAllow,
		{Server: "docs", Tool: "get_version"}: PermissionAllow,
		{Server: "deploy", Tool: "status"}:    PermissionAllow,
		{Server: "deploy", Tool: "trigger"}:   PermissionRequireApproval,
	})
}

func (p *MCPHostPolicy) Decide(server, tool string) PermissionDecision {
	if p == nil {
		return PermissionRequireApproval
	}
	decision, ok := p.decisions[MCPToolKey{Server: server, Tool: tool}]
	if !ok {
		return PermissionRequireApproval
	}
	return decision
}

type MCPToolSpec struct {
	Server      string
	RawName     string
	Name        string
	Description string
	InputSchema map[string]any
	Permission  Permission
}

type MCPManager struct {
	mu        sync.RWMutex
	factories map[string]MCPServerFactory
	clients   map[string]*MCPClient
	policy    *MCPHostPolicy
}

func NewMCPManager(policy *MCPHostPolicy) *MCPManager {
	if policy == nil {
		policy = DefaultMCPHostPolicy()
	}
	return &MCPManager{
		factories: map[string]MCPServerFactory{},
		clients:   map[string]*MCPClient{},
		policy:    policy,
	}
}

func (m *MCPManager) RegisterServer(name string, factory MCPServerFactory) error {
	if m == nil {
		return fmt.Errorf("MCP manager 为空")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("MCP server 名不能为空")
	}
	if factory == nil {
		return fmt.Errorf("MCP server %q factory 不能为空", name)
	}
	m.mu.Lock()
	m.factories[name] = factory
	m.mu.Unlock()
	return nil
}

func (m *MCPManager) Connect(ctx context.Context, name string) (string, error) {
	if m == nil {
		return "", fmt.Errorf("MCP manager 为空")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	name = strings.TrimSpace(name)
	m.mu.RLock()
	_, connected := m.clients[name]
	factory, known := m.factories[name]
	m.mu.RUnlock()
	if connected {
		return fmt.Sprintf("MCP server %q already connected", name), nil
	}
	if !known {
		return "", fmt.Errorf("MCP error: unknown server %q", name)
	}
	client, err := factory()
	if err != nil {
		return "", fmt.Errorf("MCP error: connect %q: %w", name, err)
	}
	if client == nil {
		return "", fmt.Errorf("MCP error: connect %q returned nil client", name)
	}
	tools, err := client.ListTools(ctx)
	if err != nil {
		return "", fmt.Errorf("MCP error: list tools from %q: %w", name, err)
	}
	m.mu.Lock()
	if _, exists := m.clients[name]; exists {
		m.mu.Unlock()
		return fmt.Sprintf("MCP server %q already connected", name), nil
	}
	m.clients[name] = client
	m.mu.Unlock()
	return fmt.Sprintf("MCP server %q connected (%d tools)", name, len(tools)), nil
}

func (m *MCPManager) ConnectedServers() []string {
	if m == nil {
		return []string{}
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	servers := make([]string, 0, len(m.clients))
	for server := range m.clients {
		servers = append(servers, server)
	}
	sort.Strings(servers)
	return servers
}

func (m *MCPManager) AssembleToolPool() ([]MCPToolSpec, error) {
	if m == nil {
		return []MCPToolSpec{}, nil
	}
	m.mu.RLock()
	servers := make(map[string]*MCPClient, len(m.clients))
	for server, client := range m.clients {
		servers[server] = client
	}
	policy := m.policy
	m.mu.RUnlock()

	serverNames := make([]string, 0, len(servers))
	for server := range servers {
		serverNames = append(serverNames, server)
	}
	sort.Strings(serverNames)
	pool := []MCPToolSpec{}
	origins := map[string]MCPToolKey{}
	for _, server := range serverNames {
		tools := servers[server].Tools()
		sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
		for _, tool := range tools {
			prefixed, err := PrefixedMCPToolName(server, tool.Name)
			if err != nil {
				return nil, err
			}
			key := MCPToolKey{Server: server, Tool: tool.Name}
			if origin, exists := origins[prefixed]; exists {
				return nil, fmt.Errorf("MCP tool name collision after normalization: %q (%s/%s and %s/%s)", prefixed, origin.Server, origin.Tool, server, tool.Name)
			}
			origins[prefixed] = key
			permission := PermissionRepositoryExec
			if policy.Decide(server, tool.Name) == PermissionAllow {
				permission = PermissionReadDiff
			}
			pool = append(pool, MCPToolSpec{Server: server, RawName: tool.Name, Name: prefixed, Description: tool.Description, InputSchema: cloneSchema(tool.InputSchema), Permission: permission})
		}
	}
	return pool, nil
}

func (m *MCPManager) RegisterClientTools(registry *ToolRegistry, server string) error {
	if registry == nil {
		return fmt.Errorf("MCP tool registry 为空")
	}
	pool, err := m.AssembleToolPool()
	if err != nil {
		return err
	}
	m.mu.RLock()
	client := m.clients[server]
	m.mu.RUnlock()
	if client == nil {
		return fmt.Errorf("MCP server 未连接: %q", server)
	}
	for _, spec := range pool {
		if spec.Server != server {
			continue
		}
		rawName := spec.RawName
		mcpClient := client
		registry.RegisterWithPermission(spec.Name, spec.Permission, func(ctx context.Context, in ToolInput) (ToolResult, error) {
			output, callErr := mcpClient.CallTool(ctx, rawName, in.Args)
			if callErr != nil {
				return ToolResult{Output: callErr.Error()}, nil
			}
			return ToolResult{Output: output}, nil
		})
	}
	return nil
}

func (m *MCPManager) RegisterConnectTool(registry *ToolRegistry) {
	registry.RegisterWithPermission("connect_mcp", PermissionReadDiff, func(ctx context.Context, in ToolInput) (ToolResult, error) {
		name, ok := in.Args["name"].(string)
		if !ok || strings.TrimSpace(name) == "" {
			return ToolResult{Output: "MCP error: connect_mcp requires string argument name"}, nil
		}
		output, err := m.Connect(ctx, name)
		if err != nil {
			return ToolResult{Output: err.Error()}, nil
		}
		if err := m.RegisterClientTools(registry, name); err != nil {
			return ToolResult{Output: "MCP error: register discovered tools: " + err.Error()}, nil
		}
		return ToolResult{Output: output}, nil
	})
}

var mcpNamePattern = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

func NormalizeMCPName(name string) string {
	return mcpNamePattern.ReplaceAllString(strings.TrimSpace(name), "_")
}

func normalizeMCPName(name string) string {
	return NormalizeMCPName(name)
}

func PrefixedMCPToolName(server, tool string) (string, error) {
	safeServer := NormalizeMCPName(server)
	safeTool := NormalizeMCPName(tool)
	if safeServer == "" || safeTool == "" {
		return "", fmt.Errorf("MCP server/tool 名不能为空")
	}
	prefixed := "mcp__" + safeServer + "__" + safeTool
	if len(prefixed) > 64 {
		return "", fmt.Errorf("MCP tool name 超过 64 字符: %q", prefixed)
	}
	return prefixed, nil
}

func cloneSchema(schema map[string]any) map[string]any {
	if schema == nil {
		return map[string]any{}
	}
	clone := make(map[string]any, len(schema))
	for key, value := range schema {
		clone[key] = value
	}
	return clone
}

func newDocsMCPServer() (*MCPClient, error) {
	client := NewMCPClient()
	return client, client.Register(
		[]MCPTool{
			{Name: "search", Description: "搜索代码审查文档", InputSchema: objectSchema("query")},
			{Name: "get_version", Description: "获取文档 API 版本", InputSchema: map[string]any{"type": "object"}, ReadOnlyHint: true},
		},
		map[string]MCPHandler{
			"search": func(_ context.Context, args map[string]any) (string, error) {
				query, ok := args["query"].(string)
				if !ok || strings.TrimSpace(query) == "" {
					return "", fmt.Errorf("参数 query 必须是非空字符串")
				}
				return fmt.Sprintf("docs search results for %q", strings.TrimSpace(query)), nil
			},
			"get_version": func(_ context.Context, _ map[string]any) (string, error) {
				return "docs-api v1", nil
			},
		},
	)
}

func newDeployMCPServer() (*MCPClient, error) {
	client := NewMCPClient()
	return client, client.Register(
		[]MCPTool{
			{Name: "status", Description: "查看部署状态", InputSchema: map[string]any{"type": "object"}, ReadOnlyHint: true},
			{Name: "trigger", Description: "触发 web 服务部署", InputSchema: objectSchema("service"), DestructiveHint: true},
		},
		map[string]MCPHandler{
			"status": func(_ context.Context, _ map[string]any) (string, error) {
				return "web status: healthy", nil
			},
			"trigger": func(_ context.Context, args map[string]any) (string, error) {
				service, ok := args["service"].(string)
				if !ok || strings.TrimSpace(service) == "" {
					return "", fmt.Errorf("参数 service 必须是非空字符串")
				}
				return fmt.Sprintf("deployment queued for %q", strings.TrimSpace(service)), nil
			},
		},
	)
}

func objectSchema(required string) map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []string{required},
		"properties": map[string]any{
			required: map[string]any{"type": "string"},
		},
	}
}
