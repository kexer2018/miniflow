package fixer

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ─── YAML Structs ─────────────────────────────────────────

// SeedCaseYAML mirrors the YAML structure in seeds/*.yaml.
// YAML field names use snake_case; Go fields use CamelCase.
type SeedCaseYAML struct {
	ID            string           `yaml:"id"`
	Category      string           `yaml:"category"`
	Title         string           `yaml:"title"`
	MatchPatterns []string         `yaml:"match_pattern"`
	RawLogExample string           `yaml:"raw_log_example"`
	SanitizedLog  string           `yaml:"sanitized_log"`
	RootCause     string           `yaml:"root_cause"`
	FixSuggestion FixSuggestionYAML `yaml:"fix_suggestion"`
}

// FixSuggestionYAML mirrors the fix_suggestion field structure in YAML.
type FixSuggestionYAML struct {
	Description    string         `yaml:"description"`
	ConfigOverride map[string]any `yaml:"config_override_example"`
}

// ToSeedCase converts the YAML representation to the internal SeedCase type.
// It handles the field naming differences between the YAML schema and Go struct.
func (y SeedCaseYAML) ToSeedCase() SeedCase {
	fix := FixSuggestion{
		Description: y.FixSuggestion.Description,
	}
	if len(y.FixSuggestion.ConfigOverride) > 0 {
		fix.ConfigOverride = y.FixSuggestion.ConfigOverride
	}

	return SeedCase{
		ID:            y.ID,
		Category:      y.Category,
		Title:         y.Title,
		MatchPatterns: y.MatchPatterns,
		RawLogExample: y.RawLogExample,
		SanitizedLog:  y.SanitizedLog,
		RootCause:     y.RootCause,
		FixSuggestion: fix,
	}
}

// ─── Loading ──────────────────────────────────────────────

// LoadFromYAML reads seed cases from a single YAML file and appends them.
// If a seed with the same ID already exists, the YAML version replaces it.
// The YAML file should contain a list of seed cases:
//
//	- id: "auth-001"
//	  category: "authentication"
//	  ...
func (e *SeedEngine) LoadFromYAML(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read seed file %q: %w", path, err)
	}

	var yamlSeeds []SeedCaseYAML
	if err := yaml.Unmarshal(data, &yamlSeeds); err != nil {
		return fmt.Errorf("parse seed file %q: %w", path, err)
	}

	// Track which IDs in this file replace built-in seeds
	existingIDs := make(map[string]int) // id → index in e.seeds
	for i, s := range e.seeds {
		existingIDs[s.ID] = i
	}

	for _, ys := range yamlSeeds {
		if ys.ID == "" {
			slog.Warn("skipping seed case with empty ID in YAML file", "file", path)
			continue
		}

		sc := ys.ToSeedCase()

		if idx, exists := existingIDs[sc.ID]; exists {
			// Replace existing seed with same ID
			e.seeds[idx] = sc
			slog.Debug("replaced seed case from YAML", "id", sc.ID, "file", path)
		} else {
			// Append new seed
			e.seeds = append(e.seeds, sc)
			existingIDs[sc.ID] = len(e.seeds) - 1
			slog.Debug("loaded seed case from YAML", "id", sc.ID, "file", path)
		}
	}

	return nil
}

// LoadFromDir reads all YAML files matching a glob pattern from a directory.
// It logs warnings for files that fail to read or parse but continues with others.
// If the directory does not exist, it returns nil (not an error).
func (e *SeedEngine) LoadFromDir(dir string, pattern string) error {
	// Check if directory exists
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			slog.Debug("seed directory not found, skipping", "dir", dir)
			return nil
		}
		return fmt.Errorf("stat seed directory %q: %w", dir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("seed path %q is not a directory", dir)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read seed directory %q: %w", dir, err)
	}

	var loadErrors []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !matchesGlob(entry.Name(), pattern) {
			continue
		}

		path := filepath.Join(dir, entry.Name())
		if err := e.LoadFromYAML(path); err != nil {
			slog.Warn("failed to load seed file", "file", path, "error", err)
			loadErrors = append(loadErrors, path+": "+err.Error())
		} else {
			slog.Info("loaded seed file", "file", path)
		}
	}

	if len(loadErrors) > 0 {
		return fmt.Errorf("loaded with errors:\n  %s", strings.Join(loadErrors, "\n  "))
	}

	return nil
}

// matchesGlob checks if a filename matches a simple glob pattern.
// Supports "*" (match any within a segment) and "?" (match one char).
func matchesGlob(name, pattern string) bool {
	if pattern == "" || pattern == "*" || pattern == "*.*" {
		return true
	}
	// Check extension-based pattern like "*.yaml"
	if strings.HasPrefix(pattern, "*.") {
		ext := strings.TrimPrefix(pattern, "*")
		return strings.HasSuffix(name, ext)
	}
	if pattern == name {
		return true
	}
	return false
}
