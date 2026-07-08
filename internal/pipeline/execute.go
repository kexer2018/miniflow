package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/kexer2018/miniflow/internal/container"
)

// ─── 执行引擎 ─────────────────────────────────────────────

// Executor 负责根据 DAG 定义串行执行步骤。
type Executor struct {
	containerMgr container.Manager
	wsManager    *container.WorkspaceManager
}

// NewExecutor 创建新的执行引擎。
func NewExecutor(mgr container.Manager, wsm *container.WorkspaceManager) *Executor {
	return &Executor{
		containerMgr: mgr,
		wsManager:    wsm,
	}
}

// ─── 流水线执行 ───────────────────────────────────────────

// ExecutePipeline 执行整个流水线。
//
// Phase 1 仅支持串行执行——一个 Step 执行完再执行下一个。
// 步骤按拓扑顺序运行，依赖先决条件。
func (e *Executor) ExecutePipeline(ctx context.Context, p *Pipeline) *PipelineResult {
	startedAt := time.Now()
	slog.Info("starting pipeline",
		"name", p.Name,
		"version", p.Version,
		"steps", len(p.Steps),
	)

	result := &PipelineResult{
		PipelineID:  p.ID,
		Name:        p.Name,
		Status:      StatusRunning,
		TotalSteps:  len(p.Steps),
		StepResults: make([]StepResult, 0, len(p.Steps)),
		StartedAt:   startedAt,
	}

	// 1. DAG 拓扑校验
	sorted, err := TopologicalSort(p.Steps)
	if err != nil {
		slog.Error("pipeline validation failed", "error", err)
		result.Status = StatusFailed
		result.FinishedAt = time.Now()
		result.DurationMs = time.Since(startedAt).Milliseconds()
		result.StepResults = append(result.StepResults, StepResult{
			Name:   "validation",
			Status: StatusFailed,
			Error:  fmt.Sprintf("DAG validation failed: %v", err),
		})
		return result
	}

	// 2. 创建共享工作空间
	wsPath, err := e.wsManager.CreateWorkspace(p.ID)
	if err != nil {
		slog.Error("failed to create workspace", "error", err)
		result.Status = StatusFailed
		result.FinishedAt = time.Now()
		result.DurationMs = time.Since(startedAt).Milliseconds()
		result.StepResults = append(result.StepResults, StepResult{
			Name:   "workspace",
			Status: StatusFailed,
			Error:  fmt.Sprintf("create workspace failed: %v", err),
		})
		return result
	}

	// 3. chown 工作空间权限
	if err := e.wsManager.EnsureWorkspacePermissions(ctx, e.containerMgr, wsPath); err != nil {
		slog.Warn("workspace chown failed (non-fatal)", "error", err)
		// 非致命错误，继续执行
	}

	// 4. 确定容器内工作目录（来自 spec.Workspace，兜底 /workspace）
	workDir := p.Workspace
	if workDir == "" {
		workDir = container.DefaultWorkDir
	}

	// 5. 串行执行 Steps
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	for i, step := range sorted {
		// 检查上下文是否已取消
		if ctx.Err() != nil {
			slog.Warn("pipeline cancelled mid-execution", "step", step.Name)
			result.Status = StatusCancelled
			// 剩余步骤标记为 skipped
			for j := i; j < len(sorted); j++ {
				result.StepResults = append(result.StepResults, StepResult{
					Name:   sorted[j].Name,
					Status: StatusSkipped,
				})
			}
			break
		}

		stepResult := e.executeStep(ctx, step, wsPath, workDir)
		result.StepResults = append(result.StepResults, stepResult)

		slog.Info("step completed",
			"step", step.Name,
			"status", stepResult.Status,
			"exit_code", stepResult.ExitCode,
			"duration_ms", stepResult.DurationMs,
		)

		if stepResult.Status == StatusFailed {
			slog.Warn("step failed, stopping pipeline",
				"step", step.Name,
				"exit_code", stepResult.ExitCode,
			)
			result.Status = StatusFailed
			// 剩余步骤标记为 skipped
			for j := i + 1; j < len(sorted); j++ {
				result.StepResults = append(result.StepResults, StepResult{
					Name:   sorted[j].Name,
					Status: StatusSkipped,
				})
			}
			break
		}
	}

	// 6. 如果所有步骤都成功，标记为成功
	if result.Status == StatusRunning {
		result.Status = StatusSuccess
	}

	result.FinishedAt = time.Now()
	result.DurationMs = time.Since(startedAt).Milliseconds()

	slog.Info("pipeline finished",
		"name", p.Name,
		"status", result.Status,
		"duration_ms", result.DurationMs,
	)

	return result
}

// ─── 单步执行 ─────────────────────────────────────────────

// executeStep 执行单个 Step。
func (e *Executor) executeStep(ctx context.Context, step Step, wsPath, workDir string) StepResult {
	slog.Debug("executing step", "name", step.Name, "image", step.Image, "timeout", step.Timeout)

	// 如果步骤配置了超时，创建带超时的 context
	if step.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, step.Timeout)
		defer cancel()
	}

	startedAt := time.Now()

	// 构建容器配置
	cfg := container.Config{
		Image:      step.Image,
		Commands:   step.Commands,
		Entrypoint: step.Entrypoint,
		Env:        step.Env,
		User:       container.DefaultUID,
		WorkDir:    workDir,
		NetworkEnabled: true,
		SSHAgent:  step.SSHAgent,
		Workspace: &container.WorkspaceMount{
			Source: wsPath,
			Target: workDir,
		},
	}

	// 如果有缓存配置，添加缓存挂载
	if step.Cache != nil {
		cachePath := e.wsManager.CachePath(step.Cache.Key)
		cfg.CacheMount = append(cfg.CacheMount, container.CacheMount{
			Source: cachePath,
			Target: step.Cache.Path,
		})
	}

	// 执行容器
	result, err := e.containerMgr.RunContainer(ctx, cfg)

	durationMs := time.Since(startedAt).Milliseconds()

	if err != nil {
		return StepResult{
			Name:       step.Name,
			Status:     StatusFailed,
			ExitCode:   -1,
			DurationMs: durationMs,
			Error:      fmt.Sprintf("container execution error: %v", err),
		}
	}

	status := StatusSuccess
	if result.ExitCode != 0 {
		status = StatusFailed
	}

	return StepResult{
		Name:       step.Name,
		Status:     status,
		ExitCode:   result.ExitCode,
		RawLog:     result.Output,
		DurationMs: durationMs,
		Error:      formatStepError(result),
	}
}

// formatStepError 根据容器退出码返回友好的错误描述。
func formatStepError(result container.Result) string {
	if result.ExitCode == 0 {
		return ""
	}
	return fmt.Sprintf("container exited with code %d", result.ExitCode)
}
