function sample() {
  if (busy) return;
  version++;
  closeStream();
  isDemo = true;
  start = Date.now() - 24300;
  const data = [
    ["task_create", "task", 100, 0, "创建审查任务"],
    ["load_skill", "skill", 400, 100, "已加载 code-review 指令"],
    [
      "preflight_analysis",
      "action",
      2450,
      920,
      "解析 3 个文件、323 条 diff 新增行；密钥命中 0",
    ],
    ["review_route", "planning", 3300, 10, "specialists"],
    ["subagent_correctness", "subagent", 15700, 11800, "完成正确性审查"],
    ["subagent_security", "subagent", 17000, 12100, "完成安全审查"],
    ["subagent_dependency", "subagent", 18100, 11500, "完成依赖审查"],
    ["deepseek-review", "reasoning", 23400, 5300, "汇总专项审查发现"],
    [
      "finding_verification",
      "verification",
      24100,
      100,
      "核对代码证据并进行第二轮复核",
    ],
    ["task_complete", "task", 24300, 0, "审查已完成"],
  ];
  job = {
    id: "demo-review-001",
    source: "检查订单服务的并发安全与错误处理",
    status: "completed",
    review_outcome: "completed_with_findings",
    review_scope: {
      files_reviewed: 3,
      added_lines: 323,
      tests_ran: false,
      checks: [
        {
          name: "automated_tests",
          status: "not_run",
          message: "示例未运行自动化测试",
        },
        {
          name: "finding_verification",
          status: "passed",
          message: "证据匹配并通过第二轮模型复核",
        },
      ],
    },
    started_at: new Date(start).toISOString(),
    finished_at: new Date(start + 24300).toISOString(),
    updated_at: new Date(start + 24300).toISOString(),
    spent_cents: 8,
    trace: data.map(([tool, phase, end, ms, output], i) => ({
      id: "demo-" + i,
      tool,
      phase,
      origin: ["subagent", "reasoning"].includes(phase)
        ? "model"
        : "orchestrator",
      kind: ["subagent", "reasoning"].includes(phase)
        ? "model"
        : phase === "action"
          ? "tool"
          : "input",
      at: new Date(start + end).toISOString(),
      duration_ms: ms,
      input: "示例审查",
      output,
      model_reply:
        phase === "reasoning"
          ? "发现一处错误处理问题：数据库写入失败时应返回错误，避免调用方误判为成功。"
          : "",
    })),
    todos: [
      "扫描并解析代码变更",
      "按变更规模执行代码审查",
      "校验并去重审查发现",
    ].map((content) => ({ content, status: "completed" })),
    comments: [
      {
        file: "internal/order/service.go",
        line: 48,
        severity: "high",
        confidence: "high",
        body: "数据库写入失败后继续返回成功，调用方可能误认为订单已保存。",
        evidence: "if err != nil { return nil }",
        trigger: "数据库写入失败并进入该错误分支。",
        impact: "调用方仍得到成功结果，导致订单状态与调用方认知不一致。",
        suggestion: "返回写入错误并阻止后续成功响应。",
        verification_status: "second_pass_review_passed",
        verification_reason: "证据行与 diff 一致，错误分支会掩盖写入失败。",
        trace_id: "demo-7",
      },
    ],
  };
  $("error").textContent = "";
  render();
}
