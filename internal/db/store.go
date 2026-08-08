// Package db 提供持久化存储接口。
package db

import (
	"context"
	"time"

	"github.com/kexer2018/miniflow/internal/pipeline"
)

// ArtifactStore is intentionally separate from Store so existing runners can
// execute pipelines without requiring artifact persistence.
type ArtifactStore interface {
	SaveArtifact(ctx context.Context, artifact ArtifactRecord) error
	GetArtifact(ctx context.Context, runID, name string) (*ArtifactRecord, error)
	ListArtifacts(ctx context.Context, runID string) ([]ArtifactRecord, error)
	ListArtifactsBefore(ctx context.Context, before time.Time) ([]ArtifactRecord, error)
	DeleteArtifact(ctx context.Context, runID, name string) error
}

// ArtifactRecord indexes an archive stored on the local filesystem.
type ArtifactRecord struct {
	RunID     string    `json:"run_id"`
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	Size      int64     `json:"size"`
	CreatedAt time.Time `json:"created_at"`
}

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

	// ─── 诊断历史 ──────────────────────────────────────
	// SaveDiagnosis 保存一次 AI 诊断记录。
	SaveDiagnosis(ctx context.Context, record *DiagnosisRecord) error

	// ListDiagnoses 列出最近的诊断记录。
	ListDiagnoses(ctx context.Context, limit, offset int) ([]*DiagnosisRecord, error)

	// ─── 健康检查 ──────────────────────────────────────
	// Ping 检查数据库连接是否正常。
	Ping(ctx context.Context) error

	// Close 关闭数据库连接。
	Close() error
}

// ─── 诊断记录 ─────────────────────────────────────────────

// DiagnosisRecord 保存一次 AI 诊断的历史记录。
// 用于诊断回顾和动态 RAG 积累。
type DiagnosisRecord struct {
	PipelineID           string  `json:"pipeline_id"`
	StepName             string  `json:"step_name"`
	ClassificationType   string  `json:"classification_type"`
	ClassificationReason string  `json:"classification_reason"`
	RootCause            string  `json:"root_cause"`
	FixPlan              string  `json:"fix_plan"`
	Confidence           float64 `json:"confidence"`
	Category             string  `json:"category"`
	DiagnosisJSON        string  `json:"diagnosis_json,omitempty"` // 完整诊断结果的 JSON 快照
	CreatedAt            int64   `json:"created_at"`
}
