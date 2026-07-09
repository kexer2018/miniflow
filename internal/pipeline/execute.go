package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kexer2018/miniflow/internal/container"
)

// ─── SSH 注入常量 ─────────────────────────────────────────
const (
	containerSSHDir   = "/miniflow/ssh"       // 容器模式下宿主 ~/.ssh 的挂载点
	miniflowDir       = ".miniflow"           // workspace 内的 miniflow 管理目录
	sshDirName        = "ssh"                 // workspace 内 SSH 密钥存放子目录
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

// ─── SSH 密钥注入 ─────────────────────────────────────────

// injectSSHKeys 读取宿主 SSH 密钥并注入工作空间，供步骤容器使用。
//
// 优先查找容器模式路径 /miniflow/ssh/（用户部署时 -v ~/.ssh:/miniflow/ssh）。
// 降级查找 ~/.ssh/（本地 CLI 运行模式）。
func (e *Executor) injectSSHKeys(wsPath string) error {
	candidates := []string{}

	// 容器模式路径
	if info, err := os.Stat(containerSSHDir); err == nil && info.IsDir() {
		candidates = append(candidates, containerSSHDir)
	}

	// 本地模式路径
	home, err := os.UserHomeDir()
	if err == nil {
		sshHome := filepath.Join(home, ".ssh")
		if info, err := os.Stat(sshHome); err == nil && info.IsDir() {
			candidates = append(candidates, sshHome)
		}
	}

	if len(candidates) == 0 {
		return fmt.Errorf("no SSH directory found (checked %s and ~/.ssh)", containerSSHDir)
	}

	sshSource := candidates[0] // 容器模式路径优先
	target := filepath.Join(wsPath, miniflowDir, sshDirName)

	if err := os.MkdirAll(target, 0700); err != nil {
		return fmt.Errorf("create .miniflow/ssh: %w", err)
	}

	entries, err := os.ReadDir(sshSource)
	if err != nil {
		return fmt.Errorf("read SSH dir %s: %w", sshSource, err)
	}

	copied := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()

		// 只复制私钥和已知配置文件
		isPrivateKey := strings.HasPrefix(name, "id_") && !strings.HasSuffix(name, ".pub")
		isConfig := name == "config" || name == "known_hosts"
		if !isPrivateKey && !isConfig {
			continue
		}

		data, err := os.ReadFile(filepath.Join(sshSource, name))
		if err != nil {
			slog.Debug("skipping SSH file (read error)", "file", name, "error", err)
			continue
		}

		if err := os.WriteFile(filepath.Join(target, name), data, 0600); err != nil {
			return fmt.Errorf("write SSH key %s: %w", name, err)
		}
		copied++
	}

	if copied == 0 {
		return fmt.Errorf("no SSH keys found in %s", sshSource)
	}

	slog.Info("SSH keys injected into workspace",
		"source", sshSource, "target", target, "count", copied)
	return nil
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

	// 3.5 如果有步骤需要 SSH 密钥，注入到工作空间
	if hasSSHStep(p.Steps) {
		if err := e.injectSSHKeys(wsPath); err != nil {
			slog.Warn("SSH key injection failed (will attempt per-step fallback)", "error", err)
		}
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

// hasSSHStep 检查是否有步骤启用了 SSH Agent。
func hasSSHStep(steps []Step) bool {
	for _, step := range steps {
		if step.SSHAgent {
			return true
		}
	}
	return false
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

	// 构建命令，如果需要 SSH 则前置密钥设置步骤
	commands := step.Commands
	env := make([]string, len(step.Env))
	copy(env, step.Env)

	if step.SSHAgent {
		sshDirInContainer := workDir + "/" + miniflowDir + "/" + sshDirName
		sshSetup := []string{
			"mkdir -p ~/.ssh && chmod 700 ~/.ssh",
			"cp -a " + sshDirInContainer + "/* ~/.ssh/ 2>/dev/null || true",
			"chmod 600 ~/.ssh/id_* 2>/dev/null || true",
		}
		commands = make([]string, 0, len(step.Commands)+len(sshSetup))
		commands = append(commands, sshSetup...)
		commands = append(commands, step.Commands...)
		env = append(env, "GIT_SSH_COMMAND=ssh -o StrictHostKeyChecking=accept-new")
	}

	// 构建容器配置
	cfg := container.Config{
		Image:      step.Image,
		Commands:   commands,
		Entrypoint: step.Entrypoint,
		Env:        env,
		User:       container.DefaultUID,
		WorkDir:    workDir,
		NetworkEnabled: true,
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
