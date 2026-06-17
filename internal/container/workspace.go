package container

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

// ─── 常量 ──────────────────────────────────────────────────

const (
	// DefaultUID 是所有容器统一的运行用户 UID:GID。
	DefaultUID = "1000:1000"

	// WorkspaceBaseDir 是宿主机上共享工作空间的根目录。
	WorkspaceBaseDir = "/tmp/miniflow/workspaces"

	// DefaultWorkDir 是容器内默认工作目录。
	DefaultWorkDir = "/workspace"
)

// ─── 工作空间管理 ───────────────────────────────────────

// WorkspaceManager 管理流水线的共享工作空间。
type WorkspaceManager struct {
	BaseDir string // 宿主机上的工作空间根目录
}

// NewWorkspaceManager 创建 WorkspaceManager。
func NewWorkspaceManager(baseDir string) *WorkspaceManager {
	if baseDir == "" {
		baseDir = WorkspaceBaseDir
	}
	return &WorkspaceManager{BaseDir: baseDir}
}

// CreateWorkspace 为指定流水线创建共享工作空间。
// 返回宿主机上的目录路径。
func (wm *WorkspaceManager) CreateWorkspace(pipelineID string) (string, error) {
	wsPath := filepath.Join(wm.BaseDir, pipelineID)

	if err := os.MkdirAll(wsPath, 0755); err != nil {
		return "", fmt.Errorf("create workspace dir: %w", err)
	}

	slog.Debug("workspace created", "path", wsPath)
	return wsPath, nil
}

// WorkspacePath 返回指定流水线的工作空间路径。
func (wm *WorkspaceManager) WorkspacePath(pipelineID string) string {
	return filepath.Join(wm.BaseDir, pipelineID)
}

// RemoveWorkspace 删除指定流水线的工作空间。
func (wm *WorkspaceManager) RemoveWorkspace(pipelineID string) error {
	wsPath := filepath.Join(wm.BaseDir, pipelineID)

	if err := os.RemoveAll(wsPath); err != nil {
		return fmt.Errorf("remove workspace: %w", err)
	}

	slog.Debug("workspace removed", "path", wsPath)
	return nil
}

// ─── UID 统一策略 ─────────────────────────────────────────

// EnsureWorkspacePermissions 使用一个 Alpine 容器对工作空间执行 chown，
// 确保所有文件属主为 DefaultUID (1000:1000)。
//
// 约束：基础镜像必须支持 --user 1000 运行。
func (wm *WorkspaceManager) EnsureWorkspacePermissions(ctx context.Context, mgr Manager, workspacePath string) error {
	slog.Debug("ensuring workspace permissions", "path", workspacePath, "uid", DefaultUID)

	cfg := Config{
		Image:    "alpine:latest",
		Commands: []string{"chown -R " + DefaultUID + " " + DefaultWorkDir},
		User:     "root", // chown 需要 root 权限
		Workspace: &WorkspaceMount{
			Source: workspacePath,
			Target: DefaultWorkDir,
		},
		NetworkEnabled: false,
	}

	result, err := mgr.RunContainer(ctx, cfg)
	if err != nil {
		return fmt.Errorf("chown workspace: %w", err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("chown workspace failed (exit %d): %s", result.ExitCode, result.Output)
	}

	slog.Debug("workspace permissions ensured", "path", workspacePath)
	return nil
}

// ─── 缓存目录管理 ─────────────────────────────────────────

// EnsureCacheDir 确保指定缓存目录存在并返回其路径。
func (wm *WorkspaceManager) EnsureCacheDir(cacheKey string) (string, error) {
	cachePath := filepath.Join(wm.BaseDir, ".cache", cacheKey)

	if err := os.MkdirAll(cachePath, 0755); err != nil {
		return "", fmt.Errorf("create cache dir: %w", err)
	}

	return cachePath, nil
}

// CachePath 返回指定缓存键的宿主机路径。
func (wm *WorkspaceManager) CachePath(cacheKey string) string {
	return filepath.Join(wm.BaseDir, ".cache", cacheKey)
}
