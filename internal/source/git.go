package source

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	gitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"
	ssh "golang.org/x/crypto/ssh"

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
	Submodules bool
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
	if auth == nil && isSSHRepository(repoURL) {
		auth, err = hostSSHAuth()
		if err != nil {
			return nil, fmt.Errorf("load host SSH authentication: %w", err)
		}
	}
	if isSSHRepository(repoURL) {
		callback, algorithms, knownHostsPath, err := knownHostsCallback(repoURL)
		if err != nil {
			return nil, fmt.Errorf("load known_hosts: %w", err)
		}
		if callback != nil {
			setHostKeyCallback(auth, callback, algorithms)
			slog.Info("checkout SSH host verification configured", "known_hosts", knownHostsPath, "host_key_algorithms", algorithms)
		}
	}

	cloneOptions := &git.CloneOptions{
		URL:           repoURL,
		Auth:          auth,
		ReferenceName: refName,
		Depth:         opts.Depth,
		SingleBranch:  true,
		Progress:      os.Stdout,
	}
	if opts.Submodules {
		cloneOptions.RecurseSubmodules = git.DefaultSubmoduleRecursionDepth
	}
	_, err = git.PlainCloneContext(ctx, dest, false, cloneOptions)
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

func isSSHRepository(repoURL string) bool {
	return strings.HasPrefix(repoURL, "ssh://") || strings.Contains(repoURL, "@") && strings.Contains(repoURL, ":")
}

// hostSSHAuth reuses the worker host's SSH agent first, then a mounted or
// local id_* private key. Explicit credentials always take precedence.
func hostSSHAuth() (transport.AuthMethod, error) {
	for _, dir := range sshDirectories() {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() || !strings.HasPrefix(name, "id_") || strings.HasSuffix(name, ".pub") {
				continue
			}
			auth, err := gitssh.NewPublicKeysFromFile("git", filepath.Join(dir, name), "")
			if err == nil {
				slog.Info("checkout SSH authentication configured", "source", "private_key", "path", filepath.Join(dir, name))
				return auth, nil
			}
			slog.Debug("skipping host SSH key", "path", filepath.Join(dir, name), "error", err)
		}
	}
	if os.Getenv("SSH_AUTH_SOCK") != "" {
		auth, err := gitssh.NewSSHAgentAuth("git")
		if err == nil {
			slog.Info("checkout SSH authentication configured", "source", "ssh_agent")
			return auth, nil
		}
		slog.Debug("SSH agent unavailable for checkout", "error", err)
	}
	return nil, nil
}

// knownHostsCallback deliberately uses the same SSH directory that supplies
// the key. This prevents a worker container from silently falling back to an
// unrelated home directory's known_hosts file.
func knownHostsCallback(repoURL string) (ssh.HostKeyCallback, []string, string, error) {
	endpoint, err := transport.NewEndpoint(repoURL)
	if err != nil {
		return nil, nil, "", fmt.Errorf("parse repository URL: %w", err)
	}
	port := endpoint.Port
	if port <= 0 {
		port = 22
	}
	hostWithPort := net.JoinHostPort(endpoint.Host, strconv.Itoa(port))
	for _, dir := range sshDirectories() {
		path := filepath.Join(dir, "known_hosts")
		if _, err := os.Stat(path); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, nil, "", err
		}
		db, err := gitssh.NewKnownHostsDb(path)
		if err != nil {
			return nil, nil, "", fmt.Errorf("parse %s: %w", path, err)
		}
		return db.HostKeyCallback(), db.HostKeyAlgorithms(hostWithPort), path, nil
	}
	return nil, nil, "", nil
}

func setHostKeyCallback(auth transport.AuthMethod, callback ssh.HostKeyCallback, algorithms []string) {
	switch auth := auth.(type) {
	case *gitssh.PublicKeys:
		auth.HostKeyCallback = callback
		auth.HostKeyAlgorithms = algorithms
	case *gitssh.PublicKeysCallback:
		auth.HostKeyCallback = callback
		auth.HostKeyAlgorithms = algorithms
	}
}

func sshDirectories() []string {
	dirs := []string{"/miniflow/ssh"}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		dirs = append(dirs, filepath.Join(home, ".ssh"))
	}
	return dirs
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
