// Package container 提供 Docker 容器的生命周期管理。
package container

import (
	"context"
	"io"
)

// ─── 容器配置 ──────────────────────────────────────────────

// Config 定义创建容器的参数。
type Config struct {
	Image      string            // 容器镜像
	Commands   []string          // 要执行的命令
	Entrypoint []string          // 可选 entrypoint 覆盖
	Env        []string          // 环境变量 K=V
	User       string            // 运行用户（默认 "1000:1000"）
	WorkDir    string            // 容器内工作目录
	Workspace  *WorkspaceMount   // 共享工作空间挂载
	CacheMount []CacheMount      // 缓存卷挂载
	NetworkEnabled bool          // 是否启用网络
	SSHAgent     bool            // 是否转发宿主 SSH Agent
}

// WorkspaceMount 描述共享工作空间的挂载方式。
type WorkspaceMount struct {
	Source      string // 宿主机路径
	Target      string // 容器内挂载点
	SubPath     string // 可选子路径
}

// CacheMount 描述一个缓存卷的挂载。
type CacheMount struct {
	Source string // 宿主机缓存路径
	Target string // 容器内缓存路径
	ReadOnly bool // 是否只读
}

// ─── 接口定义 ──────────────────────────────────────────────

// Manager 定义容器生命周期管理接口。
type Manager interface {
	// RunContainer 创建并运行一个容器，等待执行完毕，返回 stdout+stderr 日志。
	RunContainer(ctx context.Context, cfg Config) (Result, error)

	// PullImage 拉取容器镜像。
	PullImage(ctx context.Context, image string) error

	// ImageExists 检查本地是否已缓存指定镜像。
	ImageExists(ctx context.Context, image string) (bool, error)

	// Close 关闭清理资源。
	Close() error
}

// Result 包含容器执行后的输出和状态。
type Result struct {
	Output   string // stdout + stderr 合并输出
	ExitCode int   // 容器退出码
	Error    error // 非正常退出的错误（如创建容器失败）
}

// ─── 日志收集回调 ──────────────────────────────────────────

// LogCallback 可以用于实时收集日志行。
type LogCallback func(line string)

// ContainerLogReader 允许按行读取容器日志（用于流式收集）。
type ContainerLogReader interface {
	Read(ctx context.Context) (<-chan string, error)
}

// ─── 写入器适配 ───────────────────────────────────────────

// WriteCloser 是 io.WriteCloser 的别名，用于流式写入容器 stdin。
type WriteCloser io.WriteCloser
