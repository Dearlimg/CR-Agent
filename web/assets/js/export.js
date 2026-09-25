function safeExportName(value) {
  return String(value || "review").replace(/[^a-zA-Z0-9_-]/g, "_");
}

function downloadText(filename, content, type) {
  const url = URL.createObjectURL(new Blob([content], { type }));
  const link = document.createElement("a");
  link.href = url;
  link.download = filename;
  link.click();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}

function reportSource(source) {
  try {
    const url = new URL(String(source || ""));
    if (url.protocol === "https:" && ["github.com", "gitlab.com"].includes(url.hostname)) {
      return url.origin + url.pathname;
    }
  } catch {}
  return source ? "粘贴的代码 diff" : "未记录";
}

function inlineCode(value) {
  const text = String(value ?? "");
  const runs = text.match(/`+/g) || [];
  const fence = "`".repeat(Math.max(1, ...runs.map((run) => run.length + 1)));
  const padding = text.startsWith("`") || text.endsWith("`") ? " " : "";
  return fence + padding + text + padding + fence;
}

function markdownQuote(value) {
  return String(value ?? "")
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .split(/\r?\n/)
    .map((line) => (line ? "> " + line : ">"))
    .join("\n");
}

function markdownFence(value) {
  const text = String(value ?? "");
  const runs = text.match(/`+/g) || [];
  const fence = "`".repeat(Math.max(3, ...runs.map((run) => run.length + 1)));
  return fence + "\n" + text + (text.endsWith("\n") ? "" : "\n") + fence;
}

function reportTime(value) {
  if (!value) return "未记录";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? "未记录" : date.toLocaleString("zh-CN");
}

function reviewConclusion(job) {
  const count = (job.comments || []).length;
  switch (job.review_outcome) {
    case "completed_with_findings":
      return `审查完成，发现 ${count} 个已核验问题。`;
    case "completed_no_findings":
      return "审查完成，未发现需要评论的问题；这表示本次审查未核验出问题，不代表代码绝对没有缺陷。";
    case "incomplete":
      return `审查未完成，目前有 ${count} 个已核验问题；其余检查或核验结果请结合检查状态确认。`;
    case "failed":
      return "审查失败，无法给出完整结论。";
    default:
      return {
        queued: "审查仍在排队。",
        running: `审查进行中，目前有 ${count} 个已核验问题。`,
        completed: `审查完成，目前有 ${count} 个已核验问题。`,
        completed_with_warnings: `审查已结束，目前有 ${count} 个已核验问题，并存在未完成项。`,
        failed: "审查失败，无法给出完整结论。",
        cancelled: "审查已取消。",
      }[job.status] || "审查结论不可用。";
  }
}

function reviewCheckLabel(value) {
  return (
    {
      conflict_marker_check: "冲突标记检查",
      static_check: "静态提示检查",
      syntax_check: "语法检查",
      format_check: "格式检查",
      automated_tests: "自动化测试",
      finding_verification: "证据与第二轮复核",
      passed: "通过",
      failed: "失败",
      hint: "有提示",
      not_run: "未运行",
      not_needed: "无需执行",
      incomplete: "未完成",
      running: "执行中",
      queued: "排队中",
      completed: "已完成",
      completed_with_warnings: "已完成，含未完成项",
      cancelled: "已取消",
      high: "高",
      medium: "中",
      low: "低",
    }[value] || String(value || "未提供")
  );
}

function verificationLabel(value) {
  return (
    {
      second_pass_review_passed: "第二轮模型复核通过",
      confirmed: "已确认",
      rejected: "已拒绝",
      inconclusive: "证据不足，结论待定",
      not_run: "未运行",
    }[value] || String(value || "未提供")
  );
}

function reviewSeverityLabel(value) {
  return { high: "高", medium: "中", low: "低", info: "提示" }[
    String(value || "").toLowerCase()
  ] || "未标注";
}

function renderReviewMarkdown(job) {
  const scope = job.review_scope || {};
  const checks = Array.isArray(scope.checks) ? scope.checks : [];
  const comments = Array.isArray(job.comments) ? job.comments : [];
  const lines = [
    "# CR-Agent 审查报告",
    "",
    `- 任务：${inlineCode(job.id || "未记录")}`,
    `- 状态：${reviewCheckLabel(job.status)}`,
    `- 来源：${reportSource(job.source)}`,
    `- 开始时间：${reportTime(job.started_at)}`,
    `- 完成时间：${reportTime(job.finished_at)}`,
    "",
    "## 结论",
    "",
    reviewConclusion(job),
    "",
    "## 审查范围",
    "",
    `- 文件：${scope.files_reviewed ?? 0}`,
    `- 新增行：${scope.added_lines ?? 0}`,
    `- 自动化测试：${scope.tests_ran ? "已运行" : "未运行"}`,
    "",
    "## 检查结果",
    "",
  ];

  if (checks.length) {
    for (const check of checks) {
      const detail = check.message ? "：" + check.message : "";
      lines.push(
        `- **${reviewCheckLabel(check.name)} · ${reviewCheckLabel(check.status)}**${detail}`,
      );
    }
  } else {
    lines.push("- 当前任务没有提供检查明细。");
  }

  lines.push("", "## 已核验问题", "");
  if (!comments.length) {
    lines.push(
      job.review_outcome === "completed_no_findings"
        ? "本次审查没有已核验的问题。"
        : "当前没有已核验的问题。",
    );
  } else {
    comments.forEach((comment, index) => {
      const location =
        inlineCode(comment.file || "未提供") + ":" + (comment.line || 0);
      lines.push(
        `### ${index + 1}. [${reviewSeverityLabel(comment.severity)}] ${location}`,
        "",
        `置信度：${reviewCheckLabel(comment.confidence)}`,
        "",
        comment.body ? markdownQuote(comment.body) : "问题描述未提供。",
      );
      for (const [label, value] of [
        ["证据", comment.evidence],
        ["触发条件", comment.trigger],
        ["影响", comment.impact],
        ["修复建议", comment.suggestion],
      ]) {
        if (!value) continue;
        lines.push(
          "",
          `**${label}**`,
          "",
          label === "证据" ? markdownFence(value) : markdownQuote(value),
        );
      }
      lines.push("", `核验结果：${verificationLabel(comment.verification_status)}`);
      if (comment.verification_reason) {
        lines.push("", "**核验说明**", "", markdownQuote(comment.verification_reason));
      }
      lines.push("");
    });
  }

  lines.push(
    "",
    "---",
    "",
    "由 CR-Agent 生成；审查结论以已核验问题和各项检查状态为准。",
    "",
  );
  return lines.join("\n");
}
