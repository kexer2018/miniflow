# miniflow 项目进度与产品化状态

> 日期: 2026-07-31
> 当前阶段: 执行内核已具备，产品化设计启动
> Go 版本: 以 `go.mod` 为准，当前为 Go 1.25
> 上层约束: 以 `docs/product-principles.md` 为准

---

## 1. 项目定位

miniflow 当前定位为：

> Docker-native、single-node-first、script-first 的轻量级 CI/CD 执行平台。

项目不以内置业务流程为核心，而是提供通用平台原语：Step 编排、DAG 校验、Docker 容器执行、共享 workspace、缓存、产物、密钥、日志、运行历史和失败诊断。用户的业务逻辑应保留在自己的脚本、仓库和 Docker 镜像中。

后续开发必须优先满足：Docker 主路径、单机优先、脚本优先、图形化只提供基础 Step、PipelineSpec 作为稳定协议。

---

## 2. 当前架构状态

### 2.1 已实现主链路

```text
Pipeline JSON
  → PipelineSpec Validate
  → build internal Pipeline
  → DAG TopologicalSort
  → Create shared workspace
  → optional source checkout
  → serial Docker step execution
  → collect logs
  → sanitize logs
  → optional AI/RAG diagnosis
  → persist result to SQLite
```

### 2.2 包状态

| 包/入口 | 状态 | 说明 |
|---------|------|------|
| `cmd/miniflow` | 可用 | CLI 主入口，支持 run、version、diagnose；validate flag 绑定需修复 |
| `cmd/worker` | 骨架 | 可启动 daemon，但尚未承接远程任务 |
| `pkg/pipeline` | 可用 | 外部 PipelineSpec 类型，当前以 `image + commands` 为主 |
| `internal/pipeline` | 可用 | DAG 校验、拓扑排序、串行执行、step timeout 基础 |
| `internal/container` | 可用 | Docker SDK 封装、workspace、cache path、socket 探测 |
| `internal/source` | 可用 | go-git checkout、凭据匹配、浅克隆基础 |
| `internal/secret` | 可用 | 本地 credentials store 和 secret env 解析 |
| `internal/log` | 可用 | 日志收集、脱敏、分类 |
| `internal/fixer` | 可用 | RAG seed、LLM 诊断、降级模式 |
| `internal/llm` | 可用 | OpenAI-compatible client |
| `internal/db` | 可用 | SQLite 存储 pipeline result、exec context、diagnosis history |
| `internal/api` | 骨架 | health/history/diagnose 基础，run pipeline 仍未真正执行 |

---

## 3. 已完成能力

### 3.1 执行内核

- JSON pipeline spec 解析。
- Step 基础字段校验。
- DAG 依赖验证和循环检测。
- 串行执行。
- 每个 Step 使用独立临时容器。
- 所有 Step 挂载同一 workspace。
- 失败后后续 Step 标记 skipped。
- Ctrl+C/SIGTERM 取消。
- Step timeout 字段和执行上下文基础。

### 3.2 Docker 与 workspace

- Docker socket 自动探测。
- 镜像存在性检查。
- 自动拉取缺失镜像。
- 容器创建、启动、等待、日志收集、清理。
- workspace 创建。
- UID/GID 统一策略基础。
- cache path 挂载基础。

### 3.3 源码与凭据

- Pipeline-level source checkout。
- 支持 repository/ref/shallow/depth 基础配置。
- 支持 token、username/password、SSH key 凭据类型。
- Step secret 引用可注入环境变量。

### 3.4 日志与诊断

- 日志采集。
- 敏感信息脱敏。
- 确定性错误分类。
- YAML seed 加载。
- RAG 匹配。
- LLM 结构化诊断。
- 无 LLM/API 网络时降级为 RAG-only。

### 3.5 持久化

- SQLite 自动迁移。
- 保存 pipeline result。
- 保存 exec context。
- 保存 diagnosis history。
- 查询历史结果和诊断记录基础。

---

## 4. 已知问题

| 问题 | 影响 | 建议 |
|------|------|------|
| `miniflow validate -f` 当前不可用 | README/CLI 用法不一致 | 将 `--file` 改为 persistent flag，或为 validate 单独注册 |
| API run pipeline 未实现 | 前端无法真正触发执行 | 增加 run service 和异步状态 |
| 仍以 `image + commands` 为核心 spec | 前端表单难以产品化 | 引入 typed step 的 `type + with` |
| 无 Step Type Registry | 前后端无法共享 Step schema | 新增 registry 包和 API |
| 无 artifact store | 产物只能留在 workspace | 增加 artifact 元数据和本地存储 |
| cache 只有挂载语义 | 缺 restore/save、hit/miss | 增加 cache key 解析和状态记录 |
| 无日志流 API | 图形化运行体验不足 | 基于 collector 增加 SSE/WebSocket |
| Secret 管理仍偏本地文件 | 产品化 UI 不够 | 增加 Secret/Credential CRUD API |
| worker 只是 skeleton | 暂不影响近期主线 | 分布式 Worker 明确后置 |

---

## 5. 测试状态

最近一次验证：

```bash
go test ./... -count=1
go build -o /tmp/miniflow-check ./cmd/miniflow
go build -o /tmp/miniflow-worker-check ./cmd/worker
```

结果：单元测试通过，两个入口可构建。

注意：完整执行示例 pipeline 依赖本机 Docker daemon。当前环境如果 Docker/OrbStack 未运行，流水线会进入执行路径，但在镜像检查或容器创建阶段失败。

---

## 6. 产品化下一阶段

### 6.1 P0: 修正执行器可用性

- 修复 `validate -f`。
- 同步 README 和 docs 中的 Go 版本。
- 增加一条不依赖外部网络的本地 Docker 示例。
- 为 source checkout、timeout、secret 注入补充端到端测试。

### 6.2 P1: Step 类型系统

- 已完成 `script.run`、`git.checkout`、`file.operation`、`cache.restore`、`cache.save`、`artifact.save` 与 `artifact.restore`。
- Step Type Registry 与 Step Types API 已暴露基础 schema。

### 6.3 P2: 产品化运行 API

- 创建/查询/取消 run、查询 Step 状态、validation API 与 SSE 实时日志已完成。
- Artifact 列表和下载 API 已完成。

### 6.4 P3: 缓存、产物、密钥

- 本地 Artifact save/restore、SQLite 元数据和本地 Cache restore/save 已完成。
- Cache fallback key、保留期清理和 Secret/Credential API 仍待实现。

### 6.5 P4: 可视化编辑器

- Step Palette。
- DAG Canvas。
- Step Inspector。
- Bottom Run/Log Panel。
- Raw Spec Preview。

---

## 7. 文档索引

| 文档 | 用途 |
|------|------|
| `docs/product-principles.md` | 产品原则与开发边界，其他文档应服从它 |
| `docs/basic-steps-and-visual-pipeline-design.md` | 基础 Step 与前端交互设计 |
| `docs/CI-CD-FUNCTIONAL-ANALYSIS.md` | 产品化功能分析与路线 |
| `docs/roadmap.md` | 优先级路线图 |
| `docs/方案设计_实施计划.md` | 分阶段实施方案 |
| `docs/miniflow AI原生轻量级CICD执行引擎架构与技术白皮书.md` | 架构白皮书 |
| `docs/执行器双模驱动设计Executor设计指南.md` | 远期非 Docker executor 例外设计，不能指导近期主线 |

---

## 8. 当前结论

miniflow 已经不是纯设计草案。CLI、DAG、Docker 执行、workspace、source checkout、日志、诊断和 SQLite 都有可运行基础。下一步不应继续横向扩展 AI、分布式或非 Docker executor，而应把单机 Docker 执行内核产品化：Step 类型系统、运行 API、实时日志、artifact/cache/secret 管理和图形化编辑器。
