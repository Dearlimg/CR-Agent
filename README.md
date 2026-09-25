# Code Review Agent

基于 harness 思路的 Code Review Agent 初版：模型/工具执行由服务编排，工具通过注册表声明式接入。

## 运行

```powershell
copy .env.example .env
go run ./cmd/server
```

打开 <http://localhost:8080>，输入 MR/PR 链接或粘贴 `git diff`。服务抓取链接中的 diff，执行前置检查，再将脱敏材料交给模型审查。

凭据使用大写环境变量（如 `DEEPSEEK_API_KEY`、`GITHUB_TOKEN`、`E2B_API_KEY`），Windows、macOS、Linux 和容器均使用同一套配置名。进程环境变量优先；本地开发默认从当前工作目录加载 `.env`。从其他目录启动时，可在启动进程前设置 `CR_AGENT_ENV_FILE` 指向配置文件，例如 PowerShell：

```powershell
$env:CR_AGENT_ENV_FILE = "C:\cr-agent\.env"
& "C:\cr-agent\cr-agent.exe"
```

生产环境应由服务管理器或密钥管理系统注入环境变量。Docker 使用 `docker run --env-file /opt/cr-agent/.env ...` 将配置注入容器进程；镜像不会复制 `.env`。指定的 `CR_AGENT_ENV_FILE` 不存在或无法读取时，服务会在启动时返回具体错误。

## 设计

- **可恢复**：生产运行时将审查阶段、逐条复核游标、预算预留、评论、Todo、trace 和工具调用写入 MySQL；服务重启后从最近完成的阶段继续。
- **可观测**：每条评论带 `trace_id`，任务返回工具、脱敏输入、输出和时间戳。
- **可扩展**：`ToolRegistry.Register("name", tool)` 声明式注册工具，不修改主流程。
- **预算**：按人民币控制每次审查的总模型费用；请求可传 `budget_yuan`，默认读取 `REVIEW_BUDGET_YUAN`（默认 ¥10）。每次模型请求按返回的输入/输出 token 和模型单价计费，并在请求前预留估算额度；超出上限后停止后续调用。方案和边界见 [`docs/token-budget-design.md`](docs/token-budget-design.md)。
- **安全**：在本地原始 diff 上扫描疑似凭据，再向模型提供脱敏 diff；PR 测试只在配置的 E2B 远端沙箱运行，不在 CR-Agent 宿主执行。
- **按需 Skills**：启动时仅扫描 `skills/*/SKILL.md` 的名称和描述；审查任务会记录并加载 `code-review` 的完整指令，再交给主审查 Agent 或专项 Agent。

## 前置检查与复用

每次审查只执行一次 `preflight_analysis`，生成变更文件、新增行号、依赖文件、检查状态和疑似密钥位置。结构化结果同时提供给后续 Agent；模型工具池不再包含相同的 diff 解析、密钥扫描和格式检查工具。内存缓存仅保存不含原始代码和密钥值的解析结果，按来源、diff 摘要与分析器版本区分，30 分钟过期；原始 diff 的密钥扫描每次仍会执行。

`syntax_check` 和 `format_check` 只对完整的新建 Go 文件分别运行解析与 `gofmt` 检查；修改过的文件缺少仓库上下文时明确标为 `not_run`。`static_check` 只统计新增行的 TODO 和 `panic(` 字符串提示，不把它们直接当作缺陷。依赖变更按文件路径识别。最终 finding 必须落在实际新增行，服务再统一归一化并去重。

Trace 的 `origin` 区分编排调用和模型工具调用，`cache_hit` 表示解析结果复用；界面分别展示这两类调用与缓存命中。

## 沙箱自动测试

在 `.env` 配置 `E2B_API_KEY`、`E2B_DOMAIN` 和 `AGS_TEMPLATE` 后，GitHub PR 审查会从 PR 的固定 head SHA 下载完整源码，并在腾讯云 E2B 兼容沙箱中识别并运行 Go、Python、Node.js、Rust、Maven 或 Gradle 测试入口。默认单次测试上限为 600 秒，可用 `E2B_TEST_TIMEOUT_SECONDS` 调低；Docker 镜像会安装与已验证示例一致的 Python E2B SDK（版本见 `requirements-sandbox.txt`），本地运行需先安装该依赖，并可用 `E2B_PYTHON_EXECUTABLE` 指向对应解释器。

沙箱工具模板需要包含对应项目的语言运行时；项目依赖不会由 CR-Agent 自动安装。模板缺少运行时、源码包超过安全大小限制、来源不是 GitHub PR 或没有可识别的测试入口时，审查结果会明确显示 `incomplete` 或 `not_run`；pytest 在测试收集阶段中断也会显示为 `incomplete`，不会计为测试用例已运行。测试日志限长并记录在 `automated_tests` trace；测试 API Key 不会传入 PR 测试进程，沙箱在执行结束后会尝试销毁。`AGS_TEMPLATE` 使用控制台的沙箱工具名称，工具 ID 仅供控制台识别。

## 导出报告

审查工作台可下载 GitHub Flavored Markdown 格式的可读报告，包含审查状态、范围、检查结果和已核验问题；会话日志仍可单独导出为 JSON，包含完整 trace 详情。

## 中断恢复

生产服务启动后会自动续跑中断的 `queued`/`running` 审查。对超时、预算不足或部分复核未完成的终态任务，可调用 `POST /api/reviews/{id}/resume` 从已保存的阶段恢复；正在由其他实例执行的 Job 返回 `409`。已完成的逐条复核会跳过，未完成的 finding 从游标位置继续。模型请求前写入预算预留，恢复时将无法确认结果的预留保守计入已花费预算。外部模型请求与数据库不能构成一个事务，因此崩溃恰好发生在模型返回之后时，当前请求可能重试；已持久化的阶段结果和 finding verdict 不会重跑。详见 [`docs/review-recovery.md`](docs/review-recovery.md)。

## Skills

内置的 [`skills/code-review/SKILL.md`](skills/code-review/SKILL.md) 约束审查范围、
严重度、置信度、输出字段和敏感信息处理。新增 skill 时创建
`skills/<name>/SKILL.md`，并在 YAML frontmatter 中写入唯一的 `name` 和用于目录的
`description`；完整正文不会进入启动目录。默认目录是 `skills`，可通过
`AGENT_SKILLS_DIR` 指向其他目录。

当前审查流程固定装载 `code-review`；每个任务的 `trace` 会产生一条
`load_skill` 事件，以便确认实际生效的 skill。

## 上下文压缩

专项审查启用时，最终汇总模型接收子 Agent 报告前，服务会依次执行：大结果落盘并保留预览、
旧上下文归档、超限时缩短已消费的 tool result，最后才调用模型生成事实型摘要。
落盘文件位于 `.task_outputs/tool-results/`，完整上下文位于 `.transcripts/`，
二者均不纳入 Git，且写入前会脱敏。每次压缩会在任务 `trace` 中增加
`context_compact` 事件。

阈值可通过 `CONTEXT_CHAR_LIMIT`、`TOOL_RESULT_BUDGET`、
`LARGE_RESULT_CHAR_LIMIT` 与 `CONTEXT_MAX_MESSAGES` 配置；默认值遵循
“先可恢复地整理、后有损摘要”的顺序。

## 持久记忆

Memory 用于跨审查任务保留可复用知识，不保存完整 transcript。每条记录单独保存在
`.memory/<name>.md`，`.memory/MEMORY.md` 只保留索引。新审查任务会按照 `memory_query`
（未提供时使用来源和 diff）的关键词召回至多 `MEMORY_MAX_RECALL` 条、总计不超过 `MEMORY_MAX_CHARS` 的相关记忆；
提示词明确将它们视为背景，当前 diff 与当前请求优先。

可通过 `POST /api/memories` 显式保存经过确认的长期偏好或项目约束：

```json
{"name":"go-error-style","description":"项目要求包装错误","type":"project","body":"Go 错误应保留上下文并使用 %w 包装。","scope":"persistent"}
```

`GET /api/memories` 查看索引内容。自动提取只在模型可用时运行，且候选必须包含
`persistent` scope、通过临时性与敏感信息检查、并且不与已有记录重复。达到
`MEMORY_CONSOLIDATE_AT` 后只会清理高度相似的重复记录，不会把完整对话写入记忆。

## 任务系统

Todo 是单次审查的执行清单；Task 是跨会话保留的任务图。生产环境使用 `agent_tasks` 和
`agent_task_dependencies` 保存，包含 `subject`、`description`、`status`、`owner` 和依赖关系。
状态只能按 `pending → in_progress → completed` 转换：认领时检查所有
依赖已完成，完成时返回刚被解锁的下游任务；添加依赖会拒绝自依赖、缺失任务和环。

每个 `POST /api/reviews` 会自动创建并认领一个 Lead（`review-agent`）任务，返回的审查 Job
包含 `task_id`，审查结束后任务自动完成。未完整审查会在 Job 上体现为
`completed_with_warnings` 或 `failed`；检查点尚未到 `completed` 时，Lead 任务保持
`in_progress`，可通过恢复入口继续执行。

模型调用对 EOF、连接中断、超时、限流和 5xx 做有限指数退避重试。少于 4 个变更文件、少于 300 条 diff 新增行，且没有跨文件依赖变更的任务由一个主审查 Agent 完成；其余任务运行三个专项 Agent 和最终汇总。团队默认最多同时运行 2
个专项调用。专项调用失败但最终汇总成功时，Job 状态为
`completed_with_warnings`，不会伪装成完全成功。

## Agent Team

Lead 负责向调用方交付最终结论。规模较大的任务会启动 correctness、security、dependency 三个专项队友；小任务直接使用一个主审查 Agent。每位专项队友拥有独立模型上下文，完成后会向 MySQL `team_messages` 写入两个持久化事件：`result`（审查产出）和 `idle_notification`（可继续接收工作）。Lead 在最终汇总前消费当前 Job 的事件，并将其返回在 `team_events` 中。

专项任务同样写入共享 `.tasks/` 任务板，按 `pending → in_progress → completed` 原子认领；失败不会被标记为完成。这个迭代刻意不让队友执行代码、修改仓库或发布评论，仍沿用受限的只读审查工具边界。当前队友生命周期限定在单次审查 Job；跨 Job 的长期驻留、动态任务拆分和 worktree 隔离是后续扩展，而不是已实现能力。

## S15 集成 Harness

Lead 和专项审查员现已使用同一个模型工具循环：每轮组装工具与 MCP 状态，执行模型
返回的工具调用，经过宿主权限及 Hooks 后，把结果用正确的 tool call ID 返回下一轮。
Skills、记忆、会话 Todo、任务图和后台读取前置检查结果的任务都接入这条路径；模型工具调用写入审查 trace。
模型请求的重试只发生在推理边界，已完成的工具不会因模型重试而重新执行。

详细课程对照、验收和尚未实现的持久队友/worktree 等差异见 [S15 对照说明](docs/s15-harness.md)。

### MCP 接入

Harness 提供了 transport-agnostic 的 MCP 层：`MCPClient` 保存 server 返回的工具定义和
调用入口，`MCPManager.Connect` 负责连接与发现，`AssembleToolPool` 负责把工具以
`mcp__<server>__<tool>` 前缀加入现有 `ToolRegistry`。工具名会做规范化、64 字符长度检查和
冲突检查；MCP server 返回的 `readOnlyHint`/`destructiveHint` 只作为元数据，实际权限由宿主
`MCPHostPolicy` 决定，未配置的工具默认需要审批。

当前内置 `docs` 和 `deploy` 两个进程内 server，用于验证 `tools/list`、`tools/call` 和动态
工具池边界。可通过 `connect_mcp` 工具连接 server；工具参数会沿现有 Agent Loop 的
`ToolInput.Args` 传递，参数错误会作为 `MCP error` 工具结果返回，不会直接终止循环。真实
stdio/HTTP transport 可在不修改 Agent Loop 的情况下实现 `MCPServerFactory` 接入。

## S16 Workflow Runtime

模型可以通过一次 `Workflow` 工具调用启动宿主注册的可恢复编排。当前内置
`review-changes`，按 correctness、security、dependency 维度执行 audit → verify → summary；
生产环境的 Workflow 状态写入 MySQL `agent_runs`，事件写入 `workflow_events`，续跑时按稳定调用键复用已有结果，
不会把中间结果全部塞回主对话。

Workflow 支持 `agent`、`parallel`、`pipeline`、`phase`、`log` 和一层嵌套调用；结构化输出
失败会重试一次，工具完成后以 `<task_notification>` 唤醒当前模型会话。详细设计、恢复边界和
尚未实现的任意脚本/worktree 能力见 [S16 对照说明](docs/s16-workflow.md)。

## S17 Goal Stop Gate

审查请求可选传入 `goal`，把“最终交付应满足什么条件”交给会话级 Stop Gate。例如：

```json
{"diff":"...","goal":"完成审查并说明前置检查中未执行的项目"}
```

主模型不再调用工具时，Harness 不会因其一句“已完成”立即结束；独立的、无工具的 Goal
判断器只读取当前会话记录与目标条件，确认记录中已有实际证据后才放行。证据不足时，判断原因会
作为 `<goal_feedback>` 写回同一会话并自动继续下一轮；后台任务或 Workflow 尚未完成时，仍先等待
`<task_notification>`，不会提前判定。判断器标记目标无法完成、调用失败、达到 `GOAL_MAX_BLOCKS`
连续拦截上限或触及原有模型轮数上限时，任务会明确失败，目标不会被伪装成完成。

`GOAL_MAX_BLOCKS` 默认是 6；它只限制连续自动续轮，主循环仍受既有 `MaxRounds` 限制。

## 后台任务

后台任务将服务端注册的慢操作放到独立 Goroutine 中执行，创建后立刻返回 `bg_<id>`，
主请求无需等待。生产任务元数据保存在 MySQL `background_tasks`，状态包括 `pending`、
`running`、`completed`、`failed` 和 `cancelled`。服务重启后无法安全恢复原内存 Runner，
因此会将旧后台任务标记为 failed，并提供可消费一次的完成通知。审查服务会读取 Review Job
检查点，为可恢复的审查创建新后台任务并续跑。

审查请求自动使用后台 Runner，返回的 Job 中包含 `background_task_id`。为避免将服务变成
任意命令执行入口，HTTP API 不接受 shell command；后台执行只能由后端注册 Runner。

## 定时任务

定时任务只会调度已注册的代码审查 Runner，不能保存或执行任意 shell command。创建计划时必须
提供 `source`，服务在实际触发时抓取 diff；因此计划文件不保存粘贴的完整 diff 或审查上下文。

Cron 使用五字段格式：`分钟 小时 日期 月份 星期`，字段支持 `*`、`*/N`、数值、`N-M`、
以及逗号列表。调度器每秒轮询一次（可通过 `CRON_POLL_INTERVAL_MS` 调整），触发时先把
`pending_delivery` 与 `last_fired` 写入 `AGENT_CRON_FILE`，随后创建普通后台审查任务。
这提供至少一次投递：服务在标记后中断时会在启动后继续投递；不会补跑服务停机时错过的时刻。
若已有后台 Runner 正在执行，计划会保持待投递并在 Agent 空闲后再启动，避免并发审查争用上下文。

`durable: true` 的计划会保存在 `.cron-jobs.json` 并在重启后恢复，`durable: false` 的计划仅
保留在当前进程。一次性计划（`recurring: false`）在成功投递后删除；循环计划会保留最近的
`last_background_task_id`，可继续通过后台任务 API 查询执行结果。

## API

- `POST /api/reviews`：`{"source":"...","diff":"...","memory_query":"...","goal":"可检查的完成条件","budget_yuan":10}`；省略预算时使用 `REVIEW_BUDGET_YUAN`。
- `GET /api/reviews/:id`：查询任务、评论和 trace
- `GET /api/health`
- `GET /api/memories`：列出持久记忆
- `POST /api/memories`：保存一条 `persistent` 长期记忆
- `POST /api/tasks`：创建任务，字段为 `subject`、`description`
- `GET /api/tasks` / `GET /api/tasks/:id`：列出或读取任务
- `PATCH /api/tasks/:id/dependencies`：添加 `{"blocked_by":["task_..."]}`
- `POST /api/tasks/:id/claim` / `complete`：以 `{"owner":"agent"}` 认领或完成任务
- `GET /api/background-tasks` / `:id`：列出或读取后台任务
- `GET /api/background-tasks/notifications`：一次性收集已完成通知
- `POST /api/background-tasks/:id/cancel`：请求取消运行中的后台任务
- `POST /api/cron-jobs`：创建 `{"cron":"0 9 * * 1-5","source":"...","recurring":true,"durable":true}`
- `GET /api/cron-jobs` / `GET /api/cron-jobs/:id`：查看计划及最近投递的后台任务 ID
- `DELETE /api/cron-jobs/:id`：取消计划

## 目录结构

```text
cmd/server/              # 服务启动入口
internal/controller/     # Gin 路由、参数校验、HTTP 响应
internal/logic/          # 审查流程编排与业务规则
internal/dao/            # MySQL 持久化模型和 Repository
internal/model/          # 请求、任务、评论、trace 模型
.cron-jobs.json          # 可恢复的定时审查计划（自动忽略）
web/                     # 独立前端页面
```

生产服务要求 `PERSISTENCE_MODE=mysql` 和 `MYSQL_DSN`，不会回退到 `.checkpoints`、`.tasks`、
`.background-tasks` 或 `.team-mailboxes`。单元测试仍可使用文件适配器。开发时运行
`go run ./cmd/server`；根目录不再放置服务实现，配置、抓取和 LLM client 均归属于 `internal/logic`。
