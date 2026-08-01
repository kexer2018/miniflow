# miniflow 产品化功能分析与路线

> 版本: v0.2
> 日期: 2026-07-31
> 定位: 可视化、容器化、可扩展的流水线执行平台
> 上层约束: 以 `docs/product-principles.md` 为准

---

## 1. 新定位

miniflow 不应该被定位成“替用户写业务流程的 AI CI/CD 系统”，而应该定位成：

> 一个 Docker-native、single-node-first、script-first 的轻量级 CI/CD 执行平台。

产品的核心价值是把流水线从 JSON/YAML 心智模型转成图形化编排模型，并提供可靠的 Docker 执行环境。用户通过拖拽 Step、连线、配置参数来生成流水线；后端负责 DAG 校验、容器执行、共享 workspace、日志、缓存、产物和诊断。

这份分析必须服从五条边界：Docker 主路径、单机优先、脚本优先、图形化只提供基础 Step、PipelineSpec 作为稳定协议。

### 1.1 产品边界

miniflow 提供：

- Step 编排与 DAG 校验
- 每个 Step 一个临时容器
- 所有 Step 默认挂载同一个 workspace
- 环境变量、密钥、缓存、产物、超时、重试
- 运行状态、实时日志、历史记录
- 失败日志脱敏、分类和 AI/RAG 诊断

miniflow 不提供：

- 强绑定业务域的 Step
- 替用户决定发布流程
- 隐式修改用户业务脚本
- 早期就做庞大的插件市场
- 把聊天框作为主交互入口
- 早期分布式 Worker 调度
- 近期非 Docker executor

### 1.2 与传统 CI/CD 的区别

| 维度 | 传统 CI/CD | miniflow 产品目标 |
|------|------------|-------------------|
| 配置方式 | YAML/Groovy/脚本 | 可视化画布 + 表单 + 可查看 spec |
| 执行环境 | Runner 或宿主环境 | Step 粒度临时 Docker 容器 |
| 状态传递 | Artifact/cache 规则复杂 | 共享 workspace + 显式 artifact |
| 扩展方式 | 插件生态或自定义脚本 | 基础 Step 原语 + 用户自有脚本/镜像 |
| 排错体验 | 大段日志 | 节点状态 + 脱敏日志 + 诊断建议 |

---

## 2. 当前能力盘点

### 2.1 已完成或已有基础

| 模块 | 当前能力 | 产品化评价 |
|------|----------|------------|
| Pipeline Spec | JSON 输入、Step、依赖、env、cache、source、secret、timeout 字段 | 可作为 typed step 的兼容基础 |
| DAG 校验 | 名称唯一、依赖存在、自依赖、循环检测、拓扑排序 | 可直接服务图形化连线校验 |
| Docker 执行 | 镜像检查/拉取、容器创建、启动、等待、日志收集、清理 | 可作为 Step runner 内核 |
| Workspace | `/tmp/miniflow/workspaces/{pipeline-id}` 共享目录 | 符合产品核心执行模型 |
| Git source | go-git checkout、凭据匹配、浅克隆基础 | 可升级为 `git.checkout` Step |
| Secret | 本地 credentials JSON、secret 引用注入 env | 需要产品化 API 和 UI |
| Cache | cache key 到目录挂载 | 需要 restore/save 语义和 key 模板 |
| Log | 收集、脱敏、分类 | 可支撑运行态日志和诊断 |
| AI/RAG | YAML seed、LLM 结构化诊断、降级模式 | 作为失败诊断能力保留 |
| SQLite | pipeline result、exec context、diagnosis history | 需要扩展 run/artifact/cache 表 |
| CLI | run、version、diagnose；validate 当前 flag 绑定需修复 | CLI 继续作为开发者入口 |
| API | health、history、diagnose、fix suggest 骨架 | 需要真正 run API 和状态流 |
| Worker | daemon skeleton | 远期作为分布式执行基础，近期不进入主线 |

### 2.2 关键过期认知修正

旧文档中有几处需要统一修正：

- “无 Git 源码拉取”已过期。当前已有 `internal/source` 实现，缺的是产品化 Step 和 UI。
- “AI 原生是主线”需要降级为差异化能力。主线应是可视化流水线平台。
- “自然语言编排”不应作为近期核心路径，应在 Step Registry 和图形化编辑器稳定后再做。
- “Worker/分布式执行”不应作为近期核心路径，应在单机 Run API、Artifact/Cache/Secret 模型稳定后再做。
- “Shell/macOS/Windows executor”只能作为远期例外设计，近期主执行生态仍是 Docker。
- 当前 Go 版本以 `go.mod` 为准，为 Go 1.25。
- Step timeout 字段和执行逻辑已有基础，缺的是完整 UI/API、管道级 timeout 和运行历史表达。

---

## 3. 功能分层

```text
┌───────────────────────────────────────────────┐
│ 用户交互层                                      │
│ Visual editor / CLI / API / Run view           │
└───────────────────────┬───────────────────────┘
                        │
┌───────────────────────▼───────────────────────┐
│ 产品模型层                                      │
│ Pipeline / Typed Step / DAG / Validation        │
└───────────────────────┬───────────────────────┘
                        │
┌───────────────────────▼───────────────────────┐
│ 调度执行层                                      │
│ Scheduler / Retry / Timeout / Approval / Rerun  │
└───────────────────────┬───────────────────────┘
                        │
┌───────────────────────▼───────────────────────┐
│ 执行环境层                                      │
│ Docker / Workspace / Cache / Artifact / Secret  │
└───────────────────────┬───────────────────────┘
                        │
┌───────────────────────▼───────────────────────┐
│ 可观测与智能辅助层                              │
│ Logs / History / Sanitizer / Classifier / RAG   │
└───────────────────────────────────────────────┘
```

---

## 4. 基础 Step 原语

基础 Step 应只覆盖平台能力，不承载业务流程。

### 4.1 MVP Step

| Step | 类型 | 作用 | 优先级 |
|------|------|------|--------|
| Git Checkout | `git.checkout` | 拉取代码到 workspace | P0 |
| Shell Script | `script.run` | 用户自定义脚本执行 | P0 |
| File Operation | `file.operation` | copy/move/delete/mkdir/archive/extract | P1 |
| Cache Restore | `cache.restore` | 恢复依赖缓存 | P1 |
| Cache Save | `cache.save` | 保存依赖缓存 | P1 |
| Artifact Save | `artifact.save` | 保存构建产物或报告 | P1 |
| Artifact Restore | `artifact.restore` | 恢复上游或历史产物 | P1 |
| Docker Build | `docker.build` | 构建镜像 | P1 |
| Docker Push | `docker.push` | 推送镜像 | P2 |
| HTTP Request | `http.request` | 调 webhook/API | P2 |
| Manual Approval | `approval.manual` | 人工确认门禁 | P2 |
| Notification Webhook | `notify.webhook` | 通用通知 | P2 |

### 4.2 延后 Step

这些能力有价值，但不应进入第一版基础平台：

- Kubernetes apply
- Helm deploy
- SSH deploy
- Docker compose deploy
- Matrix build
- Parallel group
- Conditional branch
- SaaS-specific notification

它们要么更接近业务/部署适配器，要么需要更成熟的调度状态机。

---

## 5. Step Spec 演进

### 5.1 兼容现有格式

现有格式继续可用：

```json
{
  "name": "test",
  "image": "golang:1.25",
  "commands": ["go test ./..."],
  "depends_on": ["checkout"]
}
```

### 5.2 新 typed step 格式

新增 `type` 和 `with`：

```json
{
  "name": "test",
  "type": "script.run",
  "image": "golang:1.25",
  "depends_on": ["checkout"],
  "with": {
    "workdir": "/workspace",
    "shell": "sh",
    "script": "go test ./... -count=1"
  }
}
```

后端编译流程：

```text
PipelineSpec
  → Step Type Registry 校验
  → Typed Step 编译
  → internal/pipeline.Step
  → container.Config
  → Docker 执行
```

### 5.3 Step Type Registry

注册表是产品化的关键后端能力。

它需要提供：

- Step 类型列表
- Step 分组、图标、描述
- `with` 参数 JSON Schema
- 默认值
- 参数校验
- 编译函数
- 示例配置

前端不应该硬编码 Step 表单，而应该优先从 Registry 获取 schema，再渲染合适控件。

---

## 6. 前端产品形态

### 6.1 四区布局

```text
┌──────────────┬──────────────────────────┬──────────────────┐
│ Step palette │        DAG canvas         │ Step inspector   │
│              │                          │                  │
│ Source       │  checkout → test → build  │ Basics           │
│ Script       │                  ↘ push   │ Configure        │
│ Files        │                          │ Env & secrets    │
│ Cache        │                          │ Inputs/outputs   │
│ Artifacts    │                          │ Policy           │
│ Docker       │                          │ Advanced         │
│ Integration  │                          │                  │
└──────────────┴──────────────────────────┴──────────────────┘
┌────────────────────────────────────────────────────────────┐
│ Run timeline / logs / validation problems                  │
└────────────────────────────────────────────────────────────┘
```

### 6.2 Step Palette

- 按 Source、Script、Files、Cache、Artifacts、Docker、Integration、Control 分组。
- 支持搜索和拖拽创建节点。
- 点击 Step 展示简短说明、输入输出和示例。

### 6.3 DAG Canvas

- 画布节点表达 Step。
- 连线表达 `depends_on`。
- 建线时实时检测循环依赖。
- 运行时节点直接显示 pending/running/success/failed/skipped/waiting。

### 6.4 Step Inspector

右侧配置面板推荐 tab：

| Tab | 内容 |
|-----|------|
| Basics | 名称、类型、描述 |
| Configure | 当前 Step 类型参数 |
| Environment | env、secrets、credentials |
| Inputs & outputs | cache、artifacts、workspace |
| Policy | timeout、retry、continue on error |
| Advanced | entrypoint、network、raw spec |

### 6.5 输入控件原则

- Step name 用普通 input，实时校验唯一性。
- Image 用 combobox，既提供建议也允许自由输入。
- Script 用代码编辑器，不用普通 textarea。
- Env 用 key/value 表格。
- Secrets 用 picker，只显示引用名。
- Depends on 以画布连线为主，Inspector 只读展示。
- Cache key 用 builder，支持 checksum 插入。
- Artifact path 支持 glob 和运行后预览。
- HTTP body 用代码编辑器。
- Timeout/retry 用数字输入、stepper、toggle。

---

## 7. 后端产品化缺口

| 能力 | 缺口 | 说明 |
|------|------|------|
| Step Registry | 缺失 | 前后端统一 Step schema 和编译逻辑 |
| Typed Step | 缺失 | 当前仍以 commands 为中心 |
| Run API | 缺失 | API 目前不真正执行 pipeline |
| 实时状态 | 缺失 | 前端需要 SSE/WebSocket |
| 日志流 | 部分缺失 | collector 有基础，但需要按 run/step 推送 |
| Artifact | 缺失 | 当前只有 workspace，没有持久化产物 |
| Cache | 部分缺失 | 有挂载，缺 restore/save 和 key 模板 |
| Secret API | 缺失 | 当前主要是文件型 credentials |
| Rerun | 缺失 | 需要从失败 Step 重跑和全量重跑 |
| Validation API | 缺失 | 图形化编辑时需要实时校验 |

---

## 8. AI 能力定位

AI 仍然重要，但不是第一入口。

近期 AI 能力应该聚焦：

1. 失败日志诊断。
2. 配置字段建议。
3. 根据错误建议修改 Step 参数。
4. 对 pipeline spec 做解释和风险提示。

延后能力：

1. 自然语言生成整条流水线。
2. 自动修改并执行修复。
3. 自动生成任意脚本并运行。

原因：产品早期最需要可靠的执行与可视化编辑。AI 编排必须建立在稳定 Step Registry、Validation API、Run API 和用户确认机制之上。

---

## 9. 推荐路线

### Phase 1: 稳定现有执行内核

- 修复 CLI `validate -f` flag 问题。
- 确认 Go 版本文档与 `go.mod` 一致。
- 保持现有 spec 兼容。
- 补充 Docker 可用时的端到端示例验证。

### Phase 2: Step 类型系统

- 引入 `type` 和 `with`。
- 建立 Step Type Registry。
- 首批实现 `script.run` 和 `git.checkout`。
- API 暴露 Step 类型和表单 schema。

### Phase 3: 产品化运行 API

- 创建 run。
- 查询 run/step 状态。
- 取消 run。
- 日志流。
- 运行历史。
- validation API。

### Phase 4: 产物、缓存、密钥

- Artifact save/restore。
- Cache restore/save。
- Secret 管理 API。
- UI 显示 cache hit/miss 和 artifact 列表。

### Phase 5: 可视化编辑器

- Step palette。
- DAG canvas。
- Step inspector。
- Bottom logs/timeline panel。
- Raw spec preview。

### Phase 6: 集成和控制类 Step

- Docker build/push。
- HTTP request。
- Manual approval。
- Notification webhook。

### Later: 高级执行形态

- 分布式 Worker。
- 非 Docker executor。
- 插件市场。
- 自然语言生成整条流水线。

这些能力只有在单机 Docker 产品闭环稳定后才能重新评估。

---

## 10. 成功标准

第一版产品化闭环应满足：

1. 用户可以在 UI 里创建流水线。
2. 用户可以拖拽基础 Step 并连线。
3. 用户可以配置镜像、脚本、env、secrets、cache、artifacts。
4. 前端能实时提示 DAG 和字段错误。
5. 后端能执行 pipeline 并返回 step 状态。
6. 日志能按 step 实时查看。
7. 失败 step 能展示脱敏日志和诊断建议。
8. 产物能保存并在 UI 中下载。

这组目标完成后，miniflow 才真正从“CLI 执行器”进入“可视化流水线产品”。
