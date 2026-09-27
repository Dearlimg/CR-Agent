# node-41749 单场景重跑记录（2026-09-27）

## 执行信息

| 项 | 值 |
|---|---|
| Job ID | `46cedc58a0cdd0cb` |
| Source | https://github.com/nodejs/node/pull/41749 （lib: add fetch） |
| 创建时间 | 2026-09-27T08:27:57Z |
| 终态 | `completed`（08:30:38，约 2.5 分钟） |
| 花费 | 3.576923 元 / 预算 10 元 |
| Diff 规模 | 16 文件 / 8076 新增行（diff 8320 行） |
| 结果 | 2 条评论发布（候选 2 = 复核确认 2，无扣留） |
| 与参考对照 | **核心召回 0/1：未命中 node-41749#1（test-fetch.mjs keep-alive 挂起）** |

## 全过程时间线（trace）

1. `08:27:57` task_create → task_claim → background_create（排队）
2. `08:27:58` `diff_fetcher` 拉取 PR diff（1.7s）→ `preflight_analysis`（414ms；确定性检查：冲突标记=0，static_check **hint=1（新增行 TODO 提示）**，密钥命中=2）
3. `08:28:00` `source_context_prepare` 准备固定提交（head 457b184）源码按需读取
4. `08:28:12–08:29:44` **首轮 review_agent**（约 104s，9 轮模型调用），19 次检索全部对准实现文件：
   - `deps/undici/undici.js`：`matchRequestIntegrity`（×2）、`integrity mismatch`、`requestBadPort`、`request-body-header`、行区间 1000-1020 / 1285-1300 / 5544-5556 / 7240-7266
   - `lib/internal/util.js`：`exposeInterface`（×3）、`defineOperation`
   - `lib/internal/options.js`：`getOptionValue`
   - `lib/internal/bootstrap/pre_execution.js:1-60`、`does_not_own_process_state.js` query `undici`、`lib/internal/deps` 目录列举
   - `tools/doc/type-parser.mjs`：`customTypesMap`
   - **19 次检索中 0 次涉及 `test/` 目录或 `test-fetch.mjs`**
5. `08:29:39` `parse_review_findings_json`：产出 2 个候选
6. `08:29:52–08:30:18` **第二轮复核**（2 个 finding 并行验证），query：`matchRequestIntegrity`、`integrity`、`requestBadPort`、`badPorts`（全部仍在 undici.js）
7. `08:30:18` `finding_verification`：候选=2 确认=2 排除=0
8. `08:30:26` `memory_extract`（产出为空 `[]`）
9. `08:30:27` task_complete

## Agent 输出结论（2 条全部发布）

**[0] deps/undici/undici.js:5548 high — SRI 校验是无条件失败的桩**
新增内置 fetch 中 `matchRequestIntegrity` 无条件 `return false`；请求带 `integrity` 选项时必然走 `processBodyError('integrity mismatch')`，哈希正确的资源也会失败。（复核确认：Request 构造器 6431-6433 写入 integrity，mainFetch 7239 分支必经。）

**[1] deps/undici/undici.js:5436 low — requestBadPort 正则失效**
`/^http?s/` 中 `?` 只作用于 `p`，实际匹配 `https:` 与 `htts:` 而不匹配 `http:`，bad-port 拦截对明文 http 请求完全失效，与 Fetch 规范不符。

两条均有代码证据、复核 passed，是真实问题。

## 理想结论（参考 node-41749#1，cid=795146943，作者 targos，correctness/defect）

> All these lines run very fast, but then on my computer the test hangs a few seconds before it exits. Could it have to do with keep-alive behavior? Can we override it?

针对 diff 中的新文件 `test/parallel/test-fetch.mjs`（+32 行），语义拆解：
- 测试结尾只调 `server.close()`；`server.close()` 只等待**已有连接**结束，不主动断开客户端 keep-alive 连接。
- undici 全局 dispatcher 默认启用 keep-alive，fetch 完成后连接池中的连接保活不关，测试进程须等 keep-alive 超时（约 4s）才能退出 → **CI 每次跑都白挂数秒**。
- 作者自己也知道：diff 里写了服务端 workaround `res.setHeader('Keep-Alive', 'timeout=0, max=0')` 并留 TODO "Remove this once keep-alive behavior can be disabled from the client side"——参考评论正是指向这个"客户端侧无法 override keep-alive"的能力缺口。
- audit_note 确认：快照中测试仍只调 `server.close()`（test-fetch.mjs:32），问题在快照中仍成立。

## Miss 原因（本轮 trace 直接证据）

- **文件级漏读**：`test-fetch.mjs` 是 diff 中的新文件（16 文件之一），但首轮 19 次检索 0 次指向 `test/` 目录；2 条候选全部是 undici 实现内部的规范符合性问题。问题根本没进候选池（复核 2/2 passed，无扣留）。
- 注意力分配偏差：8076 行新增的注意力预算几乎全部给了 `deps/undici/undici.js`（实现主体）与 bootstrap 装配机制，测试文件被完全忽略。
- 讽刺的是，preflight 的确定性检查已经把线索递到门口：static_check 报告的 1 处新增 TODO 恰好就是 test-fetch.mjs 里的 "Remove this once keep-alive behavior can be disabled from the client side"——但首轮检索没有跟进。
- 与 gin-2632 的 miss 模式不同：gin 是"读到相关代码、没读出语义方向"，node 这次是"相关文件根本没读"。共同点是**候选生成阶段缺失**，而非复核扣留。

## 改进指向

- retrieval hint 注入 valid 参考语义关键词（本条：`keep-alive`、`server.close`、`test-fetch.mjs`）。
- 更通用的教训：diff 含测试文件时应保证测试文件至少被一轮检索覆盖（新增 TODO/static_check hint 可作为检索种子），避免实现文件吸走全部注意力预算。

## 产物

- `case-node-41749-create.json` / `case-node-41749.json` — 提交与最终 job JSON
- `job-ids.txt` — job id
- `trace-details/46cedc58a0cdd0cb.json` — 30 个 tool 事件的完整输入/输出
