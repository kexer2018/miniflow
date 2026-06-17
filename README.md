# miniflow — AI 原生轻量级 CI/CD 执行引擎

[![Go Version](https://img.shields.io/badge/Go-1.23%2B-00ADD8?logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/license-MIT-green)](LICENSE)

**miniflow** 是一个 AI 原生的轻量级 CI/CD 执行引擎，使用 Go 编写。它解析基于 DAG（有向无环图）的流水线定义（JSON），在本地 Docker 容器中按拓扑顺序执行各步骤，实时采集日志，并通过可插拔的 LLM 层进行智能错误分析与自动修复。

> ⚡ **设计理念**：相比 Jenkins/GitHub Actions 等重量级 CI/CD 系统，miniflow 追求极致的简洁——一条命令启动，一个 JSON 文件定义流水线，依赖仅需 Docker。它为 AI 时代而生，内置日志脱敏、确定性分类和 LLM 驱动修复管道。

---

## 特性

- **DAG 流水线** — 通过 `depends_on` 定义步骤依赖，自动拓扑排序与循环检测
- **Docker 容器执行** — 每步运行在独立的临时容器中，支持任意镜像（`alpine`, `golang`, `node` 等）
- **共享工作空间** — 所有步骤共享宿主机绑定的工作目录，UID 统一为 `1000:1000`
- **日志脱敏** — 内置 8 条正则规则（JWT、AWS Key、SSH Key、GitHub Token 等），保守脱敏策略
- **日志分类** — 确定性信号词引擎，区分应用错误（`app_error`）与基础设施错误（`infra_error`）
- **缓存支持** — 步骤级缓存挂载，支持基于内容的缓存键
- **RAG 种子案例** — YAML 格式的错误模式库，用于 AI 修复引擎的知识检索
- **REST API 骨架** — 健康检查、流水线历史查询、修复建议接口（Phase 2 预留）
- **SQLite 持久化** — 纯 Go SQLite 驱动，零依赖，流水线执行结果持久化存储
- **双入口设计** — CLI 工具 + 守护进程（Worker），为分布式架构预留

## 架构

```
                        ┌──────────────────┐
                        │  Pipeline JSON    │
                        │  (DAG 定义)        │
                        └────────┬─────────┘
                                 │
                    ┌────────────▼────────────┐
                    │    CLI (cobra)          │
                    │   cmd/miniflow/main.go  │
                    └────────────┬────────────┘
                                 │
              ┌──────────────────┼──────────────────┐
              ▼                  ▼                  ▼
    ┌─────────────────┐ ┌──────────────┐ ┌────────────────┐
    │  DAG 引擎         │ │  Docker 管理器 │ │  日志管道        │
    │  (拓扑排序 /      │ │ (容器创建/     │ │ (采集→脱敏→分类)  │
    │   循环检测 /       │ │  启动/清理)    │ │                │
    │   执行调度)        │ │              │ │                │
    └─────────────────┘ └──────────────┘ └────────────────┘
              │
    ┌─────────▼─────────┐
    │   LLM 层 (Phase 1B)│
    │   AI 错误分析 &    │
    │   自动修复建议      │
    └───────────────────┘
```

### 执行流程

1. CLI 读取 JSON → `PipelineSpec` 校验（名称/版本/步骤完整性）
2. Docker 客户端自动检测 socket（OrbStack / Docker Desktop / 标准 socket）
3. 在 `/tmp/miniflow/workspaces/{pipeline-id}` 创建共享工作空间
4. DAG 拓扑排序（Kahn 算法）→ 循环依赖检测 → 孤立节点检查
5. 步骤串行执行（Phase 1）：
   - 每步创建独立容器（`--user 1000:1000`，工作空间挂载）
   - 命令通过 `/bin/sh -c` 包装，支持多行 Shell 语法
   - 智能镜像拉取：自动检测本地缓存，不存在则拉取
   - 容器日志收集 → 手动清理容器（`AutoRemove: false` 确保日志可读）
6. 步骤失败 → 后续步骤标记为 `skipped`，流水线终止
7. `Ctrl+C` 优雅取消 → 取消当前步骤，后续标记为 `cancelled`
8. 结果打印（步骤状态、退出码、失败时的日志输出）

## 快速开始

### 前置条件

- **Go 1.23+**（构建需要）
- **Docker**（运行时需要，支持 OrbStack / Docker Desktop / 标准 Docker）
- **无需 CGO**（纯 Go SQLite 驱动 `modernc.org/sqlite`）

### 安装

```bash
# 克隆仓库
git clone https://github.com/kexer2018/miniflow.git
cd miniflow

# 构建 CLI
make build-miniflow

# 或者构建全部
make build

# 二进制文件输出到 bin/
./bin/miniflow version
```

### 运行示例流水线

```bash
./bin/miniflow -f examples/go-ci.json
```

### 验证流水线定义

```bash
./bin/miniflow validate -f examples/go-ci.json
```

### 启用调试日志

```bash
./bin/miniflow -v -f examples/go-ci.json
```

## 流水线定义

### JSON 格式

```json
{
  "version": "1.0",
  "name": "my-pipeline",
  "workspace": "/workspace",
  "steps": [
    {
      "name": "checkout",
      "image": "alpine:latest",
      "commands": ["echo checkout"],
      "depends_on": []
    },
    {
      "name": "test",
      "image": "golang:1.22",
      "commands": [
        "go test ./... -v"
      ],
      "depends_on": ["checkout"],
      "cache": {
        "path": "/go/pkg/mod",
        "key": "go-mod-{{ checksum \"go.sum\" }}"
      }
    }
  ]
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `version` | string | 否 | Schema 版本，默认为 `"1.0"` |
| `name` | string | 是 | 流水线名称 |
| `workspace` | string | 否 | 容器内共享工作空间路径（默认 `/workspace`） |
| `steps` | array | 是 | 步骤列表（至少 1 个） |

### 步骤定义

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `name` | string | 是 | 步骤唯一名称 |
| `image` | string | 是 | 容器镜像（如 `"golang:1.22"`） |
| `commands` | array | 是 | 按顺序执行的命令（Shell 语法） |
| `depends_on` | array | 否 | 依赖的步骤名称列表 |
| `cache` | object | 否 | 缓存挂载配置（`path` + `key`） |
| `env` | array | 否 | 环境变量（`"K=V"` 格式） |

### 完整示例

查看 [`examples/go-ci.json`](examples/go-ci.json)：

```json
{
  "version": "1.0",
  "name": "go-ci",
  "workspace": "/workspace/go-project",
  "steps": [
    {
      "name": "checkout",
      "image": "alpine:latest",
      "commands": [
        "echo '=== checkout step ==='",
        "ls -la /workspace"
      ]
    },
    {
      "name": "test",
      "image": "alpine:latest",
      "commands": ["echo 'running tests...'"],
      "depends_on": ["checkout"]
    },
    {
      "name": "build",
      "image": "alpine:latest",
      "commands": ["echo 'building...'"],
      "depends_on": ["test"]
    }
  ]
}
```

## 项目结构

```
├── cmd/
│   ├── miniflow/         # CLI 入口 (cobra)
│   │   └── main.go       # 流水线执行入口
│   └── worker/           # Worker 守护进程入口 (Phase 2)
│       └── main.go       # 任务监听骨架
│
├── pkg/pipeline/         # 公共 DAG 模型 (内外共用)
│   ├── spec.go           # PipelineSpec / StepSpec / Cache
│   └── errors.go         # 验证错误定义
│
├── internal/
│   ├── pipeline/         # DAG 核心引擎
│   │   ├── types.go      # Pipeline / Step / Result 类型
│   │   ├── execute.go    # ExecutePipeline 执行引擎
│   │   └── validate.go   # DAG 校验 (Kahn 算法) + 拓扑排序
│   │
│   ├── container/        # Docker 封装
│   │   ├── manager.go    # Manager 接口 / Config / Result
│   │   ├── docker.go     # DockerManager 实现 (socket 检测/镜像管理/容器生命周期)
│   │   └── workspace.go  # 工作空间管理 / UID 统一 / 缓存目录
│   │
│   ├── log/              # 日志管道
│   │   ├── collector.go  # 实时日志采集器
│   │   ├── sanitizer.go  # 脱敏器 (8 条默认规则)
│   │   └── classifier.go # 确定性分类器 (信号词引擎)
│   │
│   ├── llm/              # LLM 抽象层 (Phase 1B)
│   │   └── main.go       # 占位
│   │
│   ├── fixer/            # AI 修复引擎 (Phase 1B)
│   │   └── main.go       # 占位
│   │
│   ├── api/              # REST API (Phase 2)
│   │   ├── handler.go    # HTTP handler / 修复建议 API
│   │   └── router.go     # 路由定义
│   │
│   └── db/               # 持久化层
│       ├── store.go      # 存储接口定义
│       └── sqlite.go     # SQLite 实现 + 自动迁移
│
├── seeds/                # RAG 种子案例
│   ├── auth.yaml         # 凭证认证失败
│   ├── network.yaml      # 网络连接失败
│   └── image-pull.yaml   # 镜像拉取失败
│
├── examples/             # 示例流水线
│   └── go-ci.json        # 3 步 DAG 示例
│
├── docs/                 # 设计文档
│   ├── 方案设计_实施计划.md
│   └── miniflow AI原生轻量级CICD执行引擎架构与技术白皮书.md
│
├── Makefile              # 构建/测试/lint
├── go.mod / go.sum       # Go 依赖
├── CLAUDE.md             # Claude Code 指导
└── .gitignore
```

## 开发

### 构建

```bash
make build          # 构建所有二进制
make build-miniflow # 仅构建 CLI
make build-worker   # 仅构建 Worker
```

### 测试

```bash
make test                    # 运行所有测试 (含 race detector)
make test-short             # 仅运行短测试
make test-coverage          # 测试 + 覆盖率报告
make test-integration       # 集成测试 (需 Docker)
```

### 代码质量

```bash
make vet       # go vet
make fmt       # go fmt
make lint      # golangci-lint
make tidy      # go mod tidy
```

### 清理

```bash
make clean     # 删除构建产物和覆盖率报告
```

## 关键设计决策

### 为什么 Phase 1 只支持串行执行？
保持初始实现极简——串行执行不需要处理并发同步、资源竞争和死锁。DAG 拓扑排序已为 Phase 2 的并行执行打好基础。

### 为什么 `AutoRemove: false`？
Docker 的 `AutoRemove` 会在容器退出后立即删除容器，这会导致日志来不及读取。miniflow 采用手动清理模式：收集日志后再调用 `ContainerRemove`。

### 为什么日志脱敏是保守策略？
宁可漏脱敏，不可误脱敏。8 条规则覆盖常见的密钥格式（JWT、AWS Key、SSH Key、GitHub Token、Docker Auth、npm Token），高熵字符串兜底规则（长度 ≥ 40 的字母数字组合）作为最后防线。

### 为什么日志分类是确定性的而非 LLM 驱动的？
Phase 1 追求可靠性和零延迟——信号词匹配在微秒级完成，无网络依赖，行为可预测。LLM 分类将在 Phase 1B 作为可选的增强层加入。

### 为什么支持 OrbStack？
OrbStack 是 macOS 上比 Docker Desktop 更轻量的替代方案，macOS 开发者的主流选择之一。自动检测 `~/.orbstack/run/docker.sock` 确保零配置体验。

## 路线图

| Phase | 特性 | 状态 |
|-------|------|------|
| 1 | DAG 解析 + 串行容器执行 + 日志管道 | ✅ 完成 |
| 1B | LLM 错误分析 + AI 自动修复 + RAG 引擎 | 🔧 开发中 |
| 2 | Worker 守护进程 + 分布式任务队列 + Web UI | 📋 规划中 |

## 依赖

- [spf13/cobra](https://github.com/spf13/cobra) — CLI 框架
- [docker/docker](https://github.com/docker/docker) — Docker SDK
- [modernc.org/sqlite](https://modernc.org/sqlite) — 纯 Go SQLite 驱动（无 CGO）
- [google/uuid](https://github.com/google/uuid) — UUID 生成

## 许可

MIT
