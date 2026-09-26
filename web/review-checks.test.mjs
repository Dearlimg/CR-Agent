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
