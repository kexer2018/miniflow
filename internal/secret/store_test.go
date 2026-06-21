package secret

import (
	"os"
	"testing"
)

func writeTempFile(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp("", "credentials-*.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return f.Name()
}

func TestLoadFromFile(t *testing.T) {
	path := writeTempFile(t, `{
		"version": "1",
		"credentials": [
			{"id": "gh", "match": "github.com/myorg", "type": "token", "value": "tok_abc"},
			{"id": "dh", "match": "docker.io", "type": "username_password", "username": "u", "password": "p"}
		]
	}`)
	defer os.Remove(path)

	store, err := LoadFromFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(store.creds) != 2 {
		t.Fatalf("expected 2 credentials, got %d", len(store.creds))
	}
}

func TestLoadFromFileNotFound(t *testing.T) {
	store, err := LoadFromFile("/nonexistent/creds.json")
	if err != nil {
		t.Fatal(err)
	}
	if store == nil || len(store.creds) != 0 {
		t.Error("expected empty store for missing file")
	}
}

func TestLoadFromFileEmptyPath(t *testing.T) {
	store, err := LoadFromFile("")
	if err != nil {
		t.Fatal(err)
	}
	if store == nil || len(store.creds) != 0 {
		t.Error("expected empty store for empty path")
	}
}

func TestMatchLongestPrefix(t *testing.T) {
	store := &CredentialStore{creds: []*Credential{
		{ID: "catchall", Match: "*", Value: "fallback"},
		{ID: "org", Match: "github.com/myorg", Value: "org_tok"},
		{ID: "domain", Match: "github.com", Value: "domain_tok"},
	}}

	// 最长匹配 "github.com/myorg" > "github.com" > "*"
	if c := store.Match("github.com/myorg/myrepo"); c.ID != "org" {
		t.Errorf("expected 'org', got %q", c.ID)
	}

	// 没有 org 级匹配 → "github.com"
	if c := store.Match("github.com/other/repo"); c.ID != "domain" {
		t.Errorf("expected 'domain', got %q", c.ID)
	}

	// gitlab → 兜底
	if c := store.Match("gitlab.com/org/repo"); c.ID != "catchall" {
		t.Errorf("expected 'catchall', got %q", c.ID)
	}
}

func TestMatchNoMatch(t *testing.T) {
	store := NewCredentialStore()
	if c := store.Match("github.com/org/repo"); c != nil {
		t.Error("expected nil for empty store")
	}
}

func TestResolveSecret(t *testing.T) {
	store := &CredentialStore{creds: []*Credential{
		{ID: "my-token", Type: CredTypeToken, Value: "secret_val"},
		{ID: "my-env", Type: CredTypeEnv, Value: "MY_KEY=my_val"},
	}}

	val, ok := store.ResolveSecret("my-token")
	if !ok || val != "secret_val" {
		t.Errorf("expected 'secret_val', got %q (ok=%v)", val, ok)
	}

	val, ok = store.ResolveSecret("my-env")
	if !ok || val != "MY_KEY=my_val" {
		t.Errorf("expected 'MY_KEY=my_val', got %q", val)
	}

	_, ok = store.ResolveSecret("nonexistent")
	if ok {
		t.Error("expected ok=false for nonexistent secret")
	}
}

func TestAllSecretsMap(t *testing.T) {
	store := &CredentialStore{creds: []*Credential{
		{ID: "t1", Type: CredTypeToken, Value: "tok_val"},
		{ID: "e1", Type: CredTypeEnv, Value: "ENV_KEY=env_val"},
	}}
	m := store.AllSecretsMap()
	if m["t1"] != "tok_val" {
		t.Errorf("expected 'tok_val', got %q", m["t1"])
	}
	if m["ENV_KEY"] != "env_val" {
		t.Errorf("expected 'env_val', got %q", m["ENV_KEY"])
	}
}

func TestMustLoad(t *testing.T) {
	store := MustLoad("/nonexistent/path.json")
	if store == nil {
		t.Fatal("MustLoad should never return nil")
	}
}

func TestLoadFromFileParseError(t *testing.T) {
	f, err := os.CreateTemp("", "bad-*.json")
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("{invalid json}")
	f.Close()
	defer os.Remove(f.Name())

	_, err = LoadFromFile(f.Name())
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestLoadFromFileReadError(t *testing.T) {
	_, err := LoadFromFile("/proc/1/root/nonexistent")
	// should not error (file not found = empty store)
	if err != nil {
		t.Errorf("expected nil for non-existent file, got: %v", err)
	}
}

func TestResolveSecretEnv(t *testing.T) {
	store := &CredentialStore{creds: []*Credential{
		{ID: "TOKEN", Type: CredTypeToken, Value: "sgp_xxx"},
		{ID: "NPM_TOKEN", Type: CredTypeEnv, Value: "NPM_TOKEN=npm_abc"},
	}}

	// token type → "id=value"
	val, ok := store.ResolveSecretEnv("TOKEN")
	if !ok || val != "TOKEN=sgp_xxx" {
		t.Errorf("expected 'TOKEN=sgp_xxx', got %q (ok=%v)", val, ok)
	}

	// env type → "KEY=VALUE" as-is
	val, ok = store.ResolveSecretEnv("NPM_TOKEN")
	if !ok || val != "NPM_TOKEN=npm_abc" {
		t.Errorf("expected 'NPM_TOKEN=npm_abc', got %q", val)
	}

	// nonexistent
	_, ok = store.ResolveSecretEnv("nonexistent")
	if ok {
		t.Error("expected ok=false for nonexistent secret")
	}

	// nil store
	var nilStore *CredentialStore
	_, ok = nilStore.ResolveSecretEnv("test")
	if ok {
		t.Error("expected ok=false for nil store")
	}
}
