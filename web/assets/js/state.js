"use strict";
const $ = (id) => document.getElementById(id),
  esc = (v) =>
    String(v ?? "").replace(
      /[&<>"']/g,
      (c) =>
        ({
          "&": "&amp;",
          "<": "&lt;",
          ">": "&gt;",
          '"': "&quot;",
          "'": "&#39;",
        })[c],
    );
const terminal = (j) =>
    ["completed", "completed_with_warnings", "failed", "cancelled"].includes(
      j?.status,
    ),
  finalSnapshot = (j) => terminal(j) && Boolean(j?.finished_at),
  fmt = (ms) =>
    ms < 1000
      ? Math.round(ms) + " ms"
      : ms < 60000
        ? (ms / 1000).toFixed(1) + " s"
        : Math.floor(ms / 60000) + "m " + Math.floor((ms % 60000) / 1000) + "s";
let job = null,
  stream = null,
  start = 0,
  mode = "duration",
  isDemo = false,
  busy = false,
  version = 0,
  history = [];
try {
  const saved = JSON.parse(localStorage.getItem("cr-agent-sessions") || "[]");
  history = Array.isArray(saved)
    ? saved.filter((x) => x && typeof x.id === "string").slice(0, 20)
    : [];
} catch {}
