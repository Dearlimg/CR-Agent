package logic

import (
	"fmt"
	"os"
	"strings"
)

type Permission string

const (
	PermissionReadDiff       Permission = "read_diff"
	PermissionNetworkFetch   Permission = "network_fetch"
	PermissionStaticAnalysis Permission = "static_analysis"
	PermissionLLMInference   Permission = "llm_inference"
	PermissionRepositoryRead Permission = "repository_read"
	PermissionRepositoryExec Permission = "repository_exec"
	PermissionSandboxExec    Permission = "sandbox_exec"
	PermissionPublishReview  Permission = "publish_review"
	PermissionManageSchedule Permission = "manage_schedule"
)

type PermissionDecision string

const (
	PermissionAllow           PermissionDecision = "allow"
	PermissionDeny            PermissionDecision = "deny"
	PermissionRequireApproval PermissionDecision = "require_approval"
)

type PermissionPolicy struct {
	grants map[Permission]PermissionDecision
}

func DefaultPermissionPolicy() *PermissionPolicy {
	p := &PermissionPolicy{grants: map[Permission]PermissionDecision{PermissionReadDiff: PermissionAllow, PermissionNetworkFetch: PermissionAllow, PermissionStaticAnalysis: PermissionAllow, PermissionLLMInference: PermissionAllow, PermissionRepositoryRead: PermissionAllow, PermissionSandboxExec: PermissionDeny, PermissionRepositoryExec: PermissionRequireApproval, PermissionPublishReview: PermissionRequireApproval}}
	p.grants[PermissionManageSchedule] = PermissionRequireApproval
	if raw := os.Getenv("AGENT_DENY_PERMISSIONS"); raw != "" {
		for _, v := range strings.Split(raw, ",") {
			p.grants[Permission(strings.TrimSpace(v))] = PermissionDeny
		}
	}
	return p
}
func (p *PermissionPolicy) Decide(permission Permission) PermissionDecision {
	decision, ok := p.grants[permission]
	if !ok {
		return PermissionDeny
	}
	return decision
}

func reviewSourcePermissionError(policy *PermissionPolicy, tool string) error {
	if policy == nil {
		return permissionError(tool, PermissionRepositoryRead, PermissionDeny)
	}
	for _, permission := range []Permission{PermissionRepositoryRead, PermissionNetworkFetch} {
		decision := policy.Decide(permission)
		if decision != PermissionAllow {
			return permissionError(tool, permission, decision)
		}
	}
	return nil
}

func permissionError(tool string, permission Permission, decision PermissionDecision) error {
	return fmt.Errorf("权限策略阻止工具 %s：%s=%s", tool, permission, decision)
}
