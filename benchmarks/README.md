# CR-Agent Code Review Benchmark

This first benchmark revision contains 21 small, synthetic Go diffs: 12 positive
diffs with 14 manually annotated issues and 9 negative controls. Positive diffs
may include more than one independently actionable defect. It exercises correctness,
security, concurrency, resource handling, compatibility, and false-positive
control. The fixtures are intentionally small, so they are a diagnostic set,
not a claim of production-wide accuracy.

## Research basis

Recent code-review evaluation work favors PR-level issue coverage over textual
similarity. SWR-Bench uses manually checked PR instances and evaluates whether
generated reviews cover structured issues; CodeReviewBench publishes precision
and recall against confirmed bugs. This local set borrows those principles at a
smaller scale. It does not reuse either benchmark's data, and its synthetic
diffs omit repository context, so its scores cannot be compared with theirs.

## Metrics

- **Precision / recall / F1**: predicted findings are matched to the annotated
  issue by exact changed-file path, a line distance of at most two, and
  case-specific phrase groups. Unmatched predictions are false positives.
- **Valid changed-line rate**: findings must point to an added line in the
  submitted diff.
- **Trace completeness**: trace entries must have start/end times and status.
- **JSON validity**: the final model reply must parse as a JSON array.
- **Redaction canary**: verifies the fake placeholder value does not appear in
  stored trace data. The canary is not a real credential.

Phrase matching is a cheap pilot judge, not a general semantic evaluator. Review
the per-case JSON output before using a score to make model or release decisions.
There is one run per case; stochastic variance is not measured.

## Run

Validate labels and added-line locations without making any model calls:

```powershell
go run ./cmd/benchmark --validate-only
```

The benchmark uses the application's single-agent review flow. The command
below evaluates all 21 cases in manifest order:

```powershell
go run ./cmd/benchmark --limit 21 --max-calls 400 --max-output-tokens 1024
```

Each JSON and Markdown report records the route as `single`. The Markdown and
console output show TP/FP/FN, wall time, forwarded model requests, and reported
tokens for each case. If the endpoint omits usage, token counts are marked
unknown rather than treated as zero. Failed cases count missing expected issues
as false negatives; compare completion rate alongside precision and recall.
Run the benchmark more than once before drawing a latency conclusion.

Run a balanced six-case slice through the current asynchronous review service
(three positive cases and three negative controls):

```powershell
go run ./cmd/benchmark --case-ids nil-dereference,map-concurrent-write,path-traversal,negative-parameterized-sql,negative-guarded-empty-input,negative-placeholder-secret --max-calls 48 --max-output-tokens 1024
```

Use `--limit N` to select the first N manifest entries, or `--case-ids` to
choose a reproducible subset in the specified order. `--score-report path.json`
recomputes scores offline after a human changes fixture annotations.

The live command uses `DEEPSEEK_API_KEY` and `DEEPSEEK_BASE_URL` from the usual
environment or local `.env`. It forwards requests through a temporary loopback
proxy that enforces a global request count, per-request output-token maximum,
and request-body size. It never logs the credential or request bodies. Review
state is isolated per case inside a temporary directory, so one case's extracted
memory cannot influence the next. Aggregate Markdown and JSON results are
written beneath `benchmarks/results/`.

Adjust `--limit`, `--max-calls`, `--max-output-tokens`, and `--case-timeout` to
control the run. Model-reported token usage may be absent from a compatible
provider response; missing usage is recorded as unknown. The application still
does not meter or enforce a dollar budget, so the request/token caps here are
the live benchmark's resource guard.

## Known gaps

- No real PR history or full repository snapshot; only the provided diff is
  reviewed.
- No external semantic judge or human adjudication of generated findings.
- The password-in-log fixture checks that review redaction keeps password
  logging code visible while masking credential values before inference.
- No repeated runs, cross-language samples, latency target, or dollar-cost
  comparison yet.

## References

- [SWR-Bench paper](https://arxiv.org/abs/2509.01494)
- [CodeReviewBench methodology](https://www.codereviewbench.com/)
- [DeepCRCEval paper](https://arxiv.org/abs/2412.18291)
