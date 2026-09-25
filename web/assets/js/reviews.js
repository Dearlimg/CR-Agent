const loadedTraceDetails = new Set();

function mergeReviewEvent(previous, update) {
  const traces = new Map((previous?.trace || []).map((trace) => [trace.id, trace]));
  for (const trace of update.trace || []) {
    const existing = traces.get(trace.id) || {};
    traces.set(trace.id, {
      ...existing,
      ...trace,
      input: trace.input || existing.input || "",
      output: trace.output || existing.output || "",
      prompt: trace.prompt || existing.prompt || "",
      model_reply: trace.model_reply || existing.model_reply || "",
    });
  }
  const orderedTraces = [...traces.values()].sort((a, b) => {
    const startA = Date.parse(a.started_at || a.at) || 0;
    const startB = Date.parse(b.started_at || b.at) || 0;
    return startA - startB || String(a.id).localeCompare(String(b.id));
  });
  return {
    ...previous,
    ...update,
    comments: update.comments || previous?.comments || [],
    todos: update.todos || previous?.todos || [],
    trace: orderedTraces,
  };
}

async function loadTraceDetails(traceID) {
  if (!job || !traceID) return null;
  const jobID = job.id;
  const cacheKey = jobID + ":" + traceID;
  const current = job.trace?.find((trace) => trace.id === traceID);
  if (loadedTraceDetails.has(cacheKey)) return current || null;
  const response = await fetch(
    "/api/reviews/" + encodeURIComponent(jobID) + "/traces/" + encodeURIComponent(traceID),
  );
  const detail = await response.json();
  if (!response.ok) throw Error(detail.error || "读取 trace 详情失败");
  if (job?.id !== jobID) return null;
  const index = (job.trace || []).findIndex((trace) => trace.id === traceID);
  if (index < 0) return null;
  job.trace[index] = { ...job.trace[index], ...detail };
  loadedTraceDetails.add(cacheKey);
  return job.trace[index];
}

function traceDetailsLoaded(traceID) {
  return Boolean(job && loadedTraceDetails.has(job.id + ":" + traceID));
}

function connect(id) {
  closeStream();
  const token = version;
  stream = new EventSource(
    "/api/reviews/" + encodeURIComponent(id) + "/events",
  );
  stream.addEventListener("review", (e) => {
    if (token !== version) return;
    try {
      job = mergeReviewEvent(job, JSON.parse(e.data));
      if (finalSnapshot(job)) {
        closeStream();
        busy = false;
      }
      scheduleRender();
      remember();
    } catch {
      $("error").textContent = "无法解析实时事件，请重新打开此会话。";
    }
  });
  stream.onerror = () => {
    if (token !== version) return;
    closeStream();
    busy = false;
    $("error").textContent =
      "实时连接中断。点击左侧当前会话重新连接，任务可能仍在后台执行。";
    render();
  };
}
async function run(e) {
  e.preventDefault();
  if (busy) return;
  const source = $("source").value.trim(),
    diff = $("diff").value.trim();
  const budgetValue = $("budget").value.trim();
  if (!source && !diff) {
    $("error").textContent = "请填写审查需求或代码 diff。";
    return;
  }
  const request = { source, diff };
  if (budgetValue) {
    const budgetYuan = Number(budgetValue);
    if (!Number.isFinite(budgetYuan) || budgetYuan <= 0) {
      $("error").textContent = "预算金额必须是大于 0 的人民币金额。";
      return;
    }
    request.budget_yuan = budgetYuan;
  }
  version++;
  closeStream();
  isDemo = false;
  busy = true;
  job = null;
  start = Date.now();
  $("error").textContent = "";
  render();
  try {
    const r = await fetch("/api/reviews", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(request),
    });
    const j = await r.json();
    if (!r.ok) throw Error(j.error || "创建任务失败");
    job = j;
    start = Date.parse(j.started_at) || 0;
    busy = !finalSnapshot(job);
    render();
    remember();
    if (busy) connect(job.id);
  } catch (err) {
    busy = false;
    $("error").textContent = "请求失败：" + err.message;
    render();
  }
}
async function openSession(id) {
  if (busy) return;
  version++;
  const token = version;
  closeStream();
  $("error").textContent = "";
  try {
    const r = await fetch("/api/reviews/" + encodeURIComponent(id));
    if (!r.ok) throw Error("无法读取会话（HTTP " + r.status + "）");
    const j = await r.json();
    if (token !== version) return;
    job = j;
    isDemo = false;
    start = Date.parse(j.started_at) || 0;
    busy = !finalSnapshot(job);
    render();
    if (busy) connect(id);
  } catch (e) {
    $("error").textContent = e.message;
  }
}
