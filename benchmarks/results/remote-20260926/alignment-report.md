# CR-Agent Real-PR 评测：线上运行对齐报告

- 日期：2026-09-26
- 被测系统：CR-Agent 生产部署实例 `http://121.40.235.227/`（POST /api/reviews）
- 评测集：`benchmarks/real_pr_v1.json`（13 个真实 PR，49 条人工参考评论）
- 原始数据：`benchmarks/results/remote-20260926/<case>.json`（job 全量 JSON，含 trace）
- 本次运行的裁判：**自动化 LLM 裁判已完成**（DeepSeek 充值后运行，产物 `benchmarks/results/run-20260926T130214Z-realpr.{json,md}`），
  并与代理人工语义对齐交叉验证，两者结论一致（见第二节）。

## 一、运行结果总览

| Case | 场景 | 语言 | 服务状态 | 系统评论 | 复核漏斗（候选→确认/扣留/证据不足） |
|---|---|---|---|---|---|
| ts-57465 | feature | TS | completed | 0（1 条待确认摘要） | 2 → 0 确认 / 2 待核实 |
| node-41749 | feature | JS | completed | 0（1 条待确认摘要） | 1 → 0 / 1 |
| gin-2632 | feature | Go | completed | **2** | 2 → 2 / 0 |
| pandas-27237 | feature | Python | completed_with_warnings | 0（1 条待确认摘要） | 4 → 0 / 3+1 待定 |
| gin-2767 | bugfix | Go | completed_with_warnings | 0 | 3 → 0 / **3 证据不足** |
| pandas-34473 | bugfix | C | completed | **4** | 4 → 4 / 0 |
| redis-9323 | bugfix | C | completed_with_warnings | 0 | 3 → 0 / **3 证据不足**（证据匹配=0） |
| tokio-4652 | refactor | Rust | completed | 0（1 条待确认摘要） | 1 → 0 / 1 |
| pandas-22862 | refactor | Python | completed | **1**（+1 条待确认摘要） | 5 → 1 确认 / 1 排除 / 3 待核实 |
| tokio-6001 | performance | Rust | completed_with_warnings | 0 | 1 → 0 / **1 证据不足** |
| redis-14017 | performance | C | completed | 0（1 条待确认摘要） | 1 → 0 / 1 |
| gin-4224 | 阴性对照 | Go | completed | **0（正确）** | 模型未报告候选 |
| etcd-2009 | 阴性对照 | Go | completed | **0（正确）** | 模型未报告候选 |

漏斗合计（11 个非阴性 case）：**候选 26 → 确认发布 7（27%）/ 扣留待核实 11 / 判证据不足 7 / 排除 1**。

## 二、与人工参考评论的对齐（语义匹配，人工判定）

判定标准：同文件、±2 行邻域、同一根因；部分覆盖（同区域不同根因）不计为覆盖。

### 覆盖度（参考答案被系统覆盖的比例）

**严格判定 0/49（0%）；机器裁判计 3 条 partial（同函数域、不同根因，各计 0.5）后为 3.1%。**
分场景：feature 0/16，bugfix 0/15（partial 3），refactor 0/9，performance 0/9。
按维度：correctness 0/24，design 0/9，maintainability 0/8，performance 0/5，readability 0/1，testing 0/1，compatibility 0/1。

机器裁判的 3 条 partial 全部集中在 pandas-34473：系统对 `Object_getBigNumStringValue`
（objToJSON.c:2135/2136）未检查返回值/未回填长度的评论，与人类参考[2]（未 DECREF 导致 repr 泄漏）、
[3]（memcpy 先于 DECREF 的生命周期问题）、[4]（建议改用 PyUnicode_AsUTF8AndSize 传 outLen）位于同一函数、
同一调用点，但核心关切各不相同——典型"同一区域、未击中要害"。该配对质量分：accuracy 4/5，relevance 2/5，usefulness 2/5。

最接近的一条：node-41749 系统把 `require.main → process.argv[1]` 的守卫语义列为待核实疑点，与人类评审 targos
对 test/common/index.js 的质疑同源（该人类评论因锚定在上下文行未纳入参考集，不计入覆盖度）。

### 系统评论的准确性（人工逐条核实 + 机器裁判交叉验证）

**7 条已发布评论全部成立，无一条误报（precision 7/7，人工与机器判定一致）**，且全部锚定在 diff 新增行（valid-line rate 7/7）。

| # | 位置 | 内容摘要 | 与参考答案关系 |
|---|---|---|---|
| 1 | gin-2632 context.go:770 | RemoteIP 每请求写共享 engine.trustedCIDRs → 数据竞争 + 重复解析 | 参考未提（人类在讨论信任语义，未发现并发写） |
| 2 | gin-2632 gin.go:162 | 默认信任网段 0.0.0.0/0 为 IPv4-only，IPv6 下 XFF 被忽略、默认行为改变 | 参考未提（与参考[11]的 XFF 信任主题相邻但根因不同） |
| 3 | pandas-34473 objToJSON.c:2135 | Object_getBigNumStringValue 未检查 PyObject_Str/AsUTF8AndSize 返回值，未初始化 szlen 参与 memcpy | 参考未提（参考在同函数域讨论的是泄漏与 DECREF 顺序） |
| 4 | pandas-34473 test_ujson.py:566 | parametrize 参数被函数体首行覆盖，负大整数用例从未执行 | 参考未提 |
| 5 | pandas-34473 asv json.py:126 | 新增 benchmark frame 未登记 params，永不被测量 | 参考未提 |
| 6 | pandas-34473 whatsnew rst:1023 | RST role 与目标间有空格，文档链接失效 | 参考未提 |
| 7 | pandas-22862 test_constructors.py:866 | 测试断言上次构造的 result，object-ndarray 推断路径未被覆盖 | 参考未提 |

另有 11 条"有依据待核实"疑点按设计扣留为汇总评论（不计入 findings），其中 ts-57465 的
`forEachReturnStatement(undefined body)` 与参考[19]（weswigham 指出 getParameterCount/边界情形）问题域相邻。

### 阴性对照

2/2 正确保持安静，无过度评论 → 特异性 100%。

## 三、结论：系统产出与主流（人工）建议是否一致

**不一致，且差异是结构性的：**

1. **召回严重不足**：49 条人类实质性意见 0 覆盖；每 case 发布 0-4 条，而人类每 case 2-6 条。
2. **发布门槛过高（最大根因，有数据）**：26 个候选只有 7 个存活（27%）。两道闸门：
   - "证据匹配=0"（gin-2767、redis-9323、tokio-6001：模型候选的 evidence 没能被结构化匹配回 diff，整批判证据不足）；
   - "第二轮复核"将 11 条降级为扣留的待确认摘要。人类评审从不要求这种置信度——参考答案里大量是"指出问题域+给方向"的意见。
3. **评论风格/维度错配**：人类参考中 design/maintainability/readability 类占 18/49（API 兼容性、命名、文档、配置面）；
   系统按设计只发"可验证缺陷"，这类意见一条都不发。
4. **精度为正亮点**：发布的 7 条全部真实、可验证、锚定准确，阴性对照安静——误报控制达到设计目标。
5. **大 diff 行为异常**：redis-9323（82KB）证据匹配=0、tokio-6001 直接 0 候选，与"大 diff 截断"已知缺口吻合；
   4 个 case 以 completed_with_warnings 收尾。

## 四、改进方向（按优先级）

1. **降低发布门槛/分级发布**：把"证据不足"与"待核实"的候选以 `confidence=low` 附注发布（或可配置阈值），
   而非静默吞掉——本轮即少发 19/26 条。预期对召回的改善远大于任何模型侧优化。
2. **修"证据匹配"结构化回填**：gin-2767/redis-9323 的候选在验证前就被判证据不匹配（evidence 字段与 diff 脱敏后的文本对不上？），
   需要排查 evidence 归一化逻辑。
3. **扩充审查维度**：在 defect 之外引入 suggestion/design 类评论通道（API 兼容性、命名、文档缺口），对齐人类评审分布。
4. **大 diff 管线**：复核分片与 context 预算在 >50KB diff 上失效，需分块验证而非整体放弃。
5. **将本评测集纳入回归**：49 条参考 + 复核漏斗计数可直接作为每轮改动的验收基线。

## 五、可复现流程

```powershell
# 1. 提交（已执行的提交脚本见 job-ids.txt 中的 13 个 job）
# 2. 轮询并保存
curl.exe -s "http://121.40.235.227/api/reviews/<jobId>" > benchmarks\results\remote-20260926\<case>.json
# 3. 自动化裁判 → 生成 run-<ts>-realpr.{json,md}（6 参考的 case 需 4096 输出上限，2048 会截断 JSON）
go run ./cmd/benchmark --from-remote benchmarks/results/remote-20260926 --judge-max-tokens 4096
```

数据文件：原始 job JSON、复核漏斗、创建响应均在 `benchmarks/results/remote-20260926/`。
机器裁判产物：`benchmarks/results/run-20260926T130214Z-realpr.{json,md}`（最终版；T130018 为截断作废版）。
