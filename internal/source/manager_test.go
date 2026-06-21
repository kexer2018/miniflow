package source

import (
	"context"
	"testing"

	"github.com/kexer2018/miniflow/internal/secret"
	pipelinespec "github.com/kexer2018/miniflow/pkg/pipeline"
)

func TestNewManager_NilStore(t *testing.T) {
	mgr := NewManager(nil)
	if mgr == nil {
		t.Fatal("NewManager(nil) returned nil")
	}
	if mgr.credStore != nil {
		t.Error("expected nil credStore")
	}
}

func TestNewManager_WithStore(t *testing.T) {
	store := secret.NewCredentialStore()
	mgr := NewManager(store)
	if mgr == nil {
		t.Fatal("NewManager returned nil")
	}
	if mgr.credStore == nil {
		t.Error("expected non-nil credStore")
	}
}

func TestPrepareWorkspace_NilSpec(t *testing.T) {
	ctx := context.Background()
	mgr := NewManager(nil)

	result, err := mgr.PrepareWorkspace(ctx, nil, "/tmp/dest")
	if err != nil {
		t.Fatal("PrepareWorkspace with nil spec:", err)
	}
	if result != nil {
		t.Error("expected nil result for nil spec")
	}
}

func TestBuildCloneURL_NilCredential(t *testing.T) {
	url := "https://github.com/user/repo.git"
	result := buildCloneURL(url, nil)
	if result != url {
		t.Errorf("buildCloneURL = %q, want %q", result, url)
	}
}

func TestBuildCloneURL_WithCredential(t *testing.T) {
	url := "https://github.com/user/repo.git"
	cred := &secret.Credential{
		Type:  secret.CredTypeToken,
		Value: "token",
	}
	result := buildCloneURL(url, cred)
	if result != url {
		t.Errorf("buildCloneURL = %q, want %q", result, url)
	}
}

func TestPrepareWorkspace_InvalidURL(t *testing.T) {
	ctx := context.Background()
	store := secret.NewCredentialStore()
	mgr := NewManager(store)

	_, err := mgr.PrepareWorkspace(ctx, &pipelinespec.SourceSpec{
		Repository: "/nonexistent/path",
		Ref:        "main",
		Depth:      1,
	}, t.TempDir())
	if err == nil {
		t.Fatal("expected error for invalid repo URL, got nil")
	}
}
