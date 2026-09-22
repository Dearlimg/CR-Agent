package logic

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

type WorkflowMeta struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Phases      []string `json:"phases,omitempty"`
}

type WorkflowAgentResult struct {
	Value  any
	Tokens int
}

type WorkflowAgentRunner func(context.Context, string, map[string]any, string) (WorkflowAgentResult, error)
type WorkflowScript func(*WorkflowExecutionState, map[string]any) (any, error)

type workflowDefinition struct {
	Meta   WorkflowMeta
	Script WorkflowScript
}

type WorkflowRegistry struct {
	mu          sync.RWMutex
	definitions map[string]workflowDefinition
}

func NewWorkflowRegistry() *WorkflowRegistry {
	return &WorkflowRegistry{definitions: map[string]workflowDefinition{}}
}

func (r *WorkflowRegistry) Register(meta WorkflowMeta, script WorkflowScript) error {
	if err := ValidateWorkflowMeta(meta); err != nil {
		return err
	}
	if script == nil {
		return fmt.Errorf("workflow %q script 不能为空", meta.Name)
	}
	r.mu.Lock()
	r.definitions[meta.Name] = workflowDefinition{Meta: meta, Script: script}
	r.mu.Unlock()
	return nil
}

func (r *WorkflowRegistry) Get(name string) (WorkflowMeta, WorkflowScript, bool) {
	if r == nil {
		return WorkflowMeta{}, nil, false
	}
	r.mu.RLock()
	definition, ok := r.definitions[name]
	r.mu.RUnlock()
	if !ok {
		return WorkflowMeta{}, nil, false
	}
	return definition.Meta, definition.Script, true
}

func (r *WorkflowRegistry) List() []WorkflowMeta {
	if r == nil {
		return []WorkflowMeta{}
	}
	r.mu.RLock()
	metas := make([]WorkflowMeta, 0, len(r.definitions))
	for _, definition := range r.definitions {
		metas = append(metas, definition.Meta)
	}
	r.mu.RUnlock()
	sort.Slice(metas, func(i, j int) bool { return metas[i].Name < metas[j].Name })
	return metas
}

var workflowNamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

func ValidateWorkflowMeta(meta WorkflowMeta) error {
	if strings.TrimSpace(meta.Name) == "" || strings.TrimSpace(meta.Description) == "" {
		return fmt.Errorf("workflow meta 必须包含 name 和 description")
	}
	if !workflowNamePattern.MatchString(meta.Name) {
		return fmt.Errorf("workflow meta.name 必须是 1-64 字符的安全 slug")
	}
	for _, phase := range meta.Phases {
		if strings.TrimSpace(phase) == "" {
			return fmt.Errorf("workflow meta.phases 必须包含非空字符串")
		}
	}
	return nil
}

type WorkflowStatus string

const (
	WorkflowPending   WorkflowStatus = "pending"
	WorkflowRunning   WorkflowStatus = "running"
	WorkflowCompleted WorkflowStatus = "completed"
	WorkflowFailed    WorkflowStatus = "failed"
	WorkflowCancelled WorkflowStatus = "cancelled"
)

type WorkflowTask struct {
	ID         string         `json:"id"`
	Workflow   string         `json:"workflow"`
	Status     WorkflowStatus `json:"status"`
	CreatedAt  time.Time      `json:"created_at"`
	StartedAt  *time.Time     `json:"started_at,omitempty"`
	FinishedAt *time.Time     `json:"finished_at,omitempty"`
	Result     any            `json:"result,omitempty"`
	Error      string         `json:"error,omitempty"`
	AgentCount int            `json:"agent_count"`
	TokenUsage int            `json:"token_usage"`
	NotifiedAt *time.Time     `json:"notified_at,omitempty"`
}

type WorkflowEvent struct {
	Type      string         `json:"type"`
	RunID     string         `json:"run_id"`
	Workflow  string         `json:"workflow"`
	Phase     string         `json:"phase,omitempty"`
	Label     string         `json:"label,omitempty"`
	Message   string         `json:"message,omitempty"`
	Status    WorkflowStatus `json:"status,omitempty"`
	Timestamp time.Time      `json:"timestamp"`
}

type WorkflowSnapshot struct {
	RunID    string          `json:"run_id"`
	Workflow string          `json:"workflow"`
	Args     map[string]any  `json:"args"`
	Task     WorkflowTask    `json:"task"`
	Events   []WorkflowEvent `json:"events,omitempty"`
}

type WorkflowLaunch struct {
	Launched bool         `json:"launched"`
	RunID    string       `json:"run_id"`
	Task     WorkflowTask `json:"task"`
	Result   any          `json:"result,omitempty"`
}

type WorkflowRuntime struct {
	registry    *WorkflowRegistry
	persistence WorkflowPersistence
	mu          sync.Mutex
	cancels     map[string]context.CancelFunc
}

var workflowRunIDPattern = regexp.MustCompile(`^wf_[0-9a-f]{16}$`)

func NewWorkflowRuntime(root string, registry *WorkflowRegistry) *WorkflowRuntime {
	if registry == nil {
		registry = NewWorkflowRegistry()
	}
	return NewWorkflowRuntimeWithPersistence(registry, NewFileWorkflowPersistence(root))
}

func NewWorkflowRuntimeWithPersistence(registry *WorkflowRegistry, persistence WorkflowPersistence) *WorkflowRuntime {
	if registry == nil {
		registry = NewWorkflowRegistry()
	}
	if persistence == nil {
		persistence = NewFileWorkflowPersistence("")
	}
	return &WorkflowRuntime{
		registry:    registry,
		persistence: persistence,
		cancels:     map[string]context.CancelFunc{},
	}
}

func (r *WorkflowRuntime) Registry() *WorkflowRegistry { return r.registry }

func (r *WorkflowRuntime) Launch(
	ctx context.Context,
	name string,
	args map[string]any,
	resume string,
	runner WorkflowAgentRunner,
) (WorkflowLaunch, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	meta, script, ok := r.registry.Get(name)
	if !ok {
		return WorkflowLaunch{}, fmt.Errorf("unknown workflow %q", name)
	}
	if runner == nil {
		return WorkflowLaunch{}, fmt.Errorf("workflow agent runner 未配置")
	}
	if args == nil {
		args = map[string]any{}
	}
	runID := strings.TrimSpace(resume)
	if runID == "" {
		var err error
		runID, err = newWorkflowRunID()
		if err != nil {
			return WorkflowLaunch{}, err
		}
	}
	if !workflowRunIDPattern.MatchString(runID) {
		return WorkflowLaunch{}, fmt.Errorf("invalid workflow run ID %q", runID)
	}
	lock, err := r.persistence.AcquireLock(runID)
	if err != nil {
		return WorkflowLaunch{}, err
	}
	snapshot, err := r.loadSnapshot(runID)
	if err != nil && resume != "" {
		_ = lock.Release()
		return WorkflowLaunch{}, fmt.Errorf("resume workflow: %w", err)
	}
	if resume != "" && snapshot.Workflow != meta.Name {
		_ = lock.Release()
		return WorkflowLaunch{}, fmt.Errorf("resume workflow name mismatch: %q", snapshot.Workflow)
	}
	if snapshot.RunID == "" {
		now := time.Now().UTC()
		snapshot = WorkflowSnapshot{
			RunID: runID, Workflow: meta.Name, Args: cloneWorkflowArgs(args),
			Task: WorkflowTask{ID: runID, Workflow: meta.Name, Status: WorkflowPending, CreatedAt: now},
		}
	} else {
		args = cloneWorkflowArgs(snapshot.Args)
	}
	now := time.Now().UTC()
	snapshot.Task.Status = WorkflowRunning
	snapshot.Task.StartedAt = &now
	snapshot.Task.FinishedAt = nil
	snapshot.Task.NotifiedAt = nil
	snapshot.Task.Error = ""
	if err := r.writeSnapshot(snapshot); err != nil {
		_ = lock.Release()
		return WorkflowLaunch{}, err
	}
	runCtx, cancel := context.WithCancel(ctx)
	r.mu.Lock()
	r.cancels[runID] = cancel
	r.mu.Unlock()
	go func() {
		defer func() {
			r.mu.Lock()
			delete(r.cancels, runID)
			r.mu.Unlock()
			_ = lock.Release()
		}()
		_ = r.execute(runCtx, snapshot, meta, script, runner)
	}()
	return WorkflowLaunch{Launched: true, RunID: runID, Task: snapshot.Task}, nil
}

func (r *WorkflowRuntime) execute(
	ctx context.Context,
	snapshot WorkflowSnapshot,
	meta WorkflowMeta,
	script WorkflowScript,
	runner WorkflowAgentRunner,
) error {
	now := time.Now().UTC()
	snapshot.Task.Status = WorkflowRunning
	snapshot.Task.StartedAt = &now
	_ = r.writeSnapshot(snapshot)
	state := &WorkflowExecutionState{
		ctx: ctx, runtime: r, snapshot: &snapshot, runner: runner,
		phase: "", mu: &sync.Mutex{},
	}
	state.emit("task_started", "", "workflow started", WorkflowRunning)
	result, err := script(state, snapshot.Args)
	if err == nil && ctx.Err() != nil {
		err = ctx.Err()
	}
	finished := time.Now().UTC()
	snapshot.Task.FinishedAt = &finished
	if err != nil {
		if errors.Is(err, context.Canceled) {
			snapshot.Task.Status = WorkflowCancelled
		} else {
			snapshot.Task.Status = WorkflowFailed
		}
		snapshot.Task.Error = redact(err.Error())
		message := "workflow failed"
		if snapshot.Task.Status == WorkflowCancelled {
			message = "workflow cancelled"
		}
		state.emit("task_notification", "", message, snapshot.Task.Status)
	} else {
		snapshot.Task.Status = WorkflowCompleted
		snapshot.Task.Result = result
		state.emit("task_notification", "", "workflow completed", WorkflowCompleted)
	}
	snapshot.Task.AgentCount = state.agentCount
	snapshot.Task.TokenUsage = state.tokenUsage
	if err := r.writeOutput(snapshot.RunID, result); err != nil && snapshot.Task.Status == WorkflowCompleted {
		snapshot.Task.Status = WorkflowFailed
		snapshot.Task.Error = redact(err.Error())
	}
	if err := r.writeSnapshot(snapshot); err != nil {
		return err
	}
	return err
}

func (r *WorkflowRuntime) Get(runID string) (WorkflowTask, error) {
	snapshot, err := r.loadSnapshot(runID)
	if err != nil {
		return WorkflowTask{}, err
	}
	return snapshot.Task, nil
}

func (r *WorkflowRuntime) Collect(runID string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	snapshot, err := r.loadSnapshot(runID)
	if err != nil {
		return "", err
	}
	terminal := snapshot.Task.Status == WorkflowCompleted ||
		snapshot.Task.Status == WorkflowFailed || snapshot.Task.Status == WorkflowCancelled
	if !terminal {
		return "", nil
	}
	if snapshot.Task.NotifiedAt != nil {
		return "", nil
	}
	now := time.Now().UTC()
	snapshot.Task.NotifiedAt = &now
	if err := r.writeSnapshot(snapshot); err != nil {
		return "", err
	}
	content, _ := json.Marshal(map[string]any{"type": "task_notification", "run_id": runID, "task": snapshot.Task})
	return string(content), nil
}

func (r *WorkflowRuntime) Cancel(runID string) error {
	r.mu.Lock()
	cancel := r.cancels[runID]
	r.mu.Unlock()
	if cancel == nil {
		return fmt.Errorf("workflow run %q 未运行", runID)
	}
	cancel()
	return nil
}

type WorkflowExecutionState struct {
	ctx        context.Context
	runtime    *WorkflowRuntime
	snapshot   *WorkflowSnapshot
	runner     WorkflowAgentRunner
	phase      string
	depth      int
	mu         *sync.Mutex
	agentCount int
	tokenUsage int
}

func (s *WorkflowExecutionState) Agent(prompt string, schema map[string]any, label, phase string) (any, error) {
	if err := s.ctx.Err(); err != nil {
		return nil, err
	}
	key := workflowCallKey("agent", label, prompt, schema)
	if value, ok := s.cached(key); ok {
		s.emit("workflow_agent", label, "cached", WorkflowRunning)
		return value, nil
	}
	s.emitPhase(phase)
	s.emit("workflow_agent", label, "started", WorkflowRunning)
	result, err := s.runner(s.ctx, prompt, schema, label)
	if err != nil {
		return nil, err
	}
	if schema != nil {
		if validationErr := validateWorkflowValue(result.Value, schema); validationErr != nil {
			retry, retryErr := s.runner(s.ctx, prompt+"\n\n请只返回符合 JSON schema 的对象。", schema, label+":retry")
			if retryErr != nil {
				return nil, retryErr
			}
			result = retry
			if validationErr = validateWorkflowValue(result.Value, schema); validationErr != nil {
				return nil, fmt.Errorf("agent(%s) 输出不合法: %w", label, validationErr)
			}
		}
	}
	s.mu.Lock()
	s.agentCount++
	s.tokenUsage += result.Tokens
	s.mu.Unlock()
	if err := s.record(key, result.Value); err != nil {
		return nil, err
	}
	return result.Value, nil
}

func (s *WorkflowExecutionState) Parallel(thunks []func() (any, error)) ([]any, error) {
	results := make([]any, len(thunks))
	errors := make([]error, len(thunks))
	var wg sync.WaitGroup
	for i, thunk := range thunks {
		wg.Add(1)
		go func(index int, fn func() (any, error)) {
			defer wg.Done()
			results[index], errors[index] = fn()
		}(i, thunk)
	}
	wg.Wait()
	for _, err := range errors {
		if err != nil {
			return nil, err
		}
	}
	return results, nil
}

type WorkflowStage func(any, any, int) (any, error)

func (s *WorkflowExecutionState) Pipeline(items []any, stages ...WorkflowStage) ([]any, error) {
	results := make([]any, len(items))
	errors := make([]error, len(items))
	var wg sync.WaitGroup
	for i, item := range items {
		wg.Add(1)
		go func(index int, value any) {
			defer wg.Done()
			for _, stage := range stages {
				var err error
				value, err = stage(value, item, index)
				if err != nil {
					errors[index] = err
					return
				}
			}
			results[index] = value
		}(i, item)
	}
	wg.Wait()
	for _, err := range errors {
		if err != nil {
			return nil, err
		}
	}
	return results, nil
}

func (s *WorkflowExecutionState) Phase(title string) { s.emitPhase(title) }
func (s *WorkflowExecutionState) Log(message string) {
	s.emit("log", "", redact(message), WorkflowRunning)
}

func (s *WorkflowExecutionState) Workflow(name string, args map[string]any) (any, error) {
	if s.depth >= 1 {
		return nil, fmt.Errorf("嵌套 workflow 只允许一层")
	}
	meta, script, ok := s.runtime.registry.Get(name)
	if !ok {
		return nil, fmt.Errorf("unknown nested workflow %q", name)
	}
	child := &WorkflowExecutionState{
		ctx: s.ctx, runtime: s.runtime, snapshot: s.snapshot,
		runner: s.runner, phase: s.phase, depth: s.depth + 1, mu: s.mu,
	}
	child.emit("workflow", meta.Name, "nested workflow started", WorkflowRunning)
	return script(child, args)
}

func (s *WorkflowExecutionState) emitPhase(phase string) {
	if strings.TrimSpace(phase) == "" {
		return
	}
	s.mu.Lock()
	s.phase = phase
	s.mu.Unlock()
	s.emit("phase", "", phase, WorkflowRunning)
}

func (s *WorkflowExecutionState) emit(eventType, label, message string, status WorkflowStatus) {
	s.mu.Lock()
	phase := s.phase
	s.mu.Unlock()
	event := WorkflowEvent{
		Type: eventType, RunID: s.snapshot.RunID, Workflow: s.snapshot.Workflow,
		Phase: phase, Label: label, Message: message, Status: status, Timestamp: time.Now().UTC(),
	}
	s.mu.Lock()
	s.snapshot.Events = append(s.snapshot.Events, event)
	s.mu.Unlock()
	_ = s.runtime.appendJournal(s.snapshot.RunID, event)
}

func (s *WorkflowExecutionState) cached(key string) (any, bool) {
	snapshot, err := s.runtime.loadSnapshot(s.snapshot.RunID)
	if err != nil {
		return nil, false
	}
	for _, event := range snapshot.Events {
		if event.Type != "agent_result" || event.Label != key {
			continue
		}
		var value any
		if json.Unmarshal([]byte(event.Message), &value) == nil {
			return value, true
		}
	}
	return nil, false
}

func (s *WorkflowExecutionState) record(key string, value any) error {
	content, err := json.Marshal(value)
	if err != nil {
		return err
	}
	event := WorkflowEvent{
		Type: "agent_result", RunID: s.snapshot.RunID, Workflow: s.snapshot.Workflow,
		Phase: s.currentPhase(), Label: key, Message: string(content), Timestamp: time.Now().UTC(),
	}
	s.mu.Lock()
	s.snapshot.Events = append(s.snapshot.Events, event)
	s.mu.Unlock()
	return s.runtime.appendJournal(s.snapshot.RunID, event)
}

func (s *WorkflowExecutionState) currentPhase() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.phase
}

func (r *WorkflowRuntime) loadSnapshot(runID string) (WorkflowSnapshot, error) {
	if !workflowRunIDPattern.MatchString(runID) {
		return WorkflowSnapshot{}, fmt.Errorf("invalid workflow run ID %q", runID)
	}
	return r.persistence.LoadSnapshot(runID)
}

func (r *WorkflowRuntime) writeSnapshot(snapshot WorkflowSnapshot) error {
	return r.persistence.SaveSnapshot(snapshot)
}

func (r *WorkflowRuntime) writeOutput(runID string, value any) error {
	return r.persistence.WriteOutput(runID, value)
}

func (r *WorkflowRuntime) appendJournal(runID string, event WorkflowEvent) error {
	if !workflowRunIDPattern.MatchString(runID) {
		return fmt.Errorf("invalid workflow run ID %q", runID)
	}
	return r.persistence.AppendEvent(runID, event)
}

func workflowCallKey(kind, label, prompt string, schema map[string]any) string {
	encoded, _ := json.Marshal(schema)
	basis := kind + "|" + label + "|" + prompt + "|" + string(encoded)
	hash := sha256.Sum256([]byte(basis))
	return kind + "-" + hex.EncodeToString(hash[:])[:20]
}

func validateWorkflowValue(value any, schema map[string]any) error {
	if schema == nil {
		return nil
	}
	typeName, _ := schema["type"].(string)
	switch typeName {
	case "object":
		object, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("期望 object")
		}
		var requiredNames []string
		switch required := schema["required"].(type) {
		case []any:
			for _, raw := range required {
				if name, ok := raw.(string); ok {
					requiredNames = append(requiredNames, name)
				}
			}
		case []string:
			requiredNames = required
		}
		for _, name := range requiredNames {
			if _, ok := object[name]; !ok {
				return fmt.Errorf("缺少字段 %q", name)
			}
		}
		if properties, ok := schema["properties"].(map[string]any); ok {
			for name, property := range properties {
				if current, exists := object[name]; exists {
					if propertySchema, ok := property.(map[string]any); ok {
						if err := validateWorkflowValue(current, propertySchema); err != nil {
							return fmt.Errorf("字段 %s: %w", name, err)
						}
					}
				}
			}
		}
	case "array":
		array, ok := value.([]any)
		if !ok {
			return fmt.Errorf("期望 array")
		}
		if itemSchema, ok := schema["items"].(map[string]any); ok {
			for index, item := range array {
				if err := validateWorkflowValue(item, itemSchema); err != nil {
					return fmt.Errorf("数组项 %d: %w", index, err)
				}
			}
		}
	case "string":
		if _, ok := value.(string); !ok {
			return fmt.Errorf("期望 string")
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("期望 boolean")
		}
	case "number":
		switch value.(type) {
		case float32, float64, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		default:
			return fmt.Errorf("期望 number")
		}
	case "integer":
		switch value.(type) {
		case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		case float64:
			if value.(float64) != float64(int64(value.(float64))) {
				return fmt.Errorf("期望 integer")
			}
		default:
			return fmt.Errorf("期望 integer")
		}
	}
	return nil
}

func cloneWorkflowArgs(args map[string]any) map[string]any {
	copyArgs := map[string]any{}
	for key, value := range args {
		copyArgs[key] = value
	}
	return copyArgs
}

func newWorkflowRunID() (string, error) {
	bytes := make([]byte, 8)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return "wf_" + hex.EncodeToString(bytes), nil
}
