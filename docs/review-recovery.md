# 审查主流程中断恢复

主审查由宿主按固定阶段编排，生产恢复状态保存在 `review_jobs.checkpoint_json`；Review Job 的公开 JSON 不返回检查点内容。

设计对照了成熟 durable execution 平台：Temporal 将状态保存在可恢复的 Workflow 历史中，并建议外部 Activity 保持幂等；Azure Durable Task 在 await/yield 边界保存历史，恢复时重放已完成 Activity 的结果。[Temporal Durable Execution](https://docs.temporal.io/)；[Temporal Activities](https://docs.temporal.io/activities)；[Azure Durable orchestrations](https://learn.microsoft.com/en-us/azure/durable-task/common/durable-task-orchestrations)。CR-Agent 复用现有 MySQL 和宿主阶段编排，按 Review 阶段与 finding 保存结果，没有引入新工作流服务。

## 检查点

主流程按以下阶段持久化完成结果：

1. `preflight_completed`：保存脱敏 diff、文件/行信息、静态检查和原始输入的密钥扫描结果。
2. `context_prepared`：保存 Skill、记忆与审查 prompt 上下文、沙箱测试结果，以及 GitHub PR 的固定 head SHA。
3. `candidates_ready`：保存首轮模型候选、通过新增行证据校验的 findings、被证据门禁拒绝的数量和首轮使用的固定 head。
4. `finding_context_prepared`：保存逐条复核所需的固定 head 源码上下文和读取错误状态。
5. `finding_verification`：每完成一条复核，就在同一事务写入该条 verdict 和下一个游标。`confirmed`、`rejected`、`inconclusive` 都是已完成的复核结果；超时、截断或请求错误保留当前游标，恢复时只重试这条 finding。
6. `finalizing` / `completed`：保存已核验评论和最终结果，避免恢复时再次运行审查或 finding 复核。

每次阶段保存同时更新 Job 状态、检查点和相关结果。MySQL 执行租约由同一行的 `runner_owner`、`runner_lease_until` 管理，心跳间隔 20 秒、租期 90 秒；同一时刻只有一个实例能持有审查执行权。服务启动时恢复 `queued`/`running` Job，并每 15 秒扫描一次等待过期租约。需人工恢复时调用：

```http
POST /api/reviews/{id}/resume
```

接口返回 `202 Accepted` 和恢复后的 Job；任务正由其他执行器运行时返回 `409 Conflict`。已完成且检查点为 `completed` 的任务不能恢复。

## 预算恢复与调用边界

模型请求前先将估算额度写入 `reserved_micros`。请求返回后，按模型 usage 结算到 `spent_micros`，释放预留。进程中断时若仍有未结算预留，恢复器会将该预留按已花费金额保守结算，再决定是否还能继续调用；总预算上限不会在恢复时重置。

模型服务端请求与 MySQL 不共享事务。进程若在服务端完成请求后、保存响应结果前崩溃，恢复时无法证明该请求是否完成：此时按预留金额记账，并可能重试当前未完成的阶段或 finding。已经落盘的阶段和逐条复核结果不会重跑。完成一次模型请求后必须先持久化结果，再推进恢复游标；这个顺序提供可审计的至少一次恢复语义，不承诺外部模型调用恰好一次。

直接传入的 diff 在创建 Job 时完成原始密钥扫描，只把脱敏后的 diff 放入检查点。GitHub PR 上下文保存固定 head SHA，恢复后继续读取同一提交。文件型 `JobStore` 用于本地测试和开发；生产恢复与多实例租约使用 MySQL。

生产服务默认启动时运行 GORM AutoMigrate，会为 `review_jobs` 增加检查点、预算预留与执行租约列。显式设置 `AUTO_MIGRATE=false` 的部署需先手动应用等价 schema 变更，再启动新版本。
