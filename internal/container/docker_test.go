package container

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBuildMounts_SSHAgent(t *testing.T) {
	// 创建一个假的 ~/.ssh 目录（含密钥和 known_hosts）
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	sshDir := filepath.Join(homeDir, ".ssh")
	if err := os.MkdirAll(sshDir, 0755); err != nil {
		t.Fatal(err)
	}

	// 写入假密钥
	fakeKey := filepath.Join(sshDir, "id_ed25519")
	if err := os.WriteFile(fakeKey, []byte("fake-private-key\n"), 0600); err != nil {
		t.Fatal(err)
	}

	// 写入假 known_hosts
	fakeKnownHosts := filepath.Join(sshDir, "known_hosts")
	if err := os.WriteFile(fakeKnownHosts, []byte("github.com ssh-ed25519 AAAAC3...\n"), 0644); err != nil {
		t.Fatal(err)
	}

	t.Run("ssh_agent=true mounts ~/.ssh and known_hosts", func(t *testing.T) {
		cfg := Config{SSHAgent: true}
		mounts := buildMounts(cfg)

		foundSSH := false
		foundKnownHosts := false
		for _, m := range mounts {
			if m.Target == "/tmp/.ssh" {
				foundSSH = true
				if m.Source != sshDir {
					t.Errorf("expected source %q, got %q", sshDir, m.Source)
				}
				if !m.ReadOnly {
					t.Error("~/.ssh mount should be read-only")
				}
			}
			if m.Target == "/etc/ssh/ssh_known_hosts" {
				foundKnownHosts = true
				if !m.ReadOnly {
					t.Error("known_hosts mount should be read-only")
				}
			}
		}
		if !foundSSH {
			t.Error("~/.ssh mount not found when SSHAgent=true")
		}
		if !foundKnownHosts {
			t.Error("known_hosts mount not found when SSHAgent=true")
		}
	})

	t.Run("ssh_agent=false adds no ssh mounts", func(t *testing.T) {
		cfg := Config{SSHAgent: false}
		mounts := buildMounts(cfg)

		for _, m := range mounts {
			if m.Target == "/tmp/.ssh" || m.Target == "/etc/ssh/ssh_known_hosts" {
				t.Errorf("unexpected ssh mount %q when SSHAgent=false", m.Target)
			}
		}
	})

	t.Run("ssh_agent=true without ~/.ssh warns but doesn't crash", func(t *testing.T) {
		noSSHDir := t.TempDir()
		t.Setenv("HOME", noSSHDir)
		cfg := Config{SSHAgent: true}
		mounts := buildMounts(cfg)

		for _, m := range mounts {
			if m.Target == "/tmp/.ssh" {
				t.Error("should not mount ~/.ssh when directory doesn't exist")
			}
		}
	})
}

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
