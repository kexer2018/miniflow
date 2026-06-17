package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/kexer2018/miniflow/internal/pipeline"

	_ "modernc.org/sqlite"
)

// ─── 编译期接口检查 ───────────────────────────────────────
var _ Store = (*SQLiteStore)(nil)

// SQLiteStore 使用 SQLite 实现 Store 接口。
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore 创建 SQLite 存储引擎。
// dbPath 是数据库文件路径，":memory:" 表示纯内存数据库。
func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// 连接配置
	db.SetMaxOpenConns(1) // SQLite 不支持并发写入
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(time.Hour)

	store := &SQLiteStore{db: db}
	if err := store.migrate(context.Background()); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}

	slog.Info("sqlite store initialized", "path", dbPath)
	return store, nil
}

// ─── 实现 Store 接口 ─────────────────────────────────────

func (s *SQLiteStore) SavePipelineResult(ctx context.Context, result *pipeline.PipelineResult) error {
	query := `INSERT INTO pipeline_results
		(id, name, status, total_steps, step_results_json, started_at, finished_at, duration_ms, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`

	stepResultsJSON, err := json.Marshal(result.StepResults)
	if err != nil {
		return fmt.Errorf("marshal step results: %w", err)
	}

	now := time.Now()
	_, err = s.db.ExecContext(ctx, query,
		result.PipelineID,
		result.Name,
		string(result.Status),
		result.TotalSteps,
		string(stepResultsJSON),
		result.StartedAt.Unix(),
		result.FinishedAt.Unix(),
		result.DurationMs,
		now.Unix(),
	)
	if err != nil {
		return fmt.Errorf("insert pipeline result: %w", err)
	}

	return nil
}

func (s *SQLiteStore) GetPipelineResult(ctx context.Context, id string) (*pipeline.PipelineResult, error) {
	query := `SELECT id, name, status, total_steps, step_results_json, started_at, finished_at, duration_ms
		FROM pipeline_results WHERE id = ?`

	row := s.db.QueryRowContext(ctx, query, id)

	var (
		result           pipeline.PipelineResult
		stepResultsJSON  string
		startedAtUnix    int64
		finishedAtUnix   int64
	)

	err := row.Scan(
		&result.PipelineID,
		&result.Name,
		(*string)(&result.Status),
		&result.TotalSteps,
		&stepResultsJSON,
		&startedAtUnix,
		&finishedAtUnix,
		&result.DurationMs,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("pipeline result %q not found", id)
		}
		return nil, fmt.Errorf("scan pipeline result: %w", err)
	}

	if err := json.Unmarshal([]byte(stepResultsJSON), &result.StepResults); err != nil {
		return nil, fmt.Errorf("unmarshal step results: %w", err)
	}

	result.StartedAt = time.Unix(startedAtUnix, 0)
	result.FinishedAt = time.Unix(finishedAtUnix, 0)

	return &result, nil
}

func (s *SQLiteStore) ListPipelineResults(ctx context.Context, limit, offset int) ([]*pipeline.PipelineResult, error) {
	if limit <= 0 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	query := `SELECT id, name, status, total_steps, step_results_json, started_at, finished_at, duration_ms
		FROM pipeline_results ORDER BY created_at DESC LIMIT ? OFFSET ?`

	rows, err := s.db.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("query pipeline results: %w", err)
	}
	defer rows.Close()

	var results []*pipeline.PipelineResult
	for rows.Next() {
		var (
			result          pipeline.PipelineResult
			stepResultsJSON string
			startedAtUnix   int64
			finishedAtUnix  int64
		)

		if err := rows.Scan(
			&result.PipelineID,
			&result.Name,
			(*string)(&result.Status),
			&result.TotalSteps,
			&stepResultsJSON,
			&startedAtUnix,
			&finishedAtUnix,
			&result.DurationMs,
		); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}

		if err := json.Unmarshal([]byte(stepResultsJSON), &result.StepResults); err != nil {
			return nil, fmt.Errorf("unmarshal step results: %w", err)
		}

		result.StartedAt = time.Unix(startedAtUnix, 0)
		result.FinishedAt = time.Unix(finishedAtUnix, 0)

		results = append(results, &result)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration: %w", err)
	}

	if results == nil {
		results = make([]*pipeline.PipelineResult, 0)
	}

	return results, nil
}

func (s *SQLiteStore) SaveExecContext(ctx context.Context, execCtx *pipeline.ExecContext) error {
	query := `INSERT OR REPLACE INTO exec_contexts
		(pipeline_id, workspace_dir, cache_dir, updated_at)
		VALUES (?, ?, ?, ?)`

	_, err := s.db.ExecContext(ctx, query,
		execCtx.PipelineID,
		execCtx.WorkspaceDir,
		execCtx.CacheDir,
		time.Now().Unix(),
	)
	if err != nil {
		return fmt.Errorf("save exec context: %w", err)
	}
	return nil
}

func (s *SQLiteStore) GetExecContext(ctx context.Context, pipelineID string) (*pipeline.ExecContext, error) {
	query := `SELECT pipeline_id, workspace_dir, cache_dir FROM exec_contexts WHERE pipeline_id = ?`

	row := s.db.QueryRowContext(ctx, query, pipelineID)
	ec := &pipeline.ExecContext{}

	err := row.Scan(&ec.PipelineID, &ec.WorkspaceDir, &ec.CacheDir)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("exec context %q not found", pipelineID)
		}
		return nil, fmt.Errorf("scan exec context: %w", err)
	}

	return ec, nil
}

func (s *SQLiteStore) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

// ─── 数据库迁移 ───────────────────────────────────────────

func (s *SQLiteStore) migrate(ctx context.Context) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS pipeline_results (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'pending',
			total_steps INTEGER NOT NULL DEFAULT 0,
			step_results_json TEXT NOT NULL DEFAULT '[]',
			started_at INTEGER NOT NULL DEFAULT 0,
			finished_at INTEGER NOT NULL DEFAULT 0,
			duration_ms INTEGER NOT NULL DEFAULT 0,
			created_at INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_pipeline_results_created_at
			ON pipeline_results(created_at DESC)`,
		`CREATE TABLE IF NOT EXISTS exec_contexts (
			pipeline_id TEXT PRIMARY KEY,
			workspace_dir TEXT NOT NULL DEFAULT '',
			cache_dir TEXT NOT NULL DEFAULT '',
			updated_at INTEGER NOT NULL DEFAULT 0
		)`,
	}

	for _, stmt := range statements {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("execute migration: %w", err)
		}
	}

	return nil
}
