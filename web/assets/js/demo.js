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
    ["review_agent", "review", 21000, 17000, "单 Agent 审查完成"],
    [
      "finding_verification",
      "verification",
      23000,
      1200,
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
      checks: [
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
    spent_yuan: 0.0048,
    budget_yuan: 10,
    trace: data.map(([tool, phase, end, ms, output], i) => ({
      id: "demo-" + i,
      tool,
      phase,
      origin: phase === "review"
        ? "model"
        : "orchestrator",
      kind: phase === "review"
        ? "model"
        : phase === "action"
          ? "tool"
          : "input",
      at: new Date(start + end).toISOString(),
      duration_ms: ms,
      input: "示例审查",
      output,
      model_reply:
        phase === "review"
          ? "发现一处错误处理问题：数据库写入失败时应返回错误，避免调用方误判为成功。"
          : "",
    })),
    todos: [
      "扫描并解析代码变更",
      "执行单 Agent 代码审查",
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
        trace_id: "demo-4",
      },
    ],
  };
  $("error").textContent = "";
  render();
}
