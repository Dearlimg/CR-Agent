import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";
import vm from "node:vm";

const source = ["state", "helpers", "view", "export"]
  .map((name) => readFileSync(new URL(`./assets/js/${name}.js`, import.meta.url), "utf8"))
  .join("\n");

test("historical compiler and sandbox checks are hidden in UI, trace, and export", () => {
  const retired = ["syntax_check", "format_check", "automated_tests", "typecheck"];
  const inputJob = {
    status: "completed", review_outcome: "completed_no_findings", comments: [],
    review_scope: {
      files_reviewed: 1, added_lines: 9,
      checks: [
        ...retired.map((name) => ({ name, status: "not_run", message: "RETIRED" })),
        { name: "secret_scan", status: "passed", message: "RETAINED" },
      ],
    },
  };
  const result = vm.runInNewContext(source + `
    job = inputJob;
    ({html: renderOutcome(), markdown: renderReviewMarkdown(job),
      hidden: retired.every(tool => isHiddenSandboxEvent({tool})),
      contextHidden: isHiddenSandboxEvent({tool: "get_review_context"})})`,
    { inputJob, retired, URL, Date });
  for (const text of [result.html, result.markdown]) {
    assert.ok(!text.includes("RETIRED"));
    assert.ok(!text.includes("语法检查") && !text.includes("格式检查"));
    assert.ok(text.includes("RETAINED"));
  }
  assert.equal(result.hidden, true);
  assert.equal(result.contextHidden, false);
});

test("secret scan hit stays visible without changing a completed review conclusion", () => {
  const inputJob = {
    status: "completed", review_outcome: "completed_with_findings",
    comments: [{}], review_scope: {
      files_reviewed: 1, added_lines: 1,
      checks: [{ name: "secret_scan", status: "found", message: "疑似密钥命中=1" }],
    },
  };
  const result = vm.runInNewContext(source + `
    job = inputJob;
    ({html: renderOutcome(), markdown: renderReviewMarkdown(job)})`,
    { inputJob, URL, Date });
  for (const text of [result.html, result.markdown]) {
    assert.ok(text.includes("审查完成"));
    assert.ok(text.includes("疑似密钥扫描"));
    assert.ok(text.includes("疑似命中，需人工确认"));
    assert.ok(!text.includes("审查未完成"));
  }
});

test("pending-verification summary completes the review without defect claims", () => {
  const inputJob = {
    status: "completed", review_outcome: "completed_with_pending",
    comments: [
      {
        file: "", line: 0, severity: "low", confidence: "medium",
        body: "以下 1 个疑点有代码依据，但第二轮复核仍有待核实前提，未作为缺陷报告；请确认前提是否成立：\n\n1. tools.py:35 非整数浮点被降级为 -1。\n待核实前提：确认调用方是否允许该输入",
        verification_status: "second_pass_review_plausible",
        verification_reason: "有代码依据的疑点汇总；复核未发现反证，前提待人工确认。",
      },
    ],
    review_scope: {
      files_reviewed: 2, added_lines: 210,
      checks: [
        { name: "finding_verification", status: "passed", message: "候选=1；证据匹配=1；第二轮复核确认=0；有根据待核实=1；待确认评论=1" },
      ],
    },
  };
  const result = vm.runInNewContext(source + `
    job = inputJob;
    ({html: renderOutcome(), markdown: renderReviewMarkdown(job)})`,
    { inputJob, URL, Date });
  for (const text of [result.html, result.markdown]) {
    assert.ok(text.includes("待确认"));
    assert.ok(!text.includes("审查未完成"));
  }
  assert.ok(result.markdown.includes("待确认疑点"));
  assert.ok(result.markdown.includes("整体审查"));
  assert.ok(result.markdown.includes("前提经人工确认后才构成缺陷"));
});
