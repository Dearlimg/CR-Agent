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
$("export").onclick = () => {
  if (!job) return;
  const url = URL.createObjectURL(
    new Blob([JSON.stringify(job, null, 2)], { type: "application/json" }),
  );
  const a = document.createElement("a");
  a.href = url;
  a.download = "cr-agent-" + job.id.replace(/[^a-zA-Z0-9_-]/g, "_") + ".json";
  a.click();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
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
