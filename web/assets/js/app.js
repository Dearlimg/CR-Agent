document
  .querySelectorAll("[data-tab]")
  .forEach((b) => (b.onclick = () => showTab(b.dataset.tab)));
document.querySelectorAll("[data-mode]").forEach(
  (b) =>
    (b.onclick = () => {
      mode = b.dataset.mode;
      document
        .querySelectorAll("[data-mode]")
        .forEach((x) => x.classList.toggle("active", x === b));
      renderTrace();
    }),
);
$("search").oninput = scheduleRender;
$("form").onsubmit = run;
$("demo").onclick = sample;
$("menu").onclick = () =>
  setNavigation(!document.body.classList.contains("nav-open"));
$("new").onclick = () => {
  if (busy) return;
  version++;
  closeStream();
  job = null;
  isDemo = false;
  start = 0;
  $("error").textContent = "";
  $("source").value = "";
  $("diff").value = "";
  $("budget").value = "";
  $("search").value = "";
  render();
  $("source").focus();
  setNavigation(false);
};
$("history").onclick = (e) => {
  const b = e.target.closest("[data-session]");
  if (b) {
    openSession(b.dataset.session);
    setNavigation(false);
  }
};
$("lanes").onclick = (e) => {
  const b = e.target.closest("[data-event]");
  if (b) {
    const detail = document.querySelector(
      'details[data-index="' + b.dataset.event + '"]',
    );
    if (detail) {
      detail.open = true;
      detail.scrollIntoView({
        behavior: matchMedia("(prefers-reduced-motion: reduce)").matches
          ? "auto"
          : "smooth",
        block: "center",
      });
    }
  }
};
document.addEventListener(
  "toggle",
  async (e) => {
    const card = e.target.closest?.("details.event");
    if (!card || !card.open || card.dataset.detailsLoaded) return;
    card.dataset.detailsLoaded = "loading";
    const details = card.querySelector("[data-event-details]");
    if (details) details.textContent = "正在读取 trace 详情…";
    try {
      const trace = await loadTraceDetails(card.dataset.traceId);
      if (!trace || !details) return;
      card.dataset.detailsLoaded = "true";
      details.textContent = eventDetails(trace);
    } catch (error) {
      card.dataset.detailsLoaded = "";
      if (details) details.textContent = "读取失败：" + error.message;
    }
  },
  true,
);
$("conversation").onclick = async (e) => {
  const button = e.target.closest("[data-trace-details]");
  if (!button) return;
  button.disabled = true;
  try {
    await loadTraceDetails(button.dataset.traceDetails);
    render();
  } catch (error) {
    button.disabled = false;
    $("error").textContent = error.message;
  }
};
$("export").onclick = () => {
  if (!job) return;
  downloadText(
    "cr-agent-review-" + safeExportName(job.id) + ".md",
    renderReviewMarkdown(job),
    "text/markdown;charset=utf-8",
  );
};
$("export-json").onclick = async () => {
  if (!job) return;
  if (!isDemo) {
    try {
      await Promise.all((job.trace || []).map((trace) => loadTraceDetails(trace.id)));
    } catch (error) {
      $("error").textContent = "导出失败：" + error.message;
      return;
    }
  }
  downloadText(
    "cr-agent-session-" + safeExportName(job.id) + ".json",
    JSON.stringify(job, null, 2),
    "application/json;charset=utf-8",
  );
};
setInterval(() => {
  if (!document.hidden && job && !terminal(job)) metrics();
}, 1000);
fetch("/api/health")
  .then((r) => {
    if (!r.ok) throw Error();
    markup("health").html = '<span class="dot">●</span> 服务已连接';
  })
  .catch(() => {
    $("health").textContent = "◌ 服务未连接 · 可查看示例";
  });
render();

$("nav-backdrop").onclick = () => setNavigation(false);
document.addEventListener("keydown", (e) => {
  if (e.key === "Escape") setNavigation(false);
});
showTab("trajectory");
