package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTempConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoad_ValidConfig(t *testing.T) {
	path := writeTempConfig(t, `{
		"llm": {
			"api_key": "sk-test",
			"base_url": "https://custom.example.com/v1",
			"model": "gpt-4"
		}
	}`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.LLM == nil {
		t.Fatal("cfg.LLM should not be nil")
	}
	if cfg.LLM.APIKey != "sk-test" {
		t.Errorf("APIKey = %q, want %q", cfg.LLM.APIKey, "sk-test")
	}
	if cfg.LLM.BaseURL != "https://custom.example.com/v1" {
		t.Errorf("BaseURL = %q, want %q", cfg.LLM.BaseURL, "https://custom.example.com/v1")
	}
	if cfg.LLM.Model != "gpt-4" {
		t.Errorf("Model = %q, want %q", cfg.LLM.Model, "gpt-4")
	}
}

func TestLoad_ConfigNotFound(t *testing.T) {
	cfg, err := Load("/nonexistent/path/config.json")
	if err != nil {
		t.Fatalf("Load() should return empty config, got error = %v", err)
	}
	if cfg.LLM != nil {
		t.Error("cfg.LLM should be nil for empty config")
	}
}

func TestLoad_InvalidJSON(t *testing.T) {
	path := writeTempConfig(t, `{invalid json`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() should return error for invalid JSON")
	}
}

func TestLoad_EmptyFile(t *testing.T) {
	path := writeTempConfig(t, `{}`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.LLM != nil {
		t.Error("cfg.LLM should be nil for empty config")
	}
}

func TestResolveLLMConfig_FromConfigFile(t *testing.T) {
	cfg := &Config{
		LLM: &LLMConfig{
			APIKey:  "sk-config",
			BaseURL: "https://config.example.com/v1",
			Model:   "config-model",
		},
	}

	// Ensure no env vars interfere
	os.Unsetenv("LLM_API_KEY")
	os.Unsetenv("LLM_BASE_URL")
	os.Unsetenv("LLM_MODEL")

	resolved := ResolveLLMConfig(cfg)
	if resolved.APIKey != "sk-config" {
		t.Errorf("APIKey = %q, want %q", resolved.APIKey, "sk-config")
	}
	if resolved.BaseURL != "https://config.example.com/v1" {
		t.Errorf("BaseURL = %q, want %q", resolved.BaseURL, "https://config.example.com/v1")
	}
	if resolved.Model != "config-model" {
		t.Errorf("Model = %q, want %q", resolved.Model, "config-model")
	}
}

func TestResolveLLMConfig_EnvVarOverrides(t *testing.T) {
	cfg := &Config{
		LLM: &LLMConfig{
			APIKey:  "sk-config",
			BaseURL: "https://config.example.com/v1",
			Model:   "config-model",
		},
	}

	// Set env vars
	os.Setenv("LLM_API_KEY", "sk-env")
	os.Setenv("LLM_BASE_URL", "https://env.example.com/v1")
	os.Setenv("LLM_MODEL", "env-model")
	defer func() {
		os.Unsetenv("LLM_API_KEY")
		os.Unsetenv("LLM_BASE_URL")
		os.Unsetenv("LLM_MODEL")
	}()

	resolved := ResolveLLMConfig(cfg)
	if resolved.APIKey != "sk-env" {
		t.Errorf("APIKey = %q, want %q", resolved.APIKey, "sk-env")
	}
	if resolved.BaseURL != "https://env.example.com/v1" {
		t.Errorf("BaseURL = %q, want %q", resolved.BaseURL, "https://env.example.com/v1")
	}
	if resolved.Model != "env-model" {
		t.Errorf("Model = %q, want %q", resolved.Model, "env-model")
	}
}

func TestResolveLLMConfig_Defaults(t *testing.T) {
	cfg := &Config{}

	os.Unsetenv("LLM_API_KEY")
	os.Unsetenv("LLM_BASE_URL")
	os.Unsetenv("LLM_MODEL")
	os.Unsetenv("OPENAI_API_KEY")

	resolved := ResolveLLMConfig(cfg)
	if resolved.APIKey != "" {
		t.Errorf("APIKey should be empty without env var or config, got %q", resolved.APIKey)
	}
	if resolved.BaseURL != "https://api.openai.com/v1" {
		t.Errorf("BaseURL should default to %q, got %q", "https://api.openai.com/v1", resolved.BaseURL)
	}
	if resolved.Model != "gpt-4o-mini" {
		t.Errorf("Model should default to %q, got %q", "gpt-4o-mini", resolved.Model)
	}
}

func TestResolveLLMConfig_OPENAI_API_KEYFallback(t *testing.T) {
	cfg := &Config{}

	os.Setenv("OPENAI_API_KEY", "sk-openai-fallback")
	defer os.Unsetenv("OPENAI_API_KEY")
	os.Unsetenv("LLM_API_KEY")

	resolved := ResolveLLMConfig(cfg)
	if resolved.APIKey != "sk-openai-fallback" {
		t.Errorf("APIKey should fallback to OPENAI_API_KEY, got %q", resolved.APIKey)
	}
}

func TestResolveWorkspaceConfig(t *testing.T) {
	cfg := &Config{
		Workspace: &WorkspaceConfig{
			BaseDir: "/custom/workspaces",
		},
	}

	resolved := ResolveWorkspaceConfig(cfg)
	if resolved.BaseDir != "/custom/workspaces" {
		t.Errorf("BaseDir = %q, want %q", resolved.BaseDir, "/custom/workspaces")
	}

	// Test default
	cfg2 := &Config{}
	resolved2 := ResolveWorkspaceConfig(cfg2)
	if resolved2.BaseDir != "/tmp/miniflow/workspaces" {
		t.Errorf("BaseDir should default to %q, got %q", "/tmp/miniflow/workspaces", resolved2.BaseDir)
	}
}
