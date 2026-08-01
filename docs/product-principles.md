# miniflow 产品原则与开发边界

> 日期: 2026-08-01
> 状态: 当前产品与技术决策的上层约束

---

## 1. 一句话定位

miniflow 是一个 Docker-native、single-node-first、script-first 的轻量级 CI/CD 执行平台。

它可以提供可视化编排、基础 Step、运行状态、日志、缓存、产物、密钥和失败诊断，但不接管用户的业务逻辑。

---

## 2. 核心原则

### 2.1 Docker 是主执行生态

miniflow 的主执行模型必须和 Docker 生态绑定：

- 每个 Step 默认运行在独立临时 Docker 容器中。
- 用户通过镜像定义执行环境。
- workspace、cache、artifact、secret、日志都围绕容器执行模型设计。
- Docker/OrbStack/Docker Desktop 是第一阶段唯一必须支持的运行依赖。

Shell executor、macOS executor、Windows executor 等能力只作为远期例外路径讨论，不进入近期主线。不能为了少数无法容器化的场景提前破坏 Docker 执行模型的简单性。

### 2.2 单机优先，暂不做分布式

近期开发必须坚持单机执行：

- 所有 Step 默认共享同一个本地 workspace。
- Step 间文件传递优先通过共享 workspace 完成。
- artifact/cache 用于持久化、下载、跨 run 复用和审计，而不是早期分布式调度。
- Worker、任务队列、跨机器调度、缓存亲和性、远程 artifact store 都后置。

分布式会立刻引入 workspace 跨机器同步、artifact 传递、cache locality、任务调度一致性和取消语义等复杂问题。只有在 Run API、Artifact/Cache/Secret 模型稳定之后，才重新评估分布式 Worker。

### 2.3 脚本式为主，图形化为辅

miniflow 的核心表达能力来自脚本、命令、镜像和 pipeline spec：

- `script.run` 是第一公民，不是临时兜底。
- 图形化 UI 负责降低编排、校验、配置和观察成本。
- UI 不应该试图表达所有业务逻辑。
- 用户必须始终能查看、导出、提交和版本化底层 JSON spec。

图形化适合表达 DAG、基础参数、状态和日志，不适合替代用户自己的构建、测试、发布脚本。

### 2.4 图形化只提供基础 Step

内置 Step 应保持为平台原语：

- `git.checkout`
- `script.run`
- `file.operation`
- `cache.restore`
- `cache.save`
- `artifact.save`
- `artifact.restore`
- `docker.build`
- `docker.push`
- `http.request`
- `approval.manual`
- `notify.webhook`

不应该把具体业务流程封装成核心 Step，例如特定公司发布流程、特定框架一键部署、复杂 Kubernetes/Helm/SSH 发布模板等。这些能力可以由用户脚本、自定义镜像、HTTP/webhook 或未来可选插件表达。

### 2.5 Pipeline spec 是稳定协议

UI、CLI、API 和未来 AI 辅助都必须围绕同一份 PipelineSpec 工作：

- Step Type Registry 是前后端共享的 Step schema 和编译来源。
- UI 生成 spec，而不是拥有独立私有模型。
- CLI/API 能运行同一份 spec。
- 旧的 `image + commands` 格式保持兼容，并映射为脚本执行模型。

这条原则保证 miniflow 不会变成只能点击 UI 的低代码平台，也不会变成前后端各自维护一套 DSL 的系统。

---

## 3. 近期开发顺序约束

近期优先级应围绕单机 Docker 执行闭环：

1. 修正 CLI、README 和示例可用性。
2. 建立 Step Type Registry。
3. 做强 `script.run`，再实现 `git.checkout`。
4. 补齐 Run API、Step 状态和日志流。
5. 补齐 artifact/cache/secret 的本地产品化模型。
6. 再做可视化编辑器 MVP。
7. 最后再做 Docker build/push、HTTP、approval、notify 等集成和控制类 Step。

以下能力明确后置：

- 分布式 Worker。
- 多租户。
- 插件市场。
- Matrix、复杂条件执行和并行 group。
- 自然语言生成整条流水线。
- AI 自动修改代码并重跑。
- 非 Docker executor。

---

## 4. 文档与开发检查清单

任何新文档、设计或代码改动，都需要回答：

1. 是否仍以 Docker 容器执行为主路径？
2. 是否保持单机 workspace 模型简单可用？
3. 是否把业务逻辑留给用户脚本、仓库和镜像？
4. 是否只把通用平台能力做成内置 Step？
5. 是否让 PipelineSpec 成为 CLI、API、UI 的共同协议？
6. 是否避免把 AI、分布式、插件市场提前放进主路径？

如果答案是否定的，应先调整设计，而不是继续实现。
