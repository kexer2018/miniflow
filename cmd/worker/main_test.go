package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsLoopbackListen(t *testing.T) {
	for _, address := range []string{"127.0.0.1:9090", "[::1]:9090", "localhost:9090"} {
		if !isLoopbackListen(address) {
			t.Fatalf("expected %q to be loopback", address)
		}
	}
	for _, address := range []string{":9090", "0.0.0.0:9090", "192.168.1.2:9090", "invalid"} {
		if isLoopbackListen(address) {
			t.Fatalf("expected %q not to be loopback", address)
		}
	}
}

func TestResolveAPIToken(t *testing.T) {
	if _, err := resolveAPIToken("inline", "file"); err == nil {
		t.Fatal("expected conflicting token sources to fail")
	}
	path := filepath.Join(t.TempDir(), "api-token")
	if err := os.WriteFile(path, []byte(" token-from-file\n"), 0600); err != nil {
		t.Fatal(err)
	}
	token, err := resolveAPIToken("", path)
	if err != nil || token != "token-from-file" {
		t.Fatalf("unexpected token result %q, %v", token, err)
	}
}
