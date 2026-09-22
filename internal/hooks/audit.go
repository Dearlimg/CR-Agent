package hooks

import (
	"CR-Agent/internal/logic"
	"context"
	"strings"
)

// RegisterAudit installs external hook behavior without changing AgentLoop.
func RegisterAudit(bus *logic.HookBus) {
	bus.Register(logic.HookPreToolUse, func(_ context.Context, _ logic.HookEvent, p logic.HookContext) *logic.HookContext {
		if strings.Contains(strings.ToLower(p.Reason), "secret") {
			p.Reason = "执行安全扫描"
		}
		return &p
	})
	bus.Register(logic.HookPermissionDenied, func(_ context.Context, _ logic.HookEvent, p logic.HookContext) *logic.HookContext { return &p })
}
