package container

import (
	"testing"
)

// TestBuildMounts_SSHAgent 已移除：SSH 密钥注入逻辑已迁移到
// pipeline/execute.go 的 injectSSHKeys() + executeStep() 中，
// 不再通过 container/docker.go 的 bind mount 实现。

func TestBuildMounts_Workspace(t *testing.T) {
	mounts := buildMounts(Config{
		Workspace: &WorkspaceMount{
			Source: "/tmp/workspace",
			Target: "/workspace",
		},
	})

	found := false
	for _, m := range mounts {
		if m.Target == "/workspace" {
			found = true
			if m.Source != "/tmp/workspace" {
				t.Errorf("expected source /tmp/workspace, got %s", m.Source)
			}
		}
	}
	if !found {
		t.Error("workspace mount not found")
	}
}

func TestBuildMounts_Cache(t *testing.T) {
	mounts := buildMounts(Config{
		CacheMount: []CacheMount{
			{Source: "/cache/go", Target: "/root/.cache/go", ReadOnly: false},
			{Source: "/cache/apt", Target: "/var/cache/apt", ReadOnly: true},
		},
	})

	foundGo := false
	foundApt := false
	for _, m := range mounts {
		if m.Target == "/root/.cache/go" && m.Source == "/cache/go" {
			foundGo = true
		}
		if m.Target == "/var/cache/apt" && m.Source == "/cache/apt" && m.ReadOnly {
			foundApt = true
		}
	}
	if !foundGo {
		t.Error("go cache mount not found")
	}
	if !foundApt {
		t.Error("apt cache mount not found")
	}
}
