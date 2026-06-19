# miniflow CI/CD 功能分析 & 产品路线

> **版本**: v0.1 <!-- 初稿 -->
> **作者**: AI 分析
> **日期**: 2026-06-19

---

## 目录

1. [定位与愿景](#1-定位与愿景)
2. [当前能力盘点](#2-当前能力盘点)
3. [功能分层架构](#3-功能分层架构)
4. [基础设施层 — 详细分析](#4-基础设施层--详细分析)
5. [调度执行层 — 详细分析](#5-调度执行层--详细分析)
6. [用户交互层 — 详细分析](#6-用户交互层--详细分析)
7. [AI Native 差异化能力](#7-ai-native-差异化能力)
8. [Spec 演进设计](#8-spec-演进设计)
9. [推荐实施路线](#9-推荐实施路线)
10. [附录：真实 CI 管道完整示例](#10-附录真实-ci-管道完整示例)

---

## 1. 定位与愿景

### 一句话定位

> **AI-native 轻量级 CI/CD 引擎** —— 把重复的 CI/CD 运维交给 AI，让开发者只需要描述"做什么"。

### 与现有系统的差异化

| 维度 | Jenkins / GitLab CI | GitHub Actions | **miniflow（目标）** |
|------|---------------------|----------------|---------------------|
| 部署复杂度 | 重（需要 JVM、插件生态） | 无需部署（SaaS） | **极轻（单二进制）** |
| AI 能力 | 无 | Copilot 代码建议 | **AI 诊断→修复→自愈** |
| 运行环境 | 自托管或云 | GitHub 托管 | **自托管，边缘友好** |
| 配置格式 | XML / YAML | YAML | JSON/YAML + **AI 辅助生成** |
| 扩展性 | 插件系统 | Marketplace | **种子 + LLM 动态推理** |

### 核心设计原则

1. **单二进制部署** — `go build` 一个文件，到处运行
2. **AI 内建** — 每个失败自带诊断，不依赖第三方分析服务
3. **轻量化** — 不需要数据库中间件（SQLite 自包含）
4. **边缘友好** — 低资源消耗，适合树莓派/轻量服务器
5. **渐进复杂** — 从 `miniflow run` 到分布式集群，平滑升级

---

## 2. 当前能力盘点

### ✅ 已完成

| 模块 | 能力 | 成熟度 |
|------|------|--------|
| **DAG 定义与校验** | JSON 解析、拓扑排序（Kahn）、循环检测、依赖验证 | ★★★★★ |
| **串行执行** | 依次执行 Docker 容器，工作空间共享 | ★★★★☆ |
| **容器生命周期** | 镜像拉取、创建、启动、等待、日志采集、清理 | ★★★★☆ |
| **Socket 自动检测** | OrbStack → Docker Desktop → rootless 自动适配 | ★★★★★ |
| **UID 统一** | 容器以 `1000:1000` 运行，chown 权限修复 | ★★★★★ |
| **缓存挂载** | 指定路径和 key 的持久化缓存 | ★★★★☆ |
| **日志脱敏** | 8 条正则规则（JWT、AWS Key、GitHub Token 等） | ★★★★☆ |
| **日志分类** | 确定性规则分类（app_error / infra_error / unknown） | ★★★★☆ |
| **AI 诊断** | RAG + LLM 结构化输出诊断 | ★★★★☆ |
| **种子引擎** | 17 内置 + YAML 文件可扩展 | ★★★★☆ |
| **SQLite 持久化** | 管道结果、执行上下文、诊断历史 | ★★★★★ |
| **配置系统** | CLI flags → env → config file → defaults | ★★★★★ |
| **CLI 基础** | run / validate / diagnose / version | ★★★☆☆ |
| **种子文件** | 6 个 YAML 文件（auth、image-pull、network、permission、resource、app-code） | ★★★★☆ |
| **Dockerfile** | 多阶段构建，alpine 运行时 | ★★★★★ |
| **Docker Compose** | worker 部署，持久卷，健康检查 | ★★★★★ |

### ❌ 核心缺口

| 缺口 | 影响 | 严重性 |
|------|------|--------|
| **无 Git 源码拉取** | 不能真正执行 CI/CD，只能模拟 | 🔴 致命 |
| **全串行执行** | 多步骤管道速度慢 | 🟠 高 |
| **无触发机制** | 不能自动响应代码变更 | 🟠 高 |
| **无重试策略** | 偶发失败导致整体失败 | 🟡 中 |
| **无密钥管理** | 密钥硬编码在 JSON 中不安全 | 🟡 中 |
| **无条件执行** | 不能按分支/环境决定是否运行步骤 | 🟡 中 |
| **CLI 功能薄弱** | 无法管理历史、查看日志、重试 | 🟢 低 |
| **无通知机制** | 失败后只能通过 CLI 看到 | 🟢 低 |
| **无工件管理** | 构建产物无法持久化保存 | 🟢 低 |

---

## 3. 功能分层架构

```
 ┌──────────────────────────────────────────────────────────────────────┐
 │                         用户交互层（User Interface）                   │
 │                                                                      │
 │  ┌────────────┐  ┌────────────┐  ┌────────────┐  ┌───────────────┐ │
 │  │  CLI 强化   │  │  Web UI    │  │  REST API  │  │  通知/回执    │ │
 │  │ run/list    │  │ 仪表盘     │  │ 管道 CRUD  │  │ Slack/Email   │ │
 │  │ rerun/logs  │  │ 日志流     │  │ 触发管理   │  │ Webhook       │ │
 │  │ trigger     │  │ 诊断视图   │  │ WebSocket  │  │ 飞书/钉钉     │ │
 │  └────────────┘  └────────────┘  └────────────┘  └───────────────┘ │
 └────────────────────────────┬─────────────────────────────────────────┘
                              │
 ┌────────────────────────────▼─────────────────────────────────────────┐
 │                         调度执行层（Scheduler）                        │
 │                                                                      │
 │  ┌────────────┐  ┌────────────┐  ┌────────────┐  ┌───────────────┐ │
 │  │ 并行执行   │  │ 触发引擎   │  │ 重试策略   │  │ 条件引擎      │ │
 │  │ 并发池     │  │ Webhook    │  │ 退避算法   │  │ when 表达式   │ │
 │  │ 资源限制   │  │ Cron       │  │ 最大次数   │  │ if 条件       │ │
 │  │ 全局/局部  │  │ API        │  │ 间隔       │  │ 审批门禁      │ │
 │  └────────────┘  └────────────┘  └────────────┘  └───────────────┘ │
 │                                                                      │
 │  ┌────────────┐  ┌────────────┐  ┌────────────┐                     │
 │  │ 分布式     │  │ 任务队列   │  │ 心跳/健康  │                     │
 │  │ Worker池   │  │ 优先级     │  │ 探活/下线  │                     │
 │  │ leader选举 │  │ 去重       │  │ 节点信息   │                     │
 │  └────────────┘  └────────────┘  └────────────┘                     │
 │                                                                      │
 │  ┌────────────┐  ┌────────────┐  ┌────────────┐  ┌───────────────┐ │
 │  │ 超时控制   │  │ 优雅取消   │  │ 局部/全局  │  │ 状态机        │ │
 │  │ 步骤级     │  │ SIGTERM    │  │ 超时策略   │  │ pending→running│ │
 │  │ 管道级     │  │ 传播取消   │  │            │  │ →success/fail  │ │
 │  └────────────┘  └────────────┘  └────────────┘  └───────────────┘ │
 └────────────────────────────┬─────────────────────────────────────────┘
                              │
 ┌────────────────────────────▼─────────────────────────────────────────┐
 │                         基础设施层（Infrastructure）                   │
 │                                                                      │
 │  ┌────────────┐  ┌────────────┐  ┌────────────┐  ┌───────────────┐ │
 │  │ Git 集成   │  │ 工件管理   │  │ 密钥管理   │  │ 环境管理      │ │
 │  │ checkout   │  │ 上传/下载  │  │ secret     │  │ env 注入      │ │
 │  │ auth       │  │ S3/MinIO   │  │ 加密存储   │  │ 变量替换      │ │
 │  │ sparse     │  │ 跨管道共享 │  │ 运行时注入 │  │ 多环境        │ │
 │  └────────────┘  └────────────┘  └────────────┘  └───────────────┘ │
 │                                                                      │
 │  ┌────────────┐  ┌────────────┐  ┌────────────┐  ┌───────────────┐ │
 │  │ 镜像管理   │  │ 网络策略   │  │ 存储后端   │  │ 插件系统      │ │
 │  │ 预热       │  │ 隔离       │  │ 本地/S3    │  │ 自定义步骤    │ │
 │  │ 清理       │  │ 代理       │  │ 去重       │  │ hooks          │ │
 │  │ 缓存       │  │            │  │            │  │               │ │
 │  └────────────┘  └────────────┘  └────────────┘  └───────────────┘ │
 └──────────────────────────────────────────────────────────────────────┘
```

---

## 4. 基础设施层 — 详细分析

### 4.1 Git 源码集成 ⭐ 最高优先级

#### 需求

CI/CD 引擎的核心——没有代码拉取能力，就不能叫 CI/CD。当前 `examples/go-ci.json` 的 `checkout` 步骤只是 `echo` 模拟。

#### 设计方案

**方案 A：内建 `checkout` 特殊步骤**

```json
{
  "steps": [
    {
      "name": "checkout",
      "uses": "checkout@v1",
      "with": {
        "repository": "github.com/kexer2018/miniflow",
        "ref": "main",
        "ssh_key": "${{ secrets.GIT_SSH_KEY }}",
        "shallow": true,
        "depth": 50
      }
    }
  ]
}
```

实现方式：
- `uses` 字段标记为内建动作，不走 Docker 容器
- 引擎内部调用 git CLI（通过 `os/exec`）或 go-git 库
- 在 workspace 中 checkout 代码

**方案 B：注入 git 容器步骤**

```json
{
  "steps": [
    {
      "name": "checkout",
      "image": "alpine/git:latest",
      "commands": [
        "git clone --depth 50 --branch main git@github.com:kexer2018/miniflow.git /workspace"
      ],
      "env": ["GIT_SSH_KEY=${{ secrets.GIT_SSH_KEY }}"]
    }
  ]
}
```

**推荐方案 A**：
- 更好的 UX（不需要用户写 git 命令）
- 能自动处理认证、SSH key 注入、submodule 等复杂性
- 未来可以内置缓存优化（如 `--reference`）

#### 认证方式

| 方式 | 适用场景 | 实现 |
|------|----------|------|
| SSH key | 私有仓库 | 写入临时 SSH key，`ssh-agent` 注入容器 |
| Token | GitHub/GitLab API | `Authorization: Bearer` header |
| HTTP Basic | 自托管 Git | 嵌入 URL |
| OAuth | CI 触发场景 | 通过 webhook payload 携带 |

#### 性能优化

- Shallow clone（`--depth 1`）—— 默认选项
- 稀疏 checkout（仅取需要的目录）
- 文件变更列表（`git diff --name-only HEAD~1`）用于条件触发

---

### 4.2 工件（Artifact）管理

#### 需求

构建产物需要持久化保存、跨步骤传递、跨管道共享。

#### 设计方案

```json
{
  "steps": [
    {
      "name": "build",
      "image": "golang:1.23-alpine",
      "commands": ["go build -o /workspace/bin/app ."],
      "artifacts": {
        "paths": ["/workspace/bin/"],
        "retention_days": 30
      }
    }
  ]
}
```

#### 存储后端

| 后端 | 优点 | 缺点 |
|------|------|------|
| 本地磁盘 | 零依赖，简单 | 不持久（容器重启丢失） |
| 挂载卷 | 持久化，简单 | 不能共享到其他机器 |
| MinIO / S3 | 分布式，可共享 | 需要外部服务 |
| SQLite BLOB | 统一存储 | 大文件性能差 |

**建议路线**：本地磁盘 → 挂载卷 → S3/MinIO（渐进增强）

#### 关键功能

- `upload` / `download` 动作（与 `commands` 同级）
- 自动文件 glob 匹配
- 去重（content hash）
- 清理策略（TTL / 保留数量）
- 跨管道引用（`from_pipeline`）

---

### 4.3 密钥（Secret）管理

#### 需求

管道中的密码、token、key 不应该出现在 JSON/YAML 明文里。

#### 设计方案

**Spec 层定义：**

```json
{
  "secrets": {
    "DOCKER_USERNAME": "${{ env.DOCKER_USERNAME }}",
    "DOCKER_PASSWORD": "${{ secrets.DOCKER_PASSWORD_FILE }}",
    "GIT_SSH_KEY": "${{ file.ssh_key }}"
  },
  "steps": [
    {
      "name": "login",
      "image": "docker:latest",
      "commands": ["docker login -u $DOCKER_USERNAME -p $DOCKER_PASSWORD"],
      "secrets": ["DOCKER_USERNAME", "DOCKER_PASSWORD"]
    }
  ]
}
```

#### 注入来源优先级

```
CLI --secret KEY=VAL > 环境变量 SECRETS_* > vault.enc 文件 > 密钥管理服务
```

#### 安全措施

- 日志脱敏（已有 sanitizer，需扩展 pattern 覆盖 secret 名称）
- 内存零值覆写（`defer zero(secret)`）
- 文件权限 `0600`
- 仅注入明确引用的步骤

---

### 4.4 环境管理

#### 需求

```json
{
  "env": {
    "GO_VERSION": "1.23",
    "NODE_ENV": "production"
  },
  "steps": [
    {
      "name": "build",
      "image": "golang:${GO_VERSION}-alpine",
      "commands": ["go version"]
    }
  ]
}
```

#### 变量替换

- `${{ env.VAR }}` — spec 层模板替换
- `${VAR}` — 运行时 shell 变量（传给容器）
- 支持嵌套、条件默认值

---

## 5. 调度执行层 — 详细分析

### 5.1 并行执行引擎

#### 需求

拓扑排序后，同一 DAG 层次的无依赖步骤应该并发执行以提升速度。

```
当前：  checkout → test → build → deploy   （全串行，慢）
目标：  checkout → [test, lint] → build → deploy  （test 和 lint 并行）
```

#### 算法

```
1. TopologicalSort() 生成分层列表
2. 每层中的步骤无依赖关系，可以并发运行
3. 并发池控制最大并行数（max_concurrency）
4. 层的所有步骤完成后，进入下一层
```

#### 并发控制

```json
{
  "concurrency": {
    "max_concurrency": 4,
    "cancel_in_progress": true
  }
}
```

| 参数 | 说明 | 默认 |
|------|------|------|
| `max_concurrency` | 最大并行步骤数 | CPU 核心数 |
| `cancel_in_progress` | 新触发时取消正在运行的 | `false` |
| `group` | 并发分组（同名组共享限制） | 无 |

#### 资源限制

- 每步 CPU/Memory 限制（Docker 原生支持）
- 全局资源池（防止容器撑爆宿主机）
- 资源预留（高优先级步骤保证资源）

---

### 5.2 触发引擎

#### 需求

当前只有手动 CLI 运行，真正的 CI/CD 需要自动触发。

#### 触发类型

```
┌────────────────────────────────────────────┐
│              触发引擎                       │
│                                            │
│  外部事件 ──→ Webhook Listener             │
│  定时     ──→ Cron Scheduler               │
│  API      ──→ REST API / gRPC              │
│  上游完成 ──→ Pipeline Completion Hook     │
│  手动     ──→ CLI / UI                     │
│                                            │
│  ┌──────┐  ┌──────┐  ┌──────┐  ┌──────┐  │
│  │去重   │  │频率  │  │分支   │  │路径   │  │
│  │去重   │  │限制   │  │过滤   │  │过滤   │  │
│  └──────┘  └──────┘  └──────┘  └──────┘  │
└────────────────────────────────────────────┘
```

#### Spec 定义

```json
{
  "on": {
    "push": {
      "branches": ["main", "release/*"],
      "paths": ["src/**", "!docs/**"],
      "tags": ["v*"]
    },
    "pull_request": {
      "branches": ["main"],
      "types": ["opened", "synchronize"]
    },
    "schedule": [
      { "cron": "0 6 * * 1", "timezone": "Asia/Shanghai" }
    ],
    "workflow_dispatch": {
      "inputs": {
        "environment": { "type": "choice", "options": ["dev", "staging", "prod"] }
      }
    }
  }
}
```

#### Webhook 处理器

| 平台 | 认证 | 事件格式 |
|------|------|----------|
| GitHub | Webhook Secret + HMAC 校验 | Push, Pull Request, Release |
| GitLab | Token + IP 白名单 | Push, Merge Request, Tag |
| Gitea | Token | Push, Pull Request |
| 通用 HTTP | Bearer Token | JSON body → 自定义映射 |

**最小实现**：先支持 GitHub webhook，通过监听 → 解析 payload → 匹配 `on` 条件 → 触发管道。

---

### 5.3 重试策略

#### 需求

网络抖动、镜像拉取超时、临时资源争用——偶发失败不应该导致整个管道失败。

#### Spec 定义

```json
{
  "steps": [
    {
      "name": "test",
      "image": "golang:1.23-alpine",
      "commands": ["go test ./..."],
      "retry": {
        "max_attempts": 3,
        "backoff": "exponential",
        "initial_interval": "5s",
        "max_interval": "60s",
        "retry_on": [1, 125, 137, 255],
        "retry_when": ["infra_error", "timeout"]
      }
    }
  ]
}
```

#### 退避策略

| 策略 | 说明 | 适用场景 |
|------|------|----------|
| `immediate` | 立即重试 | 并发竞争 |
| `fixed` | 固定间隔 | 资源等待 |
| `exponential` | 指数退避 | 网络/限流 |
| `jittered` | 指数退避 + 随机抖动 | 大规模重试 |

#### 重试条件

- **按退出码**：`retry_on` 列表
- **按分类**：`infra_error` 自动重试，`app_error` 不重试
- **按超时**：超时失败可以重试
- **自定义**：`retry_when` 表达式

---

### 5.4 条件执行引擎

#### 需求

按分支、环境、前一步输出决定是否执行某一步。

#### Spec 定义

```json
{
  "steps": [
    {
      "name": "deploy-prod",
      "image": "alpine:latest",
      "commands": ["deploy.sh"],
      "if": "branch == 'main' && env == 'production'"
    },
    {
      "name": "notify",
      "image": "alpine:latest",
      "commands": ["echo 'build failed'"],
      "if": "failure()"   // 仅在管道失败时执行
    }
  ]
}
```

#### 条件表达式

| 表达式 | 说明 |
|--------|------|
| `branch == 'main'` | 当前分支 |
| `tag =~ 'v.*'` | 标签匹配正则 |
| `env == 'production'` | 环境变量 |
| `steps.test.status == 'success'` | 前一步状态 |
| `steps.build.exit_code == 0` | 前一步退出码 |
| `failure()` | 管道中的任意一步失败 |
| `success()` | 所有前置步骤成功 |
| `always()` | 无论成功失败都执行 |
| `changed('src/**')` | 指定路径有变更 |

#### 内置函数

- `success()` / `failure()` / `always()` / `cancelled()`
- `changed(path)` — 仅 PR/Push 场景
- `env(key)` — 环境变量
- `contains(list, val)` — 列表包含

---

### 5.5 超时控制

#### 需求

当前有 `Step.Timeout` 字段，但没有管道级超时和 CLI 暴露。

#### 设计方案

```json
{
  "timeout": 600,
  "steps": [
    {
      "name": "long-test",
      "timeout": 120
    }
  ]
}
```

| 级别 | 字段 | 默认 | 行为 |
|------|------|------|------|
| 管道级 | `timeout` | 3600s | 超时 → 取消所有运行中步骤 |
| 步骤级 | `steps[].timeout` | 继承管道 | 超时 → 取消该步骤 |
| 全局默认 | `--default-timeout` | 3600s | CLI/Config 配置 |

---

### 5.6 分布式执行（Phase 2 完整形态）

```
┌────────────────────────────────────────────┐
│               Control Plane                │
│                                            │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐ │
│  │ 任务调度  │  │ 节点管理  │  │ API Gateway│
│  │ 排队+分发 │  │ 注册+健康 │  │ REST/gRPC│
│  └──────────┘  └──────────┘  └──────────┘ │
└─────────────────┬──────────────────────────┘
                  │  gRPC / NATS / Redis 队列
    ┌─────────────┼─────────────┐
    ▼             ▼             ▼
┌──────────┐ ┌──────────┐ ┌──────────┐
│ Worker 1 │ │ Worker 2 │ │ Worker 3 │
│ 执行步骤  │ │ 执行步骤  │ │ 执行步骤  │
└──────────┘ └──────────┘ └──────────┘
```

**当前 `cmd/worker/`** 已经有骨架代码，可以直接朝这个方向演进。

---

## 6. 用户交互层 — 详细分析

### 6.1 CLI 强化

#### 当前命令

```
miniflow                  # 执行管道
miniflow validate -f ...  # 校验管道
miniflow diagnose ...      # AI 诊断日志
miniflow version          # 版本
```

#### 目标命令集

```
miniflow
├── run          <file>                    # 执行管道（已有）
├── validate     <file>                    # 校验管道定义（已有）
├── diagnose     --step --log              # AI 诊断（已有）
│
├── list         [--limit] [--status]      # 查看执行历史（新增）
├── logs         <run-id> [--step]         # 查看步骤日志（新增）
├── status       <run-id>                  # 查看执行状态（新增）
├── rerun        <run-id> [--step]         # 重新执行（新增）
├── cancel       <run-id>                  # 取消执行（新增）
│
├── trigger      --event push              # 手动触发 webhook（新增）
├── secret       set/get/rm                # 密钥管理（新增）
│
├── version                                # 版本（已有）
└── help                                   # 帮助
```

#### 输出格式

| 标志 | 说明 |
|------|------|
| `--output json` | JSON 格式（适合脚本消费） |
| `--output table` | 表格格式（默认） |
| `--output ansi` | ANSI 彩色（当前风格） |

---

### 6.2 Web UI（Phase 2）

> 当前 `internal/api/` 有 REST 骨架（6 路由），可以在此基础上构建。

#### 页面规划

```
Dashboard
├── 管道列表（状态、时间、触发方式）
├── 实时执行状态（WebSocket 推送）
├── 查看步骤日志（流式）
├── 诊断结果可视化（图表/卡片）
├── 历史趋势（成功/失败率）
└── 管道配置编辑器（JSON/YAML 编辑器 + 语法校验）
```

#### 技术栈建议

| 层 | 技术 | 理由 |
|------|------|------|
| API 框架 | `chi` 或 `gin` | `http.ServeMux` 够用但缺中间件生态 |
| WebSocket | `gorilla/websocket` | 实时日志推送 |
| 前端 | 独立 SPA（React/Vue），或嵌入式 htmx | 看团队技术栈 |
| 认证 | Session / JWT / OAuth2 Proxy | 最少初始方案：静态 Token |

---

### 6.3 通知集成

#### 需求

管道完成或失败时，主动通知相关人员。

#### 设计

```json
{
  "notifications": [
    {
      "on": ["failure", "success"],
      "to": {
        "type": "slack",
        "webhook_url": "${{ secrets.SLACK_WEBHOOK }}",
        "channel": "#ci-alerts"
      }
    },
    {
      "on": ["failure"],
      "to": {
        "type": "email",
        "recipients": ["team@example.com"]
      }
    }
  ]
}
```

#### 通知渠道（按优先级）

| 渠道 | 实现方式 | 优先级 |
|------|----------|--------|
| Slack | Incoming Webhook | ⭐ 第一优先 |
| 飞书 | Webhook | ⭐ |
| 钉钉 | Webhook | ⭐ |
| Email | SMTP | 🟡 |
| WebSocket | UI 内实时通知 | 🟡 |
| 自定义 | `webhook_url` 通用回调 | 🟢 |

---

## 7. AI Native 差异化能力

这是 miniflow 与 Jenkins/GitHub Actions 最大的不同点。AI 不是附加功能，而是内建在核心流程中的。

### 7.1 智能诊断（已实现 ✅）

```
失败 → 脱敏 → 分类 → RAG 匹配 → LLM 分析 → 输出根因+修复
```

已有：快速路径、降级、结构化输出。维持现有设计，持续优化。

### 7.2 智能重试（新增）

```
失败 → 分类
  ├── app_error（代码 bug）→ 不重试，触发 AI 诊断
  ├── infra_error（基础设施）→ 自动重试（指数退避）
  ├── timeout → 重试（更长的超时）
  └── unknown → LLM 判断是否值得重试
```

AI 判断是否重试比固定规则更聪明：
```json
{
  "retry": {
    "ai_decision": true,
    "max_attempts": 3
  }
}
```

当 `ai_decision: true` 时，每次失败后调用 LLM 判断：
- 是临时问题 → 重试
- 是代码 bug → 生成诊断，不重试
- 不确定 → 保守重试一次

### 7.3 自动修复执行（已有基础）

当前 `fix_suggestion` 包含 `config_override_example`，可以更进一步：

```
诊断完成 → 有修复建议 → 置信度 ≥ 0.85 → 用户确认 → 自动执行修复
                                              → 自动重新运行
```

```json
{
  "auto_fix": {
    "enabled": true,
    "min_confidence": 0.85,
    "require_approval": true,
    "max_iterations": 3
  }
}
```

### 7.4 动态 RAG（优化）

当前种子是静态的（内置 17 个 + YAML 文件），可以做得更智能：

```
诊断完成
  ├── 高置信度 → 根因 + 日志模式 → 自动生成新种子 → 加入本地种子库
  ├── 低置信度 → 存入候选库 → 人工审核后加入种子库
  └── 重复问题 → 更新已有种子评分/优先级
```

### 7.5 多步骤联合诊断（新增）

当前 `diagnose` 只分析单一步骤的日志，但很多故障需要跨步骤分析：

```
场景：
  step-1: build → 成功
  step-2: test → 失败，日志显示 "package not found"
  
联合诊断：
  检查 step-1 的输出 + step-2 的日志
  → 根因：build 步骤未正确安装依赖
  → step-1 标记为 "suspect"，step-2 是 "symptom"
```

### 7.6 Prompt 优化

当前 Chinese prompt 是好的开始，可以持续优化：

- 内置多语言模板（zh-CN / en-US）
- 根据 `$LANG` 自动选择
- 系统 prompt 定期通过单元测试验证输出结构
- 不同分类场景使用不同 prompt 模板

---

## 8. Spec 演进设计

### 8.1 版本升级路线

```
v1.0（当前 JSON 格式）
│
├── + source        # 源码定义
├── + secrets       # 密钥声明
├── + env           # 环境变量
├── + on            # 触发条件
├── + timeout       # 管道级超时
├── + concurrency   # 并发控制
├── + notifications # 通知配置
│
v2.0（扩展格式）
│
├── uses 动作系统    # 内建动作 + 自定义步骤
├── artifacts       # 工件声明
├── retry            # 重试策略
├── when/if          # 条件执行
├── 嵌套步骤         # 步骤组
├── 矩阵执行         # 多版本并行测试
│
v3.0（高级）
├── 参数化管道       # 输入参数
├── 环境部署门禁     # 审批流程
├── AI 策略         # ai_decision 等
└── 自定义插件       # 插件注册
```

### 8.2 JSON Schema 演进

建议为每个 spec 版本维护 JSON Schema，提供：
- 编辑器自动补全
- 静态校验（`miniflow validate`）
- AI 辅助生成

当前的 `Validate()` 函数可以逐步扩展为 Schema 校验。

---

## 9. 推荐实施路线

### 三阶段路线

```
第一阶段：CI/CD 可用
├── Git 源码拉取（checkout 内建动作）        ⏱ 1-2 周
├── 并行执行引擎（同层并发）                  ⏱ 1-2 周
├── 密钥注入（env + secret 分离）             ⏱ 1 周
├── 重试策略（指数退避）                      ⏱ 1 周
├── 步骤超时 CLI 暴露                         ⏱ 2 天
└── CI/CD 自己跑起来（dogfooding）             ⏱ 持续

第二阶段：CI/CD 好用
├── 触发引擎（GitHub Webhook → 自动运行）     ⏱ 2 周
├── CLI 强化（list / logs / rerun / cancel）  ⏱ 1 周
├── 条件执行（when / if 表达式引擎）          ⏱ 1-2 周
├── 工件管理（本地存储 → MinIO）              ⏱ 1-2 周
├── 通知集成（Slack / 飞书 / Email）          ⏱ 1 周
└── 管道历史管理（SQLite 扩展 + 清理策略）     ⏱ 3 天

第三阶段：AI Native 差异化
├── 智能重试（AI 判断重试 vs 诊断）            ⏱ 1 周
├── 自动修复执行（应用 config_override）      ⏱ 1-2 周
├── 动态 RAG（诊断结果 → 新种子）              ⏱ 1 周
├── 多步骤联合诊断                            ⏱ 2 周
├── 置信度升级 / 人工审批集成                  ⏱ 1 周
└── Web UI 仪表盘（Phase 2）                   ⏱ 3-4 周
```

### 推荐优先级矩阵

```
                      高价值              中价值              低价值
 ┌─────────────────────────────────────────────────────────────────
 │
 易实施  │  Git checkout        密钥管理          通知集成
        │  并行执行             step 超时         CLI list/logs
        │  重试策略             条件执行
 │
 中实施  │  工件管理            定时触发          审批门禁
        │  Webhook 触发         Smart Retry
 │
 难实施  │  自动修复执行         分布式 Worker      Web UI
        │  多步骤诊断           动态 RAG
 │
```

---

## 10. 附录：真实 CI 管道完整示例

### Go 项目完整 CI 管道

```json
{
  "version": "2.0",
  "name": "go-ci-full",
  "workspace": "/workspace",

  "source": {
    "repository": "github.com/myorg/myapp",
    "ref": "${{ github.ref_name }}",
    "token": "${{ secrets.GIT_TOKEN }}"
  },

  "secrets": {
    "DOCKER_USERNAME": "${{ env.REGISTRY_USER }}",
    "DOCKER_PASSWORD": "${{ secrets.REGISTRY_PASSWORD }}",
    "SLACK_WEBHOOK": "${{ secrets.SLACK_CI_WEBHOOK }}"
  },

  "env": {
    "GO_VERSION": "1.23",
    "NODE_VERSION": "22"
  },

  "on": {
    "push": { "branches": ["main", "release/*"] },
    "pull_request": { "branches": ["main"] },
    "schedule": [{ "cron": "0 2 * * 1", "timezone": "Asia/Shanghai" }]
  },

  "concurrency": {
    "max_concurrency": 4,
    "cancel_in_progress": true
  },

  "timeout": 1800,

  "notifications": [
    {
      "on": ["failure"],
      "to": { "type": "slack", "webhook_url": "${{ secrets.SLACK_WEBHOOK }}", "channel": "#ci-alerts" }
    },
    {
      "on": ["success"],
      "to": { "type": "email", "recipients": ["team@example.com"] }
    }
  ],

  "steps": [
    {
      "name": "checkout",
      "uses": "checkout@v1",
      "with": {
        "shallow": true,
        "depth": 50,
        "submodules": true
      }
    },

    {
      "name": "lint",
      "image": "golang:${GO_VERSION}-alpine",
      "commands": [
        "go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest",
        "golangci-lint run ./... --timeout=5m"
      ],
      "retry": { "max_attempts": 2, "backoff": "fixed", "interval": "10s" },
      "timeout": 300
    },

    {
      "name": "vet",
      "image": "golang:${GO_VERSION}-alpine",
      "commands": ["go vet ./..."],
      "timeout": 120
    },

    {
      "name": "test-unit",
      "image": "golang:${GO_VERSION}-alpine",
      "commands": ["go test ./... -race -count=1 -coverprofile=coverage.out -timeout=120s"],
      "timeout": 180
    },

    {
      "name": "build",
      "image": "golang:${GO_VERSION}-alpine",
      "commands": ["CGO_ENABLED=0 go build -ldflags='-s -w' -o /workspace/bin/app ."],
      "depends_on": ["lint", "vet", "test-unit"],
      "retry": { "max_attempts": 2, "backoff": "exponential" },
      "timeout": 300,
      "artifacts": {
        "paths": ["/workspace/bin/"],
        "retention_days": 30
      }
    },

    {
      "name": "docker-build",
      "image": "docker:latest",
      "commands": [
        "docker build -t myapp:latest .",
        "docker tag myapp:latest registry.example.com/myapp:${{ github.sha }}",
        "docker push registry.example.com/myapp:${{ github.sha }}"
      ],
      "depends_on": ["build"],
      "env": ["DOCKER_USERNAME", "DOCKER_PASSWORD"],
      "secrets": ["DOCKER_USERNAME", "DOCKER_PASSWORD"],
      "if": "branch == 'main'",
      "timeout": 600
    },

    {
      "name": "deploy-staging",
      "image": "alpine:latest",
      "commands": ["echo 'Deploying to staging...'", "curl -X POST https://deploy.example.com/staging"],
      "depends_on": ["docker-build"],
      "if": "branch == 'main'"
    },

    {
      "name": "deploy-production",
      "image": "alpine:latest",
      "commands": ["echo 'Deploying to production...'", "curl -X POST https://deploy.example.com/prod"],
      "depends_on": ["docker-build"],
      "if": "startsWith(github.ref_name, 'release/')",
      "timeout": 600,
      "retry": { "max_attempts": 3, "backoff": "jittered" }
    },

    {
      "name": "notify-success",
      "image": "alpine:latest",
      "commands": ["echo 'Pipeline completed successfully!'"],
      "depends_on": ["deploy-staging", "deploy-production"],
      "if": "success()"
    },

    {
      "name": "notify-failure",
      "image": "alpine:latest",
      "commands": ["echo 'Pipeline failed! Triggering AI diagnosis...'"],
      "depends_on": ["test-unit", "lint", "vet", "build"],
      "if": "failure()"
    }
  ]
}
```

### 最小可用管道（快速验证）

```json
{
  "version": "1.0",
  "name": "quick-check",
  "source": {
    "repository": "github.com/kexer2018/miniflow",
    "ref": "main"
  },
  "steps": [
    { "name": "checkout", "uses": "checkout@v1" },
    { "name": "test", "image": "golang:1.23-alpine", "commands": ["go test ./... -short -count=1"] },
    { "name": "build", "image": "golang:1.23-alpine", "commands": ["go build ./..."], "depends_on": ["test"] }
  ]
}
```

---

> **下一步**: 本分析文档已经涵盖了 miniflow 作为 CI/CD 系统的完整功能画像。团队可以根据优先级和资源决定从哪个模块开始实施。
>
> 建议第一优先级实现 **Git 源码拉取** + **并行执行引擎**，让 pipeline 跑真正的 Go 项目而不是 `echo` 模拟。
