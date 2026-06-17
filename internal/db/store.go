// Package db 提供持久化存储接口。
package db

import (
	"context"

	"github.com/kexer2018/miniflow/internal/pipeline"
)

// ─── 存储接口 ─────────────────────────────────────────────

// Store 定义流水线的持久化存储接口。
//
// Phase 1 使用 SQLite 实现。
// 后期可替换为 PostgreSQL 实现。
type Store interface {
	// ─── 流水线历史 ────────────────────────────────────
	// SavePipelineResult 保存一次流水线的执行结果。
	SavePipelineResult(ctx context.Context, result *pipeline.PipelineResult) error

	// GetPipelineResult 根据 ID 获取流水线执行结果。
	GetPipelineResult(ctx context.Context, id string) (*pipeline.PipelineResult, error)

	// ListPipelineResults 列出最近的流水线执行记录。
	ListPipelineResults(ctx context.Context, limit, offset int) ([]*pipeline.PipelineResult, error)

	// ─── 执行上下文 ────────────────────────────────────
	// SaveExecContext 保存执行上下文（用于恢复中断的执行）。
	SaveExecContext(ctx context.Context, execCtx *pipeline.ExecContext) error

	// GetExecContext 获取执行上下文。
	GetExecContext(ctx context.Context, pipelineID string) (*pipeline.ExecContext, error)

	// ─── 健康检查 ──────────────────────────────────────
	// Ping 检查数据库连接是否正常。
	Ping(ctx context.Context) error

	// Close 关闭数据库连接。
	Close() error
}
