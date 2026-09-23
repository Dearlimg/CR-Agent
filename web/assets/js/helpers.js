function kind(e) {
  if (e.kind === "model_request") return "model";
  if (["input", "model", "tool"].includes(e.kind)) return e.kind;
  return ["review", "subagent", "reasoning"].includes(e.phase) ||
    /deepseek|llm|subagent|review_agent/.test(e.tool)
    ? "model"
    : ["task", "background", "planning", "memory", "context", "skill"].includes(
          e.phase,
        )
      ? "input"
      : "tool";
}
function at(e) {
  const started = Date.parse(e.started_at);
  if (Number.isFinite(started)) return started;
  const ended = Date.parse(e.ended_at || e.at) || 0;
  return Math.max(0, ended - (e.duration_ms || 0));
}
function endAt(e) {
  return Date.parse(e.ended_at || e.at) || Date.now();
}
function elapsed() {
  if (!job) return 0;
  const first = Date.parse(job.started_at);
  if (!Number.isFinite(first)) return null;
  if (terminal(job)) {
    const finished = Date.parse(job.finished_at);
    return Number.isFinite(finished) ? Math.max(0, finished - first) : null;
  }
  return Math.max(0, Date.now() - first);
}
function wallLabel() {
  if (!job) return "—";
  const ms = elapsed();
  return ms === null ? "结算中" : fmt(ms);
}
function hasDuration(e) {
  return e.duration_ms > 0 || Boolean(e.started_at && e.ended_at);
}
function durationLabel(e) {
  if (e.duration_ms > 0) return fmt(e.duration_ms);
  return e.started_at && e.ended_at ? "<1 ms" : "—";
}
function severityLabel(value) {
  return (
    { high: "高", medium: "中", low: "低", info: "提示" }[
      String(value || "").toLowerCase()
    ] || "未标注"
  );
}
function confidenceLabel(value) {
  return (
    { high: "高", medium: "中", low: "低" }[
      String(value || "").toLowerCase()
    ] || "未标注"
  );
}
