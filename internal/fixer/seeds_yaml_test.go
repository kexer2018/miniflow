package fixer

import (
	"os"
	"path/filepath"
	"testing"
)

// ─── YAML Test Fixtures ────────────────────────────────────

const validSeedYAML = `
- id: "custom-001"
  category: "network"
  title: "Custom DNS Failure"
  match_pattern:
    - "dns lookup failed"
    - "cannot resolve"
  raw_log_example: "dns lookup failed for example.com"
  sanitized_log: "dns lookup failed for ***"
  root_cause: "Custom DNS failure root cause"
  fix_suggestion:
    description: "Check DNS configuration"
`

const overrideSeedYAML = `
- id: "auth-001"
  category: "authentication"
  title: "Custom 401 Handler"
  match_pattern:
    - "401"
  raw_log_example: "401 error"
  sanitized_log: "401 error"
  root_cause: "Custom: authentication failed"
  fix_suggestion:
    description: "Custom fix: check credentials"
`

const partialSeedYAML = `
- id: "partial-001"
  category: "app_code"
  title: "Partial Seed"
  match_pattern:
    - "custom error"
  raw_log_example: "custom error occurred"
  sanitized_log: "custom error"
  root_cause: "Custom application error"
  fix_suggestion:
    description: "Fix the custom error"
`

const invalidYAML = `
- id: "bad-yaml"
  category: "test
  title: "Bad YAML
  match_pattern:
    - "test"
`

func writeTempYAML(t *testing.T, dir, filename, content string) string {
	t.Helper()
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write temp file %s: %v", path, err)
	}
	return path
}

// ─── LoadFromYAML Tests ────────────────────────────────────

func TestLoadFromYAML_ValidFile(t *testing.T) {
	engine := NewSeedEngine()
	dir := t.TempDir()
	path := writeTempYAML(t, dir, "custom.yaml", validSeedYAML)

	if err := engine.LoadFromYAML(path); err != nil {
		t.Fatalf("LoadFromYAML failed: %v", err)
	}

	// Should have one additional seed
	seeds := engine.Seeds()
	found := false
	for _, s := range seeds {
		if s.ID == "custom-001" {
			found = true
			if s.Category != "network" {
				t.Errorf("expected category 'network', got %q", s.Category)
			}
			if s.Title != "Custom DNS Failure" {
				t.Errorf("expected title 'Custom DNS Failure', got %q", s.Title)
			}
			if len(s.MatchPatterns) != 2 {
				t.Errorf("expected 2 match patterns, got %d", len(s.MatchPatterns))
			}
			if s.RootCause != "Custom DNS failure root cause" {
				t.Errorf("unexpected root cause: %q", s.RootCause)
			}
			if s.FixSuggestion.Description != "Check DNS configuration" {
				t.Errorf("unexpected fix description: %q", s.FixSuggestion.Description)
			}
			break
		}
	}
	if !found {
		t.Error("custom-001 seed not found after loading")
	}
}

func TestLoadFromYAML_FileNotFound(t *testing.T) {
	engine := NewSeedEngine()
	err := engine.LoadFromYAML("/nonexistent/path/seeds.yaml")
	if err == nil {
		t.Fatal("expected error for non-existent file")
	}
}

func TestLoadFromYAML_InvalidYAML(t *testing.T) {
	engine := NewSeedEngine()
	dir := t.TempDir()
	path := writeTempYAML(t, dir, "bad.yaml", invalidYAML)

	err := engine.LoadFromYAML(path)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestLoadFromYAML_OverrideBuiltin(t *testing.T) {
	engine := NewSeedEngine()

	// Verify auth-001 exists as built-in before override
	preSeeds := engine.Seeds()
	var preFound bool
	for _, s := range preSeeds {
		if s.ID == "auth-001" {
			preFound = true
			if s.Title == "Custom 401 Handler" {
				t.Fatal("auth-001 should not be overridden before loading")
			}
			break
		}
	}
	if !preFound {
		t.Skip("built-in auth-001 not found, skipping override test")
	}

	dir := t.TempDir()
	path := writeTempYAML(t, dir, "override.yaml", overrideSeedYAML)

	if err := engine.LoadFromYAML(path); err != nil {
		t.Fatalf("LoadFromYAML failed: %v", err)
	}

	// Verify auth-001 is now overridden
	postSeeds := engine.Seeds()
	var overridden bool
	for _, s := range postSeeds {
		if s.ID == "auth-001" {
			overridden = true
			if s.Title != "Custom 401 Handler" {
				t.Errorf("expected title 'Custom 401 Handler' after override, got %q", s.Title)
			}
			if s.RootCause != "Custom: authentication failed" {
				t.Errorf("expected custom root cause, got %q", s.RootCause)
			}
			// Only one match pattern after override
			if len(s.MatchPatterns) != 1 || s.MatchPatterns[0] != "401" {
				t.Errorf("unexpected match patterns after override: %v", s.MatchPatterns)
			}
			break
		}
	}
	if !overridden {
		t.Error("auth-001 not found after loading")
	}
}

func TestLoadFromYAML_PartialFields(t *testing.T) {
	engine := NewSeedEngine()
	dir := t.TempDir()
	path := writeTempYAML(t, dir, "partial.yaml", partialSeedYAML)

	if err := engine.LoadFromYAML(path); err != nil {
		t.Fatalf("LoadFromYAML failed: %v", err)
	}

	// Verify the seed loaded without config_override_example
	seeds := engine.Seeds()
	for _, s := range seeds {
		if s.ID == "partial-001" {
			if s.FixSuggestion.ConfigOverride != nil {
				t.Errorf("expected nil ConfigOverride for partial seed, got %v", s.FixSuggestion.ConfigOverride)
			}
			return
		}
	}
	t.Error("partial-001 not found")
}

// ─── LoadFromDir Tests ─────────────────────────────────────

func TestLoadFromDir_MultipleFiles(t *testing.T) {
	engine := NewSeedEngine()
	dir := t.TempDir()

	// Count built-in seeds
	builtInCount := len(engine.Seeds())

	writeTempYAML(t, dir, "custom1.yaml", validSeedYAML)
	writeTempYAML(t, dir, "custom2.yaml", partialSeedYAML)

	if err := engine.LoadFromDir(dir, "*.yaml"); err != nil {
		t.Fatalf("LoadFromDir failed: %v", err)
	}

	seeds := engine.Seeds()
	expectedCount := builtInCount + 2
	if len(seeds) != expectedCount {
		t.Errorf("expected %d seeds (built-in %d + 2), got %d", expectedCount, builtInCount, len(seeds))
	}
}

func TestLoadFromDir_NoFiles(t *testing.T) {
	engine := NewSeedEngine()
	dir := t.TempDir()

	if err := engine.LoadFromDir(dir, "*.yaml"); err != nil {
		t.Fatalf("LoadFromDir on empty dir failed: %v", err)
	}

	// Count should match built-in only
	if len(engine.Seeds()) != len(DefaultSeeds()) {
		t.Errorf("expected %d seeds, got %d", len(DefaultSeeds()), len(engine.Seeds()))
	}
}

func TestLoadFromDir_MissingDir(t *testing.T) {
	engine := NewSeedEngine()
	err := engine.LoadFromDir("/nonexistent/seeds/dir", "*.yaml")
	if err != nil {
		t.Fatalf("LoadFromDir on missing dir should not error, got: %v", err)
	}
}

func TestLoadFromDir_PartialFailure(t *testing.T) {
	engine := NewSeedEngine()
	dir := t.TempDir()

	// Valid file
	writeTempYAML(t, dir, "good.yaml", validSeedYAML)
	// Invalid file
	writeTempYAML(t, dir, "bad.yaml", invalidYAML)

	err := engine.LoadFromDir(dir, "*.yaml")
	if err == nil {
		t.Fatal("expected error for partial failure")
	}

	// The valid file should still have been loaded
	seeds := engine.Seeds()
	found := false
	for _, s := range seeds {
		if s.ID == "custom-001" {
			found = true
			break
		}
	}
	if !found {
		t.Error("custom-001 should have been loaded despite partial failure")
	}
}

func TestLoadFromDir_NonYamlFiles(t *testing.T) {
	engine := NewSeedEngine()
	dir := t.TempDir()

	// Write a .txt file that should be ignored
	txtPath := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(txtPath, []byte("not yaml"), 0644); err != nil {
		t.Fatalf("failed to write txt file: %v", err)
	}

	if err := engine.LoadFromDir(dir, "*.yaml"); err != nil {
		t.Fatalf("LoadFromDir failed: %v", err)
	}

	// Count should be unchanged (no YAML files loaded)
	if len(engine.Seeds()) != len(DefaultSeeds()) {
		t.Errorf("expected %d seeds, got %d", len(DefaultSeeds()), len(engine.Seeds()))
	}
}

func TestLoadFromDir_EmptyDir(t *testing.T) {
	engine := NewSeedEngine()
	dir := t.TempDir()

	// The whole directory has no files
	if err := engine.LoadFromDir(dir, "*.yaml"); err != nil {
		t.Fatalf("LoadFromDir on empty dir failed: %v", err)
	}

	if len(engine.Seeds()) != len(DefaultSeeds()) {
		t.Errorf("expected %d seeds, got %d", len(DefaultSeeds()), len(engine.Seeds()))
	}
}

// ─── NewSeedEngineWithSeedsDir Tests ────────────────────────

func TestNewSeedEngineWithSeedsDir_Existing(t *testing.T) {
	dir := t.TempDir()
	writeTempYAML(t, dir, "test.yaml", validSeedYAML)

	engine := NewSeedEngineWithSeedsDir(dir)
	if engine == nil {
		t.Fatal("expected non-nil engine")
	}

	found := false
	for _, s := range engine.Seeds() {
		if s.ID == "custom-001" {
			found = true
			break
		}
	}
	if !found {
		t.Error("custom-001 should have been loaded from seeds dir")
	}
}

func TestNewSeedEngineWithSeedsDir_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	engine := NewSeedEngineWithSeedsDir(dir)
	if engine == nil {
		t.Fatal("expected non-nil engine")
	}
	if len(engine.Seeds()) != len(DefaultSeeds()) {
		t.Errorf("expected %d seeds, got %d", len(DefaultSeeds()), len(engine.Seeds()))
	}
}

func TestNewSeedEngineWithSeedsDir_NonExistent(t *testing.T) {
	engine := NewSeedEngineWithSeedsDir("/nonexistent/seeds")
	if engine == nil {
		t.Fatal("expected non-nil engine")
	}
	if len(engine.Seeds()) != len(DefaultSeeds()) {
		t.Errorf("expected %d seeds, got %d", len(DefaultSeeds()), len(engine.Seeds()))
	}
}

func TestNewSeedEngineWithSeedsDir_EmptyString(t *testing.T) {
	engine := NewSeedEngineWithSeedsDir("")
	if engine == nil {
		t.Fatal("expected non-nil engine")
	}
	if len(engine.Seeds()) != len(DefaultSeeds()) {
		t.Errorf("expected %d seeds, got %d", len(DefaultSeeds()), len(engine.Seeds()))
	}
}

// ─── Integration: Real Seeds Directory ──────────────────────

func TestLoadFromSeedsDirectory(t *testing.T) {
	// Determine the project root seeds directory.
	// When running go test ./internal/fixer/..., the working directory
	// is internal/fixer/, so we need to go up two levels.
	seedsDir := findProjectSeedsDir(t)
	if seedsDir == "" {
		t.Skip("seeds directory not found, skipping integration test")
	}

	engine := NewSeedEngine()
	err := engine.LoadFromDir(seedsDir, "*.yaml")
	if err != nil {
		t.Fatalf("failed to load seeds directory %q: %v", seedsDir, err)
	}

	// Expected YAML seed IDs
	expectedYAMLIDs := map[string]bool{
		"auth-001": true,
		"auth-002": true,
		"auth-003": true,
		"img-001":  true,
		"img-002":  true,
		"img-003":  true,
		"net-001":  true,
		"net-002":  true,
		"net-003":  true,
		"perm-001": true,
		"res-001":  true,
		"res-002":  true,
		"res-003":  true,
		"app-001":  true,
		"app-002":  true,
		"app-003":  true,
	}

	seeds := engine.Seeds()
	yAMLLoaded := 0
	for _, s := range seeds {
		if expectedYAMLIDs[s.ID] {
			yAMLLoaded++
		}
	}

	// The YAML-overridden seeds should have non-empty content
	overriddenIDs := []string{"auth-001", "auth-002", "img-001", "app-001", "net-001"}
	for _, id := range overriddenIDs {
		for _, s := range seeds {
			if s.ID == id && s.RootCause == "" {
				t.Errorf("%s: root cause should not be empty after YAML override", id)
			}
		}
	}

	// Verify the YAML-only seed app-003 was loaded
	foundApp003 := false
	for _, s := range seeds {
		if s.ID == "app-003" {
			foundApp003 = true
			if s.Title != "编译错误" {
				t.Errorf("expected title '编译错误' for app-003, got %q", s.Title)
			}
			break
		}
	}
	if !foundApp003 {
		t.Error("app-003 (YAML-only) should have been loaded")
	}
}

// findProjectSeedsDir attempts to locate the seeds directory relative to the
// test file. It checks common locations such as the current directory, parent,
// and grandparent, since go test may change the working directory.
func findProjectSeedsDir(t *testing.T) string {
	t.Helper()
	candidates := []string{
		"seeds",
		"../seeds",
		"../../seeds",
	}
	for _, path := range candidates {
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			return path
		}
	}
	return ""
}
