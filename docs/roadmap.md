# miniflow 产品化路线图

> 日期: 2026-07-31
> 主线: 从 CLI 执行器演进为可视化流水线平台

---

## 1. 路线原则

1. 先产品闭环，再扩展复杂调度。
2. 先基础 Step 原语，再业务/部署适配器。
3. 先可靠表单和图形化编辑，再自然语言编排。
4. 先单机运行体验，再分布式 Worker。
5. AI 诊断保留为差异化能力，但不抢主路径。

---

## 2. 当前状态

已具备：

- Pipeline JSON 解析与基础校验。
- DAG 拓扑排序和循环检测。
- 每个 Step 一个临时 Docker 容器。
- 共享 workspace。
- 源码 checkout 基础。
- Secret/env 注入基础。
- Cache mount 基础。
- 日志脱敏、分类、RAG/LLM 诊断。
- SQLite 持久化。
- CLI 主执行路径。

仍缺：

- Typed Step 和 Step Type Registry。
- 真正可供前端使用的 run API。
- 实时 step 状态和日志流。
- Artifact save/restore。
- Cache restore/save 语义。
- Secret/Credential 管理 API。
- 可视化编辑器。

---

## 3. P0: 修正当前可用性

目标：让现有 CLI 和文档保持一致，保证执行内核可以被稳定复用。

任务：

- 修复 `miniflow validate -f examples/go-ci.json`。
- 同步 Go 版本说明，以 `go.mod` 为准。
- 补充最小 pipeline 示例，明确依赖 Docker daemon。
- 给 source checkout、timeout、secret 注入补更贴近真实执行的测试。
- 为 Docker 不可用时的错误输出增加更清晰提示。

验收：

- README 和 docs 中的命令可以直接执行。
- `go test ./... -count=1` 通过。
- Docker 可用时示例 pipeline 能跑通。

---

## 4. P1: Step 类型系统

目标：把后端从“命令数组执行器”升级为“基础 Step 平台”。

任务：

- 扩展 `PipelineSpec`，支持 `type` 和 `with`。
- 保留现有 `image + commands` 格式兼容。
- 新增 `internal/stepregistry` 或类似包。
- 定义 Step 描述结构：ID、名称、分组、schema、默认值、编译器。
- 实现 `script.run`。
- 实现 `git.checkout`。
- 暴露 `GET /api/v1/step-types`。
- 暴露 `POST /api/v1/pipelines/validate`。

验收：

- 前端可以从 API 获取 Step 类型列表和字段 schema。
- typed step 可以编译为现有 internal step 并执行。
- 旧 pipeline JSON 仍可执行。

---

## 5. P2: 运行 API 与实时反馈

目标：前端可以真正触发、观察、取消一次流水线运行。

任务：

- 增加 Run 模型：run id、pipeline snapshot、status、timestamps。
- 实现 `POST /api/v1/runs`。
- 实现 `GET /api/v1/runs/{id}`。
- 实现 `GET /api/v1/runs/{id}/steps`。
- 实现 `POST /api/v1/runs/{id}/cancel`。
- 实现日志流，优先 SSE，后续可选 WebSocket。
- 将执行器状态事件化：step pending/running/success/failed/skipped。

验收：

- API 可以启动 pipeline。
- 前端或 curl 可以轮询/订阅状态。
- 运行中可以看到 step 日志。
- 可以取消运行。

---

## 6. P3: Artifact、Cache、Secret 产品化

目标：把共享 workspace 的隐式传递升级为可见、可管理的输入输出模型。

任务：

- 增加 artifact metadata 表。
- 增加本地 artifact store。
- 实现 `artifact.save`。
- 实现 `artifact.restore`。
- 增加 cache key 模板解析，例如 `${checksum:go.sum}`。
- 实现 `cache.restore` 和 `cache.save`。
- 记录 cache hit/miss。
- 增加 Secret/Credential CRUD API。
- 前端只显示 secret 引用，不显示 secret 值。

验收：

- 构建产物可以在运行结束后下载。
- 下游 Step 可以恢复上游 artifact。
- cache 命中状态可见。
- secret 不以明文进入 pipeline spec。

---

## 7. P4: 可视化编辑器 MVP

目标：形成可用的图形化流水线创建和运行体验。

页面：

- Pipeline editor。
- Run detail。
- Pipeline history。
- Secret/Credential settings。

编辑器布局：

- 左侧 Step Palette。
- 中间 DAG Canvas。
- 右侧 Step Inspector。
- 底部 Run Timeline / Logs / Validation Problems。

核心交互：

- 拖拽 Step 创建节点。
- 连线生成 `depends_on`。
- 选中节点后在 Inspector 配置。
- 实时校验字段和 DAG。
- 查看生成的 JSON。
- 一键运行。
- 点击失败节点查看日志和诊断。

验收：

- 用户不写 JSON 也能创建并运行一条 Go/Node/Python 基础流水线。
- 用户可以看到每个 Step 的运行状态和日志。
- 用户可以下载 artifact。

---

## 8. P5: 集成与控制类 Step

目标：覆盖常见交付流程，但仍保持平台原语定位。

Step：

- `file.operation`
- `docker.build`
- `docker.push`
- `http.request`
- `approval.manual`
- `notify.webhook`

验收：

- 可以完成 checkout → test → build → artifact → docker build → webhook notify 的完整链路。
- Manual approval 可以暂停 pipeline 并等待用户确认。

---

## 9. P6: 高级能力

这些能力延后，避免早期复杂度过高：

- 并行执行。
- Matrix。
- 条件执行。
- 从失败 Step 重跑。
- 分布式 Worker。
- 多租户。
- 插件市场。
- 自然语言生成整条流水线。
- AI 自动修复并重跑。

高级能力的前置条件是 Step Registry、Run API、Validation API、Artifact/Cache/Secret 模型稳定。

---

## 10. 推荐近期开发顺序

```text
1. 修复 CLI/documentation 可用性
2. Step Type Registry
3. script.run + git.checkout typed step
4. Run API + 状态事件
5. 日志流
6. Artifact save/restore
7. Cache restore/save
8. Secret/Credential API
9. 可视化编辑器 MVP
10. Docker build/push + HTTP request + approval
```

---

## 11. 北极星指标

第一阶段产品化成功的判断标准：

- 一个没有 DevOps 背景的开发者可以在 10 分钟内创建一条可运行流水线。
- 用户可以不写 JSON，但随时能查看和导出 JSON。
- 每个 Step 的输入、输出、日志和失败原因都能在 UI 中定位。
- 用户的业务逻辑仍然保留在自己的脚本和镜像中。
