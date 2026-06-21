package source

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	gitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"

	"github.com/kexer2018/miniflow/internal/secret"
)

// ─── 类型 ──────────────────────────────────────────────

// CheckoutResult 表示一次 checkout 的结果。
type CheckoutResult struct {
	CommitSHA string
	Ref       string
	RepoURL   string
}

// CloneOptions 是 Clone 的参数。
type CloneOptions struct {
	Ref        string
	Depth      int
	Credential *secret.Credential
}

// ─── Clone ──────────────────────────────────────────────

// Clone 执行 git clone 到 dest 目录。
// 如果没有凭证，尝试匿名 clone（适用于公共仓库）。
func Clone(ctx context.Context, repoURL, dest string, opts *CloneOptions) (*CheckoutResult, error) {
	slog.Info("cloning repository",
		"url", repoURL,
		"ref", opts.Ref,
		"depth", opts.Depth,
	)

	refName := plumbing.ReferenceName("refs/heads/" + opts.Ref)

	auth, err := buildAuth(opts.Credential)
	if err != nil {
		return nil, fmt.Errorf("build auth: %w", err)
	}

	_, err = git.PlainCloneContext(ctx, dest, false, &git.CloneOptions{
		URL:          repoURL,
		Auth:         auth,
		ReferenceName: refName,
		Depth:        opts.Depth,
		SingleBranch: true,
		Progress:     os.Stdout,
	})
	if err != nil {
		return nil, fmt.Errorf("git clone: %w", err)
	}

	// 重新打开获取 HEAD commit
	repo, err := git.PlainOpen(dest)
	if err != nil {
		return nil, fmt.Errorf("open cloned repo: %w", err)
	}
	ref, err := repo.Head()
	if err != nil {
		return nil, fmt.Errorf("get HEAD: %w", err)
	}

	slog.Info("repository cloned",
		"ref", opts.Ref,
		"commit", ref.Hash().String(),
		"dest", dest,
	)

	return &CheckoutResult{
		CommitSHA: ref.Hash().String(),
		Ref:       opts.Ref,
		RepoURL:   repoURL,
	}, nil
}

// ─── Auth ──────────────────────────────────────────────

// buildAuth 根据凭证类型创建 go-git 的 AuthMethod。
func buildAuth(cred *secret.Credential) (transport.AuthMethod, error) {
	if cred == nil {
		return nil, nil
	}

	switch cred.Type {
	case secret.CredTypeToken:
		return &githttp.TokenAuth{Token: cred.Value}, nil

	case secret.CredTypeUsernamePass:
		return &githttp.BasicAuth{
			Username: cred.Username,
			Password: cred.Password,
		}, nil

	case secret.CredTypeSSHKey:
		signer, err := gitssh.NewPublicKeys("git", []byte(cred.Value), "")
		if err != nil {
			return nil, fmt.Errorf("parse ssh key: %w", err)
		}
		return signer, nil

	default:
		return nil, nil
	}
}
