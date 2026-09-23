# Web 审查工作台

原生 HTML + CSS + JavaScript，无前端构建步骤、运行时依赖或外部字体请求。

## 文件职责

- index.html：页面语义结构和资源入口，不包含内联脚本或样式。
- assets/css/components.css：基础布局与组件。
- assets/css/theme.css：配色、字体、间距和表面样式。
- assets/css/responsive.css：桌面、平板、手机断点布局。
- assets/css/motion.css：入场、切换动画及减少动态效果偏好。
- assets/js/state.js：共享会话状态、初始化和基础格式化。
- assets/js/helpers.js：事件分类、计时及展示标签等纯计算。
- assets/js/dom.js：DOM 更新缓存、事件节点复用、动画帧调度及导航状态。
- assets/js/view.js：轨迹、结果、对话、指标和历史列表渲染。
- assets/js/reviews.js：审查请求、会话加载和 SSE 生命周期。
- assets/js/demo.js：明确标记的本地示例数据。
- assets/js/app.js：交互绑定、导出、健康检测和启动入口。

脚本使用 defer 按 HTML 中的顺序执行，共享同一页面作用域；新增脚本时保留依赖顺序，避免重复声明共享变量。

## 运行与验证

从仓库根目录执行 go run ./cmd/server，沿用服务现有 MySQL 配置。Go 同时提供 / 和 /assets 静态资源，API 路径不变。首次部署此重构需重启 Go 服务以加载新增资源路由。

执行 node --test web/timing.test.mjs 检查服务端计时语义；执行 go test ./cmd/server 检查服务入口。

可单独通过静态 HTTP 服务预览 web 目录并点击“示例轨迹”；这种模式不提供真实审查 API，页面会明确显示服务未连接。

## 动画与实时更新

入场使用 opacity 和 transform，系统开启减少动态效果时禁用动画及平滑滚动。SSE 更新通过 requestAnimationFrame 合并；相同 HTML 不重复写入，未变更的事件节点保留展开状态和焦点。计时每秒更新，后台标签页跳过计时渲染。
