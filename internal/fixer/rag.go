package fixer

import (
	"sort"
	"strings"
)

// ─── Seed Match ──────────────────────────────────────────

// SeedMatch represents a matched seed case with a relevance score.
type SeedMatch struct {
	Seed    SeedCase `json:"seed"`
	Score   float64  `json:"score"`   // 0.0–1.0, how well the log matches this seed
	Reasons []string `json:"reasons"` // Which patterns matched
}

// ─── Seed Engine ─────────────────────────────────────────

// SeedEngine manages the seed case library and performs matching.
type SeedEngine struct {
	seeds []SeedCase
}

// NewSeedEngine creates a SeedEngine with default built-in seeds.
func NewSeedEngine() *SeedEngine {
	return &SeedEngine{
		seeds: DefaultSeeds(),
	}
}

// NewSeedEngineWithCustom creates a SeedEngine with a custom seed list.
func NewSeedEngineWithCustom(seeds []SeedCase) *SeedEngine {
	return &SeedEngine{
		seeds: seeds,
	}
}

// Seeds returns the current seed cases.
func (e *SeedEngine) Seeds() []SeedCase {
	seeds := make([]SeedCase, len(e.seeds))
	copy(seeds, e.seeds)
	return seeds
}

// AddSeed adds a custom seed case to the engine.
func (e *SeedEngine) AddSeed(seed SeedCase) {
	e.seeds = append(e.seeds, seed)
}

// ─── Matching ────────────────────────────────────────────

// Match finds seed cases that match the given log text.
// Returns top-K matches sorted by relevance score (descending).
// Only returns matches with score > 0.
func (e *SeedEngine) Match(logText string, topK int) []SeedMatch {
	if logText == "" || topK <= 0 {
		return nil
	}

	logLower := strings.ToLower(logText)
	var matches []SeedMatch

	for _, seed := range e.seeds {
		match, ok := matchSeed(logLower, seed)
		if !ok {
			continue
		}
		matches = append(matches, match)
	}

	// Sort by score descending
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].Score > matches[j].Score
	})

	if len(matches) > topK {
		matches = matches[:topK]
	}

	return matches
}

// matchSeed checks if a log text matches a seed case.
// Returns a SeedMatch if any pattern matches.
func matchSeed(logLower string, seed SeedCase) (SeedMatch, bool) {
	var matchedPatterns []string

	for _, pattern := range seed.MatchPatterns {
		if strings.Contains(logLower, strings.ToLower(pattern)) {
			matchedPatterns = append(matchedPatterns, pattern)
		}
	}

	if len(matchedPatterns) == 0 {
		return SeedMatch{}, false
	}

	totalPatterns := len(seed.MatchPatterns)
	if totalPatterns == 0 {
		return SeedMatch{}, false
	}

	score := float64(len(matchedPatterns)) / float64(totalPatterns)

	return SeedMatch{
		Seed:    seed,
		Score:   score,
		Reasons: matchedPatterns,
	}, true
}

// MatchBest returns the single best matching seed, or nil if none match.
func (e *SeedEngine) MatchBest(logText string) *SeedMatch {
	matches := e.Match(logText, 1)
	if len(matches) == 0 {
		return nil
	}
	return &matches[0]
}

// BuildContext builds a few-shot context string from matched seeds for LLM prompts.
func (e *SeedEngine) BuildContext(logText string, maxSeeds int) string {
	matches := e.Match(logText, maxSeeds)
	if len(matches) == 0 {
		return "No similar past incidents found."
	}

	var b strings.Builder
	for i, m := range matches {
		if i > 0 {
			b.WriteString("\n---\n")
		}
		b.WriteString(m.Seed.Title)
		b.WriteString(" (")
		b.WriteString(m.Seed.Category)
		b.WriteString(", score: ")
		b.WriteString(formatScore(m.Score))
		b.WriteString(")\n")
		b.WriteString("  Root Cause: ")
		b.WriteString(m.Seed.RootCause)
		b.WriteString("\n")
		if m.Seed.FixSuggestion.Description != "" {
			b.WriteString("  Fix: ")
			b.WriteString(m.Seed.FixSuggestion.Description)
		}
	}
	return b.String()
}

func formatScore(s float64) string {
	if s >= 0.99 {
		return "high"
	} else if s >= 0.5 {
		return "medium"
	}
	return "low"
}
