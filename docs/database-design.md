# 数据库设计（待 CR）

本设计只定义 GORM 模型和迁移入口，不会在服务启动时自动连接或修改 MySQL。

## 表关系

```text
review_jobs 1 ─── N review_comments
review_jobs 1 ─── N trace_events
review_jobs 1 ─── N tool_calls
review_comments.trace_id ─── trace_events.trace_id
tool_calls.trace_id ─── trace_events.trace_id
```

| 表 | 用途 | 关键字段 |
|---|---|---|
| `review_jobs` | 一次 MR/PR 审查及预算、状态、错误 | `public_id`、`input_hash`、`status`、`budget_cents`、`spent_cents` |
| `review_comments` | 可单独采纳/忽略的结构化评论 | `file`、`line`、`severity`、`confidence`、`trace_id` |
| `trace_events` | 模型/工具的可审计事件 | `tool`、`phase`、`input`、`output`、`model_reply`、`duration_ms` |
| `tool_calls` | 工具执行状态和耗时 | `tool`、`status`、`input_hash`、`duration_ms` |

## 设计取舍

- `public_id` 与自增主键分离：API 不暴露数据库顺序 ID，checkpoint/API 可以继续使用短 ID。
- `input_hash` 用于幂等和重复审查去重；是否加唯一约束需要 CR 决定，因为相同 diff 可能需要重新审查。
- `trace_events` 保存脱敏后的输入；原始 secret 不应进入数据库。
- `review_comments` 单独建表，避免模型多条意见被压成一个长文本，并支持后续评论状态流转。
- `tool_calls` 与 `trace_events` 分开：trace 是统一审计事件，tool call 是可重试、可统计的执行记录。
- 大字段使用 `LONGTEXT`，生产环境可进一步改成对象存储地址，数据库只留摘要和 hash。
- `AutoMigrate` 只作为开发期入口；生产建议改为版本化迁移。
