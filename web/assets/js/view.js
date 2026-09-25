function closeStream() {
  stream?.close();
  stream = null;
}
function isHiddenSandboxEvent(event) {
  return ["automated_tests", "typecheck", "syntax_check", "format_check"].includes(event.tool);
}
function showTab(name) {
  for (const n of ["trajectory", "conversation", "findings"])
    $(n).hidden = n !== name;
  document.querySelectorAll("[data-tab]").forEach((b) => {
    b.classList.toggle("active", b.dataset.tab === name);
    b.setAttribute("aria-pressed", String(b.dataset.tab === name));
  });
}
function historyRender() {
  markup("history").html =
    history
      .map(
        (x) =>
          '<button data-session="' +
          esc(x.id) +
          '" class="' +
          (job?.id === x.id ? "active" : "") +
          '">◉ &nbsp; ' +
          esc(x.title) +
          "</button>",
      )
      .join("") || '<span class="small muted">你的审查会出现在这里</span>';
}
function remember() {
  if (isDemo || !job) return;
  history = [
    { id: job.id, title: (job.source || "代码 diff 审查").slice(0, 55) },
    ...history.filter((x) => x.id !== job.id),
  ].slice(0, 20);
  try {
    localStorage.setItem("cr-agent-sessions", JSON.stringify(history));
  } catch {}
  historyRender();
}
function metrics() {
  const trace = (job?.trace || []).filter((event) => !isHiddenSandboxEvent(event));
  $("duration").textContent = wallLabel();
  for (const k of ["model", "tool"]) {
    const timed = trace.filter(
      (e) => kind(e) === k && e.kind !== "model_request" && hasDuration(e),
    );
    const total = timed.reduce((n, e) => n + (e.duration_ms || 0), 0);
    $(k + "-time").textContent = timed.length
      ? total > 0
        ? fmt(total)
        : "<1 ms"
      : "未上报";
  }
  const requests = trace.filter((e) => e.kind === "model_request");
  $("model-request-count").textContent = requests.length;
  $("model-request-time").textContent = requests.length
    ? fmt(requests.reduce((n, e) => n + (e.duration_ms || 0), 0))
    : "未上报";
  const calls = trace.filter((e) => kind(e) === "tool");
  $("preflight-calls").textContent = calls.filter(
    (e) => e.origin === "orchestrator" || (!e.origin && e.phase === "action"),
  ).length;
  $("model-calls").textContent = calls.filter(
    (e) => e.origin === "model",
  ).length;
  $("cache-hits").textContent = calls.filter((e) => e.cache_hit).length;
  const money = (yuan) =>
    new Intl.NumberFormat("zh-CN", {
      style: "currency",
      currency: "CNY",
      minimumFractionDigits: 2,
      maximumFractionDigits: 6,
    }).format(Number(yuan) || 0);
  $("cost").textContent = job
    ? money(job.spent_yuan) + " / " + money(job.budget_yuan)
    : "—";
  $("footer-time").textContent = job ? "总耗时 " + wallLabel() : "等待新任务";
}
function eventDetails(e) {
  return (
    "输入\n" +
    (e.input || "—") +
    "\n\n输出\n" +
    (e.output || "—") +
    (e.prompt ? "\n\n提示词\n" + e.prompt : "") +
    (e.model_reply ? "\n\n模型回复\n" + e.model_reply : "")
  );
}
function renderTrace() {
  const all = job?.trace || [],
    visible = all
      .map((e, i) => ({ ...e, index: i }))
      .filter((e) => e.kind !== "model_request" && !isHiddenSandboxEvent(e)),
    q = $("search").value.toLowerCase();
  let trace = visible
    .filter((e) =>
      [e.tool, e.input, e.output, e.model_reply]
        .join(" ")
        .toLowerCase()
        .includes(q),
    );
  if (mode === "calls")
    trace.sort((a, b) => (b.duration_ms || 0) - (a.duration_ms || 0));
  const opened = new Set(
    [...document.querySelectorAll("details.event[open]")].map(
      (e) => e.dataset.index,
    ),
  );
  $("event-count").textContent = trace.length + " / " + visible.length + " EVENTS";
  $("footer-events").textContent = visible.length + " 个执行事件";
  markup("events").html =
    trace
      .map(
        (e) =>
          '<details class="event" data-index="' +
          e.index +
          '" data-trace-id="' +
          esc(e.id) +
          '" ' +
          (opened.has(String(e.index)) ? "open" : "") +
          '><summary><span class="badge ' +
          kind(e) +
          '">' +
          { model: "MODEL", tool: "TOOL", input: "SYSTEM" }[kind(e)] +
          '</span><span class="event-title"><b>' +
          esc(e.tool) +
          "</b><span>" +
          esc(
            ({ orchestrator: "编排", model: "模型", cache: "缓存" }[e.origin] ||
              "未标注") +
              (e.cache_hit ? " · 解析缓存命中" : "") +
              " · " +
              (e.status || e.model || "展开加载输入与输出"),
          ) +
          '</span></span><span class="time">' +
          durationLabel(e) +
          "</span></summary><pre data-event-details>" +
          esc(eventDetails(e)) +
          "</pre></details>",
      )
      .join("") ||
    '<div class="empty"><b>' +
      (q ? "没有匹配的事件" : "等待 Agent 开始执行") +
      "</b>" +
      (q ? "尝试其他关键词" : "发送审查需求，或查看示例轨迹") +
      "</div>";
  const starts = visible.map(at).filter(Boolean),
    ends = visible.map(endAt),
    timelineStart = starts.length
      ? Math.min(start || Infinity, ...starts)
      : start || Date.now(),
    wallEnd =
      terminal(job) && job?.finished_at
        ? Date.parse(job.finished_at) || Date.now()
        : Date.now(),
    timelineEnd = Math.max(wallEnd, ...ends),
    total = Math.max(timelineEnd - timelineStart, 1);
  markup("ruler").html = Array.from(
    { length: 6 },
    (_, i) =>
      "<span>" +
      (mode === "duration"
        ? fmt((total * i) / 5)
        : Math.round((trace.length * i) / 5)) +
      "</span>",
  ).join("");
  markup("lanes").html = ["input", "model", "tool"]
    .map(
      (k) =>
        '<div class="lane"><span class="lane-label">' +
        { input: "Input", model: "Model", tool: "Tools" }[k] +
        '</span><div class="track">' +
        trace
          .map((e, i) => {
            if (kind(e) !== k) return "";
            const begin = at(e),
              end = endAt(e),
              w =
                mode === "duration"
                  ? Math.max(0.25, Math.min(100, ((end - begin) / total) * 100))
                  : 90 / Math.max(trace.length, 1),
              left =
                mode === "duration"
                  ? Math.max(
                      0,
                      Math.min(
                        100 - w,
                        ((begin - timelineStart) / total) * 100,
                      ),
                    )
                  : (i / Math.max(trace.length, 1)) * 100;
            return (
              '<button class="bar ' +
              k +
              '" data-event="' +
              e.index +
              '" style="left:' +
              left +
              "%;width:" +
              w +
              '%" aria-label="' +
              esc(e.tool) +
              '" title="' +
              esc(e.tool) +
              " · " +
              (durationLabel(e) === "—" ? "未上报耗时" : durationLabel(e)) +
              '"></button>'
            );
          })
          .join("") +
        "</div></div>",
    )
    .join("");
}
function renderOutcome() {
  if (!job || !terminal(job)) return "";
  const scope = job.review_scope || {},
    checks = Array.isArray(scope.checks)
      ? scope.checks.filter((check) => !["automated_tests", "typecheck", "syntax_check", "format_check"].includes(check.name))
      : [],
    outcome = job.review_outcome;
  const titles = {
    completed_no_findings: "本次审查完成：未发现需要评论的问题。",
    completed_with_findings:
      "本次审查完成：发现 " + (job.comments || []).length + " 个已核验问题。",
    incomplete: "审查未完成",
    failed: "审查失败",
  };
  const title = titles[outcome] || "审查结论不可用";
  const checkNames = {
    conflict_marker_check: "冲突标记检查",
    static_check: "静态提示检查",
    syntax_check: "语法检查",
    format_check: "格式检查",
    finding_verification: "证据与第二轮复核",
  };
  const checkStatuses = {
    passed: "通过",
    failed: "失败",
    hint: "有提示",
    not_run: "未运行",
    not_needed: "无需执行",
    incomplete: "未完成",
  };
  const rows = checks
    .map(
      (c) =>
        '<div class="small muted outcome-row"><b>' +
        esc(checkNames[c.name] || c.name) +
        " · " +
        esc(checkStatuses[c.status] || c.status) +
        "</b>：" +
        esc(c.message) +
        "</div>",
    )
    .join("");
  const note =
    outcome === "completed_no_findings"
      ? '<div class="small muted outcome-note">这表示本次审查没有发现已确认的问题，不代表代码绝对没有 bug。</div>'
      : "";
  const outcomeClass =
    outcome === "completed_no_findings"
      ? "success"
      : outcome === "completed_with_findings"
        ? "findings"
        : "warning";
  return (
    '<div class="outcome outcome-' +
    outcomeClass +
    '"><b>' +
    esc(title) +
    '</b><div class="small muted outcome-row">审查范围：' +
    esc(scope.files_reviewed ?? 0) +
    " 个文件、" +
    esc(scope.added_lines ?? 0) +
    " 条新增行</div>" +
    note +
    (rows ? '<div class="outcome-checks">检查情况' + rows + "</div>" : "") +
    "</div>"
  );
}
function render() {
  const trace = job?.trace || [],
    comments = job?.comments || [],
    modelTraces = trace.filter((e) => e.kind === "model_request");
  $("status").textContent = isDemo
    ? "示例预览"
    : terminal(job) && !job.finished_at
      ? "结算中"
      : {
          queued: "排队中",
          running: "执行中",
          completed: "已完成",
          completed_with_warnings: "审查未完成",
          failed: "执行失败",
          cancelled: "已取消",
        }[job?.status] ||
        job?.status ||
        "准备就绪";
  $("run-title").textContent = job
    ? isDemo
      ? "一次代码审查的完整轨迹"
      : (job.source || "代码 diff 审查").slice(0, 70)
    : "让每一步审查，都清晰可见。";
  $("run-subtitle").textContent = job
    ? "Session " + job.id
    : "从代码变更到审查结论，跟踪 Agent 的每一次行动。";
  $("demo-notice").hidden = !isDemo;
  $("review-outcome").hidden = !job || !terminal(job);
  markup("review-outcome").html = renderOutcome();
  $("export").disabled = !job;
  $("export-json").disabled = !job;
  for (const id of ["send", "new", "demo"]) $(id).disabled = busy;
  $("finding-count").textContent = comments.length;
  markup("findings").html =
    comments
      .map(
        (c) =>
          '<article class="comment"><span class="badge">' +
          esc(severityLabel(c.severity)) +
          "</span> <b>" +
          esc(c.file) +
          ":" +
          esc(c.line) +
          "</b><p>" +
          esc(c.body) +
          '</p><div class="small muted"><b>证据</b><pre>' +
          esc(c.evidence || "无") +
          "</pre><b>触发条件</b><p>" +
          esc(c.trigger || "无") +
          "</p><b>影响</b><p>" +
          esc(c.impact || "无") +
          "</p><b>修复建议</b><p>" +
          esc(c.suggestion || "无") +
          "</p><b>核验结果</b><p>" +
          esc(
            c.verification_status === "second_pass_review_passed"
              ? "第二轮模型复核通过：" + (c.verification_reason || "")
              : c.verification_status || "未提供",
          ) +
          "</p>置信度 " +
          esc(confidenceLabel(c.confidence)) +
          " · 轨迹 " +
          esc(c.trace_id || "—") +
          "</div></article>",
      )
      .join("") ||
    '<div class="empty"><b>' +
      (job?.review_outcome === "incomplete"
        ? "审查未完成，未核验的候选问题不会作为确认评论展示。"
        : job?.review_outcome === "failed"
          ? "审查失败，无法给出完整结论。"
          : job?.review_outcome === "completed_no_findings"
            ? "本次审查完成：未发现需要评论的问题。"
            : job && terminal(job)
              ? "没有已核验的审查评论"
              : "审查结果将在这里显示") +
      "</b>" +
      (job?.status === "failed"
        ? "任务失败，请查看错误和执行轨迹。"
        : "结果以当前任务实际产出为准。") +
      "</div>";
  markup("todos").html =
    (job?.todos || [])
      .map(
        (t) =>
          '<div class="todo"><em>' +
          ({ completed: "✓", in_progress: "◉", pending: "○" }[t.status] ||
            "○") +
          "</em>" +
          esc(t.content) +
          "</div>",
      )
      .join("") || "任务开始后显示执行计划";
  markup("conversation").html = job
    ? '<div class="source"><span class="badge input">USER</span><p>' +
      esc(job.source || "审查粘贴的代码 diff") +
      "</p></div>" +
      (modelTraces.length
        ? modelTraces
            .map(
          (e) =>
            '<article class="comment"><span class="badge model">' +
            esc(e.tool) +
            "</span>" +
            (e.model_reply
              ? "<pre>" + esc(e.model_reply) + "</pre>"
              : traceDetailsLoaded(e.id)
                ? '<p class="small muted">该模型调用没有文本回复。</p>'
                : '<button type="button" data-trace-details="' +
                  esc(e.id) +
                  '">按需加载模型回复</button>') +
            "</article>",
            )
            .join("")
        : '<div class="empty"><b>暂无模型调用</b>模型回复会在按需加载后显示。</div>')
    : '<div class="empty"><b>开始一次有依据的代码审查</b>输入 PR / MR 链接，或展开输入框粘贴 diff。</div>';
  if (job?.error) $("error").textContent = job.error;
  metrics();
  renderTrace();
  historyRender();
}
