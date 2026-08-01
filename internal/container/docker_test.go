package container

import (
	"bytes"
	"reflect"
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

func TestCallbackWriterEmitsCompleteLines(t *testing.T) {
	var lines []string
	var out bytes.Buffer
	w := &callbackWriter{
		buffer: &out,
		callback: func(line string) {
			lines = append(lines, line)
		},
	}

	if _, err := w.Write([]byte("one\nt")); err != nil {
		t.Fatalf("write first chunk: %v", err)
	}
	if _, err := w.Write([]byte("wo\r\nthree")); err != nil {
		t.Fatalf("write second chunk: %v", err)
	}
	w.flush()

	want := []string{"one", "two", "three"}
	if !reflect.DeepEqual(lines, want) {
		t.Fatalf("lines mismatch\nwant %#v\n got %#v", want, lines)
	}
	if out.String() != "one\ntwo\r\nthree" {
		t.Fatalf("buffer mismatch: %q", out.String())
	}
}
