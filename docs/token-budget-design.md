# 按人民币控制单次审查预算

## 常见做法

模型 API 通常在每次响应中返回输入与输出 token 用量；应用按模型单价折算费用，并把请求归属到用户、项目或一次 Agent 运行。输入、输出、缓存命中和批处理可能采用不同费率。OpenAI 同时提供按 token 的 Usage 数据和用于对账的 Costs 接口；Anthropic 也按模型和输入/输出类型计价。[OpenAI Usage 与 Costs](https://platform.openai.com/docs/api-reference/usage), [Anthropic 定价](https://docs.anthropic.com/en/docs/about-claude/pricing)

生产网关还会在发起请求前预留额度，收到实际用量后再结算，避免 Agent 循环只在事后发现超支。LiteLLM 的用户预算也以模型成本和 spend limit 管理。[LiteLLM budgets](https://docs.litellm.ai/docs/proxy/users)

## 本项目选择

- 用户以 `budget_yuan` 为一次 MR/PR 的总上限；省略时使用 `REVIEW_BUDGET_YUAN`，默认 ¥10。
- 每条模型请求记录模型名、输入/输出 token、费率快照和折算费用；重试、目标评估和 finding 复核共用同一个任务额度。
- 请求前按输入内容字节数加协议开销估算输入 token 上界，并为输出 `max_tokens` 预留费用；剩余额度不足时停止后续模型调用。成功响应后按 API 回报 token 核算；缺少 usage 的响应按已预留金额保守记账。
- 金额内部按百万分之一元保存，避免把低价模型的单次成本粗略取整到“分”。界面展示“已用 / 上限”，trace 保留每次请求的成本和费率。
- 默认模型改为当前 DeepSeek 文档推荐的 `deepseek-flash`。默认费率使用 DeepSeek 高峰时段、输入缓存未命中的价格：输入 ¥2 / 百万 token、输出 ¥8 / 百万 token。服务读取 `REVIEW_INPUT_PRICE_YUAN_PER_MILLION` 和 `REVIEW_OUTPUT_PRICE_YUAN_PER_MILLION`，价格调整时可配置更新。[DeepSeek 模型与定价](https://api-docs.deepseek.com/zh-cn/quick_start/pricing/), [模型更新日志](https://api-docs.deepseek.com/zh-cn/updates/)

当前费率刻意采用较高档位，因此预算计量是保守上界估算，不承诺和供应商账单逐分相等；缓存命中、低峰折扣和供应商价格变化可能令真实账单更低。供应商返回的 usage 是实际 token 数的依据；请求前的输入预算使用字节上界，是为了在没有单独 tokenizer 的情况下先限制最大生成量。

旧 `budget_cents` 请求字段仍作为兼容入口，值按人民币“分”转换；新调用应使用 `budget_yuan`。MySQL 增加微元存储列，读取旧记录时将旧分值换算为人民币，避免历史审查无法读取。
