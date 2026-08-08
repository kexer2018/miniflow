package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	logutil "github.com/kexer2018/miniflow/internal/log"
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
	if result == nil {
		return fmt.Errorf("pipeline result is required")
	}
	query := `INSERT INTO pipeline_results
		(id, name, status, total_steps, step_results_json, started_at, finished_at, duration_ms, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`

	stepResults := persistedStepResults(result.StepResults)
	stepResultsJSON, err := json.Marshal(stepResults)
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

// persistedStepResults deliberately removes raw container output before it
// crosses the in-memory execution boundary. Secret redaction must not depend
// on individual callers remembering to sanitize a result first.
func persistedStepResults(results []pipeline.StepResult) []pipeline.StepResult {
	persisted := make([]pipeline.StepResult, len(results))
	copy(persisted, results)
	for i := range persisted {
		if persisted[i].Sanitized == "" && persisted[i].RawLog != "" {
			persisted[i].Sanitized = logutil.SanitizeString(persisted[i].RawLog)
		}
		persisted[i].RawLog = ""
	}
	return persisted
}

func (s *SQLiteStore) SaveArtifact(ctx context.Context, artifact ArtifactRecord) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO artifacts (run_id, name, path, size, created_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(run_id, name) DO UPDATE SET path=excluded.path, size=excluded.size, created_at=excluded.created_at`,
		artifact.RunID, artifact.Name, artifact.Path, artifact.Size, artifact.CreatedAt.Unix())
	if err != nil {
		return fmt.Errorf("save artifact: %w", err)
	}
	return nil
}

func (s *SQLiteStore) GetArtifact(ctx context.Context, runID, name string) (*ArtifactRecord, error) {
	row := s.db.QueryRowContext(ctx, `SELECT run_id, name, path, size, created_at FROM artifacts WHERE run_id = ? AND name = ?`, runID, name)
	var artifact ArtifactRecord
	var createdAt int64
	if err := row.Scan(&artifact.RunID, &artifact.Name, &artifact.Path, &artifact.Size, &createdAt); err != nil {
		return nil, fmt.Errorf("get artifact: %w", err)
	}
	artifact.CreatedAt = time.Unix(createdAt, 0)
	return &artifact, nil
}

func (s *SQLiteStore) ListArtifacts(ctx context.Context, runID string) ([]ArtifactRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT run_id, name, path, size, created_at FROM artifacts WHERE run_id = ? ORDER BY name`, runID)
	if err != nil {
		return nil, fmt.Errorf("list artifacts: %w", err)
	}
	defer rows.Close()

	artifacts := make([]ArtifactRecord, 0)
	for rows.Next() {
		var artifact ArtifactRecord
		var createdAt int64
		if err := rows.Scan(&artifact.RunID, &artifact.Name, &artifact.Path, &artifact.Size, &createdAt); err != nil {
			return nil, fmt.Errorf("scan artifact: %w", err)
		}
		artifact.CreatedAt = time.Unix(createdAt, 0)
		artifacts = append(artifacts, artifact)
	}
	return artifacts, rows.Err()
}

func (s *SQLiteStore) ListArtifactsBefore(ctx context.Context, before time.Time) ([]ArtifactRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT run_id, name, path, size, created_at FROM artifacts WHERE created_at < ? ORDER BY created_at`, before.Unix())
	if err != nil {
		return nil, fmt.Errorf("list expired artifacts: %w", err)
	}
	defer rows.Close()

	var artifacts []ArtifactRecord
	for rows.Next() {
		var artifact ArtifactRecord
		var createdAt int64
		if err := rows.Scan(&artifact.RunID, &artifact.Name, &artifact.Path, &artifact.Size, &createdAt); err != nil {
			return nil, fmt.Errorf("scan expired artifact: %w", err)
		}
		artifact.CreatedAt = time.Unix(createdAt, 0)
		artifacts = append(artifacts, artifact)
	}
	return artifacts, rows.Err()
}

func (s *SQLiteStore) DeleteArtifact(ctx context.Context, runID, name string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM artifacts WHERE run_id = ? AND name = ?`, runID, name); err != nil {
		return fmt.Errorf("delete artifact: %w", err)
	}
	return nil
}

func (s *SQLiteStore) CreateRun(ctx context.Context, run RunRecord, steps []StepRunRecord) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin create run: %w", err)
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO runs (id, name, status, spec_json, started_at, finished_at, duration_ms, error) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, run.ID, run.Name, run.Status, run.SpecJSON, run.StartedAt.UnixMilli(), run.FinishedAt.UnixMilli(), run.DurationMs, run.Error)
	if err != nil {
		return fmt.Errorf("insert run: %w", err)
	}
	for position, step := range steps {
		if _, err := tx.ExecContext(ctx, `INSERT INTO run_steps (run_id, position, name, status, exit_code, duration_ms, error) VALUES (?, ?, ?, ?, ?, ?, ?)`, run.ID, position, step.Name, step.Status, step.ExitCode, step.DurationMs, step.Error); err != nil {
			return fmt.Errorf("insert run step: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit create run: %w", err)
	}
	return nil
}

func (s *SQLiteStore) UpdateRun(ctx context.Context, run RunRecord) error {
	_, err := s.db.ExecContext(ctx, `UPDATE runs SET status=?, finished_at=?, duration_ms=?, error=? WHERE id=?`, run.Status, run.FinishedAt.UnixMilli(), run.DurationMs, run.Error, run.ID)
	return err
}

func (s *SQLiteStore) UpdateStepRun(ctx context.Context, runID string, step StepRunRecord) error {
	_, err := s.db.ExecContext(ctx, `UPDATE run_steps SET status=?, exit_code=?, duration_ms=?, error=? WHERE run_id=? AND name=?`, step.Status, step.ExitCode, step.DurationMs, step.Error, runID, step.Name)
	return err
}

func (s *SQLiteStore) GetRun(ctx context.Context, id string) (*RunRecord, error) {
	r := &RunRecord{}
	var startedAt, finishedAt int64
	err := s.db.QueryRowContext(ctx, `SELECT id, name, status, spec_json, started_at, finished_at, duration_ms, error FROM runs WHERE id=?`, id).Scan(&r.ID, &r.Name, &r.Status, &r.SpecJSON, &startedAt, &finishedAt, &r.DurationMs, &r.Error)
	if err != nil {
		return nil, err
	}
	r.StartedAt, r.FinishedAt = time.UnixMilli(startedAt), time.UnixMilli(finishedAt)
	return r, nil
}

func (s *SQLiteStore) ListRuns(ctx context.Context, limit, offset int) ([]RunRecord, error) {
	if limit <= 0 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, status, spec_json, started_at, finished_at, duration_ms, error FROM runs ORDER BY started_at DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []RunRecord
	for rows.Next() {
		var r RunRecord
		var startedAt, finishedAt int64
		if err := rows.Scan(&r.ID, &r.Name, &r.Status, &r.SpecJSON, &startedAt, &finishedAt, &r.DurationMs, &r.Error); err != nil {
			return nil, err
		}
		r.StartedAt, r.FinishedAt = time.UnixMilli(startedAt), time.UnixMilli(finishedAt)
		result = append(result, r)
	}
	return result, rows.Err()
}

func (s *SQLiteStore) SavePipelineDefinition(ctx context.Context, definition PipelineDefinitionRecord) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO pipeline_definitions (name, spec_json, updated_at) VALUES (?, ?, ?) ON CONFLICT(name) DO UPDATE SET spec_json=excluded.spec_json, updated_at=excluded.updated_at`, definition.Name, definition.SpecJSON, definition.UpdatedAt.UnixMilli())
	return err
}

func (s *SQLiteStore) ListPipelineDefinitions(ctx context.Context, limit, offset int) ([]PipelineDefinitionRecord, error) {
	if limit <= 0 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.db.QueryContext(ctx, `SELECT name, spec_json, updated_at FROM pipeline_definitions ORDER BY updated_at DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []PipelineDefinitionRecord
	for rows.Next() {
		var r PipelineDefinitionRecord
		var updatedAt int64
		if err := rows.Scan(&r.Name, &r.SpecJSON, &updatedAt); err != nil {
			return nil, err
		}
		r.UpdatedAt = time.UnixMilli(updatedAt)
		result = append(result, r)
	}
	return result, rows.Err()
}

func (s *SQLiteStore) ListStepRuns(ctx context.Context, runID string) ([]StepRunRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT name, status, exit_code, duration_ms, error FROM run_steps WHERE run_id=? ORDER BY position`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []StepRunRecord
	for rows.Next() {
		var step StepRunRecord
		if err := rows.Scan(&step.Name, &step.Status, &step.ExitCode, &step.DurationMs, &step.Error); err != nil {
			return nil, err
		}
		result = append(result, step)
	}
	return result, rows.Err()
}

func (s *SQLiteStore) MarkActiveRunsInterrupted(ctx context.Context, message string, finishedAt time.Time) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE run_steps SET status='interrupted', error=? WHERE status IN ('queued', 'running') AND run_id IN (SELECT id FROM runs WHERE status IN ('queued', 'running'))`, message); err != nil {
		return 0, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE runs SET status='interrupted', error=?, finished_at=?, duration_ms=CASE WHEN started_at > 0 THEN ? - started_at ELSE 0 END WHERE status IN ('queued', 'running')`, message, finishedAt.UnixMilli(), finishedAt.UnixMilli())
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	count, err := result.RowsAffected()
	return int(count), err
}

func (s *SQLiteStore) GetPipelineResult(ctx context.Context, id string) (*pipeline.PipelineResult, error) {
	query := `SELECT id, name, status, total_steps, step_results_json, started_at, finished_at, duration_ms
		FROM pipeline_results WHERE id = ?`

	row := s.db.QueryRowContext(ctx, query, id)

	var (
		result          pipeline.PipelineResult
		stepResultsJSON string
		startedAtUnix   int64
		finishedAtUnix  int64
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

// ─── 诊断历史 ─────────────────────────────────────────────

func (s *SQLiteStore) SaveDiagnosis(ctx context.Context, record *DiagnosisRecord) error {
	query := `INSERT INTO diagnosis_history
		(pipeline_id, step_name, classification_type, classification_reason,
		 root_cause, fix_plan, confidence, category, diagnosis_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	now := time.Now().Unix()

	_, err := s.db.ExecContext(ctx, query,
		record.PipelineID,
		record.StepName,
		record.ClassificationType,
		record.ClassificationReason,
		record.RootCause,
		record.FixPlan,
		record.Confidence,
		record.Category,
		record.DiagnosisJSON,
		now,
	)
	if err != nil {
		return fmt.Errorf("save diagnosis: %w", err)
	}

	return nil
}

func (s *SQLiteStore) ListDiagnoses(ctx context.Context, limit, offset int) ([]*DiagnosisRecord, error) {
	if limit <= 0 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	query := `SELECT pipeline_id, step_name, classification_type, classification_reason,
		root_cause, fix_plan, confidence, category, diagnosis_json, created_at
		FROM diagnosis_history ORDER BY created_at DESC LIMIT ? OFFSET ?`

	rows, err := s.db.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("query diagnoses: %w", err)
	}
	defer rows.Close()

	var records []*DiagnosisRecord
	for rows.Next() {
		var r DiagnosisRecord
		if err := rows.Scan(
			&r.PipelineID,
			&r.StepName,
			&r.ClassificationType,
			&r.ClassificationReason,
			&r.RootCause,
			&r.FixPlan,
			&r.Confidence,
			&r.Category,
			&r.DiagnosisJSON,
			&r.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan diagnosis: %w", err)
		}
		records = append(records, &r)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration: %w", err)
	}

	if records == nil {
		records = make([]*DiagnosisRecord, 0)
	}

	return records, nil
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
		`CREATE TABLE IF NOT EXISTS artifacts (
			run_id TEXT NOT NULL,
			name TEXT NOT NULL,
			path TEXT NOT NULL,
			size INTEGER NOT NULL DEFAULT 0,
			created_at INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (run_id, name)
		)`,
		`CREATE TABLE IF NOT EXISTS runs (
			id TEXT PRIMARY KEY, name TEXT NOT NULL, status TEXT NOT NULL,
			spec_json TEXT NOT NULL, started_at INTEGER NOT NULL, finished_at INTEGER NOT NULL DEFAULT 0,
			duration_ms INTEGER NOT NULL DEFAULT 0, error TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS run_steps (
			run_id TEXT NOT NULL, position INTEGER NOT NULL, name TEXT NOT NULL, status TEXT NOT NULL,
			exit_code INTEGER NOT NULL DEFAULT 0, duration_ms INTEGER NOT NULL DEFAULT 0, error TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (run_id, name), FOREIGN KEY(run_id) REFERENCES runs(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS pipeline_definitions (
			name TEXT PRIMARY KEY, spec_json TEXT NOT NULL, updated_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS exec_contexts (
			pipeline_id TEXT PRIMARY KEY,
			workspace_dir TEXT NOT NULL DEFAULT '',
			cache_dir TEXT NOT NULL DEFAULT '',
			updated_at INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS diagnosis_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			pipeline_id TEXT NOT NULL DEFAULT '',
			step_name TEXT NOT NULL DEFAULT '',
			classification_type TEXT NOT NULL DEFAULT '',
			classification_reason TEXT NOT NULL DEFAULT '',
			root_cause TEXT NOT NULL DEFAULT '',
			fix_plan TEXT NOT NULL DEFAULT '',
			confidence REAL NOT NULL DEFAULT 0.0,
			category TEXT NOT NULL DEFAULT '',
			diagnosis_json TEXT NOT NULL DEFAULT '{}',
			created_at INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_diagnosis_created_at
			ON diagnosis_history(created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_diagnosis_pipeline
			ON diagnosis_history(pipeline_id)`,
	}

	for _, stmt := range statements {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("execute migration: %w", err)
		}
	}

	return nil
}
