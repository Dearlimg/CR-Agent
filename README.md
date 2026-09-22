# Code Review Agent

基于 harness 思路的 Code Review Agent 初版：模型/工具执行由服务编排，工具通过注册表声明式接入。

## 运行

```powershell
copy .env.example .env
go run ./cmd/server
```

打开 <http://localhost:8080>，输入 MR/PR 链接或粘贴 `git diff`。当前初版对 diff 进行脱敏和内置静态规则检查；链接抓取和 DeepSeek 适配器作为后续扩展点。

## 设计

- **可恢复**：每个任务写入 `.checkpoints/<id>.json`，状态含 queued/running/completed/failed。
- **可观测**：每条评论带 `trace_id`，任务返回工具、脱敏输入、输出和时间戳。
- **可扩展**：`ToolRegistry.Register("name", tool)` 声明式注册工具，不修改主流程。
- **预算**：请求支持 `budget_cents`，默认读取 `REVIEW_BUDGET_CENTS`。
- **安全**：不执行仓库代码；输入在 trace 前做敏感字段脱敏；配置只来自环境变量。
- **按需 Skills**：启动时仅扫描 `skills/*/SKILL.md` 的名称和描述；审查任务会记录并加载 `code-review` 的完整指令，再交给专项子 Agent 和汇总 Agent。

## Skills

内置的 [`skills/code-review/SKILL.md`](skills/code-review/SKILL.md) 约束审查范围、
严重度、置信度、输出字段和敏感信息处理。新增 skill 时创建
`skills/<name>/SKILL.md`，并在 YAML frontmatter 中写入唯一的 `name` 和用于目录的
`description`；完整正文不会进入启动目录。默认目录是 `skills`，可通过
`AGENT_SKILLS_DIR` 指向其他目录。

当前审查流程固定装载 `code-review`；每个任务的 `trace` 会产生一条
`load_skill` 事件，以便确认实际生效的 skill。

## API

- `POST /api/reviews`：`{"source":"...","diff":"...","budget_cents":1000}`
- `GET /api/reviews/:id`：查询任务、评论和 trace
- `GET /api/health`

## 目录结构

```text
cmd/server/              # 服务启动入口
internal/controller/     # Gin 路由、参数校验、HTTP 响应
internal/logic/          # 审查流程编排与业务规则
internal/dao/            # checkpoint / 任务存储，后续替换 MySQL、Redis
internal/model/          # 请求、任务、评论、trace 模型
web/                     # 独立前端页面
```

开发时运行 `go run ./cmd/server`。根目录不再放置服务实现，配置、抓取和 LLM client 均归属于 `internal/logic`。
