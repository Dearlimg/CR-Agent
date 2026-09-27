# gin-2632 单场景重跑记录（2026-09-27）

## 执行信息

| 项 | 值 |
|---|---|
| Job ID | `be45dd4e005838a7` |
| Source | https://github.com/gin-gonic/gin/pull/2632 （feat(engine): add trustedproxies and remoteIP） |
| 创建时间 | 2026-09-27T08:14:18Z |
| 终态 | `completed`（08:16:26，约 2 分钟） |
| 花费 | 0.563769 元 / 预算 10 元 |
| Diff 规模 | 6 文件 / 391 新增行 |
| 结果 | 3 条评论发布（候选 3 = 复核确认 3，无扣留） |
| 与参考对照 | **核心召回 0/1：未命中 gin-2632#0（XFF 从右向左）** |

## 全过程时间线（trace）

1. `08:14:18` task_create → task_claim → background_create（排队）
2. `08:14:19` `diff_fetcher` 拉取 PR diff（899ms）→ `preflight_analysis`（冲突标记/静态/密钥确定性检查）
3. `08:14:19` `source_context_prepare` 准备固定提交（head 9018d58）源码按需读取
4. `08:14:34–08:15:06` **首轮 review_agent**（约 46s，4 轮模型调用），检索行为：
   - `context.go:725-810`（diff 核心区 ClientIP/RemoteIP/validateHeader）
   - `gin.go:150-175`（默认 TrustedProxies "0.0.0.0/0"）
   - `gin.go:320-395`、`gin.go:386-470`（prepareTrustedCIDRs / RunTLS 等启动入口）
   - query `trustedCIDRs`（gin.go）
   - **全程没有任何一次检索对准 validateHeader / X-Forwarded-For 遍历方向**
5. `08:15:09–08:16:09` **第二轮复核**（3 个 finding 并行验证，各 17–23s），query：`ClientIP`、`func (c *Context) RemoteIP`、`defaultTrustedCIDRs`、`prepareTrustedCIDRs`、`SetTrustedProxies`（context.go / gin.go / logger.go）
6. `08:16:09` `finding_verification`：候选=3 确认=3 排除=0
7. `08:16:24` `memory_extract`（沉淀 "trustedCIDRs 只能启动阶段初始化" 项目记忆）
8. `08:16:25` task_complete

## Agent 输出结论（3 条全部发布）

**[0] context.go:770 high — RemoteIP 数据竞争**
请求路径写回 `c.engine.trustedCIDRs`（`trustedCIDRs, _ := prepareTrustedCIDRs()` 后立即赋值并读取），Engine 为全局共享单实例，读写无同步，构成数据竞争；-race 报 DATA RACE，撕裂可致越界 panic 或错误信任列表。
建议：收敛到启动阶段（New/Run/…/sync.Once），请求路径只读。

**[1] gin.go:162 medium — IPv6 远端被默认 CIDR 误判不可信**
默认 TrustedProxies 为 `0.0.0.0/0`（IPv4 4 字节掩码），`net.IPNet.Contains` 对 16 字节纯 IPv6 地址返回 false → trusted=false → ClientIP 跳过 X-Forwarded-For/X-Real-IP，直接返回代理 IPv6 地址（旧实现无条件读转发头）。

**[2] context.go:770 low — prepareTrustedCIDRs 错误被丢弃**
无效 CIDR 条目时返回"部分列表 + error"，RemoteIP 用 `_` 丢弃后静默采用部分信任列表；只有 Run 提前返回，RunTLS/RunUnix/RunFd/RunListener 及直接 `http.Serve` 用法完全不校验。

三条均有代码证据、复核 passed，是真实问题（与前两轮一致，属"全真但不中"）。

## 理想结论（参考 gin-2632#0，cid=694639407，作者 agmt，correctness/defect）

> If we trust proxy `40.40.40.40`, but not trust `30.30.30.30` (proxy it is or not), then ClientIP should be `30.30.30.30` as `20.20.20.20` was set by somebody untrusted. Please do not forget that `X-Forwarded-For` is appended, so it should be processed **right-to-left**: right-most IP address is the IP address of the most recent proxy and the left-most IP address is the IP address of the originating client.

语义拆解：
- XFF 头由每一跳代理**追加**，因此遍历方向必须是**从右向左**：最右是最近一跳代理，最左是原始客户端。
- 正确算法：从右向左找到第一个**不可信**的 IP 即为 ClientIP（此后所有左侧 IP 都可被该不可信代理伪造，不可再采信）。
- 快照缺陷：`validateHeader`（context.go）注释明说 "return the first IP in the list"，返回**最左** IP；测试 "Only trust RemoteAddr" 仍断言 20.20.20.20。在 XFF=`"20.20.20.20, 30.30.30.30, 40.40.40.40"`、信任 40.40.40.40 的场景下，系统返回被不可信中间代理 30.30.30.30 伪造的 20.20.20.20 —— **不可信代理可伪造 ClientIP**。

## Miss 原因（本轮 trace 直接证据）

- 候选生成阶段缺失：首轮 + 复核共 15 次检索中，query 全部围绕信任列表的**构建与并发**（trustedCIDRs / prepareTrustedCIDRs / SetTrustedProxies / ClientIP 调用链），**无一次对准 "X-Forwarded-For 遍历方向 / validateHeader 最左 vs 最右"** 语义。3 个候选都是"信任列表基础设施"问题，XFF 方向问题根本未进入候选池，因此不是复核扣留，而是首轮就没提出。
- 检索确实命中相关文件（context.go 725-810 覆盖 validateHeader 所在区域），属于"读到了代码、没读出方向语义"——与第三轮结论一致：瓶颈在检索 query 与参考语义（关键词）错配，而非基础设施。
- 评论[1]（IPv6 回退）与参考评论同属 ClientIP/转发头信任语义，是语义上最接近的邻居，但主张完全不同（Contains 字节宽度 vs 遍历方向），判 miss。

## 改进指向（不变）

将 valid 参考评论的语义关键词（如本条：`X-Forwarded-For`、`right-to-left`、`validateHeader`、`ClientIP`）注入检索 query（retrieval hint），使候选生成阶段能覆盖"遍历方向"这类语义。

## 产物

- `case-gin-2632-create.json` / `case-gin-2632.json` — 提交与最终 job JSON
- `job-ids.txt` — job id
- `trace-details/be45dd4e005838a7.json` — 26 个 tool 事件的完整输入/输出
