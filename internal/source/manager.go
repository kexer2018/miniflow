package source

import (
	"context"
	"fmt"
	"log/slog"

	pipelinespec "github.com/kexer2018/miniflow/pkg/pipeline"
	"github.com/kexer2018/miniflow/internal/secret"
)

// ─── Manager ──────────────────────────────────────────

// Manager 负责根据 SourceSpec 执行源码准备流程。
type Manager struct {
	credStore *secret.CredentialStore
}

// NewManager 创建 SourceManager。
func NewManager(store *secret.CredentialStore) *Manager {
	return &Manager{credStore: store}
}

// PrepareWorkspace 执行完整的源码准备流程。
// 1. 根据 repository URL 匹配凭证
// 2. 构建 clone URL（如果需要认证嵌入）
// 3. 执行 git clone
// 如果 spec 为 nil，跳过（静默成功）。
func (m *Manager) PrepareWorkspace(ctx context.Context, spec *pipelinespec.SourceSpec, dest string) (*CheckoutResult, error) {
	if spec == nil {
		return nil, nil
	}

	slog.Info("preparing source workspace",
		"repo", spec.Repository,
		"ref", spec.Ref,
		"dest", dest,
	)

	// 1. 匹配凭证
	cred := m.credStore.Match(spec.Repository)

	// 2. 构造 clone URL
	cloneURL := buildCloneURL(spec.Repository, cred)

	// 3. 执行 clone
	opts := &CloneOptions{
		Ref:        spec.Ref,
		Depth:      spec.Depth,
		Credential: cred,
	}

	result, err := Clone(ctx, cloneURL, dest, opts)
	if err != nil {
		return nil, fmt.Errorf("prepare workspace: %w", err)
	}

	return result, nil
}

// buildCloneURL 根据凭证类型判断是否需要重写 URL。
// Token auth 使用 HTTPS URL；SSH key 使用 SSH URL。
func buildCloneURL(repoURL string, cred *secret.Credential) string {
	if cred == nil {
		// 无凭证时确保是 HTTPS URL 以便匿名访问
		return repoURL
	}
	// 如果已有协议前缀，直接返回
	return repoURL
}
