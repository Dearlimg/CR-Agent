# 审查 Prompt 组装

审查请求由 `PromptEnvelope` 组装，模型适配器保留消息角色：

| 层 | 内容 | 加载时机 |
| --- | --- | --- |
| 核心 system | 身份、证据和信任边界 | 每轮模型调用 |
| 任务 system | 当前审查重点、选中的 `code-review` Skill、JSON 输出契约 | 主审或复核任务启动时 |
| user 数据 | 前置检查摘要、相关记忆、diff、待复核候选与固定提交源码 | 仅对应任务 |
| tool 消息 | 工具返回值及调用 ID | 工具调用后 |
| 会话状态 | 模型写入的计划 | 有状态时作为 user 数据提供 |

主审通过 `BuildReviewPromptEnvelope` 选择 Skill；源码查询能力由当前会话实际注册的工具决定。复核使用独立的判定规则，不加载主审 Skill。格式修复使用单独的输入，不继承主审的长 prompt。仓库内容、记忆、工具结果和模型写入的计划都不进入 system 层。

`PromptEnvelope` 只负责消息分层。工具权限、固定提交源码读取、finding 的新增行匹配和最终发布判断仍由宿主代码执行。`BuildReviewSubagentPrompt` 保留字符串形式供现有调用方使用；正式 Eino 请求使用结构化 envelope。

验证时检查提供商请求的消息角色、Skill 是否按需出现、diff 与记忆是否只在 user 层，以及工具结果是否保持 tool 角色。长度指标可比较输入字符或实际 provider token 用量；质量变化需要用同一批真实 PR 比较有效发现、误报、漏报与工具调用。
