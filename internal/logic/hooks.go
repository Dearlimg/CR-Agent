package logic

import (
	"context"
	"sync"
)

type HookEvent string

const (
	HookLoopStart        HookEvent = "loop_start"
	HookPreToolUse       HookEvent = "pre_tool_use"
	HookPostToolUse      HookEvent = "post_tool_use"
	HookToolError        HookEvent = "tool_error"
	HookPermissionDenied HookEvent = "permission_denied"
	HookLoopStop         HookEvent = "loop_stop"
)

type HookContext struct {
	JobID      string
	Tool       string
	Permission Permission
	Reason     string
	Output     string
	Error      error
	DurationMs int64
}
type Hook func(context.Context, HookEvent, HookContext) *HookContext
type HookBus struct {
	mu    sync.RWMutex
	hooks map[HookEvent][]Hook
}

func NewHookBus() *HookBus { return &HookBus{hooks: map[HookEvent][]Hook{}} }
func (b *HookBus) Register(event HookEvent, hook Hook) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.hooks[event] = append(b.hooks[event], hook)
}
func (b *HookBus) Emit(ctx context.Context, event HookEvent, payload HookContext) *HookContext {
	b.mu.RLock()
	hooks := append([]Hook{}, b.hooks[event]...)
	b.mu.RUnlock()
	for _, hook := range hooks {
		if result := hook(ctx, event, payload); result != nil {
			payload = *result
		}
	}
	return &payload
}
