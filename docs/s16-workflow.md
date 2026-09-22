# S16 Workflow Runtime 对照

课程原文：[s16 Workflow Runtime](https://github.com/shareAI-lab/learn-claude-code/blob/main/s16_workflow_runtime/README.zh.md)

## 课程要求与项目落点

S16 的边界是：模型用一次 `Workflow` tool call 选择宿主已注册的 workflow，模型只传
`name`、`args` 和可选的 `resume_from_run_id`；workflow 脚本由宿主维护，使用
`agent`、`parallel`、`pipeline`、`phase`、`log` 和一层嵌套 `workflow` 原语。每次
`agent` 调用使用稳定语义键写入 journal，workflow 中断后可以从快照和 journal 续跑。

本项目现在注册了 `review-changes`。它对 correctness、security、dependency 三个维度
并行执行 audit，再对每个 finding 并行执行 verify，最后汇总 confirmed findings。模型仍然
负责生成单步结构化结果，编排顺序、并发边界和恢复规则由宿主代码负责。

核心实现位于：

- `internal/logic/workflow.go`：registry、元数据校验、运行任务、编排原语、稳定调用键、快照、journal、输出文件和 resume。
- `internal/logic/workflow_review.go`：代码审查 workflow 的 audit → verify → summary 脚本。
- `internal/logic/harness.go`：把 `Workflow` 作为模型可见工具，并在下一轮注入完成通知。
- `internal/logic/harness_service.go`：绑定当前 Job 的模型 runner、权限、归档、取消和 trace。

运行文件默认放在 `.workflows/`，可用 `AGENT_WORKFLOW_DIR` 修改。一次运行产生：

```text
.workflows/<run_id>.json          # workflow 快照和任务状态
.workflows/<run_id>.journal.jsonl # 每个 agent 结果和进度事件
.workflows/<run_id>.output.json   # 最终结构化输出
.workflows/<run_id>.lock          # 执行/续跑排他锁
```

## 恢复与安全边界

元数据在注册时校验：名称只能包含字母、数字、`.`、`_`、`-`，长度 1–64；描述必须非空；
阶段名称必须是非空字符串。未知 workflow、错误参数、错误 resume ID 和运行锁冲突会作为
工具错误返回。

每次 `agent` 的 key 由调用类型、label、prompt 和 schema 的稳定哈希组成，不依赖并发完成
顺序。resume 会读取快照和 journal 中的 `agent_result`，命中缓存的调用不再访问模型；修改
prompt、label 或 schema 会产生新 key。结构化结果不符合 schema 时只重试一次，仍不合法则
workflow 失败。

workflow 脚本只能通过 `ExecutionState` 使用编排原语，不能直接运行 shell 或读取任意文件。
模型生成的 workflow 名称和参数不能提交脚本；`Workflow` 工具不接受可执行代码。每次运行
持有 `.lock`，同一个 run 不能被两个进程同时 resume。任务取消会取消运行上下文并记录
`cancelled` 状态。

## 与 S15 的关系

S15 的模型循环仍然是外层循环：`Workflow` 调用先返回启动信息，workflow 的中间结果保留
在运行时变量和 journal，不塞入主对话。完成或失败后，宿主通过 `<task_notification>` 注入
下一轮；模型可以根据最终结果继续总结或发起其他工具调用。

已有的 `ReviewTeam` 仍保留为审查服务的默认专项队友路径；`review-changes` 是可恢复的宿主
编排能力，适合模型明确选择一次完整的审查脚本。两者共享模型适配、权限、脱敏和 trace
边界，不把队友临时对话直接伪装成 workflow journal。

## 尚未实现的课程全量能力

当前完成的是 S16 的 Workflow 核心运行时，仍有明确范围差异：

- 没有开放任意脚本、Shell、文件编辑或 worktree 操作；这符合本项目只读 Code Review 边界。
- 没有把 workflow 任务迁移到跨进程数据库；文件锁和快照适合单机本地运行，生产部署仍需外部租约和存储设计。
- 没有独立的 SDK 事件流接口；事件保存在快照/journal，并通过通知回到模型。
- 嵌套 workflow 只允许一层，尚未实现课程后续版本中的更复杂 workflow DAG。

## 验收

`internal/logic/workflow_test.go` 覆盖元数据校验、pipeline/parallel、结构化输出重试、
锁冲突、取消、通知只消费一次，以及 resume 命中 journal 后不重放模型调用。`go test ./...`
和 `go vet ./...` 均应通过。
