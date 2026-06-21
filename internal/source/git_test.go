package source

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/kexer2018/miniflow/internal/secret"
)

// setupTestRepo 在临时目录创建一个 git 仓库并返回路径和 HEAD commit SHA。
func setupTestRepo(t *testing.T, branch string) (repoPath, commitSHA string) {
	t.Helper()
	repoPath = t.TempDir()

	r, err := git.PlainInitWithOptions(repoPath, &git.PlainInitOptions{
		InitOptions: git.InitOptions{
			DefaultBranch: plumbing.NewBranchReferenceName(branch),
		},
	})
	if err != nil {
		t.Fatal("init repo:", err)
	}

	if err := os.WriteFile(filepath.Join(repoPath, "README.md"), []byte("# test repo\n"), 0644); err != nil {
		t.Fatal("write file:", err)
	}

	w, err := r.Worktree()
	if err != nil {
		t.Fatal("get worktree:", err)
	}

	if _, err := w.Add("README.md"); err != nil {
		t.Fatal("git add:", err)
	}

	hash, err := w.Commit("initial commit", &git.CommitOptions{
		Author: &object.Signature{
			Name:  "Test User",
			Email: "test@example.com",
			When:  time.Now(),
		},
	})
	if err != nil {
		t.Fatal("commit:", err)
	}

	return repoPath, hash.String()
}

func TestClone_LocalRepo(t *testing.T) {
	ctx := context.Background()
	src, wantSHA := setupTestRepo(t, "main")
	dest := t.TempDir()

	result, err := Clone(ctx, src, dest, &CloneOptions{
		Ref:   "main",
		Depth: 1,
	})
	if err != nil {
		t.Fatal("Clone:", err)
	}

	if result.CommitSHA != wantSHA {
		t.Errorf("CommitSHA = %q, want %q", result.CommitSHA, wantSHA)
	}
	if result.Ref != "main" {
		t.Errorf("Ref = %q, want %q", result.Ref, "main")
	}
	if result.RepoURL != src {
		t.Errorf("RepoURL = %q, want %q", result.RepoURL, src)
	}

	if _, err := os.Stat(filepath.Join(dest, "README.md")); err != nil {
		t.Error("cloned README.md not found:", err)
	}
}

func TestClone_MasterBranch(t *testing.T) {
	ctx := context.Background()
	src, wantSHA := setupTestRepo(t, "master")
	dest := t.TempDir()

	result, err := Clone(ctx, src, dest, &CloneOptions{
		Ref:   "master",
		Depth: 1,
	})
	if err != nil {
		t.Fatal("Clone:", err)
	}
	if result.CommitSHA != wantSHA {
		t.Errorf("CommitSHA = %q, want %q", result.CommitSHA, wantSHA)
	}
}

func TestClone_InvalidRef(t *testing.T) {
	ctx := context.Background()
	src, _ := setupTestRepo(t, "main")
	dest := t.TempDir()

	_, err := Clone(ctx, src, dest, &CloneOptions{
		Ref:   "nonexistent-branch",
		Depth: 1,
	})
	if err == nil {
		t.Fatal("expected error for nonexistent branch, got nil")
	}
}

func TestClone_InvalidURL(t *testing.T) {
	ctx := context.Background()
	dest := t.TempDir()

	_, err := Clone(ctx, "/nonexistent/path", dest, &CloneOptions{
		Ref:   "main",
		Depth: 1,
	})
	if err == nil {
		t.Fatal("expected error for invalid URL, got nil")
	}
}

func TestBuildAuth_NilCredential(t *testing.T) {
	auth, err := buildAuth(nil)
	if err != nil {
		t.Fatal("buildAuth(nil):", err)
	}
	if auth != nil {
		t.Error("expected nil auth for nil credential")
	}
}

func TestBuildAuth_Token(t *testing.T) {
	auth, err := buildAuth(&secret.Credential{
		Type:  secret.CredTypeToken,
		Value: "ghp_testtoken",
	})
	if err != nil {
		t.Fatal("buildAuth(token):", err)
	}
	if auth == nil {
		t.Fatal("expected non-nil auth")
	}
}

func TestBuildAuth_UsernamePass(t *testing.T) {
	auth, err := buildAuth(&secret.Credential{
		Type:     secret.CredTypeUsernamePass,
		Username: "user",
		Password: "pass",
	})
	if err != nil {
		t.Fatal("buildAuth(username/password):", err)
	}
	if auth == nil {
		t.Fatal("expected non-nil auth")
	}
}
