# 执行器双模驱动设计（Executor Dual-Mode Design）

> **文档状态**：设计提案 v1.0  
> **当前定位**：远期例外设计，不指导近期开发  
> **上层约束**：以 `docs/product-principles.md` 为准
> **核心思路**：仅当单机 Docker 产品闭环稳定后，再评估 `runs-on` 与非 Docker executor

---

## 零、原则约束

这份文档保留为远期技术预研。近期开发不得按本文推进 Worker、分布式调度、Shell executor、macOS executor、Windows executor 或 AI 自动绑定 `runs-on`。

当前项目主线必须保持：

1. Docker 是唯一主执行生态。
2. 单机 workspace 是近期唯一主运行模型。
3. `script.run` 和用户镜像承载业务逻辑。
4. 图形化只提供基础 Step，不封装业务流程。
5. PipelineSpec 是 CLI、API、UI 的稳定协议。

本文描述的双模能力只有在 Step Registry、Run API、日志流、Artifact/Cache/Secret 模型和可视化编辑器 MVP 稳定后，才能作为高级执行形态重新评估。

---

## 一、设计动机与背景

### 1.1 当前架构的局限性

当前的 `Executor`（位于 `internal/pipeline/execute.go`）直接持有 `container.Manager` 接口，所有 Step 默认进入 Docker 容器执行。按照当前产品原则，这不是短板，而是近期需要保持的核心简化。

```go
// 当前架构——Executor 与 Docker 耦合
type Executor struct {
    containerMgr container.Manager  // 仅支持 Docker
    wsManager    *container.WorkspaceManager
}
```

只有当项目进入远期高级执行形态，并且面对以下无法容器化的真实场景时，才需要重新讨论单执行器模式：

| 场景 | 问题 |
|:---|:---|
| **iOS 打包** | Xcode 只能在 macOS 上运行，无法容器化 |
| **旧版 .NET Framework 编译** | 需要 Windows 宿主机的完整 GAC 注册表 |
| **Windows 桌面安装包签名** | 需要 Windows 证书存储和 Authenticode 工具链 |
| **macOS 代码签名（Notarization）** | 需要 Apple 开发者证书和 `altool`/`notarytool` |
| **物理机/边缘设备测试** | 直接挂载硬件外设（USB、串口、GPIO），无法通过 Docker 透传 |

**结论**：非 Docker executor 是远期例外能力。近期不应为了这些场景提前引入 Worker 能力、`runs-on` 路由或多执行器调度。

### 1.2 `runs-on` 标签体系

远期如果需要支持非 Docker executor，可以在流水线 JSON 中引入 `runs-on` 标签。该字段不应由 AI 在近期自动绑定，也不应进入当前主 schema：

```json
{
  "version": "1.0",
  "name": "cross-platform-build",
  "steps": [
    {
      "name": "lint-and-test",
      "image": "golang:1.25",
      "commands": ["go test ./..."],
      "runs-on": "docker/linux"       ← 远期可选字段
    },
    {
      "name": "ios-archive",
      "image": "",                     ← 无需镜像，或仅作版本提示
      "commands": ["xcodebuild archive -workspace ..."],
      "runs-on": "shell/darwin"        ← 远期可选字段
    },
    {
      "name": "windows-sign",
      "commands": ["signtool sign /f cert.pfx ..."],
      "runs-on": "shell/windows"       ← 远期可选字段
    }
  ]
}
```

Worker 节点注册、Server 亲和性调度和缓存 locality 都属于分布式阶段问题，明确后置。

---

## 二、执行器抽象层架构

### 2.1 核心接口定义

在 `internal/executor/` 包中定义统一的执行器接口，将具体的执行策略与 Pipeline 编排逻辑解耦：

```go
// Package executor 定义执行器抽象层，支持 Docker / Shell 等运行模式。
package executor

import "context"

// RunsOn 表示 Worker 支持的执行环境标签。
type RunsOn string

const (
    RunsOnDockerLinux RunsOn = "docker/linux"   // Docker 容器（通用 Linux）
    RunsOnShellDarwin RunsOn = "shell/darwin"    // Shell 子进程（macOS 特化）
    RunsOnShellWindows RunsOn = "shell/windows"  // PowerShell 子进程（Windows 特化）
)

// Config 是执行器通用的步骤执行配置。
type Config struct {
    // 步骤基本信息
    Name      string
    Image     string           // Docker Executor 使用；Shell Executor 可能用作版本匹配提示
    Commands  []string
    Env       map[string]string

    // 工作空间
    WorkDir   string           // 工作目录（容器内视角 or 宿主机绝对路径）
    Workspace WorkspaceConfig

    // 缓存
    Cache     []CacheMount

    // 运行时约束
    Timeout   int              // 秒，0 表示不限制
    Network   bool             // 是否启用网络
}

type WorkspaceConfig struct {
    SourcePath string           // 宿主机上的共享工作空间路径
    MountPath  string           // 执行器内部的目标路径（容器内 or 子进程 cwd）
}

type CacheMount struct {
    Source   string
    Target   string
    ReadOnly bool
}

// Result 是统一的执行结果。
type Result struct {
    Status     string // "success" | "failed" | "cancelled" | "timeout"
    ExitCode   int
    Output     string // stdout + stderr 合并
    Error      string // 非正常退出的错误描述
    StartedAt  int64  // Unix 毫秒时间戳
    FinishedAt int64
}

// Executor 是所有执行器必须实现的接口。
type Executor interface {
    // RunsOn 返回此执行器支持的标签。
    RunsOn() RunsOn

    // Execute 执行一个步骤，等待完成并返回结果。
    Execute(ctx context.Context, cfg Config) (*Result, error)

    // HealthCheck 检查执行器所需的后端服务是否可用。
    // 例如：Docker Executor 检查 dockerd 是否可达；
    // Shell Executor 检查必要的运行时工具链是否存在。
    HealthCheck(ctx context.Context) error

    // Close 释放执行器持有的资源。
    Close() error
}
```

### 2.2 工厂方法与自动路由

```go
// Registry 是执行器注册表，Worker 启动时初始化。
type Registry struct {
    executors map[RunsOn]Executor
}

// NewRegistry 根据 Worker 自身环境自动注册可用的执行器。
func NewRegistry() *Registry {
    r := &Registry{executors: make(map[RunsOn]Executor)}

    // 检查 Docker 是否可用
    if dockerExec, err := NewDockerExecutor(); err == nil {
        r.executors[dockerExec.RunsOn()] = dockerExec
    }

    // 远期例外路径：只有明确启用非 Docker executor 时才注册 Shell Executor
    r.executors[RunsOnShellDarwin] = NewShellExecutor()
    r.executors[RunsOnShellWindows] = NewShellExecutor()

    return r
}

// Select 根据 runs-on 标签选择匹配的执行器。
// 如果指定标签不存在，fallback 到最相近的执行器。
func (r *Registry) Select(runsOn RunsOn) Executor {
    if exec, ok := r.executors[runsOn]; ok {
        return exec
    }
    // Fallback 策略：docker/linux 优先，否则用本地 shell
    if exec, ok := r.executors[RunsOnDockerLinux]; ok {
        return exec
    }
    return r.executors[RunsOnShellDarwin] // 最后手段
}
```

### 2.3 执行流程总览

```
                            ┌──────────────┐
                            │  Step JSON   │
                            │ runs-on: xxx │
                            └──────┬───────┘
                                   │
                                   ▼
                    ┌──────────────────────────┐
                    │  Executor Router         │
                    │  (Registry.Select)       │
                    └──────────┬───────────────┘
                               │
                ┌──────────────┴──────────────┐
                │                              │
                ▼                              ▼
    ┌─────────────────────┐      ┌─────────────────────────┐
    │  Docker Executor    │      │  Shell Executor         │
    │  (docker/linux)     │      │  (shell/darwin | windows)│
    │                     │      │                         │
    │  1. Pull 镜像(异步)  │      │  1. 准备环境变量        │
    │  2. 创建临时容器     │      │  2. 在本地 Shell 中     │
    │  3. Volume 挂载     │      │     启动子进程           │
    │     + Cache 挂载    │      │  3. 收集 Stdout/Stderr  │
    │  4. 执行业务命令     │      │  4. 清理工作空间        │
    │  5. 收集日志        │      │  5. 返回结果            │
    │  6. 销毁容器        │      │                         │
    │  7. 返回结果        │      │                         │
    └─────────────────────┘      └─────────────────────────┘
```

---

## 三、Docker Executor（通用 Linux 节点）

### 3.1 模式定义

针对通用 Linux CI/CD 节点，采用 **"共享工作空间 + 临时容器"** 模式，完全复用当前 `container` 包中的基础设施。

```
Worker 节点 (Linux / x86_64 / arm64)
    │
    ├── /tmp/miniflow/workspaces/<pipeline-id>/   ← 共享工作空间
    │       ├── src/                                (源码 checkout)
    │       ├── .cache/m2/                          (持久化 Maven 缓存)
    │       └── .cache/node_modules/                (持久化 npm 缓存)
    │
    └── Docker 引擎 (dockerd / OrbStack / colima)
            │
            └── 临时容器 (Step 级别)
                    ├── Volume: 工作空间 → /workspace
                    ├── Volume: .m2 → /root/.m2 (只读/读写)
                    └── CMD: /bin/sh -c "go build ./..."
```

### 3.2 流程详解

```
收到任务 ──► 1. 异步镜像预热 ──► 2. 创建共享工作空间 ──► 3. chown 统一 UID
                │                                                       │
                ▼                                                       ▼
            4. 构造 Container Config ──► 5. Docker Run ──► 6. 收集日志
                                                        │
                                                        ▼
                                                 7. 自动清理容器
```

### 3.3 AI 原生优势

| 优势 | 说明 |
|:---|:---|
| **严格隔离** | 变量、依赖、进程被锁定在临时容器内，前一步绝不污染后一步 |
| **搜索空间压缩** | 排错时 AI 只需要关注当前容器的日志和退出码，不需要考虑 Side Effect 残留 |
| **可复制性** | 允许 AI 一键销毁错误环境，重新拉起一模一样的容器完全复现 |
| **快照冻结** | 可将失败时的容器镜像+工作空间内容打包作为 bug report 附件 |

### 3.4 防范机制（需硬编码填坑）

#### 3.4.1 文件权限地狱（File Permission Hell）

**问题**：镜像的默认用户可能是 `root`（UID 0），而宿主机工作空间由 `1000:1000` 创建。root 容器创建的文件（如 `go build` 产物）的属主为 root，后续非 root 容器读不到这些文件。

**解决方案**（已在当前代码中部分实现）：

```go
// internal/container/workspace.go
// 在每个 Step 之前，用一个 root 容器执行 chown
func (wm *WorkspaceManager) EnsureWorkspacePermissions(ctx context.Context, mgr Manager, workspacePath string) error {
    cfg := Config{
        Image:    "alpine:latest",
        Commands: []string{"chown -R 1000:1000 /workspace"},
        User:     "root",                    // chown 需要 root
        Workspace: &WorkspaceMount{
            Source: workspacePath,
            Target: DefaultWorkDir,
        },
        NetworkEnabled: false,
    }
    result, err := mgr.RunContainer(ctx, cfg)
    // ...
}
```

**进一步优化**：
- 在 `Execute()` 开始时主动执行一次 chown（跑一个最简单的 alpine 容器），确保目录所有权落在 `DefaultUID`
- 在 `Execute()` 结束时再次 chown，确保宿主机残留文件属主正确，避免后续人工排查时遇到 Permission denied
- 允许每个 Step 独立声明 `runs-as-user` 覆盖默认 UID

#### 3.4.2 冷启动延迟（Cold Start Latency）

**问题**：每次拉取大镜像（如 `golang:1.25`、`node:20`）需要 5-30 秒，AWS/GCR 首次拉取甚至可能超过 60 秒。

**解决方案——异步镜像预热（Image Pre-warming）**：

```go
// 在 Planner 节点编排完 JSON 的瞬间，异步发起镜像预热
func (e *DockerExecutor) Preheat(ctx context.Context, images []string) {
    for _, img := range images {
        go func(image string) {
            exists, _ := e.mgr.ImageExists(ctx, image)
            if exists {
                return // 已缓存，跳过
            }
            slog.Info("preheating image", "image", image)
            if err := e.mgr.PullImage(ctx, image); err != nil {
                slog.Warn("image preheat failed (will retry at execute)", "image", image, "error", err)
            }
        }(img)
    }
}
```

**预热触发时机**：
1. **控制面调度后立即触发**：Server 端完成 DAG 编排后，将需要用到的镜像列表通过一个非阻塞 RPC 发送给 Worker，Worker 立即开始 pull
2. **CI Webhook 入队时触发**：收到 GitHub Push Webhook 后立即解析 `.miniflow.yaml`，预热所有步骤的镜像
3. **缓存亲和性调度**：Server 优先将任务分发给已经缓存了所需镜像的 Worker

#### 3.4.3 依赖包缓存丢失（Dependency Cache）

**问题**：每次 CI 都全量下载 `node_modules`（~200MB）、`GOPATH/pkg`（~500MB）或 `~/.m2`（~1GB）是极大的带宽浪费。

**解决方案——智能 Cache 挂载策略**：

```json
{
  "name": "build-java",
  "image": "maven:3.9",
  "commands": ["mvn package"],
  "cache": {
    "path": "/root/.m2",
    "key": "maven-v1-{{ checksum \"pom.xml\" }}",
    "read_only": false
  }
}
```

**实现机制**：

```go
type CacheMount struct {
    Source   string // 宿主机: /tmp/miniflow/workspaces/.cache/maven-v1-xxx
    Target   string // 容器内: /root/.m2
    ReadOnly bool   // 是否只读
}
```

- **读/写缓存**（`read_only: false`，默认）：缓存目录存在则挂载，不存在则创建后挂载，步骤可写入新包。写入后更新 Cache Key 的 TTL。
- **只读缓存**（`read_only: true`）：仅挂载已有缓存，防止步骤意外篡改。适用于多步骤共享一个缓存基线的场景。
- **Cache Key 模板**：支持 `{{ checksum "file" }}` 语法，当依赖描述文件（如 `pom.xml`、`package-lock.json`）变化时自动切换缓存键。
- **缓存清理策略**：LRU + 全局 TTL（默认 7 天），超过大小的缓存自动裁剪。

---

## 四、Shell Executor（Windows/macOS 特化节点）

### 4.1 模式定义

针对 **iOS 打包**、**旧版 .NET Framework 编译**、**Windows 桌面安装包签名**等无法轻易容器化的异构环境，采用 **"本地子进程 + 工作空间清理"** 模式。

```
Worker 节点 (macOS / Windows)
    │
    ├── 固化的开发环境
    │   ├── Xcode 15.4 (macOS)
    │   ├── .NET SDK 4.8, PowerShell 7.0 (Windows)
    │   ├── Visual Studio Build Tools 2022
    │   └── Apple Developer Certificate (macOS)
    │
    └── Shell Executor
            │
            └── 子进程 (subprocess)
                    ├── CWD = 工作空间目录
                    ├── Env = 步骤级环境变量
                    └── Stdout/Stderr → 管道收集
```

### 4.2 流程详解

```
收到任务 ──► 1. 解析 runs-on 标签 ──► 2. 选择 Shell（bash/pwsh/cmd）
                │
                ▼
         3. 设置工作目录 CWD ──► 4. 注入环境变量 ──► 5. 启动子进程
                │                                               │
                ▼                                               ▼
         6. 管道收集 Stdout/Stderr ←─── 7. 等待子进程退出
                │
                ▼
         8. 清理工作空间（可选） ──► 9. 返回 Result
```

### 4.3 Shell 选择策略

| 操作系统 | 默认 Shell | 兜底 Shell |
|:---|:---|:---|
| macOS | `/bin/bash` | `/bin/zsh` |
| Windows | `powershell.exe` | `cmd.exe` |
| Linux（兜底） | `/bin/bash` | `/bin/sh` |

```go
func (e *ShellExecutor) resolveShell() (string, []string) {
    switch runtime.GOOS {
    case "darwin":
        // macOS: 优先 bash，但很多新系统默认 zsh
        if _, err := os.Stat("/bin/bash"); err == nil {
            return "/bin/bash", []string{"-c"}
        }
        return "/bin/zsh", []string{"-c"}
    case "windows":
        // Windows: 优先 pwsh (PowerShell Core)，次选 powershell.exe
        if _, err := os.Stat("pwsh.exe"); err == nil {
            return "pwsh.exe", []string{"-NoProfile", "-Command"}
        }
        return "powershell.exe", []string{"-NoProfile", "-Command"}
    default:
        return "/bin/sh", []string{"-c"}
    }
}
```

### 4.4 元数据上报协议

Shell Executor 的 Worker 在启动时必须将自身**固定的开发基准环境**作为元数据上报给 Server。Server 将元数据聚合后形成环境目录，供大模型在编排阶段进行精确的"环境匹配"。

#### 上报数据格式（JSON）

```json
{
  "worker_id": "macmini-2024-01",
  "host": {
    "hostname": "macmini-2024.local",
    "os": "darwin",
    "arch": "arm64",
    "version": "15.2",
    "kernel": "24.2.0"
  },
  "shell": {
    "type": "bash",
    "version": "5.2.37"
  },
  "runs_on": ["shell/darwin"],
  "capabilities": {
    "xcode": "15.4",
    "swift": "5.10",
    "cocoapods": "1.15.2",
    "fastlane": "2.221.0",
    "java": "17.0.10",
    "python": "3.12.3",
    "node": "20.14.0",
    "homebrew": "4.3.0"
  },
  "certificates": [
    { "type": "apple_development", "team_id": "ABCDE12345" },
    { "type": "apple_distribution", "team_id": "ABCDE12345" }
  ],
  "volumes": {
    "free_disk_gb": 128,
    "total_disk_gb": 512
  }
}
```

#### 上报时机

1. **Worker 启动时**：一次性全量上报 `/api/worker/register`
2. **心跳上报**（默认每 60 秒）：增量上报，仅携带变化的字段
3. **环境变更时**：例如用户手动安装了新版本的 Xcode，Worker 通过文件系统监控（或定时扫描 `/Applications/Xcode.app`）检测到变更后主动触发上报

### 4.5 安全边界

| 风险点 | 防范措施 |
|:---|:---|
| **恶意命令执行** | 子进程与 Worker 同权限，通过容器级别（Shell Executor 仅运行在容器内的 Worker）或操作系统级别（专用低权限账户 `miniflow-runner`）约束 |
| **环境变量泄漏** | Secret 字段走环境变量传入后即引用清理（`unset SECRET_KEY` 或 CloseHandle），日志脱敏器覆盖 Shell Executor 的输出 |
| **残留进程** | Context 超时或取消时，发送 SIGKILL（Unix）/ TerminateProcess（Windows）到进程树 |
| **磁盘填满** | 工作空间清理策略：`post-step cleanup` 删除产物或 `post-pipeline cleanup` 销毁整个工作空间目录 |
| **凭据泄露** | 仅通过环境变量注入 Secret，不在 commands 中明文拼写；子进程退出后立即 `os.Unsetenv` |

---

## 五、双模对比与选择决策树

| 维度 | Docker Executor | Shell Executor |
|:---|:---|:---|
| **隔离级别** | 强（容器级 Namespace + Cgroup） | 弱（操作系统进程级） |
| **环境一致性** | 镜像定义即环境，N 节点一致 | 依赖 Worker 本地环境配置 |
| **可复制性** | ⭐⭐⭐ 启动时从镜像恢复 | ⭐ 高度依赖 Worker 状态 |
| **启动延迟** | 中（Pull + Create + Start） | 低（直接 fork 子进程） |
| **权限隔离** | UID 1000:1000 + 用户命名空间 | Worker 进程权限 = 子进程权限 |
| **最佳场景** | 通用 Linux 编译/测试/部署 | iOS 打包 / Windows 签名 / 硬件测试 |
| **资源开销** | 额外容器运行时开销 | 无额外开销 |
| **调试便利性** | 可 exec 进入容器现场调查 | 通过环境变量 + 快照复现 |

### 选择决策树

```
Step JSON 中的 runs-on 是否存在？
    ├── 否 → 默认使用 Docker Executor（当前行为）
    │
    └── 是 → 本地 Registry 是否注册了此 runs-on？
              ├── 是 → 使用匹配的执行器
              │
              └── 否 → 向下兼容降级尝试
                         ├── 如果有 Docker：用 Docker Executor 兜底
                         └── 否则：用 Shell Executor + Log Warning
```

---

## 六、AI 原生集成点

### 6.1 排错搜索空间压缩

AI 在诊断 Shell Executor 的错误时，面临的环境变量比 Docker Executor 更多。为此需要：

1. **预处理环境快照**：在子进程启动前和执行完成后，各打一份环境快照（`env` / `Get-ChildItem Env:`），附在 Result 中
2. **差异分析辅助**：AI 只需对比两份快照中的关键差异，即可判断是否有环境变量残留导致的 Side Effect
3. **工作空间文件清单**：`find workspace -type f | head -100` 在前后各执行一次，作为调试上下文

### 6.2 修复策略差异

| 执行器 | AI 修复策略 |
|:---|:---|
| **Docker Executor** | 修改 Step JSON 中的 `image`、`commands` 或 `env` 即可，重试时自动拉取新容器 |
| **Shell Executor** | 仅能修改 `commands` 和 `env`；若环境本身有问题（如 Xcode 版本不匹配），需要通知 Server 将任务重新调度到其他 Worker |

### 6.3 镜像预热智能调度

AI 编排层（Planner）在生成 DAG 后，将每个 Step 的 `image` 汇总为去重列表，通过异步 gRPC 推送给目标 Worker：

```
Planner Node (LLM)
    │
    ├── 生成流水线 JSON（含 steps[].image）
    │
    ├── 构造 ImagePreheatRequest { images: ["golang:1.25", "node:20", "maven:3.9"] }
    │   └── → 非阻塞推送至目标 Worker
    │
    └── Worker 收到后 -> 后台 goroutine 并发 PullImage()
        └── → 真正的 Execute() 到来时，镜像已缓存完毕
```

---

## 七、与现有代码的迁移路径

### 7.1 增量重构，不破坏 Phase 1

当前 Phase 1 代码中 `internal/pipeline/execute.go` 的 `Executor` 直接与 `container.Manager` 耦合。重构分三步走，每一步均可独立合并和发布：

#### Step 1：提取抽象接口（远期可做）

在 `internal/executor/` 下新建包，定义上述 `Executor` 接口。同时将当前 `internal/pipeline/Executor` 改名为 `DockerStepRunner` 作为 Docker 执行器的内部实现。

```
internal/
├── executor/
│   ├── interface.go       ← Executor 接口定义
│   ├── registry.go        ← Registry + Select 路由逻辑
│   ├── docker_executor.go ← Docker Executor 实现
│   └── shell_executor.go  ← Shell Executor 实现
```

#### Step 2：Pipeline 编排层消费接口（不改变 CLI 行为）

`internal/pipeline/execute.go` 不再直接创建容器，而是接收 `executor.Registry` 或 `executor.Executor`：

```go
// 重构后的 Executor（orchestrator）持有执行器注册表
type PipelineOrchestrator struct {
    registry *executor.Registry
    wsManager *container.WorkspaceManager
}

func (o *PipelineOrchestrator) ExecuteStep(ctx context.Context, step Step, wsPath, workDir string) StepResult {
    // 1. 根据 runs-on 选择执行器
    runsOn := executor.RunsOnDockerLinux // 默认
    if step.RunsOn != "" {
        runsOn = executor.RunsOn(step.RunsOn)
    }
    exec := o.registry.Select(runsOn)

    // 2. 构造通用 Config
    cfg := executor.Config{ ... }

    // 3. 执行
    result, err := exec.Execute(ctx, cfg)
    // ...
}
```

#### Step 3：Worker Daemon 集成（远期）

`cmd/worker/main.go` 在 Worker 启动时根据自身环境初始化 Registry，通过心跳向 Server 注册自己的 `runs_on` 标签列表：

```go
func main() {
    // 初始化执行器注册表
    reg := executor.NewRegistry()

    // 向 Server 注册
    server.RegisterWorker(WorkerInfo{
        RunsOn:  reg.SupportedRunsOn(),
        Capabilities: loadCapabilities(),
        Resources:    loadResources(),
    })

    // 建立任务消费循环
    for task := range taskChan {
        exec := reg.Select(task.RunsOn)
        go runTask(exec, task)
    }
}
```

### 7.2 向后兼容保证

- `StepSpec` 推荐新增可选字段 `RunsOn string \`json:"runs_on,omitempty"\``，缺失时默认为 `docker/linux`
- 现有的 Pipeline JSON 无需任何改动
- `cmd/miniflow/main.go`（Phase 1 CLI）继续使用 Docker Executor
- `cmd/worker/main.go`（Phase 2 Worker）完整双模支持

---

## 八、最佳实践与注意事项

### 8.1 何时选择 Docker Executor

- ✅ 所有通用 Linux 编译、测试、代码检查
- ✅ 需要严格的工作空间隔离的场景
- ✅ 跨节点的环境一致性有严格要求的场景
- ✅ AI 自动修复时需要"销毁环境重试"的场景

### 8.2 何时选择 Shell Executor

- ✅ macOS / Windows 独占的工具链（Xcode、signtool、`csc`）
- ✅ 需要访问宿主机硬件的场景（USB 调试、串口烧录）
- ❌ 除非绝对必要，不要在 Linux 上使用 Shell Executor（隔离性差距太大）

### 8.3 镜像预热最佳实践

- 预热是纯异步的，即使预热失败也不影响 Execute 的主流程
- 预热失败的镜像在 Execute 时会重新 Pull，最多增加一次冷启动延迟
- 在 CI Webhook 入队时立即预热的收益最大
- 镜像列表去重后再预热，避免重复拉取

### 8.4 Shell Executor 环境一致性保障

- **Painless（可重复）**：关键工具链版本写入 `Worker.Capabilities`，AI 编排时自动检查版本兼容性
- **Immutable（不变）**：Worker 的环境配置变更通过 CI/CD 管理，禁止手动 SSH 修改
- **Measurable（可度量）**：每次执行前运行 `doctor` 命令（如 `xcode-select -p`）确认环境就绪

---

## 九、参考

- 白皮书：[AI 原生轻量级 CI/CD 执行引擎架构与技术白皮书](miniflow%20AI原生轻量级CICD执行引擎架构与技术白皮书.md)
- 实施方案：[方案设计与实施计划](方案设计_实施计划.md)
- 源码位置：`internal/pipeline/execute.go`、`internal/container/manager.go`
- 当前 `Container Manager` 接口：`internal/container/manager.go:41-53`
