# 第三方脚本 Step 扩展教程

miniflow 的内置 Step 只承载通用平台原语。第三方业务逻辑应以脚本扩展交付，而不是修改 worker 或把企业流程加入内置 Registry。

这份协议遵循三个边界：Docker 负责执行用户脚本；PipelineSpec 是唯一的运行输入协议；扩展只能访问自己的共享 workspace、声明的环境变量和密钥引用。

## 当前可运行的扩展格式

当前 worker 只接受已注册的 Step type。第三方脚本扩展应使用 `script.run`，将扩展名称放在 Step `name`，将版本化脚本放在仓库中。例如：

```text
.miniflow/
  steps/
    acme-build.sh
pipeline.json
```

`pipeline.json`：

```json
{
  "version": "1.1",
  "name": "third-party-step-example",
  "steps": [
    {
      "name": "acme.build@1",
      "type": "script.run",
      "image": "node:22-alpine",
      "depends_on": ["checkout"],
      "env": ["BUILD_MODE=production"],
      "secrets": ["npm-token"],
      "timeout": 900,
      "with": {
        "script": "sh .miniflow/steps/acme-build.sh"
      }
    }
  ]
}
```

脚本从容器内 `/workspace` 执行。不要把 token、私钥或 webhook URL 写入脚本；通过 `secrets` 引用 worker 的凭据存储。脚本应以非零退出码表示失败，并将诊断信息写到 stdout/stderr，miniflow 会实时推送和脱敏日志。

## 脚本约定

推荐脚本遵守以下固定约定，以便未来升级为可视化的独立 Step：

```sh
#!/bin/sh
set -eu

# Inputs come from declared environment variables.
: "${BUILD_MODE:?BUILD_MODE is required}"

# Work only inside the shared workspace.
mkdir -p dist
printf 'mode=%s\n' "$BUILD_MODE" > dist/build-info.txt
```

- 输入使用环境变量，名称使用全大写 `SNAKE_CASE`。
- 产物写入 workspace 内的相对目录，并由后续 `artifact.save` 保存。
- 不依赖容器运行时状态；每个 Step 都可能使用新容器。
- 对需要 Git 的脚本显式设置 `ssh_agent: true`；`git.checkout` 本身由 worker 的受控 SSH 认证完成。
- 对外部网络、磁盘路径和超时作出明确假设。

## 独立扩展 Manifest v1

worker 可以从管理员指定的受信任目录加载下列 JSON manifest。使用 `--step-dir` 指定目录；目录本身可以是一个 Bundle，也可以包含多个子 Bundle。worker 在启动时校验所有 manifest，任一无效 Bundle 都会阻止启动。

```json
{
  "$schema": "https://miniflow.dev/schemas/step-extension/v1.json",
  "apiVersion": "miniflow.dev/v1",
  "kind": "ScriptStepExtension",
  "metadata": {
    "id": "com.acme.node.build",
    "version": "1.0.0",
    "name": "Acme Node Build",
    "description": "Build a Node.js application into dist/"
  },
  "spec": {
    "runtime": {
      "image": "node:22-alpine",
      "script": "run.sh"
    },
    "inputs": {
      "type": "object",
      "required": ["build_mode"],
      "properties": {
        "build_mode": {
          "type": "string",
          "enum": ["development", "production"],
          "default": "production"
        }
      },
      "additionalProperties": false
    },
    "outputs": [
      { "name": "dist", "path": "dist" }
    ]
  }
}
```

固定字段要求：

| 字段 | 要求 |
|---|---|
| `metadata.id` | 反向域名格式，例如 `com.acme.node.build`；全局唯一 |
| `metadata.version` | 语义化版本，例如 `1.2.3` |
| `spec.runtime.image` | 固定 Docker image，不能由不受约束的用户输入拼接 |
| `spec.runtime.script` | Bundle 内的相对脚本文件，例如 `run.sh`；不是 workspace 命令 |
| `spec.inputs` | JSON Schema object；字段映射为受控环境变量 |
| `spec.outputs` | workspace 内相对路径；用于生成 artifact 建议 |

## 发布和运行流程

1. 将 manifest 与脚本一起发布到受管理员控制的 Bundle 目录。
2. 为 manifest 的版本、Docker image tag 和脚本内容做代码审查。
3. 启动 worker。开发时可以直接运行：

```bash
go run ./cmd/worker --step-dir ./examples/extensions
```

或先构建二进制再运行：

```bash
make build-worker
./bin/miniflow-worker --step-dir ./examples/extensions
```

4. Pipeline 使用 `metadata.id` 作为 `type`，并把 `with` 作为 schema 输入：

```json
{
  "name": "acme build",
  "type": "com.acme.node.build",
  "with": { "build_mode": "production" }
}
```

5. UI 从 `GET /api/v1/step-types` 获取已启用的扩展 schema，不自行猜测表单字段。

运行时，worker 把扩展目录只读挂载到 `/opt/miniflow/extensions/<extension-id>`，并执行 `/opt/miniflow/extensions/<extension-id>/<spec.runtime.script>`。`with.build_mode` 会被映射为 `MINIFLOW_INPUT_BUILD_MODE`；输入值不会拼接进 shell 命令。

## 安全限制

- 只加载管理员配置的扩展目录，绝不在 checkout 的用户仓库中自动发现并信任 manifest。
- Bundle 目录、`step.json` 与脚本不得是符号链接，且不得对 group 或 other 可写；不满足时 Worker 拒绝启动。
- 禁止扩展请求 Docker socket、任意宿主机 bind mount 或宿主机命令执行。
- 任何 secret 只能通过 `secrets` 引用注入，日志必须经过现有脱敏链路。
- `metadata.id`、image 和 schema 变更都应作为版本化兼容性契约审查。
- 生产 Bundle 应使用 image digest（`image@sha256:...`）固定镜像；使用 tag 仍兼容，但 Worker 会记录告警。

## 版本与更新

Registry 在 worker 启动时建立不可变快照，运行中的 Pipeline 不会受 Bundle 文件变化影响。更新扩展时应发布新的 `metadata.version`，完成审查后重启 worker；不支持运行期热加载。远程分发、Bundle 签名和扩展市场在本地受信任目录模型稳定后再引入。

这让第三方能够先以脚本方式稳定交付，再逐步获得 schema 驱动的 UI，而不会破坏 miniflow 的单机 Docker 和脚本优先原则。
