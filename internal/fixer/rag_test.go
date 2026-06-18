package fixer

import (
	"testing"
)

func TestSeedEngine_Match(t *testing.T) {
	engine := NewSeedEngine()

	tests := []struct {
		name     string
		logText  string
		wantMin  int // minimum number of matches expected
		wantMax  int // maximum number of matches expected
		category string // expected best match category (if wantMin > 0)
	}{
		{
			name:    "empty log returns no matches",
			logText: "",
			wantMin: 0,
			wantMax: 0,
		},
		{
			name:     "401 unauthorized matches auth category",
			logText:  "Error: 401 Unauthorized",
			wantMin:  1,
			wantMax:  3,
			category: "authentication",
		},
		{
			name:     "connection refused matches network category",
			logText:  "dial tcp 10.0.1.100:443: connect: connection refused",
			wantMin:  1,
			wantMax:  3,
			category: "network",
		},
		{
			name:     "no such image matches image_pull category",
			logText:  "manifest for golang:1.99 not found: manifest unknown",
			wantMin:  1,
			wantMax:  3,
			category: "image_pull",
		},
		{
			name:     "permission denied matches permission category",
			logText:  "mkdir: cannot create directory '/workspace/build': Permission denied",
			wantMin:  1,
			wantMax:  3,
			category: "permission",
		},
		{
			name:     "no space left on device matches resource category",
			logText:  "write error: no space left on device",
			wantMin:  1,
			wantMax:  3,
			category: "resource",
		},
		{
			name:    "log with no known patterns returns no matches",
			logText: "Everything is fine. No errors here.",
			wantMin: 0,
			wantMax: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := engine.Match(tt.logText, 3)

			if len(matches) < tt.wantMin || len(matches) > tt.wantMax {
				t.Errorf("Match() returned %d matches, want between %d and %d",
					len(matches), tt.wantMin, tt.wantMax)
			}

			if tt.wantMin > 0 && len(matches) > 0 {
				if matches[0].Seed.Category != tt.category {
					t.Errorf("Match() best category = %q, want %q",
						matches[0].Seed.Category, tt.category)
				}
				if matches[0].Score <= 0 || matches[0].Score > 1 {
					t.Errorf("Match() score = %f, want between 0 and 1", matches[0].Score)
				}
				if len(matches[0].Reasons) == 0 {
					t.Error("Match() best match has no reasons")
				}
			}
		})
	}
}

func TestSeedEngine_MatchBest(t *testing.T) {
	engine := NewSeedEngine()

	// Test that best match returns nil for empty input
	if m := engine.MatchBest(""); m != nil {
		t.Error("MatchBest() with empty input should return nil")
	}

	// Test that best match returns a result for a known pattern
	m := engine.MatchBest("401 Unauthorized")
	if m == nil {
		t.Fatal("MatchBest() should find a match for '401 Unauthorized'")
	}
	if m.Seed.Category != "authentication" {
		t.Errorf("MatchBest() category = %q, want %q", m.Seed.Category, "authentication")
	}
}

func TestSeedEngine_BuildContext(t *testing.T) {
	engine := NewSeedEngine()

	// Test with matching log
	ctx := engine.BuildContext("connection refused to database.example.com:5432", 2)
	if ctx == "" {
		t.Fatal("BuildContext() should not return empty string for matching log")
	}
	if ctx == "No similar past incidents found." {
		t.Error("BuildContext() should find matches, not return 'no similar'")
	}

	// Test with non-matching log
	ctx = engine.BuildContext("completely random noise that does not match anything", 2)
	if ctx != "No similar past incidents found." {
		t.Errorf("BuildContext() with non-matching log should return fallback, got: %q", ctx)
	}
}

func TestNewSeedEngineWithCustom(t *testing.T) {
	customSeeds := []SeedCase{
		{
			ID:            "custom-001",
			Category:      "custom",
			Title:         "Custom Error",
			MatchPatterns: []string{"custom error"},
			RootCause:     "This is a custom error for testing.",
		},
	}

	engine := NewSeedEngineWithCustom(customSeeds)
	if len(engine.Seeds()) != 1 {
		t.Errorf("expected 1 seed, got %d", len(engine.Seeds()))
	}

	matches := engine.Match("this is a custom error in the log", 1)
	if len(matches) != 1 {
		t.Errorf("expected 1 match, got %d", len(matches))
	}
}

func TestSeedEngine_AddSeed(t *testing.T) {
	engine := NewSeedEngine()
	initialCount := len(engine.Seeds())

	engine.AddSeed(SeedCase{
		ID:            "test-001",
		Category:      "test",
		Title:         "Test Seed",
		MatchPatterns: []string{"test pattern"},
	})

	if len(engine.Seeds()) != initialCount+1 {
		t.Errorf("expected %d seeds, got %d", initialCount+1, len(engine.Seeds()))
	}
}
