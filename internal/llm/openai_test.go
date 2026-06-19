package llm

import (
	"os"
	"testing"
	"time"
)

func TestNewOpenAIClient_Defaults(t *testing.T) {
	// Set env vars for testing
	t.Setenv("LLM_API_KEY", "test-key-123")
	t.Setenv("LLM_BASE_URL", "https://custom.api.com/v1")
	t.Setenv("LLM_MODEL", "test-model-v2")

	client := NewOpenAIClient()
	if client == nil {
		t.Fatal("NewOpenAIClient() should not return nil")
	}

	if client.apiKey != "test-key-123" {
		t.Errorf("expected apiKey 'test-key-123', got %q", client.apiKey)
	}
	if client.baseURL != "https://custom.api.com/v1" {
		t.Errorf("expected baseURL 'https://custom.api.com/v1', got %q", client.baseURL)
	}
	if client.model != "test-model-v2" {
		t.Errorf("expected model 'test-model-v2', got %q", client.model)
	}
}

func TestNewOpenAIClient_FallbackEnv(t *testing.T) {
	t.Setenv("LLM_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "openai-fallback-key")

	client := NewOpenAIClient()
	if client.apiKey != "openai-fallback-key" {
		t.Errorf("expected fallback apiKey 'openai-fallback-key', got %q", client.apiKey)
	}
}

func TestNewOpenAIClient_DefaultValues(t *testing.T) {
	client := NewOpenAIClient()
	if client.baseURL != "https://api.openai.com/v1" {
		t.Errorf("expected default baseURL 'https://api.openai.com/v1', got %q", client.baseURL)
	}
	if client.model != DefaultModel {
		t.Errorf("expected default model %q, got %q", DefaultModel, client.model)
	}
}

func TestNewOpenAIClient_BaseURLTrimmed(t *testing.T) {
	t.Setenv("LLM_BASE_URL", "https://api.openai.com/v1/")

	client := NewOpenAIClient()
	if client.baseURL != "https://api.openai.com/v1" {
		t.Errorf("trailing slash should be trimmed, got %q", client.baseURL)
	}
}

func TestNewOpenAIClient_TimeoutSet(t *testing.T) {
	client := NewOpenAIClient()
	if client.httpClient.Timeout != 120*time.Second {
		t.Errorf("expected timeout 120s, got %v", client.httpClient.Timeout)
	}
}

func TestNewOpenAIClientWithConfig(t *testing.T) {
	client := NewOpenAIClientWithConfig("cfg-key", "https://cfg.url/v1", "cfg-model")
	if client.apiKey != "cfg-key" {
		t.Errorf("expected apiKey 'cfg-key', got %q", client.apiKey)
	}
	if client.baseURL != "https://cfg.url/v1" {
		t.Errorf("expected baseURL 'https://cfg.url/v1', got %q", client.baseURL)
	}
	if client.model != "cfg-model" {
		t.Errorf("expected model 'cfg-model', got %q", client.model)
	}
	if client.httpClient.Timeout != 120*time.Second {
		t.Errorf("expected timeout 120s, got %v", client.httpClient.Timeout)
	}
}

func TestNewOpenAIClientWithConfig_TrailingSlash(t *testing.T) {
	client := NewOpenAIClientWithConfig("k", "https://url.com/v1/", "m")
	if client.baseURL != "https://url.com/v1" {
		t.Errorf("trailing slash should be trimmed, got %q", client.baseURL)
	}
}

func TestOpenAIClient_Model(t *testing.T) {
	client := NewOpenAIClientWithConfig("k", "https://url/v1", "my-model")
	if client.Model() != "my-model" {
		t.Errorf("expected 'my-model', got %q", client.Model())
	}
}

func TestNewDefaultClient_Success(t *testing.T) {
	t.Setenv("LLM_API_KEY", "test-key")
	client, err := NewDefaultClient()
	if err != nil {
		t.Fatalf("NewDefaultClient() should succeed with API key: %v", err)
	}
	if client == nil {
		t.Fatal("NewDefaultClient() should not return nil")
	}
}

func TestNewDefaultClient_NoAPIKey(t *testing.T) {
	// Ensure no API key is set
	os.Unsetenv("LLM_API_KEY")
	os.Unsetenv("OPENAI_API_KEY")

	_, err := NewDefaultClient()
	if err == nil {
		t.Fatal("NewDefaultClient() should fail without API key")
	}
}

func TestNewOpenAIClient_ModelEnvVar(t *testing.T) {
	t.Setenv("LLM_MODEL", "gpt-4")
	client := NewOpenAIClient()
	if client.model != "gpt-4" {
		t.Errorf("expected model 'gpt-4', got %q", client.model)
	}
}
