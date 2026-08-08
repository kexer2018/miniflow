package container

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCachePathDoesNotEmbedUntrustedKey(t *testing.T) {
	manager := NewWorkspaceManager(t.TempDir())
	path := manager.CachePath("../../outside")
	if filepath.Dir(path) != filepath.Join(manager.BaseDir, ".cache") {
		t.Fatalf("cache path escaped its root: %s", path)
	}
}

func TestPruneWorkspaceAndCacheDirectories(t *testing.T) {
	manager := NewWorkspaceManager(t.TempDir())
	oldWorkspace := filepath.Join(manager.BaseDir, "old-run")
	newWorkspace := filepath.Join(manager.BaseDir, "new-run")
	oldCache := manager.CachePath("old-cache")
	for _, path := range []string{oldWorkspace, newWorkspace, oldCache} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(oldWorkspace, old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(oldCache, old, old); err != nil {
		t.Fatal(err)
	}
	if removed, err := manager.PruneWorkspacesOlderThan(time.Now().Add(-24 * time.Hour)); err != nil || removed != 1 {
		t.Fatalf("unexpected workspace prune result %d, %v", removed, err)
	}
	if _, err := os.Stat(newWorkspace); err != nil {
		t.Fatalf("new workspace should remain: %v", err)
	}
	if removed, err := manager.PruneCachesOlderThan(time.Now().Add(-24 * time.Hour)); err != nil || removed != 1 {
		t.Fatalf("unexpected cache prune result %d, %v", removed, err)
	}
}
