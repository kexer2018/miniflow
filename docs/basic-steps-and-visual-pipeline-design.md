# miniflow 基础 Step 与可视化流水线产品设计

> 版本: v0.1
> 日期: 2026-07-31
> 状态: 产品与技术设计草案

---

## 1. 产品定位

miniflow 的产品定位不是“内置业务逻辑的 CI/CD 平台”，而是：

> 一个可视化、容器化、可扩展的流水线执行平台。

miniflow 负责提供平台能力：图形化编排、DAG 校验、容器化执行、共享工作空间、缓存、产物、密钥、日志、运行历史和失败诊断。用户负责提供自己的业务逻辑：脚本、命令、仓库、镜像、部署策略和团队流程。

这意味着内置 Step 应该是通用执行原语，而不是面向某种业务或框架的封闭模板。比如可以提供 `Docker build`、`Shell script`、`HTTP request`，但不应该把“Java 项目发布到某公司内部环境”这类业务流程做成核心 Step。

### 1.1 核心原则

1. 平台只提供环境与编排，不接管业务逻辑。
2. 每个 Step 默认使用临时容器执行，容器之间不共享运行时状态。
3. 所有 Step 默认挂载同一个 workspace，使文件产物可以自然传递。
4. 内置 Step 是可编辑模板，不是封闭能力。
5. Shell Script 永远是兜底能力，高级用户可以绕过表单直接写命令。
6. UI 负责降低配置成本和错误率，底层 spec 仍然是稳定协议。

---

## 2. 产品分层

```text
┌─────────────────────────────────────────────┐
│ 可视化编排层                                  │
│ Pipeline canvas / Step palette / Inspector   │
└──────────────────────┬──────────────────────┘
                       │ 生成 / 编辑 pipeline spec
┌──────────────────────▼──────────────────────┐
│ 平台模型层                                    │
│ Step type / DAG / Validation / Policy         │
└──────────────────────┬──────────────────────┘
                       │ 编译成可执行计划
┌──────────────────────▼──────────────────────┐
│ 执行环境层                                    │
│ Docker runner / Workspace / Cache / Artifacts │
└──────────────────────┬──────────────────────┘
                       │ 运行结果、日志、诊断
┌──────────────────────▼──────────────────────┐
│ 可观测与历史层                                │
│ Logs / Status / Runs / Diagnosis / Rerun      │
└─────────────────────────────────────────────┘
```

现有项目已经具备部分执行环境层和可观测能力：Docker 容器执行、workspace 挂载、DAG 校验、SQLite 记录、日志脱敏、RAG/LLM 诊断。下一阶段重点应放在 Step 类型系统、产物/缓存语义、运行状态 API 和可视化编排 UI。

---

## 3. Step 模型设计

### 3.1 推荐 Spec 形态

现有 Step 主要是 `image + commands`。产品化后建议扩展为 typed step：

```json
{
  "name": "build",
  "type": "script.run",
  "image": "golang:1.25",
  "depends_on": ["checkout"],
  "with": {
    "workdir": "/workspace",
    "script": "go test ./...\ngo build -o bin/app ./cmd/miniflow"
  },
  "env": {
    "CGO_ENABLED": "0"
  },
  "secrets": ["GITHUB_TOKEN"],
  "timeout": 600,
  "retry": {
    "max_attempts": 2,
    "backoff_seconds": 10
  },
  "cache": [
    {
      "key": "go-mod-${checksum:go.sum}",
      "path": "/go/pkg/mod"
    }
  ],
  "artifacts": {
    "save": [
      {
        "name": "miniflow-binary",
        "path": "bin/miniflow"
      }
    ]
  }
}
```

### 3.2 通用字段

| 字段 | 说明 |
|------|------|
| `name` | Step 唯一名称，也是图中节点名称 |
| `type` | Step 类型，如 `script.run`、`git.checkout` |
| `image` | 执行镜像。部分系统 Step 可以不需要用户配置 |
| `depends_on` | 上游依赖，主要由画布连线生成 |
| `with` | 当前 Step 类型特有的参数 |
| `env` | 普通环境变量 |
| `secrets` | 密钥引用，不直接保存密钥值 |
| `timeout` | Step 超时时间 |
| `retry` | 重试策略 |
| `cache` | 缓存挂载或恢复保存声明 |
| `artifacts` | 产物保存或恢复声明 |
| `continue_on_error` | 当前 Step 失败后是否继续执行后续节点 |
| `condition` | 后续阶段支持，按分支、变量或上游状态决定是否执行 |

### 3.3 Step 编译模型

后端应维护 Step Type Registry。每个 Step 类型包含：

1. 类型 ID，例如 `script.run`。
2. 展示名称，例如 `Shell Script`。
3. 表单 schema，用于前端生成配置表单。
4. 默认镜像和默认工作目录。
5. 参数校验逻辑。
6. 编译逻辑，将 typed step 编译为底层容器执行配置。

```text
Typed Step
    ↓ validate(type schema)
Executable Step
    ↓ compile
container.Config
    ↓ run
StepResult
```

这样 UI、API、CLI 和 Worker 可以共用同一套 Step 定义，避免前后端各自理解一套字段。

---

## 4. MVP 基础 Step 目录

### 4.1 Git Checkout

用途：把用户代码拉取到 workspace。

建议类型：`git.checkout`

参数：

| 参数 | 类型 | 说明 |
|------|------|------|
| `repository` | string | Git 仓库地址 |
| `ref` | string | 分支、tag 或 commit |
| `target_dir` | string | checkout 到 workspace 下的目录 |
| `shallow` | bool | 是否浅克隆 |
| `depth` | number | 浅克隆深度 |
| `submodules` | bool | 是否拉取 submodule |
| `credential` | string | 凭据引用 |

设计建议：

- 支持 pipeline-level source，也支持作为普通 Step 出现在图中。
- 如果作为普通 Step，用户可以在一个流水线里拉取多个仓库。
- 默认 `target_dir` 为 workspace 根目录。

### 4.2 Shell Script

用途：用户自定义脚本执行，是整个平台的兜底 Step。

建议类型：`script.run`

参数：

| 参数 | 类型 | 说明 |
|------|------|------|
| `image` | string | 执行镜像 |
| `workdir` | string | 容器内工作目录 |
| `shell` | enum | `sh`、`bash`、`powershell` 后续可扩展 |
| `script` | string | 脚本内容 |

设计建议：

- 第一版必须把这个 Step 做强。
- UI 应使用代码编辑器，而不是普通 textarea。
- 提供常用镜像建议，如 `alpine`、`ubuntu`、`golang`、`node`、`python`。

### 4.3 File Operation

用途：对 workspace 文件做通用处理。

建议类型：`file.operation`

参数：

| 参数 | 类型 | 说明 |
|------|------|------|
| `operation` | enum | `copy`、`move`、`delete`、`mkdir`、`archive`、`extract` |
| `source` | string | 源路径 |
| `target` | string | 目标路径 |
| `overwrite` | bool | 是否覆盖 |

设计建议：

- 这类 Step 很适合低代码表单。
- 底层可编译成 shell 命令，也可用 Go 原生执行。
- 第一版建议仍然编译成容器内命令，保持执行模型一致。

### 4.4 Cache Restore

用途：在 Step 执行前恢复依赖缓存。

建议类型：`cache.restore`

参数：

| 参数 | 类型 | 说明 |
|------|------|------|
| `key` | string | 主缓存 key |
| `restore_keys` | array | fallback key 前缀 |
| `path` | string | 恢复到 workspace 或容器路径 |

设计建议：

- 短期可以复用现有 cache mount。
- 中期应区分 mount 型缓存和 restore/save 型缓存。

### 4.5 Cache Save

用途：Step 执行后保存依赖缓存。

建议类型：`cache.save`

参数：

| 参数 | 类型 | 说明 |
|------|------|------|
| `key` | string | 缓存 key |
| `path` | string | 要保存的路径 |
| `overwrite` | bool | 是否覆盖已有缓存 |

设计建议：

- 与 `cache.restore` 配对使用。
- UI 可为常见语言提供 key 建议，例如 `go-mod-${checksum:go.sum}`。

### 4.6 Artifact Save

用途：保存构建产物、测试报告、覆盖率报告等。

建议类型：`artifact.save`

参数：

| 参数 | 类型 | 说明 |
|------|------|------|
| `name` | string | 产物名称 |
| `path` | string | workspace 内路径 |
| `if_no_files` | enum | `error`、`warn`、`ignore` |
| `retention_days` | number | 保留天数 |
| `compress` | bool | 是否压缩保存 |

设计建议：

- 这是产品化 CI/CD 的关键能力。
- 第一版可以保存到本地目录，后续扩展到 S3、MinIO、OSS。

### 4.7 Artifact Restore

用途：恢复当前 run 或历史 run 中保存的产物。

建议类型：`artifact.restore`

参数：

| 参数 | 类型 | 说明 |
|------|------|------|
| `name` | string | 产物名称 |
| `from` | enum | `current_run`、`latest_success`、`run_id` |
| `run_id` | string | 指定 run 时使用 |
| `target_dir` | string | 恢复目录 |

设计建议：

- 第一版优先支持 `current_run`。
- 跨 run 恢复依赖执行历史和 artifact store。

### 4.8 Docker Build

用途：构建容器镜像。

建议类型：`docker.build`

参数：

| 参数 | 类型 | 说明 |
|------|------|------|
| `context` | string | build context |
| `dockerfile` | string | Dockerfile 路径 |
| `tags` | array | 镜像 tag |
| `build_args` | map | build args |
| `target` | string | 多阶段构建 target |
| `platform` | string | 目标平台 |
| `no_cache` | bool | 是否禁用 Docker 缓存 |

设计建议：

- 该 Step 更适合由 runner 后端直接调用 Docker daemon。
- 如果在容器内执行 `docker build`，需要处理 Docker socket 挂载，安全边界更复杂。
- MVP 可以先实现为 shell 模板，但产品化应抽象成原生 Docker operation。

### 4.9 Docker Push

用途：推送镜像到 registry。

建议类型：`docker.push`

参数：

| 参数 | 类型 | 说明 |
|------|------|------|
| `image` | string | 镜像 tag |
| `registry` | string | registry 地址 |
| `credential` | string | registry 凭据引用 |

设计建议：

- 与 `docker.build` 配套。
- 凭据必须通过 secret/credential store 注入，不能明文写在 spec。

### 4.10 HTTP Request

用途：调用 webhook、触发外部系统或做轻量集成。

建议类型：`http.request`

参数：

| 参数 | 类型 | 说明 |
|------|------|------|
| `method` | enum | `GET`、`POST`、`PUT`、`PATCH`、`DELETE` |
| `url` | string | 请求地址 |
| `headers` | map | 请求头 |
| `body` | string | 请求体 |
| `success_codes` | array | 认为成功的 HTTP 状态码 |
| `timeout` | number | 请求超时 |

设计建议：

- 不要第一版就内置大量 SaaS 特定 Step。
- Slack、飞书、企业微信等都可以先通过 HTTP Request 解决。

### 4.11 Manual Approval

用途：人工审批门禁，常用于发布前确认。

建议类型：`approval.manual`

参数：

| 参数 | 类型 | 说明 |
|------|------|------|
| `title` | string | 审批标题 |
| `description` | string | 审批说明 |
| `approvers` | array | 审批人或角色 |
| `timeout` | number | 超时时间 |
| `on_timeout` | enum | `fail`、`continue`、`cancel` |

设计建议：

- 需要后端执行状态机支持 paused/waiting。
- 如果 MVP 暂时没有用户系统，可以先作为单机确认节点。

### 4.12 Notification Webhook

用途：发送运行结果通知。

建议类型：`notify.webhook`

参数：

| 参数 | 类型 | 说明 |
|------|------|------|
| `url` | string | webhook 地址 |
| `event` | enum | `on_success`、`on_failure`、`always` |
| `template` | string | 消息模板 |
| `headers` | map | 请求头 |

设计建议：

- 可以复用 `http.request` 的底层实现。
- 第一版只做 generic webhook，避免过早绑定 Slack/飞书/邮件。

---

## 5. 后端产品化缺口

### 5.1 Step Type Registry

需要新增 Step 类型注册表，统一管理基础 Step。

注册表职责：

1. 暴露 Step 类型列表给前端。
2. 暴露每个 Step 的表单 schema。
3. 校验 `with` 参数。
4. 将 typed step 编译为执行器可运行的 internal step。
5. 提供默认值和示例配置。

### 5.2 产物管理

当前 workspace 可以传递文件，但缺少持久化 artifact 概念。产品化需要：

1. Artifact 元数据表。
2. 本地 artifact 存储目录。
3. 保存、恢复、列出、下载 API。
4. 路径不存在时的策略。
5. 保留时间和清理机制。

### 5.3 缓存语义

当前有 cache mount 雏形，但还需要：

1. key 模板解析，例如 `${checksum:go.sum}`。
2. restore key fallback。
3. cache hit/miss 记录。
4. 手动清理和过期策略。
5. UI 中展示缓存命中状态。

### 5.4 运行状态 API

前端图形化运行需要实时状态：

1. 创建 run。
2. 查询 run。
3. 查询每个 Step 状态。
4. 取消 run。
5. 重跑 run。
6. 从失败 Step 重跑。
7. WebSocket 或 Server-Sent Events 推送状态变化。

### 5.5 日志流

图形化产品不能只在执行结束后看日志。需要：

1. Step 运行中实时日志。
2. 日志脱敏后推送给前端。
3. 原始日志是否保存需要明确权限策略。
4. 前端支持按 Step、级别、关键字过滤。

### 5.6 Secret 与 Credential 管理

产品化后不应只依赖本地 JSON 文件。需要：

1. 创建、更新、删除 secret 的 API。
2. Secret 引用与运行时注入。
3. 前端只显示 secret 名称和状态，不显示值。
4. 支持 Git、Registry、HTTP header 等 credential 场景。

---

## 6. 前端信息架构

推荐采用四区布局：

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

### 6.1 Step Palette

左侧 Step 库按能力分组：

| 分组 | Step |
|------|------|
| Source | Git Checkout |
| Script | Shell Script |
| Files | File Operation |
| Cache | Cache Restore、Cache Save |
| Artifacts | Artifact Save、Artifact Restore |
| Docker | Docker Build、Docker Push |
| Integration | HTTP Request、Notification Webhook |
| Control | Manual Approval |

交互：

1. 拖拽 Step 到画布创建节点。
2. 点击 Step 可查看简短说明和常用场景。
3. 搜索框支持按名称、能力、关键词过滤。
4. 常用 Step 固定在顶部。

### 6.2 DAG Canvas

中间画布负责表达流程结构。

节点内容：

1. Step 名称。
2. Step 类型图标。
3. 运行状态。
4. 执行耗时。
5. 缓存命中或产物数量等小状态。

连线含义：

1. 默认表示 `depends_on`。
2. 连线由上游 Step 指向下游 Step。
3. 创建连线时实时检测循环依赖。
4. 删除连线会同步更新 spec。

运行态：

1. pending：灰色。
2. running：蓝色或动态边框。
3. success：绿色。
4. failed：红色。
5. skipped：浅灰。
6. waiting：用于 manual approval。

### 6.3 Step Inspector

右侧配置面板是产品体验的核心。

推荐 tab：

| Tab | 内容 |
|-----|------|
| Basics | 名称、类型、描述 |
| Configure | 当前 Step 类型独有参数 |
| Environment | env、secrets、credentials |
| Inputs & outputs | cache、artifacts、workspace 路径 |
| Policy | timeout、retry、continue on error |
| Advanced | entrypoint、network、raw spec |

设计原则：

1. 默认只展示常用字段。
2. 高风险字段放入 Advanced。
3. 表单改动实时校验，但保存动作明确。
4. 每个字段都有短 placeholder，不用大段说明文字占据界面。
5. 用户永远可以查看当前 Step 生成的 JSON。

### 6.4 Bottom Panel

底部面板承载运行反馈：

1. Run timeline。
2. 当前 Step 日志。
3. 校验问题。
4. AI 诊断结果。
5. Artifact 列表。

面板应支持折叠，避免挤压画布。

---

## 7. 输入控件设计

### 7.1 Step Name

控件：普通 input。

交互：

1. 创建节点时自动生成默认名，如 `script-1`。
2. 输入时实时检查唯一性。
3. 名称变化同步更新依赖引用。
4. 不允许空格或特殊字符时，应在输入旁明确提示。

### 7.2 Image

控件：combobox。

交互：

1. 支持自由输入任意镜像。
2. 提供常用建议：`alpine:latest`、`ubuntu:latest`、`golang:1.25`、`node:22`、`python:3.13`。
3. 显示“本地是否已存在”或“运行时会拉取”的状态。
4. 可选镜像预热按钮。

### 7.3 Script

控件：代码编辑器。

交互：

1. 支持 shell 高亮。
2. 支持全屏编辑。
3. 支持插入变量、secret 引用和 workspace 路径。
4. 支持基本 lint，例如空脚本、危险命令提示。
5. 支持查看最终生成命令。

不要使用普通 textarea 承载脚本。脚本是核心输入，应当是前端里最舒服的控件之一。

### 7.4 Env

控件：key/value 表格。

交互：

1. 一行一个变量。
2. key 做格式校验。
3. value 支持普通文本和变量引用。
4. 支持从 `.env` 文本粘贴批量导入。
5. 支持 pipeline-level env 和 step-level env 的覆盖提示。

### 7.5 Secrets

控件：secret picker。

交互：

1. 只显示 secret 名称，不显示值。
2. 支持创建新 secret。
3. 支持映射为 env，例如 `GITHUB_TOKEN`。
4. 如果 secret 不存在，运行前校验失败。

### 7.6 Depends On

控件：画布连线为主，Inspector 中只读展示为辅。

交互：

1. 用户主要通过拖线建立依赖。
2. Inspector 中展示上游节点列表。
3. 高级模式允许手动编辑依赖。
4. 修改时实时进行 DAG 校验。

### 7.7 Cache

控件：路径输入 + key builder。

交互：

1. 路径输入提供 workspace 和语言常用目录建议。
2. key builder 支持插入 checksum 表达式。
3. 显示上次运行是否命中缓存。
4. Restore 和 Save 可以作为独立 Step，也可以作为 Script Step 的附加配置。

### 7.8 Artifacts

控件：路径选择 + artifact name input。

交互：

1. 保存路径支持 glob。
2. 运行后展示文件数量和压缩包大小。
3. 路径不存在时按 `error/warn/ignore` 策略处理。
4. Artifact 列表支持下载和复制路径。

### 7.9 Timeout 和 Retry

控件：数字输入、stepper、toggle。

交互：

1. timeout 使用秒或分钟输入，但 spec 中统一保存秒。
2. retry 默认关闭。
3. 打开 retry 后展示 max attempts 和 backoff。
4. 对不可重试 Step 给出提示，例如 manual approval。

### 7.10 HTTP Request

控件：

1. method segmented control。
2. url input。
3. headers key/value 表格。
4. body code editor。
5. success codes token input。

交互：

1. 支持发送测试请求。
2. 支持把 secret 插入 header。
3. 响应预览应脱敏。

---

## 8. 示例 Pipeline

```json
{
  "version": "1.1",
  "name": "go-ci-visual",
  "workspace": "/workspace",
  "steps": [
    {
      "name": "checkout",
      "type": "git.checkout",
      "with": {
        "repository": "https://github.com/kexer2018/miniflow.git",
        "ref": "main",
        "target_dir": "."
      }
    },
    {
      "name": "restore-go-cache",
      "type": "cache.restore",
      "depends_on": ["checkout"],
      "with": {
        "key": "go-mod-${checksum:go.sum}",
        "path": "/go/pkg/mod"
      }
    },
    {
      "name": "test",
      "type": "script.run",
      "image": "golang:1.25",
      "depends_on": ["restore-go-cache"],
      "with": {
        "workdir": "/workspace",
        "shell": "sh",
        "script": "go test ./... -count=1"
      }
    },
    {
      "name": "build",
      "type": "script.run",
      "image": "golang:1.25",
      "depends_on": ["test"],
      "with": {
        "workdir": "/workspace",
        "shell": "sh",
        "script": "go build -o bin/miniflow ./cmd/miniflow"
      },
      "artifacts": {
        "save": [
          {
            "name": "miniflow-cli",
            "path": "bin/miniflow"
          }
        ]
      }
    }
  ]
}
```

---

## 9. 实施优先级

### Phase A: Step 类型系统

1. 扩展 PipelineSpec，支持 `type` 和 `with`。
2. 增加 Step Type Registry。
3. 实现 `script.run` 和 `git.checkout` typed step。
4. 保持现有 `image + commands` spec 兼容。

### Phase B: 运行产品化 API

1. Pipeline CRUD。
2. Run 创建、查询、取消。
3. Step 状态查询。
4. 日志流 API。
5. Validation API。

### Phase C: 产物与缓存

1. Artifact store。
2. Artifact save/restore Step。
3. Cache key 模板解析。
4. Cache restore/save Step。

### Phase D: 可视化编辑器

1. Step palette。
2. DAG canvas。
3. Step inspector。
4. Bottom run/log panel。
5. Raw spec preview。

### Phase E: 集成与控制类 Step

1. Docker build/push。
2. HTTP request。
3. Manual approval。
4. Notification webhook。

---

## 10. 风险与取舍

### 10.1 不要过早插件化

Step Registry 应先以内置注册表实现。等基础 Step 稳定后，再考虑外部插件。过早插件化会拖慢核心产品打磨。

### 10.2 不要过早绑定具体业务

内置 Step 应保持通用。即使将来支持 Kubernetes、SSH Deploy、Helm 等，也应作为可选集成或插件，而不是核心路径。

### 10.3 Docker Build 的安全边界

在容器内挂载 Docker socket 执行 build 简单但风险较高。长期应由 runner 后端原生调用 Docker API，避免把宿主 Docker socket 暴露给用户容器。

### 10.4 Workspace 隐式传递不够产品化

共享 workspace 可以传递文件，但 UI 里仍应鼓励用户声明 artifacts。这样运行历史、下载、审计和跨 run 复用才有清晰模型。

### 10.5 高级能力延后

Matrix、条件执行、并行 group、部署适配器都很有价值，但不应进入第一版基础 Step。第一版目标是让用户能用图形化方式完成最常见的 checkout、script、cache、artifact、docker、webhook 流程。

---

## 11. 一句话总结

miniflow 的基础 Step 应该围绕“提供可视化编排和容器化执行环境”设计。平台给用户可靠的运行容器、共享 workspace、缓存、产物、日志和诊断；用户把自己的业务脚本、镜像和流程放进来。这样产品既有低门槛，又不会被特定业务逻辑绑死。
