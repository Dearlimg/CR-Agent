---
name: code-review
description: Review a git diff for high-confidence correctness, security, dependency, and maintainability defects. Use for code review, PR review, or pre-merge checks.
version: 1.0.0
---

# Code Review

Review only the supplied diff. Report defects introduced or exposed by this
change; do not turn style preferences, speculative redesigns, or pre-existing
problems into findings.

## Review flow

1. Identify changed files, public interfaces, data flows, and dependency changes.
2. Inspect changed control flow and error paths before commenting on style.
3. Review auth, input handling, secrets, file/network I/O, and permission changes
   when they are in scope.
4. Check compatibility, migrations, retries/idempotency, and test coverage when
   the diff touches those boundaries.
5. Emit only findings with a concrete changed-line location and a reproducible
   failure mode. Return `[]` if there is no actionable finding.

## Severity and confidence

- `high`: security boundary bypass, data loss/corruption, outage, or a clear
  correctness regression on a common path.
- `medium`: a likely defect on a supported path, unsafe error handling, or a
  meaningful compatibility break.
- `low`: a bounded maintainability or test gap that can plausibly cause a future
  defect. Do not use this for personal style.
- Use `high` confidence only when the diff itself proves the behavior; use
  `medium` when a direct, reasonable inference is needed. Omit low-confidence
  speculation.

## Required finding shape

Each JSON item must contain `file`, `line`, `severity`, `confidence`, `body`,
and `suggestion`. Explain the failure mode and consequence in `body`; make the
smallest safe repair concrete in `suggestion`.

## Output language

- Write all user-facing prose in `body` and `suggestion` in Simplified Chinese,
  regardless of the language used in the diff or reports.
- Keep `severity` and `confidence` as the required English enum values. Preserve
  identifiers, file paths, API names, and necessary source literals verbatim.
- Keep `body` concise and specific about the trigger, defect, and impact. Put the
  concrete repair in `suggestion`; do not duplicate the repair text in `body`.
- When synthesizing an English report, translate its explanation faithfully
  without changing technical meaning or adding unsupported claims.

## Guardrails

- Never invent repository context that is absent from the diff or supplied report.
- Do not reveal values that resemble credentials, tokens, passwords, or private
  connection strings; describe the exposure without reproducing the value.
- Do not propose executing code, publishing comments, or modifying files.
- Deduplicate reports of the same root cause and prefer the most actionable line.
