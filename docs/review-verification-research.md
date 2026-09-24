# 代码审查证据核验排障与方案

## 复现与根因

用户提供的 [OpenDerisk PR #195](https://github.com/derisk-ai/OpenDerisk/pull/195) 审查记录显示：8 条候选中仅 2 条通过变更行证据匹配，第二轮又将这 2 条排除，最终返回 `completed_with_warnings / incomplete`。

对照该 PR 的 72,195 字节 diff，8 条候选的 `evidence` 都能在各自文件的连续新增行中找到。6 条被过滤的原因是模型给出的 `line` 不等于引用片段首行，偏移分别为 33、1、2、8、2、43 行。原有核验只从模型给定行开始逐行匹配，因此把“定位错误”记成“证据不足”。

第二轮原先只看到候选行前后 4 行的 diff。`sync_app_detail` 是否为异步函数取决于 diff 外的现行定义；另一条关于调用方按下标使用返回列表的断言也依赖未展示的调用方。原协议只有 `is_real` 布尔值，模型把缺少上下文压成 `false`，服务端随即将它记作“第二轮复核排除”。

## 参考做法

- [GitHub Copilot code review](https://docs.github.com/en/copilot/concepts/agents/code-review) 说明其审查会收集完整项目上下文，并提醒用户核验 AI 反馈。这里借鉴的是扩大有边界的源码上下文，不声称拥有与其相同的仓库级能力。
- [Google 代码审查指南](https://google.github.io/eng-practices/review/reviewer/looking-for.html#context)指出 diff 常只显示少数邻近行，必要时应查看完整文件。
- [GitHub Contents API](https://docs.github.com/en/rest/repos/contents?apiVersion=2022-11-28) 支持使用 `ref` 指定提交；读取 PR head SHA 后可固定被审查源码版本。
- [Anthropic code-review 插件](https://github.com/anthropics/claude-code/blob/main/plugins/code-review/README.md)强调只报告有证据的 PR 新增问题并过滤低置信度候选。这里保留确定性的新增行门槛。
- [OpenAI 关于模型幻觉的研究](https://openai.com/index/why-language-models-hallucinate/)强调准确、错误与弃答的区分；这里将证据不足表示为 `inconclusive`，不冒充已证伪。

## 已实现

1. 在同文件的新增行中查找候选引用的完整连续代码块。原位置正确时保留；原位置错误且代码块唯一时修正 `line`；存在多个匹配或引用非新增行时仍拒绝。
2. 第二轮 diff 片段扩大到前后 12 行。对于 GitHub PR，由宿主按 PR head SHA 读取最多 16 个变更文件，每文件最多 256 KiB、总计最多 1 MiB；请求不跟随重定向，源码在进入模型前脱敏，只发送有限的目标行与相关定义/引用片段。若固定提交源码与 diff 的引用行不一致，则不把该源码当作证据。
3. 第二轮协议使用 `confirmed / rejected / inconclusive`。仅发布 `confirmed`；`inconclusive` 使审查保持 `completed_with_warnings / incomplete`，并单独计数；明确证伪或纯假设性主张计入 `rejected`。

当前 GitLab MR 和直接粘贴 diff 没有固定提交源码读取路径；这些审查仍可用 diff 复核，依赖仓库外上下文的候选会如实标记为待定。真实模型的结论仍需人工验证；本次验收使用确定性的样本比对、模拟模型/API 测试、`go test ./...` 与 `go vet ./...`。
