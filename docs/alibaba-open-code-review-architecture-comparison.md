# 阿里 OpenCodeReview 架构调研与 CR-Agent 对比

> 调研日期：2026-09-24
> 上游范围：alibaba/open-code-review 的 README、架构文档与 ROADMAP（main 分支，页面可能继续更新）
> 本地范围：当前 CR-Agent 工作区 feat/version2 分支中的服务入口、审查编排、Harness、前置分析、finding 核验和持久化代码。本文只新增文档，没有改动审查实现。

## 结论摘要

阿里 OpenCodeReview 的核心不是“让一个大模型读完整个仓库”，而是把**确定性的审查流水线**和**有边界的 Agent**组合起来：宿主先确定 diff、过滤文件、装配规则，再按语义把文件分组；每组以独立会话并行审查；最后把模型评论重新定位到 diff、过滤错误评论并输出。

CR-Agent 当前则是一个服务型受限 Agent：HTTP 服务负责 Job 生命周期和固定前后置步骤，默认由一个 Review Agent 阅读脱敏后的完整 diff，Harness 提供受限工具循环；模型结果必须匹配新增行证据，并经过逐条独立复核后才能成为正式评论。代码中虽然有 Team 和 specialist 实现，默认审查入口目前仍调用 RunReviewAgent 单 Agent 路径。

**最值得借鉴的是大改动场景下的审查范围管理、可解释的预览/跳过原因，以及有预算上限的分组执行。**评论定位和过滤方面，CR-Agent 已有严格的新增行证据匹配与独立复核；不应为了复刻上游流程而放松这个门槛。两边的运行产品也不同：阿里项目以 CLI、IDE 和 CI 集成为中心，CR-Agent 当前以异步 HTTP 审查服务和通用 Agent Runtime 为中心，不需要整体照搬其产品拓扑。

## 1. OpenCodeReview 的架构

### 1.1 一条确定性的外层流水线

架构文档给出的主路径可以概括为：

**CLI 启动与配置 → Git diff → 文件筛选与规则注入 → 语义分组 → 并行组任务 → 评论处理 → 文本/JSON 输出**

其中模型主要负责语义分组、代码推理、评论重定位和评论过滤；diff 解析、路径筛选、Token 保护、行号映射、会话事件记录由程序负责。这种切分让模型专注在需要语义判断的部分，也让未被审查、无法定位或执行失败的情况可以明确表达。

### 1.2 输入与范围筛选

Diff Provider 支持三类 Git 输入：

- Workspace：暂存、未暂存和未跟踪文件。
- Commit：检查指定提交引入的变化。
- Range：检查两个引用之间的变化。

每个 diff 携带路径、状态、hunk、增删行数、二进制和重命名信息。随后文件依次经过二进制、用户排除、用户包含、扩展名支持、内置路径规则等筛选；删除文件和超大 diff 也会被单独处理。Preview 可以在不发起模型调用的情况下解释每个文件为何保留或排除。

规则会按文件路径匹配并合并到当前组的提示中。它是确定性地选择适用规则，再由模型按规则做语义判断；不能简单等同于对每个文件运行完整的静态分析器。

### 1.3 语义分组与并行

通过筛选的文件先进入一次分组模型调用。该调用只提供路径、文件状态和增删数量等元数据，不把源码 diff 交给分组模型。模型返回相关文件组，例如把 handler、service 和对应测试放在同一组。

分组外有三道保护：

- 每组最多 10 个文件，超出后拆分。
- 合并后的 diff 超出组预算时退回单文件组。
- 模型没有分配到的文件自动各自组成单文件组，避免静默漏审。

各组并行执行，使用各自的对话上下文。好处是大型变更可以提高吞吐量、控制单次上下文大小；代价是组间代码推理有限，跨组文件只能通过只读上下文工具查看，而且不能在未提供给该组的文件上发表评论。

### 1.4 单组内的 Agent 工作

每组可以先执行可选 Plan 阶段：模型基于只读工具生成检查清单；随后进入带工具的主审查循环。主循环有工具调用次数、连续无有效结果轮数、上下文预算和取消等停止条件。

Effort 预设控制每组的主审查轮数：low 一轮、medium 两轮、high 三轮。后续轮会带上早先已确认的评论，但不再注入 Plan；如果一轮没有增加新问题、评论数量到上限或总 Token 预算耗尽，就提前结束。这给了大型变更一个可调的召回与成本旋钮。

### 1.5 评论后处理

模型通过 code_comment 工具提交候选评论后，处理链会：

1. 在 diff 中匹配 existing_code，确定 start/end 行。
2. 无法匹配时，可调用模型尝试把评论重新定位到 diff。
3. 主循环完成后，再由 Review Filter 删除可证明错误的候选。
4. 在顶层对全部评论再做一次行号解析，处理跨文件或重定位更新后的评论。
5. 按文本或 JSON 格式输出。

行号无法解析时评论可以保留为未锚定状态（行号为 0），让下游使用者知道它需要人工定位。过滤器失败会记日志并忽略过滤错误，因此它是减误报的一层，而不是正式结果的唯一真实性门槛。

### 1.6 Token、上下文与失败边界

OpenCodeReview 会在多个层次做预算保护：单文件、组内和发送模型前的消息上下文都会检查；达到提示上限 80% 的组会跳过并以 warning 报告。上下文压缩在 60% 阈值启动异步任务，在 80% 阈值前同步完成；模型输入上限与输出 Token 上限分开设置。

组任务失败会被隔离，其他组仍可继续；失败会形成 warning，而不是让一个组拖垮整次审查。架构文档也明确承认语义组之间的跨文件推理有限，这是用范围和成本换取可控性的取舍。

### 1.7 会话与产品集成

本地审查会话以 JSONL 事件追加保存，包括提示、模型回复、工具调用和评论等；可通过 Viewer 查看。遥测记录流程、模型和工具调用信息，但架构文档说明 Prompt 和回复正文不会附到遥测中。

README 与 ROADMAP 还列出 CLI、VS Code、CI、多个模型供应商、MCP 等集成。这些是产品表面和扩展方式，不是单组审查流程本身的必要组件。

## 2. CR-Agent 当前实际路径

按当前工作区代码，默认审查过程是：

**HTTP 请求 → Review Job → 必要时抓取 GitHub/GitLab diff → 固定 preflight → 加载 Skill/召回记忆 → 一个 Review Agent + 受限 Harness → 新增行证据校验 → 逐条独立复核 → Job/Trace/评论落库**

值得区分的是，Harness 本身有通用工具循环、权限策略、Hooks、上下文压缩和 Workflow；这不等于主审查采用多个 reviewer。默认入口仍是一个 Agent 会话。

### 2.1 已有的确定性前置工作

Preflight 会解析变更文件和新增行、识别依赖文件、扫描原始 diff 新增行中的疑似密钥，并对送入模型的 diff 做脱敏。还会检查冲突标记、TODO/panic 提示、新建 Go 文件的语法和 gofmt。修改过的 Go 文件无法从 diff 单独完整检查时会标记未运行；这不是完整的构建或测试。

前置结果会随审查提示提供给 Agent。完整脱敏 diff 会一次提交到当前审查 Agent，而不是先按文件分组。Agent 可使用有限工具；审查路径默认最多 4 个工具轮、最多 2 个停滞轮，总模型轮次有上限。

### 2.2 证据核验是当前设计的强项

模型输出必须是结构化 finding。程序会确认文件、行号和 evidence 确实指向 diff 的新增行；如果报告行号错误，只有同一文件中证据文本唯一匹配时才会自动校正。缺少必要字段或证据不匹配的候选不会成为评论。

通过证据检查的候选会逐条进入第二次独立模型复核。对 GitHub PR，复核还可以在权限允许时查询固定 head 提交的源码上下文。复核为 inconclusive、未完成或证据不足时，Job 会以 completed_with_warnings 等状态表达不完整，而不是把候选当作已确认问题。

### 2.3 运行时与数据边界

CR-Agent 是面向服务端 Job、权限和运行时能力的架构：生产服务要求 MySQL；Trace 保存模型、工具、复核阶段和计量信息；另有 Skill、项目记忆、Workflow、任务和后台作业能力。完整 ReviewHarness 对话本身不是可跨进程恢复的审查 checkpoint，Workflow 的持久化恢复能力不能自动等同于整个 Review Job 可恢复。

这与 OpenCodeReview 的本地 CLI JSONL 会话记录用途不同。可借鉴事件边界、脱敏和阶段可观测性；不能把 JSONL 当成 CR-Agent MySQL Job 状态的直接替代。

## 3. 两者对比

| 维度 | 阿里 OpenCodeReview | 当前 CR-Agent | 对 CR-Agent 的启示 |
|---|---|---|---|
| 产品形态 | 本地 CLI 为主，兼顾 IDE/CI 等调用 | 异步 HTTP Review Service，外围是 Agent Runtime | 保留服务形态；只借鉴审查子流程 |
| Diff 输入 | 本地 Workspace、Commit、Range | 请求直接给 diff，或抓取 GitHub PR/GitLab MR | 若要支持本地/仓库模式，再独立增加 Provider |
| 审查范围 | 多层文件筛选、规则匹配、无模型 Preview | 有 diff 解析和轻量 preflight，但没有同类完整的文件选择/预览流水线 | 优先补“本次审查范围及跳过理由” |
| 文件上下文 | 先语义分组，组间并行、组内联合推理 | 全部脱敏 diff 进入一个 Agent 会话 | 大变更时可实验分组；小变更维持单 Agent 更简单 |
| 主 Agent | 每组可 Plan、工具循环，并按 effort 做多轮 review | 单 Review Agent + 有界 Harness 工具循环；格式异常可修复一次 | 可加高风险场景的可选复审轮次，不宜默认复制多轮 |
| 评论锚点 | 文本匹配、可选模型重定位、Review Filter、二次解析 | 新增行 evidence 强校验、唯一匹配修正、逐条独立 verifier | 保留 CR-Agent 的硬证据门槛；重定位只能辅助，不可替代验证 |
| 成本控制 | 文件/组/上下文多层 Token guard；effort 轮数 | Token 上下文压缩、单次输出限制、按请求预留费用的 Job 预算 | 将“哪些文件被跳过”与现有费用/Token 预算统一展示 |
| 故障隔离 | 子组失败时其他组继续，返回 warning | 主审查或逐条复核失败可标成 incomplete/warnings | 如果以后并行，沿用逐组失败隔离与显式不完整状态 |
| 持久化 | 本地 append-only JSONL 会话事件 | MySQL 保存 Job/运行时数据和 Trace；完整主对话不支持断点恢复 | 借鉴事件可诊断性；服务恢复需按 Job、checkpoint 和幂等调用独立设计 |
| 规则/知识 | 按文件路径组合规则文本 | code-review Skill、记忆召回和审查提示 | 可补项目/路径级审查规则，但由宿主确定加载范围 |

## 4. 建议借鉴顺序

### P0：先补可解释的范围控制

为输入 diff 建立显式的选择结果：每个文件记录保留或跳过、原因、增删行数和预算估算，并提供不调用模型的 Preview。大文件、二进制、生成文件和依赖锁文件可以按项目配置筛选；超预算时显式返回 warning 或拆分建议，不能静默丢弃。

这能直接增强现有 preflight，不改变 Review Agent 主循环。应将人工 include 与硬性安全规则区分开，不能让 include 绕过敏感数据或 Token 上限。

### P1：对大型变更试验语义分组与并发

先只对超过文件数、diff 字节数或 Token 阈值的 PR 启用。分组可读取文件名、状态、依赖和有限 diff 元数据；每组大小与并发数均有上限。必须保留覆盖率约束：未分配文件仍要单独审查，组超预算要继续拆分，任一组失败都在最终结果中可见。

首轮应做对照评测，观察多文件问题召回率、重复/冲突 finding、Token 与耗时。如果组间依赖导致召回下降，可提供跨组只读上下文工具；评论仍必须锚定到该组实际提供并最终验证的变更行。不要一开始就把所有审查切成小块，避免削弱 CR-Agent 当前可在一个上下文里跨文件推理的优势。

### P1：把逐条 verifier 改为有界并发的候选队列

当前每个候选 finding 按序复核。候选较多时可考虑固定大小并发池，先按严重程度调度，并保留每条 finding 独立的 verdict、trace 和不完整状态。共享预算预留、Job 写入、取消和速率限制需要先保证并发安全；不能因并行把预算超发或把部分失败误报成完整成功。

### P2：增加按风险选择的 review effort

借鉴 low/medium/high 的设计，为高风险 diff 提供额外复审轮次：前轮确认的问题作为已知项注入，后轮只找新增问题；没有新增问题时提前停止，并把总轮数纳入 Job 预算。建议先做成显式可选模式，等评测证明召回收益大于延迟、费用和误报成本后再考虑默认值。

### P2：项目/路径级规则配置

支持按路径加载小而明确的规则集合，例如安全敏感目录、数据库访问层和 API handler。规则作为审查上下文，不由模型自行决定是否生效；记录最终应用的规则版本，便于复现和分析规则是否导致误报。

## 5. 不建议直接照搬的部分

- 不需要把 CR-Agent 改成 CLI 主程序，也不需要把 CI、IDE 插件当作这次架构升级的前提。
- 不要为了“多 Agent”默认启用并行 reviewer。先看基准集上的召回、精度、成本和延迟。
- 不要把未锚定评论（如行号 0）作为 confirmed finding 返回。CR-Agent 已要求证据对应实际新增行，这是更合适的真实性门槛。
- 不要把 Review Filter 当成证据验证的替代品。模型过滤只能补充判断，不能证明文件和行内容确实存在。
- 不要把 diff 全量过滤掉却不返回可见原因。对跳过和超预算的文件，应给调用方留下 warning 和可追查结果。

## 6. 推荐目标结构

建议保持 CR-Agent 当前宿主控制的主流程，将 OpenCodeReview 的长处作为外围审查阶段逐步加入：

**Diff Provider → Scope Planner / Preview → Preflight 与脱敏 →（可选）Group Planner → 有预算的审查 Agent → 新增行证据验证 → 有界并行 verifier → 聚合与显式完成状态**

实施时每个阶段输出结构化产物并写入现有 Trace/Job；新增的 planner、分组和 effort 都从服务层或独立模块进入，不要让它们绕开 Harness 的权限、预算和证据边界。这样既能处理大型 PR，也能继续维持当前只读、可审计的 Review Agent 核心。

## 参考资料

### 上游

- [OpenCodeReview 仓库与 README](https://github.com/alibaba/open-code-review)
- [OpenCodeReview 架构文档](https://github.com/alibaba/open-code-review/blob/main/pages/src/content/docs/en/architecture.md)
- [OpenCodeReview ROADMAP](https://github.com/alibaba/open-code-review/blob/main/ROADMAP.md)

### 本地代码

- [审查服务与 Job 编排](../internal/logic/service.go)
- [确定性 preflight、diff 解析与脱敏](../internal/logic/preflight.go)
- [Review Agent 默认入口和结构化结果解析](../internal/logic/subagent.go)
- [服务级 Harness 工具与轮数限制](../internal/logic/harness_service.go)
- [证据匹配、逐条复核和最终评论](../internal/logic/finding_verification.go)
- [审查预算计量](../internal/logic/review_budget.go)
- [MySQL / 运行时持久化](../internal/dao/mysql.go)
