# 业务语义审查改造（2026-09-25）

## 已确认的代码问题

- 输出契约排除“只依赖 diff 外上下文”的候选，与首轮源码工具冲突；skill 又要求先有具体候选才查询，容易在理解阶段放弃跨文件问题。此为代码规则分析，尚不能断言是用户每次空报告的唯一原因。
- get_review_context 的无 file 搜索只覆盖已读取文件，原工具没有目录发现入口，未知调用方路径难以查证。
- 前置分析始终生成 syntax_check/format_check，即使未运行也在页面、Markdown 报告中展示。E2B 主路径已经不调用，但执行器、配置和镜像 Python SDK 仍保留。

## 公开一手资料及取舍

| 来源 | 借鉴 | 不采用/适用边界 |
| --- | --- | --- |
| [阿里 P3C 并发处理](https://github.com/alibaba/p3c/blob/master/p3c-gitbook/编程规约/并发处理.md) | 原子性、锁范围、有界线程资源 | Java 的具体 API 规则不能机械套用到 Go |
| [字节 CloudWeGo Kitex FAQ](https://www.cloudwego.io/docs/kitex/faq/) | 超时后的读写、request/response 复用与并发安全 | 公开项目实践，不冒充字节全公司内部手册 |
| [百度 FEX JavaScript 规范](https://github.com/fex-team/styleguide/blob/master/javascript.md) | 隐式转换、原型属性枚举造成的语义差异 | 团队规范中排版、命名及过时版本建议不作为缺陷标准 |
| [腾讯 Go 安全指南](https://github.com/Tencent/secguide/blob/main/Go安全指南.md) | 输入到危险 API 的数据流及具体安全实现 | 仍需确认入口可达和现有防护，不能仅因缺少某函数就报漏洞 |
| [obra/superpowers reviewer](https://github.com/obra/superpowers/blob/main/skills/requesting-code-review/code-reviewer.md) | 对照需求、集成语义，解释原因与影响 | 不引入泛化风格建议、强制多 Agent 或其整套流程 |
| [anthropics/claude-code review](https://github.com/anthropics/claude-code/blob/main/plugins/code-review/commands/code-review.md) | 独立复核、按根因去重 | 不采用只读 diff、忽略特定输入/状态、让模型查编译错误的限制 |

检索时 GitHub 页面显示 Superpowers 约 291.4k stars、Claude Code 约 148.0k stars；这是仓库热度而非单个 skill 的质量分数，数字随时间变化。上述内容为独立提炼，未复制整份模板。

## 实现

移除语法/格式分析及未使用的沙箱执行器、ZIP 下载器、配置、SDK 依赖；历史检查项在页面和导出中过滤。保留宿主的脱敏、路径、权限和证据定位规则。

首轮重点是业务不变量、失败路径、语言语义和适用的成熟方案。新增 directory 查询读取固定 head 的目录；读取指定行段无需无意义的 query。仍限制路径、响应体、输出长度和网络权限，无 file 的符号搜索明确只覆盖缓存。GitHub Contents API 每目录最多 1000 项，不宣称全仓库搜索。

首轮工具轮数从 4 调至 8，复核从 3 调至 6，以容纳目录发现、实现和调用方读取；任务总预算、超时和连续停滞限制仍生效。输出保持原 JSON 字段，解释字段从 80 字放宽到 300 字，以容纳成因、跨文件依据和推荐写法。二轮允许已查证的语言契约和静态路径推理，不要求沙箱复现。

## 验证边界

Go 测试验证工具权限、固定提交、路径安全、目录发现与源码读取，以及现有审查流水线的 confirmed/rejected/inconclusive 分流。前端测试覆盖旧报告隐藏及有效检查保留。这些确定性测试不等于真实模型召回率评测；尚未对线上历史 PR 做付费模型重放，不承诺每次必须产生 finding。
