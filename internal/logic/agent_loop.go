package logic

import (
	"CR-Agent/internal/model"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/eino-contrib/jsonschema"
	gojsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

type ToolInput struct {
	Job       *model.ReviewJob
	Diff      string
	Args      map[string]any
	Tracer    *TraceRecorder
	Artifacts *ReviewArtifacts
}
type ToolResult struct {
	Output      string
	Diff        string
	Done        bool
	CacheHit    bool
	ToolVersion string
	InputDigest string
	Comments    []model.ReviewComment
}
type Tool func(context.Context, ToolInput) (ToolResult, error)

// ToolMetadata is the shared contract used by the host loop, model harness,
// and discovered MCP tools.
type ToolMetadata struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema,omitempty"`
	Permission  Permission     `json:"permission"`
}

type ToolDefinition struct {
	ToolMetadata
	Run            Tool
	inputValidator *gojsonschema.Schema
}

var toolNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

func (metadata ToolMetadata) Validate() error {
	if !toolNamePattern.MatchString(metadata.Name) {
		return fmt.Errorf("工具名称必须为 1 到 64 个字母、数字、下划线或连字符: %q", metadata.Name)
	}
	if strings.TrimSpace(metadata.Description) == "" {
		return fmt.Errorf("工具 %q 缺少描述", metadata.Name)
	}
	if strings.TrimSpace(string(metadata.Permission)) == "" {
		return fmt.Errorf("工具 %q 缺少权限声明", metadata.Name)
	}
	switch metadata.Permission {
	case PermissionReadDiff, PermissionNetworkFetch, PermissionStaticAnalysis, PermissionLLMInference,
		PermissionRepositoryRead, PermissionRepositoryExec, PermissionSandboxExec, PermissionPublishReview,
		PermissionManageSchedule, PermissionJSONValidation:
	default:
		return fmt.Errorf("工具 %q 的权限类型无效: %q", metadata.Name, metadata.Permission)
	}
	if metadata.InputSchema == nil {
		return nil
	}
	encoded, err := json.Marshal(metadata.InputSchema)
	if err != nil {
		return fmt.Errorf("编码工具 %q 的输入 schema: %w", metadata.Name, err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(encoded, &parsed); err != nil {
		return fmt.Errorf("解析工具 %q 的输入 schema: %w", metadata.Name, err)
	}
	var parsedEinoSchema jsonschema.Schema
	if err := json.Unmarshal(encoded, &parsedEinoSchema); err != nil {
		return fmt.Errorf("解析工具 %q 的 Eino schema: %w", metadata.Name, err)
	}
	if parsed["type"] != "object" {
		return fmt.Errorf("工具 %q 的输入 schema 根类型必须为 object", metadata.Name)
	}
	properties, _ := parsed["properties"].(map[string]any)
	if rawRequired, ok := parsed["required"]; ok {
		required, ok := rawRequired.([]any)
		if !ok {
			return fmt.Errorf("工具 %q 的 required 必须是数组", metadata.Name)
		}
		for _, rawName := range required {
			name, ok := rawName.(string)
			if !ok || strings.TrimSpace(name) == "" {
				return fmt.Errorf("工具 %q 的 required 包含无效字段", metadata.Name)
			}
			if _, exists := properties[name]; !exists {
				return fmt.Errorf("工具 %q 将未声明字段 %q 标为必填", metadata.Name, name)
			}
		}
	}
	return nil
}

func (definition ToolDefinition) ValidateArguments(args map[string]any) error {
	if args == nil {
		args = map[string]any{}
	}
	if definition.InputSchema == nil {
		if len(args) > 0 {
			return fmt.Errorf("工具 %q 不接受参数", definition.Name)
		}
		return nil
	}
	validator := definition.inputValidator
	if validator == nil {
		var err error
		validator, err = definition.compileInputSchema()
		if err != nil {
			return fmt.Errorf("工具 %q 的输入 schema 无效: %w", definition.Name, err)
		}
	}
	if validator == nil {
		return nil
	}
	if err := validator.Validate(args); err != nil {
		return fmt.Errorf("工具 %q 的参数不符合输入 schema: %w", definition.Name, err)
	}
	return nil
}

func (definition ToolDefinition) compileInputSchema() (*gojsonschema.Schema, error) {
	if definition.InputSchema == nil {
		return nil, nil
	}
	encoded, err := json.Marshal(definition.InputSchema)
	if err != nil {
		return nil, fmt.Errorf("编码输入 schema: %w", err)
	}
	schemaDoc, err := gojsonschema.UnmarshalJSON(strings.NewReader(string(encoded)))
	if err != nil {
		return nil, fmt.Errorf("解析输入 schema: %w", err)
	}
	compiler := gojsonschema.NewCompiler()
	compiler.DefaultDraft(gojsonschema.Draft2020)
	compiler.UseLoader(disallowExternalToolSchemaLoader{})
	const schemaURL = "https://schemas.cr-agent.invalid/tool-input.json"
	if err := compiler.AddResource(schemaURL, schemaDoc); err != nil {
		return nil, fmt.Errorf("注册输入 schema: %w", err)
	}
	validator, err := compiler.Compile(schemaURL)
	if err != nil {
		return nil, fmt.Errorf("编译输入 schema: %w", err)
	}
	return validator, nil
}

type disallowExternalToolSchemaLoader struct{}

func (disallowExternalToolSchemaLoader) Load(url string) (any, error) {
	return nil, fmt.Errorf("不允许加载外部 JSON Schema 引用 %q", url)
}

func (metadata ToolMetadata) EinoInfo() (*schema.ToolInfo, error) {
	if err := metadata.Validate(); err != nil {
		return nil, err
	}
	info := &schema.ToolInfo{Name: metadata.Name, Desc: metadata.Description}
	if metadata.InputSchema == nil {
		return info, nil
	}
	encoded, err := json.Marshal(metadata.InputSchema)
	if err != nil {
		return nil, err
	}
	var params jsonschema.Schema
	if err := json.Unmarshal(encoded, &params); err != nil {
		return nil, fmt.Errorf("解析工具 %q 的 Eino schema: %w", metadata.Name, err)
	}
	info.ParamsOneOf = schema.NewParamsOneOfByJSONSchema(&params)
	return info, nil
}

func cloneToolDefinition(definition ToolDefinition) ToolDefinition {
	if definition.InputSchema != nil {
		encoded, err := json.Marshal(definition.InputSchema)
		if err == nil {
			var cloned map[string]any
			if json.Unmarshal(encoded, &cloned) == nil {
				definition.InputSchema = cloned
			}
		}
	}
	return definition
}

type ToolRegistry struct {
	mu    sync.RWMutex
	tools map[string]ToolDefinition
}

func NewToolRegistry() *ToolRegistry { return &ToolRegistry{tools: map[string]ToolDefinition{}} }

func (r *ToolRegistry) Register(definition ToolDefinition) error {
	return r.register(definition, false)
}

func (r *ToolRegistry) RegisterOrReplace(definition ToolDefinition) error {
	return r.register(definition, true)
}

func (r *ToolRegistry) MustRegister(definition ToolDefinition) {
	if err := r.Register(definition); err != nil {
		panic(err)
	}
}

func (r *ToolRegistry) register(definition ToolDefinition, replace bool) error {
	if r == nil {
		return fmt.Errorf("工具注册表为空")
	}
	if err := definition.ToolMetadata.Validate(); err != nil {
		return err
	}
	if definition.Run == nil {
		return fmt.Errorf("工具 %q 缺少执行函数", definition.Name)
	}
	definition = cloneToolDefinition(definition)
	validator, err := definition.compileInputSchema()
	if err != nil {
		return fmt.Errorf("工具 %q 的输入 schema 无效: %w", definition.Name, err)
	}
	definition.inputValidator = validator
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.tools == nil {
		r.tools = map[string]ToolDefinition{}
	}
	if _, exists := r.tools[definition.Name]; exists && !replace {
		return fmt.Errorf("工具 %q 已注册", definition.Name)
	}
	r.tools[definition.Name] = cloneToolDefinition(definition)
	return nil
}

func (r *ToolRegistry) Get(name string) (ToolDefinition, bool) {
	if r == nil {
		return ToolDefinition{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return cloneToolDefinition(t), ok
}

func (r *ToolRegistry) List() []ToolDefinition {
	if r == nil {
		return []ToolDefinition{}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	sort.Strings(names)
	definitions := make([]ToolDefinition, 0, len(names))
	for _, name := range names {
		definitions = append(definitions, cloneToolDefinition(r.tools[name]))
	}
	return definitions
}

type LoopStep struct {
	Tool   string
	Reason string
	Args   map[string]any
}
type AgentLoop struct {
	Registry *ToolRegistry
	Plan     []LoopStep
	MaxSteps int
	Record   func(string, string, string, string, string, string, string, int64) error
	Policy   *PermissionPolicy
	Hooks    *HookBus
}

func (a *AgentLoop) Run(ctx context.Context, input ToolInput) error {
	maxSteps := a.MaxSteps
	if maxSteps <= 0 {
		maxSteps = len(a.Plan)
	}
	if a.Hooks != nil {
		a.Hooks.Emit(ctx, HookLoopStart, HookContext{JobID: input.Job.ID})
		defer a.Hooks.Emit(ctx, HookLoopStop, HookContext{JobID: input.Job.ID})
	}
	for i, step := range a.Plan {
		if err := ctx.Err(); err != nil {
			return err
		}
		if i >= maxSteps {
			return fmt.Errorf("agent loop 超过最大步数 %d", maxSteps)
		}
		tool, ok := a.Registry.Get(step.Tool)
		if !ok {
			return fmt.Errorf("工具未注册: %s", step.Tool)
		}
		setTodoStatus(input.Job, i, "in_progress")
		var span *TraceSpan
		if input.Tracer != nil {
			span = input.Tracer.Start("tool", step.Tool, "action", step.Reason, "")
		}
		decision := a.Policy.Decide(tool.Permission)
		if decision != PermissionAllow {
			err := permissionError(step.Tool, tool.Permission, decision)
			traceID := id(step.Tool + err.Error())
			if span != nil {
				span.End(TraceResult{Status: "denied", Err: err, Origin: "orchestrator"})
				traceID = span.ID()
			} else {
				input.Job.Trace = append(input.Job.Trace, model.TraceEvent{ID: traceID, Tool: step.Tool, Input: step.Reason, Output: err.Error(), At: time.Now(), Phase: "permission"})
			}
			if a.Record != nil {
				_ = a.Record(input.Job.ID, traceID, step.Tool, string(decision), step.Reason, "", err.Error(), 0)
			}
			if a.Hooks != nil {
				a.Hooks.Emit(ctx, HookPermissionDenied, HookContext{JobID: input.Job.ID, Tool: step.Tool, Permission: tool.Permission, Reason: step.Reason, Error: err})
			}
			return err
		}
		if a.Hooks != nil {
			pre := a.Hooks.Emit(ctx, HookPreToolUse, HookContext{JobID: input.Job.ID, Tool: step.Tool, Permission: tool.Permission, Reason: step.Reason})
			if pre.Error != nil {
				return pre.Error
			}
		}
		started := time.Now()
		toolInput := input
		if step.Args != nil {
			toolInput.Args = step.Args
		}
		var result ToolResult
		var err error
		if validationErr := tool.ValidateArguments(toolInput.Args); validationErr != nil {
			err = fmt.Errorf("工具参数类型检查失败: %w", validationErr)
			result, err = ToolResult{}, err
		} else {
			result, err = tool.Run(ctx, toolInput)
		}
		traceID := id(step.Tool + step.Reason + time.Now().String())
		if err != nil {
			duration := time.Since(started).Milliseconds()
			if span != nil {
				span.End(TraceResult{Err: err, Origin: "orchestrator"})
				traceID = span.ID()
			} else {
				input.Job.Trace = append(input.Job.Trace, model.TraceEvent{ID: traceID, Tool: step.Tool, Input: step.Reason, Output: err.Error(), At: time.Now(), DurationMs: duration, Phase: "action"})
			}
			if a.Record != nil {
				_ = a.Record(input.Job.ID, traceID, step.Tool, "failed", step.Reason, "", err.Error(), duration)
			}
			if a.Hooks != nil {
				a.Hooks.Emit(ctx, HookToolError, HookContext{JobID: input.Job.ID, Tool: step.Tool, Permission: tool.Permission, Reason: step.Reason, Error: err, DurationMs: duration})
			}
			return err
		}
		setTodoStatus(input.Job, i, "completed")
		duration := time.Since(started).Milliseconds()
		if span != nil {
			span.End(TraceResult{
				Output: result.Output, Origin: "orchestrator", CacheHit: result.CacheHit,
				ToolVersion: result.ToolVersion, InputDigest: result.InputDigest,
			})
			traceID = span.ID()
		} else {
			input.Job.Trace = append(input.Job.Trace, model.TraceEvent{ID: traceID, Tool: step.Tool, Input: step.Reason, Output: result.Output, At: time.Now(), DurationMs: duration, Phase: "action"})
		}
		if a.Record != nil {
			_ = a.Record(input.Job.ID, traceID, step.Tool, "succeeded", step.Reason, result.Output, "", duration)
		}
		if a.Hooks != nil {
			a.Hooks.Emit(ctx, HookPostToolUse, HookContext{JobID: input.Job.ID, Tool: step.Tool, Permission: tool.Permission, Reason: step.Reason, Output: result.Output, DurationMs: duration})
		}
		if result.Diff != "" {
			input.Diff = result.Diff
		}
		if len(result.Comments) > 0 {
			input.Job.Comments = result.Comments
		}
		if result.Done {
			return nil
		}
	}
	return nil
}

func setTodoStatus(job *model.ReviewJob, order int, status string) {
	for i := range job.Todos {
		if job.Todos[i].Order == order {
			job.Todos[i].Status = status
			return
		}
	}
}
