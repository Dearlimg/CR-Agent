package logic

import "fmt"

var workflowFindingSchema = map[string]any{
	"type": "object", "required": []any{"findings"},
	"properties": map[string]any{"findings": map[string]any{"type": "array"}},
}

var workflowVerdictSchema = map[string]any{
	"type": "object", "required": []any{"is_real"},
	"properties": map[string]any{
		"is_real": map[string]any{"type": "boolean"},
		"reason":  map[string]any{"type": "string"},
	},
}

func registerReviewWorkflow(registry *WorkflowRegistry) error {
	return registry.Register(
		WorkflowMeta{Name: "review-changes", Description: "并行审计并逐条验证代码改动", Phases: []string{"Review", "Verify", "Summary"}},
		reviewChangesWorkflow,
	)
}

func reviewChangesWorkflow(ctx *WorkflowExecutionState, args map[string]any) (any, error) {
	changes, ok := args["changes"].(string)
	if !ok || changes == "" {
		return nil, fmt.Errorf("review-changes args.changes 必须是非空字符串")
	}
	ctx.Phase("Review")
	dimensions := []any{"correctness", "security", "dependency"}
	audited, err := ctx.Pipeline(dimensions,
		func(_ any, dimension any, _ int) (any, error) {
			name := fmt.Sprint(dimension)
			prompt := fmt.Sprintf("检查以下代码改动中的 %s 问题，只返回 findings 数组，每条包含 title、file、line、body：\n%s", name, changes)
			value, err := ctx.Agent(prompt, workflowFindingSchema, "audit:"+name, "Review")
			if err != nil {
				return nil, err
			}
			return map[string]any{"dimension": name, "value": value}, nil
		},
		func(value any, dimension any, _ int) (any, error) {
			name := fmt.Sprint(dimension)
			entry, ok := value.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("audit %s 输出结构无效", name)
			}
			payload, _ := entry["value"].(map[string]any)
			rawFindings, _ := payload["findings"].([]any)
			thunks := make([]func() (any, error), 0, len(rawFindings))
			for index, finding := range rawFindings {
				finding := finding
				thunks = append(thunks, func() (any, error) {
					prompt := fmt.Sprintf("根据以下代码改动，验证该 finding 是否真实；只返回 is_real 布尔值和 reason：\n改动：%s\nFinding：%v", changes, finding)
					return ctx.Agent(prompt, workflowVerdictSchema, fmt.Sprintf("verify:%s:%d", name, index), "Verify")
				})
			}
			verdicts, err := ctx.Parallel(thunks)
			if err != nil {
				return nil, err
			}
			confirmed := []any{}
			for index, verdict := range verdicts {
				object, _ := verdict.(map[string]any)
				if real, _ := object["is_real"].(bool); real && index < len(rawFindings) {
					confirmed = append(confirmed, rawFindings[index])
				}
			}
			return map[string]any{"dimension": name, "confirmed": confirmed}, nil
		},
	)
	if err != nil {
		return nil, err
	}
	ctx.Phase("Summary")
	confirmed := []any{}
	for _, item := range audited {
		object, _ := item.(map[string]any)
		values, _ := object["confirmed"].([]any)
		confirmed = append(confirmed, values...)
	}
	ctx.Log(fmt.Sprintf("确认了 %d 个真实问题", len(confirmed)))
	return map[string]any{"confirmed": confirmed}, nil
}
