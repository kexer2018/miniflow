// Package config provides miniflow configuration loading and resolution.
//
// Priority (highest to lowest):
//
//	1. CLI flags
//	2. Environment variables
//	3. Config file (JSON)
//	4. Code defaults
//
// Config file search paths:
//  1. --config <path>  (explicit flag)
//  2. ./.miniflow.json  (project root)
//  3. ~/.miniflow.json  (user home)
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// ─── Config File Structure ───────────────────────────────

// Config represents the complete miniflow configuration.
type Config struct {
	LLM       *LLMConfig       `json:"llm,omitempty"`
	Workspace *WorkspaceConfig `json:"workspace,omitempty"`
}

// LLMConfig configures the LLM client.
type LLMConfig struct {
	APIKey  string `json:"api_key,omitempty"`
	BaseURL string `json:"base_url,omitempty"`
	Model   string `json:"model,omitempty"`
}

// WorkspaceConfig configures the workspace manager.
type WorkspaceConfig struct {
	BaseDir string `json:"base_dir,omitempty"`
}

// ─── Constants ─────────────────────────────────────────────

const (
	envAPIKey    = "LLM_API_KEY"
	envBaseURL   = "LLM_BASE_URL"
	envModel     = "LLM_MODEL"
	defaultURL   = "https://api.openai.com/v1"
	defaultModel = "gpt-4o-mini"
)

// ─── Loading ───────────────────────────────────────────────

// Load reads configuration from the given file paths.
// Returns the first successfully loaded config.
// If all paths are missing files, returns an empty Config (no error).
// If a file exists but is invalid (parse error), returns the error.
func Load(paths ...string) (*Config, error) {
	for _, p := range paths {
		if p == "" {
			continue
		}
		cfg, err := loadFile(p)
		if err == nil {
			slog.Debug("config loaded", "file", p)
			return cfg, nil
		}
		// File exists but is invalid — propagate error
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("config: %s: %w", p, err)
		}
		// File not found — try next path
		slog.Debug("config not found", "file", p, "error", err)
	}
	slog.Debug("no config file found, using defaults")
	return &Config{}, nil
}

// LoadDefault searches the default paths and loads the first found.
// Search order:
//  1. ./.miniflow.json (current directory)
//  2. ~/.miniflow.json (user home directory)
func LoadDefault() *Config {
	paths := defaultPaths()
	cfg, err := Load(paths...)
	if err != nil {
		slog.Warn("config error, using defaults", "error", err)
		return &Config{}
	}
	return cfg
}

// LoadWithFlag searches with an explicit --config flag first, then falls back to defaults.
// If the flag is set but the file doesn't exist, returns an error.
func LoadWithFlag(flagPath string) (*Config, error) {
	if flagPath != "" {
		cfg, err := loadFile(flagPath)
		if err != nil {
			return nil, fmt.Errorf("config: %s: %w", flagPath, err)
		}
		return cfg, nil
	}
	return LoadDefault(), nil
}

// ─── Env Resolution ────────────────────────────────────────

// ResolvedLLMConfig holds the final resolved LLM configuration.
type ResolvedLLMConfig struct {
	APIKey  string
	BaseURL string
	Model   string
}

// ResolveLLMConfig merges config file values + env var overrides + defaults.
//
// Priority:
//  1. Environment variable LLM_API_KEY / LLM_BASE_URL / LLM_MODEL
//  2. Config file value (from JSON)
//  3. Code default
func ResolveLLMConfig(cfg *Config) ResolvedLLMConfig {
	r := ResolvedLLMConfig{}

	// 1. Config file values as base
	if cfg.LLM != nil {
		r.APIKey = cfg.LLM.APIKey
		r.BaseURL = cfg.LLM.BaseURL
		r.Model = cfg.LLM.Model
	}

	// 2. Environment variables override config file
	if v := os.Getenv(envAPIKey); v != "" {
		r.APIKey = v
	}
	// Fallback: OPENAI_API_KEY if LLM_API_KEY is not set
	if r.APIKey == "" {
		r.APIKey = os.Getenv("OPENAI_API_KEY")
	}

	if v := os.Getenv(envBaseURL); v != "" {
		r.BaseURL = v
	}
	if r.BaseURL == "" {
		r.BaseURL = defaultURL
	}
	r.BaseURL = strings.TrimRight(r.BaseURL, "/")

	if v := os.Getenv(envModel); v != "" {
		r.Model = v
	}
	if r.Model == "" {
		r.Model = defaultModel
	}

	return r
}

// ResolvedWorkspaceConfig holds the final resolved workspace configuration.
type ResolvedWorkspaceConfig struct {
	BaseDir string
}

// ResolveWorkspaceConfig merges config file + env vars + defaults.
// Currently only reads from config file; no env var override for workspace.
func ResolveWorkspaceConfig(cfg *Config) ResolvedWorkspaceConfig {
	r := ResolvedWorkspaceConfig{
		BaseDir: "/tmp/miniflow/workspaces",
	}
	if cfg.Workspace != nil && cfg.Workspace.BaseDir != "" {
		r.BaseDir = cfg.Workspace.BaseDir
	}
	return r
}

// ─── Internal ──────────────────────────────────────────────

func defaultPaths() []string {
	paths := []string{"./.miniflow.json"}
	home, err := os.UserHomeDir()
	if err == nil && home != "" {
		paths = append(paths, filepath.Join(home, ".miniflow.json"))
	}
	return paths
}

func loadFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err // returns fs.ErrNotExist for missing files
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}

	return &cfg, nil
}
