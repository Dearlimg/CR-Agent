function connect(id) {
  closeStream();
  const token = version;
  stream = new EventSource(
    "/api/reviews/" + encodeURIComponent(id) + "/events",
  );
  stream.addEventListener("review", (e) => {
    if (token !== version) return;
    try {
      job = JSON.parse(e.data);
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
  if (!source && !diff) {
    $("error").textContent = "请填写审查需求或代码 diff。";
    return;
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
      body: JSON.stringify({ source, diff }),
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
