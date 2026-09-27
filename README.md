# Code Review Agent

基于 harness 思路的代码审查 Agent：输入 GitHub PR、GitLab MR 链接或 `git diff`，执行"前置分析 → 模型审查与按需取证 → 证据校验 → 逐条复核 → 中文审查报告"的完整流程，支持 Markdown 导出，并已在远程部署环境（阿里云 ECS，Docker）完成真实 GitHub PR 评测。

**交付定位是可演示、可试用、可追溯问题的工程原型**：主要工程机制已实现，但审查质量尚未达到稳定覆盖关键缺陷的程度（核心参考召回 0/3，详见[评测](#评测)）。本文区分"功能存在"与"效果已验证"。

核心特性：

- **只读语义审查** — 不执行目标仓库代码，专注 CI 做不了的事：业务不变量、失败路径、语言语义、并发与安全
- **证据门禁 + 逐条复核** — 正式发现必须匹配 diff 新增行证据，每个候选经第二轮复核四态判定
- **确定性工作交给程序** — diff 解析、密钥扫描、证据匹配由代码保证；模型只提出风险假设并按需取证
- **可恢复** — 审查阶段、复核游标、预算预留持久化到 MySQL，服务重启后续跑
- **预算可控** — 按人民币计费，超限自动熔断
- **凭据脱敏** — 模型只见脱敏后的 diff，疑似密钥不出本地

## 目录

- [快速开始](#快速开始)
- [核心设计](#核心设计)
- [审查流水线](#审查流水线)
- [运行时机制](#运行时机制)
- [辅助系统](#辅助系统)
- [评测](#评测)
- [API](#api)
- [目录结构](#目录结构)
- [文档索引](#文档索引)

## 快速开始

```powershell
copy .env.example .env
go run ./cmd/server
```

打开 <http://localhost:8080>，输入 MR/PR 链接或粘贴 `git diff`。服务抓取链接中的 diff，执行前置检查，再将脱敏材料交给模型审查。

### 凭据与配置

凭据使用大写环境变量（如 `DEEPSEEK_API_KEY`、`GITHUB_TOKEN`），Windows、macOS、Linux 和容器统一一套配置名。进程环境变量优先；本地开发默认从当前工作目录加载 `.env`。从其他目录启动时，可设置 `CR_AGENT_ENV_FILE` 指向配置文件，例如 PowerShell：

```powershell
$env:CR_AGENT_ENV_FILE = "C:\cr-agent\.env"
& "C:\cr-agent\cr-agent.exe"
```

指定的 `CR_AGENT_ENV_FILE` 不存在或无法读取时，服务会在启动时返回具体错误。生产环境应由服务管理器或密钥管理系统注入环境变量；Docker 使用 `docker run --env-file /opt/cr-agent/.env ...` 注入容器进程，镜像不复制 `.env`。

常用配置（完整列表见 [`.env.example`](.env.example)）：

| 环境变量 | 默认值 | 说明 |
| --- | --- | --- |
| `DEEPSEEK_API_KEY` | — | 模型凭据（必填） |
| `GITHUB_TOKEN` | — | 抓取 PR diff 与源码上下文；未配置时匿名限流 60 次/小时/IP，会导致取证大面积失败 |
| `PERSISTENCE_MODE` / `MYSQL_DSN` | — | 生产必须为 `mysql` + 有效 DSN |
| `REVIEW_BUDGET_YUAN` | `10` | 单次审查预算上限（元） |
| `GOAL_MAX_BLOCKS` | `6` | Goal Stop Gate 连续拦截上限 |
| `AGENT_SKILLS_DIR` | `skills` | Skill 目录 |

## 核心设计

| 要求 | 已实现的能力 | 适用范围与边界 |
| --- | --- | --- |
| **可恢复** | MySQL 保存审查阶段、候选结果、逐条复核游标和预算状态；支持启动恢复与 `POST /api/reviews/{id}/resume` | 已持久化的阶段和复核结果可复用；模型请求完成但响应尚未落盘时，恢复可能重试该调用，不承诺外部调用恰好一次 |
| **可观测** | 评论关联 trace，记录工具输入输出、模型调用、`finish_reason`、耗时；支持 SSE 展示与日志导出 | 模型输入与日志遵守脱敏边界；trace 用于解释结果及定位失败，不能单独证明评论正确 |
| **可扩展** | 统一 `ToolMetadata` / `ToolDefinition`，通过定义、JSON Schema 与 handler 注册模型工具；宿主循环、ReviewHarness 和 MCP 共享同一套元数据，注册时编译并校验 Schema、权限和 handler | 常规模型工具可通过注册接入；涉及新权限或生命周期的能力仍需相应宿主支持 |
| **预算控制** | 请求支持 `budget_yuan`，调用前预留估算额度，返回后按模型价格与 token usage 结算 | 恢复时不重置预算；未结算预留采取保守记账，费用估算依赖模型定价配置与 usage 数据 |
| **置信度分级** | 逐条复核区分 `confirmed` / `plausible` / `rejected` / `inconclusive`，置信度与严重度分离 | 通过复核的发现进入正式结果；待确认疑点单独汇总，不作为已确认缺陷计分 |
| **安全** | 对疑似凭据进行扫描和脱敏，约束下载凭据适用范围、源码读取路径与大小；移除目标仓库代码执行路径 | 编译与测试交给项目 CI；规则扫描不能保证识别所有未知形式的敏感信息 |

- **预算**：按人民币控制每次审查的总模型费用。请求可传 `budget_yuan`，默认读取 `REVIEW_BUDGET_YUAN`（默认 ¥10）。每次模型请求按返回的输入/输出 token 和模型单价计费，并在请求前预留估算额度；超出上限后停止后续调用。方案和边界见 [`docs/token-budget-design.md`](docs/token-budget-design.md)。
- **按需 Skills**：启动时仅扫描 `skills/*/SKILL.md` 的名称和描述；审查任务会记录并加载 `code-review` 的完整指令。

## 审查流水线

### 1. 前置分析

每次审查只执行一次 `preflight_analysis`，由程序解析变更文件、新增行号、依赖文件、检查状态和疑似密钥位置。结构化结果直接提供给后续 Agent，模型工具池不再保留重复的 diff 解析、密钥扫描和格式检查工具。内存缓存仅保存不含原始代码和密钥值的解析结果，按来源、diff 摘要与分析器版本区分，30 分钟过期；原始 diff 的密钥扫描每次仍会执行。

前置分析只提供变更范围、冲突标记、静态提示及脱敏信息，不运行语法、格式、编译或测试检查。Trace 的 `origin` 区分编排调用和模型工具调用，`cache_hit` 表示解析结果复用。

### 2. 模型审查与按需取证

主审查由**单个 Agent** 完成（Eino ReAct + Harness 工具循环）。模型负责提出风险假设，并通过 `get_review_context` 按需读取固定 head 提交的目录与源码片段：查什么由模型决策，但读取范围、路径约束、脱敏和版本固定全部由宿主代码强制。新增行用于定位，完整论证可以引用已检索的跨文件源码。

Prompt 按 `PromptEnvelope` 分层组装：system 层只承载身份、证据信任边界与输出契约；diff、前置检查摘要、记忆作为任务数据进入 user 层；工具返回保持 tool 角色；仓库内容、工具结果与模型自写的计划永远不进 system 层。主审、复核和 JSON 修复使用各自的输入边界——格式修复只修格式，不补造判断和证据。实现说明见 [`docs/prompt-assembly.md`](docs/prompt-assembly.md)。

> 多 Agent 说明：早期版本默认运行 correctness / security / dependency 三个专项 Agent 的 Team 编排，因收益不明确、排障成本翻倍已主动收敛为单 Agent 主审查。Team 任务板与消息总线代码保留为扩展点（`internal/logic/team.go`、`subagent.go`），当前主流程不启用；Workflow 编排见下文。

### 3. 证据校验

正式行级发现必须提供真实代码证据并匹配 diff 新增行：模型行号报错时，仅在同文件中存在唯一证据匹配的情况下修正行号；多匹配或引用非新增行则拒绝该候选。缺少证据的候选不会静默丢弃，而是计入"证据不足"并反映到审查结论。

### 4. 逐条复核

每个候选由独立的复核调用给出结构化判定：verdict（`confirmed` / `plausible` / `rejected` / `inconclusive`）、reason、supporting_evidence、counterevidence、assumptions 与 confidence，要求给出支持证据、反证与待核实前提。判定为 `plausible`（有依据但前提待确认）的疑点不会静默丢弃：合并为一条不锚定具体代码行的待确认评论（`verification_status=second_pass_review_plausible`），写明致错机制、待核实前提与核实方法，与正式缺陷采用不同口径。

### 5. 结果状态

- `confirmed` 结论逐行发布为已核验评论；仅含待确认疑点时结论为 `completed_with_pending`，仍属成功完成
- 输出截断、解析失败、取证失败、复核未完成等情况保留明确状态（`completed_with_warnings` / `failed`），避免把"没有完成有效审查"显示成"没有问题"
- 评论为中文输出，解释包含成因、触发条件、影响与建议写法；支持导出 GitHub Flavored Markdown 报告，会话日志可单独导出 JSON（完整 trace）
- 界面支持 SSE 实时事件流；评论带 `trace_id` 可查看 trace 详情

## 运行时机制

### Harness 工具循环

审查 Agent 运行在统一的模型工具循环上：每轮组装工具与 MCP 状态，执行模型返回的工具调用，经过宿主权限及 Hooks 后，把结果用正确的 tool call ID 返回下一轮。Skills、记忆、会话 Todo、任务图和读取前置检查结果的任务都接入这条路径；模型工具调用写入审查 trace。模型请求的重试只发生在推理边界，已完成的工具不会因模型重试而重新执行。详细课程对照与验收差异见 [`docs/s15-harness.md`](docs/s15-harness.md)。

上下文压缩挂在这条循环上：消息接近上限时，服务依次执行大结果落盘并保留预览、旧上下文归档、超限时缩短已消费的 tool result，最后才调用模型生成事实型摘要——先可恢复地整理，后有损地压缩。落盘文件位于 `.task_outputs/tool-results/`，完整上下文位于 `.transcripts/`，均不纳入 Git 且写入前脱敏；每次压缩在 trace 中记录 `context_compact` 事件。阈值可通过 `CONTEXT_CHAR_LIMIT`、`TOOL_RESULT_BUDGET`、`LARGE_RESULT_CHAR_LIMIT` 与 `CONTEXT_MAX_MESSAGES` 配置。

#### MCP 接入

Harness 提供了 transport-agnostic 的 MCP 层：`MCPClient` 保存 server 返回的工具定义和调用入口，`MCPManager.Connect` 负责连接与发现，`AssembleToolPool` 负责把工具以 `mcp__<server>__<tool>` 前缀加入现有 `ToolRegistry`。工具名会做规范化、64 字符长度检查和冲突检查；MCP server 返回的 `readOnlyHint`/`destructiveHint` 只作为元数据，实际权限由宿主 `MCPHostPolicy` 决定，未配置的工具默认需要审批。

当前内置 `docs` 和 `deploy` 两个进程内 server，用于验证 `tools/list`、`tools/call` 和动态工具池边界。可通过 `connect_mcp` 工具连接 server；参数错误会作为 `MCP error` 工具结果返回，不会直接终止循环。真实 stdio/HTTP transport 可在不修改 Agent Loop 的情况下实现 `MCPServerFactory` 接入。

### Goal Stop Gate

审查请求可选传入 `goal`，把"最终交付应满足什么条件"交给会话级 Stop Gate：

```json
{"diff":"...","goal":"完成审查并说明前置检查中未执行的项目"}
```

主模型不再调用工具时，Harness 不会因其一句"已完成"立即结束；独立的、无工具的 Goal 判断器只读取当前会话记录与目标条件，确认记录中已有实际证据后才放行。证据不足时，判断原因会作为 `<goal_feedback>` 写回同一会话并自动继续下一轮；后台任务或 Workflow 尚未完成时，仍先等待 `<task_notification>`，不会提前判定。判断器标记目标无法完成、调用失败、达到 `GOAL_MAX_BLOCKS` 连续拦截上限或触及原有模型轮数上限时，任务会明确失败，目标不会被伪装成完成。

`GOAL_MAX_BLOCKS` 默认是 6；它只限制连续自动续轮，主循环仍受既有 `MaxRounds` 限制。

### Workflow Runtime

主审查默认由单个 Agent 完成；模型也可以通过一次 `Workflow` 工具调用启动宿主注册的可恢复编排。当前内置 `review-changes`，按 correctness、security、dependency 维度执行 audit → verify → summary；生产环境的 Workflow 状态写入 MySQL `agent_runs`，事件写入 `workflow_events`，续跑时按稳定调用键复用已有结果，不会把中间结果全部塞回主对话。

Workflow 支持 `agent`、`parallel`、`pipeline`、`phase`、`log` 和一层嵌套调用；结构化输出失败会重试一次，工具完成后以 `<task_notification>` 唤醒当前模型会话。详细设计、恢复边界和尚未实现的任意脚本/worktree 能力见 [`docs/s16-workflow.md`](docs/s16-workflow.md)。

### 中断恢复

生产服务启动后会自动续跑中断的 `queued`/`running` 审查。对超时、预算不足或部分复核未完成的终态任务，可调用 `POST /api/reviews/{id}/resume` 从已保存的阶段恢复；正在由其他实例执行的 Job 返回 `409`。已完成的逐条复核会跳过，未完成的 finding 从游标位置继续。模型请求前写入预算预留，恢复时将无法确认结果的预留保守计入已花费预算。外部模型请求与数据库不能构成一个事务，因此崩溃恰好发生在模型返回之后时，当前请求可能重试；已持久化的阶段结果和 finding verdict 不会重跑。详见 [`docs/review-recovery.md`](docs/review-recovery.md)。

### 后台任务

后台任务将服务端注册的慢操作放到独立 Goroutine 中执行，创建后立刻返回 `bg_<id>`，主请求无需等待。生产任务元数据保存在 MySQL `background_tasks`，状态包括 `pending`、`running`、`completed`、`failed` 和 `cancelled`。服务重启后无法安全恢复原内存 Runner，因此会将旧后台任务标记为 failed，并提供可消费一次的完成通知。审查服务会读取 Review Job 检查点，为可恢复的审查创建新后台任务并续跑。

审查请求自动使用后台 Runner，返回的 Job 中包含 `background_task_id`。为避免将服务变成任意命令执行入口，HTTP API 不接受 shell command；后台执行只能由后端注册 Runner。

### 定时任务

定时任务只会调度已注册的代码审查 Runner，不能保存或执行任意 shell command。创建计划时必须提供 `source`，服务在实际触发时抓取 diff；因此计划文件不保存粘贴的完整 diff 或审查上下文。

Cron 使用五字段格式：`分钟 小时 日期 月份 星期`，字段支持 `*`、`*/N`、数值、`N-M` 以及逗号列表。调度器每秒轮询一次（`CRON_POLL_INTERVAL_MS` 可调），触发时先把 `pending_delivery` 与 `last_fired` 写入 `AGENT_CRON_FILE`，随后创建普通后台审查任务。这提供至少一次投递：服务在标记后中断时会在启动后继续投递；不会补跑服务停机时错过的时刻。若已有后台 Runner 正在执行，计划会保持待投递并在 Agent 空闲后再启动，避免并发审查争用上下文。

`durable: true` 的计划会保存在 `.cron-jobs.json` 并在重启后恢复，`durable: false` 的计划仅保留在当前进程。一次性计划（`recurring: false`）在成功投递后删除；循环计划会保留最近的 `last_background_task_id`，可继续通过后台任务 API 查询执行结果。

## 辅助系统

### Skills

内置的 [`skills/code-review/SKILL.md`](skills/code-review/SKILL.md) 约束审查范围、严重度、置信度、输出字段和敏感信息处理。新增 skill 时创建 `skills/<name>/SKILL.md`，并在 YAML frontmatter 中写入唯一的 `name` 和用于目录的 `description`；完整正文不会进入启动目录。默认目录是 `skills`，可通过 `AGENT_SKILLS_DIR` 指向其他目录。

当前审查流程固定装载 `code-review`；每个任务的 `trace` 会产生一条 `load_skill` 事件，以便确认实际生效的 skill。

### 持久记忆

Memory 用于跨审查任务保留可复用知识，不保存完整 transcript。每条记录单独保存在 `.memory/<name>.md`，`.memory/MEMORY.md` 只保留索引。新审查任务会按照 `memory_query`（未提供时使用来源和 diff）的关键词召回至多 `MEMORY_MAX_RECALL` 条、总计不超过 `MEMORY_MAX_CHARS` 的相关记忆；提示词明确将它们视为背景，当前 diff 与当前请求优先。

可通过 `POST /api/memories` 显式保存经过确认的长期偏好或项目约束：

```json
{"name":"go-error-style","description":"项目要求包装错误","type":"project","body":"Go 错误应保留上下文并使用 %w 包装。","scope":"persistent"}
```

`GET /api/memories` 查看索引内容。自动提取只在模型可用时运行，且候选必须包含 `persistent` scope、通过临时性与敏感信息检查、并且不与已有记录重复。达到 `MEMORY_CONSOLIDATE_AT` 后只会清理高度相似的重复记录，不会把完整对话写入记忆。

### 任务系统

Todo 是单次审查的执行清单；Task 是跨会话保留的任务图。生产环境使用 `agent_tasks` 和 `agent_task_dependencies` 保存，包含 `subject`、`description`、`status`、`owner` 和依赖关系。状态只能按 `pending → in_progress → completed` 转换：认领时检查所有依赖已完成，完成时返回刚被解锁的下游任务；添加依赖会拒绝自依赖、缺失任务和环。

每个 `POST /api/reviews` 会自动创建并认领一个 Lead（`review-agent`）任务，返回的审查 Job 包含 `task_id`，审查结束后任务自动完成。未完整审查会在 Job 上体现为 `completed_with_warnings` 或 `failed`；检查点尚未到 `completed` 时，Lead 任务保持 `in_progress`，可通过恢复入口继续执行。

模型调用对 EOF、连接中断、超时、限流和 5xx 做有限指数退避重试。

## 评测

项目自带真实 PR 评测集 [`benchmarks/real_pr_v1.json`](benchmarks/real_pr_v1.json)：13 个真实 GitHub PR（含 2 个阴性对照）、49 条人工参考评论，每条锚点必须通过"落在最终 diff 新增行"的机械校验，diff 快照固定在仓库内。评分入口为 `cmd/benchmark`（DeepSeek 机器裁判），采用分通道评分：快照中仍有效的 `valid` 参考计入核心召回、`fixed_in_snapshot` 重报计误报、`suggestion` 单独评分。

经版本对齐审核，49 条参考中仅 3 条在当前快照中仍然有效（33 条针对已修复的历史问题，13 条为建议）。**三轮真实 PR 评测的核心参考召回均为 0/3**；第三轮中系统发现了参考集之外的问题（如 Gin 共享字段并发写、IPv6 默认信任范围），其独立正确性仍需额外评审确认。评测方法、三轮结果与统计边界详见 [`to_Interviewer.md`](to_Interviewer.md) 与 [`benchmarks/README.md`](benchmarks/README.md)。

## API

- `POST /api/reviews`：`{"source":"...","diff":"...","memory_query":"...","goal":"可检查的完成条件","budget_yuan":10}`；省略预算时使用 `REVIEW_BUDGET_YUAN`
- `POST /api/reviews/{id}/resume`：从检查点恢复终态审查任务
- `GET /api/reviews/:id`：查询任务、评论和 trace
- `GET /api/reviews/:id/events`：SSE 实时事件流
- `GET /api/reviews/:id/traces/:traceID`：单条 trace 详情
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
cmd/benchmark/           # 评测集采集与机器裁判评分入口
internal/controller/     # Gin 路由、参数校验、HTTP 响应
internal/logic/          # 审查流程编排、配置、抓取与 LLM client
internal/dao/            # MySQL 持久化模型和 Repository
internal/model/          # 请求、任务、评论、trace 模型
skills/                  # 按需装载的审查指令（SKILL.md）
web/                     # 独立前端页面
benchmarks/              # 真实 PR 评测集、结果与脚本
.cron-jobs.json          # 可恢复的定时审查计划（自动忽略）
```

生产服务要求 `PERSISTENCE_MODE=mysql` 和 `MYSQL_DSN`，不会回退到 `.checkpoints`、`.tasks`、`.background-tasks` 或 `.team-mailboxes`。单元测试仍可使用文件适配器。开发时运行 `go run ./cmd/server`；根目录不再放置服务实现。

## 文档索引

- [`docs/token-budget-design.md`](docs/token-budget-design.md) — 预算计费方案与边界
- [`docs/review-recovery.md`](docs/review-recovery.md) — 检查点与中断恢复
- [`docs/prompt-assembly.md`](docs/prompt-assembly.md) — PromptEnvelope 分层组装规则
- [`docs/s15-harness.md`](docs/s15-harness.md) — Harness 工具循环的课程对照与验收
- [`docs/s16-workflow.md`](docs/s16-workflow.md) — Workflow Runtime 设计与恢复边界
- [`docs/review-verification-research.md`](docs/review-verification-research.md) — 复核判定研究
- [`docs/semantic-review-research.md`](docs/semantic-review-research.md) — 只读语义审查取舍
- [`docs/agent-architecture-research.md`](docs/agent-architecture-research.md) — Agent 架构调研与代码入口索引
- [`docs/alibaba-open-code-review-architecture-comparison.md`](docs/alibaba-open-code-review-architecture-comparison.md) — 与 OpenCodeReview 架构对比
- [`to_Interviewer.md`](to_Interviewer.md) / [`to_Interviewer_v2.md`](to_Interviewer_v2.md) — 开发复盘、评测结果与已知不足
