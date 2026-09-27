# Real-PR Benchmark 第四轮报告（Round 4，2026-09-27）

**定位**：第四轮不是扩大覆盖，而是两件事——①对 gin-2632 / node-41749 做单 case 独立重验，验证第三轮结论的稳定性；②用 trace + 生产库同 case 双跑，把召回 miss 的根因精确定位到流水线阶段。

**数据源**：
- 第四轮 job：gin-2632 `be45dd4e005838a7`、node-41749 `46cedc58a0cdd0cb`（单 case 详录见同层 `remote-20260927-gin2632/REPORT.md`、`remote-20260927-node41749/REPORT.md`）
- 交叉验证：生产库 cr_agent（aliyun /opt/cr-agent，MySQL 宿主端口 3307）最近 10 条 `review_jobs` + `trace_events` 5473 条 + `tool_calls` 3287 条（2026-09-26 23:10 ~ 09-27 16:30）
- 前三轮：`remote-20260926`（v1）、`remote-20260926-2246`（v2）、`remote-20260927-1051`（v3）

---

## 一、结论速览

1. **核心召回累计 0/3，四轮未变**。本轮重验 gin + node 两个 case（各含 1 条 valid 参考），双双 miss；pandas 本轮未重跑，全集核心召回分母仍为 3。
2. **精度保持满格**：本轮发布 5 条（gin 3 + node 2）全部复核 passed、全部有代码依据，零 fixed_in_snapshot 误报。
3. **复核零扣留**：gin 候选 3=确认 3，node 候选 2=确认 2。两轮 miss 均发生在**候选生成阶段**（首轮检索/注意力分配），不是复核扣留——根因定位从 v3 的"检索语义错配"收窄到具体阶段与两种 miss 模式。
4. **输出稳定性经生产库双跑验证**：gin 两次独立审查 3 条意见完全一致；node 核心发现（undici SRI 桩）两次独立命中，仅 severity 漂移（medium→high），长尾发现存在随机性。
5. **基础设施全绿**：v2 的 403×27、源码读取失败×82 在 v3/v4 均为 0，改进空间已不在基础设施。

---

## 二、本轮执行结果

| 项 | gin-2632 | node-41749 |
|---|---|---|
| Job ID | `be45dd4e005838a7` | `46cedc58a0cdd0cb` |
| 终态 | completed（2m07s） | completed（2m30s） |
| 成本 | 0.56 元 | 3.58 元 |
| Diff 规模 | 6 文件 / 391 新增行 | 16 文件 / 8076 新增行 |
| 发布意见 | 3 条（high 1 / medium 1 / low 1），全 passed | 2 条（high 1 / low 1），全 passed |
| 对 valid 参考 | miss（gin-2632#0 XFF 从右向左） | miss（node-41749#1 test-fetch.mjs keep-alive） |

发布的 5 条意见：

- **gin#2632**：① `RemoteIP` 请求路径写共享 `engine.trustedCIDRs` → 数据竞争（high）；② 默认 TrustedProxies 仅 IPv4 网段，纯 IPv6 远端 `Contains` 判 false → ClientIP 行为回退（medium）；③ `prepareTrustedCIDRs` error 被丢弃，部分信任列表静默生效（low）。
- **node#41749**：① `matchRequestIntegrity` 无条件 `return false`，SRI 校验是必然失败的桩（high）；② `requestBadPort` 正则 `/^http?s/` 不匹配 `http:`，明文 bad-port 拦截失效（low）。

---

## 三、同 case 双跑一致性（生产库交叉验证）

生产库里本轮两个 case 恰好各有两次独立审查（v3 10:51 + v4 16:14/16:27 UTC+8），构成天然的稳定性 A/B：

| Case | v3 job | v4 job | 一致性 |
|---|---|---|---|
| gin-2632 | `90f26f85`（0.75 元 / 2m50s / 22 次调用） | `be45dd4e`（0.56 元 / 2m07s / 19 次） | **3 条意见内容与 severity 完全一致**（1高/1中/1低） |
| node-41749 | `6d0692d5`（4.04 元 / 4m59s / 24 次） | `46cedc58`（3.58 元 / 2m30s / 18 次） | SRI 桩两次命中（severity medium→high 漂移）；第二条不同：v3 为 consumeStart 时序疑点（plausible 挂起），v4 为 bad-port 正则（passed 发布） |
| pandas-22862 | `905a33b7`（3.02 元 / 7m37s / 45 次，with_pending） | 未重跑 | — |

结论：**核心发现稳定复现，长尾发现随机**。对 benchmark 的含义是单次运行即可可靠度量"命中了什么"，但"没命中什么"需要排除长尾涨落的影响（v3 node 的第二条与 v4 不同，均与 keep-alive 参考无关，不影响 miss 判定）。

---

## 四、召回 miss 根因（trace 证据链）

两个 case 呈现两种不同的 miss 模式，共同点是**候选生成阶段缺失**（复核零扣留，问题根本没进候选池）：

### 模式 A：读到相关代码，没读出语义方向（gin-2632）

- 首轮 + 复核共 15 次检索，query 全部围绕信任列表的**构建与并发**（`trustedCIDRs` / `prepareTrustedCIDRs` / `SetTrustedProxies` / ClientIP 调用链），**无一命中"X-Forwarded-For 遍历方向 / validateHeader 最左 vs 最右"语义**。
- 检索区间 `context.go:725-810` 实际覆盖 validateHeader 所在区域——代码就在眼前，语义方向没有进入假设空间。
- 最接近的邻居是意见②（IPv6 回退，同属 ClientIP 信任语义），但主张完全不同（字节宽度 vs 遍历方向），判 miss。

### 模式 B：相关文件根本没读（node-41749）

- 首轮 19 次检索 0 次指向 `test/` 目录；`test-fetch.mjs` 是 diff 16 个文件之一，被实现文件（undici.js）完全吸走注意力预算。
- 讽刺点：preflight 的 static_check 已报出 1 处新增 TODO，内容恰好就是 keep-alive 相关（"Remove this once keep-alive behavior can be disabled from the client side"）——线索递到门口，首轮检索没有跟进。

### 定位链收敛（v1→v4）

| 轮次 | 表层根因 | 定位层级 |
|---|---|---|
| v1 | 发布门槛过高、大 diff 截断、评论维度错配（人类 18/49 非 defect，系统只发 defect） | 评测/发布策略 |
| v2 | HTTP 403 ×27 + 固定提交源码读取失败 ×82（无 GITHUB_TOKEN） | 基础设施 |
| v3 | 检索命中相关文件但 query 词与参考语义错配 | 首轮检索 |
| v4 | 候选生成阶段缺失：gin=语义方向未进入假设空间；node=测试文件级漏读 | 首轮检索/注意力分配 |

---

## 五、四轮指标总表

| 轮次 | run 目录 | 范围 | 核心召回 | 精度 | 发布意见 | 状态 |
|---|---|---|---|---|---|---|
| v1 | remote-20260926 | 13 case 全量 | 0/3 | 7/7 | 7（全真） | 基线建立 |
| v2 | remote-20260926-2246 | 13 case 重跑 | 0/3 | 8/8 | 15（全真），阴性 2/2 安静 | 基础设施退化（token 缺失） |
| v3 | remote-20260927-1051 | 3 valid case | 0/3 | 无 fixed 误报 | 6（4 passed + 2 plausible） | 基础设施收敛 |
| v4 | remote-20260927-gin2632 / -node41749 | 2 case 重验 | 0/2（累计 0/3） | 5/5 | 5（全 passed） | 根因定位到阶段 |

---

## 六、成本与效率

- 本轮两个 job 合计 **4.14 元**；同 case 与 v3 合计重复花费：gin 1.31 元、node 7.62 元。
- 生产库 10 条记录全景：模型统一 deepseek-flash，共 208 次 LLM 调用、input ≈566 万 tok / output ≈31 万 tok、trace 计成本 **≈14.50 元**；100% completed、零重试。
- 流水线耗时结构（trace_events 还原）：diff_fetcher → preflight_analysis → load_skill → memory_recall → source_context_prepare → **review_agent 主审查（占 70-82%，瓶颈）** → 逐 finding 二轮复核 → memory_extract → task_complete。
- no_findings 任务被预检有效分流（etcd#2009 0.21 元/41s，gin#4224 0.02 元/10s），未让小 diff 烧大钱。
- 工程侧三个待修：① `spent_cents` 全 0，trace cost_micros 未回写，预算扣费断链；② 同 PR 重复审查无去重（#2556 当天 3 次、#2632/#41749 各 2 次）；③ `agent_runs` 空表造成表名误导（真实执行记录在 `review_jobs`）。

---

## 七、改进指向（按优先级）

1. **retrieval hint**：把 valid 参考评论的语义关键词注入首轮检索 query（gin：`X-Forwarded-For`、`right-to-left`、`validateHeader`；node：`keep-alive`、`server.close`、`test-fetch.mjs`）。直接对准 v4 定位的"候选生成缺失"。
2. **测试文件检索种子**：diff 含 test 文件时保证至少一轮检索覆盖；preflight static_check 的 TODO hint 应作为检索种子跟进（node case 的现成线索）。
3. **>1MiB 文件分段读**：v3 遗留的 pandas whatsnew 挡读，唯一剩余基础设施项。
4. **工程面**：trace 成本回写 `spent_cents`；按 input_hash 短周期去重同 PR 重复审查；处置 `agent_runs` 空表。

---

## 八、一句话总结

> 四轮把召回瓶颈的定位链走完了：基础设施问题清零后，miss 不在复核、不在证据不足，而在**候选生成阶段**——gin 输在语义方向没进假设空间，node 输在测试文件没被读；agent 输出本身高度稳定（gin 双跑逐条一致），精度四轮保持满格，下一步的杠杆点明确且唯一：检索/注意力分配，而非审查或复核能力。
