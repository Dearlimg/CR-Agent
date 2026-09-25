# S15 课程对照与本项目集成

课程原文：https://github.com/shareAI-lab/learn-claude-code/blob/main/s15_integrated_harness/README.zh.md

## 审查结论

迭代前项目不符合 S15 的核心集成要求。固定的 `AgentLoop.Plan` 是宿主工作流，
并不是模型工具循环；Eino ReAct 没有绑定工具，模型无法自主调用 Registry 中的工具。
MCP 的注册测试通过只能证明内部调用可用，不能证明模型能发现或使用它。
Skills、记忆、压缩、团队、后台和 cron 已有实现，但多个模块只通过服务层串联。

本次实现 CR-Agent 的受限审查 Harness。保留宿主前置检查和最终审查交付流程，
在 Lead 和专项模型调用中使用同一个 ReviewHarness。独立会话持有自己的 messages、
MCP 连接、Todo、归档引用和任务所有权；公共 Hook 与权限策略来自宿主。

## 对照表

| 课程机制 | 迭代前 | 本次后的实际行为 |
| --- | --- | --- |
| 工具元数据 | 宿主循环、ReviewHarness、MCP 分别拼装工具信息 | 共用 `ToolMetadata`/`ToolDefinition`，注册验证必需字段、编译 JSON Schema 并检查权限和 handler；执行前按 schema 验证参数，Harness 从注册表构造模型工具池 |
| 模型工具循环 | 没有向模型绑定工具 | 每轮重新绑定动态工具池，执行实际 tool calls，回填带 ID 的 tool messages |
| Hooks | 静态循环发事件，未处理 Pre hook 拒绝 | 模型循环支持输入、执行前后、拒绝、错误和 Stop；前置检查也遵守 Pre 拒绝 |
| 权限 | 通用 permission，MCP 策略映射丢失 deny/approval 区别 | 通用 permission 加 MCP 精确宿主策略；后台不弹交互审批，直接返回错误结果 |
| MCP | Registry 接入但模型不可见 | connect 后下一轮模型可见；失败连接不污染可用工具池；仅 mock transport |
| Skills | 服务预加载 code-review | 保留必需审查规则，模型还可按目录调用 load_skill |
| Memory | 审查前召回、结束提取 | 模型每轮 prompt 组装时召回，也可 memory_recall；结束提取沿用原实现 |
| Todo | 宿主固定检查列表 | 增加模型会话 Todo，可替换并在每轮 system prompt 中保留 |
| Task graph | HTTP/服务操作 | 模型可创建、添加依赖、列举、认领和完成本会话任务，不能操作其他会话任务 |
| Compaction | 只压缩最终汇总的文本报告 | 保留原报告管线；模型对话按完整多工具轮归档，保持协议配对，引用只在本会话可恢复 |
| 模型恢复 | 整次 ReAct 重试 | 只重试单次推理，避免重放工具；429/5xx/529 退避；长度上限从 4096 升至 8192，再失败；上下文溢出压缩后重试一次 |
| Background | 整个审查异步运行 | 模型可后台执行注册静态检查，立即获得 ID；完成通知注入原对话，空闲等待后自动续轮 |
| Cron | 独立 scheduler 创建审查 Job | 保留该 HTTP 服务的唤醒方式；可读计划，模型创建/取消默认需要审批而被后台拒绝 |
| Team | 单 Job 内三个短生命周期专项成员 | 每个成员使用同一 Harness，保留独立上下文；结果/idle 事件注入 Lead 对话并保留 trace |

## 与通用 coding-agent 示例的范围差异

下列内容**没有实现，不属于已完成的课程全量复刻**：

- 长期驻留队友、IDLE 扫描任务板、成员之间主动通信、plan approval / shutdown 协议及 assignment version。
- 任务绑定的 worktree 和队友 cwd 切换。当前项目输入是 diff，没有可信仓库 checkout，工具也不修改仓库。
- 任意 bash、文件读写、后台进程组管理。后台工具只接受已经注册的审查检查名称。
- CLI cron prompt 队列。当前 cron 的至少一次边界是创建后台审查任务，不是模型成功消费 prompt。
- 自动切换 fallback 模型和输出 continuation。超过二次输出预算时明确失败，不返回截断 JSON 为成功结果。
- MCP stdio / HTTP 传输，以及真实文档或部署集成。docs/deploy 输出明确作为模拟数据，不能当审查事实。

这些差异意味着项目完成了 S15 的核心循环集成及适用于只读审查的模块接入，
但不能宣称与教程完整 coding-agent runtime 等价。要全量复刻，需要继续实现上面的
持久团队协议、任务工作区和系统命令执行能力，并为每种生命周期补集成测试。

## 工具注册与 typecheck

新增宿主工具时，在注册表中提供 `ToolMetadata{Name, Description, InputSchema, Permission}` 和 `ToolDefinition.Run`。注册表使用 Draft 2020-12 JSON Schema 编译器检查 schema，并在执行前验证完整参数；为避免动态工具 schema 触发外部资源访问，外部 `$ref` 被拒绝。元数据转换为 Eino `ToolInfo`；Harness 通过通用分发器执行 handler，新增工具无需修改模型主循环。MCP 发现的工具也转换成同一份元数据结构，继续使用 MCP 精确宿主策略做二次授权。

模型可按需调用 `typecheck` 检查 PR 固定 head 的 Go module。它要求 `E2B_API_KEY`、GitHub PR 固定 head 和根目录 `go.mod`，在受限 E2B 沙箱执行宿主固定的 `go build ./...`，不会执行项目测试或模型提供的命令。工具输出明确区分 `passed`、`failed`、`incomplete` 和 `not_run`。

## 入口与验证

- `internal/logic/harness.go`：统一模型循环、动态发现、分发、配对、恢复、归档引用。
- `internal/logic/agent_loop.go`：共享工具元数据、注册校验和 AgentLoop 定义。
- `internal/logic/harness_service.go`：把项目能力与 `typecheck` 接入当前审查会话。
- `internal/logic/eino_agent.go`：真实 Eino/OpenAI-compatible 适配；重试不包含工具执行。
- `internal/logic/harness_test.go`：脚本模型和本地 HTTP 假模型验证完整协议链路。

验证命令：`go test ./...`、`go vet ./...`、`git diff --check`。
核心断言覆盖统一元数据校验、新工具注册后无需改 Harness 主循环即可发现和调用、
typecheck 的 E2B/Go module 前置条件、连接后 MCP 动态工具可见、多个 tool call 一一配对、
错误回传、Hook 拒绝、重试不重放工具、MCP 显式 deny、后台自动续轮和归档访问范围。
测试不使用真实模型密钥或远程 MySQL，不应据此宣称真实外部服务联调已通过。
