# CR-Agent Code Review Benchmark

- Benchmark: `code-review-v1`
- Route: `specialists`
- Started: 2026-09-23T06:23:25Z
- Model: deepseek-chat via configured endpoint
- Cases completed: 21/21
- Total wall time: 78348 ms
- Forwarded model requests: 86 (cap 400)
- Model-reported tokens: 191846

| Precision | Recall | F1 | Valid changed-line rate | Trace completeness |
|---:|---:|---:|---:|---:|
| 37.0% | 71.4% | 48.8% | 100.0% | 100.0% |

TP=10, FP=17, FN=4. Redaction canary leaked: **false**.

| Case | Category | Status | TP | FP | FN | Line valid/invalid | Duration | Model requests | Reported tokens |
|---|---|---|---:|---:|---:|---:|---:|---:|---:|
| `nil-dereference` | correctness | completed | 1 | 1 | 0 | 2/0 | 4123 ms | 4 | 9217 |
| `off-by-one-slice` | correctness | completed | 1 | 0 | 0 | 1/0 | 2866 ms | 4 | 8930 |
| `map-concurrent-write` | concurrency | completed | 1 | 1 | 0 | 2/0 | 7063 ms | 4 | 11589 |
| `sql-injection` | security | completed | 0 | 4 | 2 | 4/0 | 6864 ms | 4 | 11411 |
| `http-response-body-leak` | resource | completed | 2 | 4 | 0 | 6/0 | 7713 ms | 4 | 11909 |
| `path-traversal` | security | completed | 1 | 0 | 0 | 1/0 | 3716 ms | 4 | 8520 |
| `empty-average` | correctness | completed | 1 | 0 | 0 | 1/0 | 3613 ms | 4 | 9023 |
| `retry-payment-without-idempotency` | reliability | completed_with_warnings | 0 | 0 | 1 | 0/0 | 3167 ms | 4 | 8058 |
| `broad-file-permissions` | security | completed_with_warnings | 1 | 2 | 0 | 3/0 | 5727 ms | 4 | 9828 |
| `json-api-breaking-change` | compatibility | completed | 1 | 0 | 0 | 1/0 | 3567 ms | 5 | 10693 |
| `password-in-log` | security | completed | 0 | 0 | 1 | 0/0 | 2914 ms | 5 | 9791 |
| `ignored-error` | error-handling | completed | 1 | 0 | 0 | 1/0 | 3263 ms | 4 | 8838 |
| `negative-parameterized-sql` | negative-control | completed | 0 | 0 | 0 | 0/0 | 1913 ms | 4 | 7796 |
| `negative-guarded-empty-input` | negative-control | completed | 0 | 1 | 0 | 1/0 | 3776 ms | 4 | 9149 |
| `negative-error-checked-close` | negative-control | completed | 0 | 3 | 0 | 3/0 | 5413 ms | 4 | 9937 |
| `negative-safe-join-after-containment-check` | negative-control | completed | 0 | 0 | 0 | 0/0 | 1922 ms | 4 | 7799 |
| `negative-test-only-fatal` | negative-control | completed_with_warnings | 0 | 0 | 0 | 0/0 | 2712 ms | 4 | 7888 |
| `negative-placeholder-secret` | negative-control | completed | 0 | 0 | 0 | 0/0 | 1665 ms | 4 | 7642 |
| `negative-safe-file-permission` | negative-control | completed | 0 | 0 | 0 | 0/0 | 1715 ms | 4 | 7694 |
| `negative-safe-retry-read` | negative-control | completed | 0 | 1 | 0 | 1/0 | 2676 ms | 4 | 8304 |
| `negative-transaction-rollback` | negative-control | completed | 0 | 0 | 0 | 0/0 | 1914 ms | 4 | 7830 |

Scoring uses fixture-specific phrase groups plus changed-file and ±2-line localization. It is a pilot heuristic, not an independent semantic judge. Failed cases count their missing expected issues as false negatives; compare completion rate alongside precision and recall.
