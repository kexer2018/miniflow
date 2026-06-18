package fixer

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/kexer2018/miniflow/internal/llm"
	internalLog "github.com/kexer2018/miniflow/internal/log"
	"github.com/kexer2018/miniflow/internal/pipeline"
)

// ─── Configuration ───────────────────────────────────────

// DiagnoseConfig provides the dependencies for the diagnosis engine.
type DiagnoseConfig struct {
	LLM        llm.LLMClient
	Sanitizer  *internalLog.Sanitizer
	Classifier *internalLog.Classifier
	SeedEngine *SeedEngine
}

// DiagnoseDefaults creates a DiagnoseConfig with default instances.
// LLM client is created via llm.NewDefaultClient (may return error if env not set).
func DiagnoseDefaults() (DiagnoseConfig, error) {
	client, err := llm.NewDefaultClient()
	if err != nil {
		return DiagnoseConfig{}, err
	}
	return DiagnoseConfig{
		LLM:        client,
		Sanitizer:  internalLog.NewSanitizer(),
		Classifier: internalLog.NewClassifier(),
		SeedEngine: NewSeedEngine(),
	}, nil
}

// ─── Result ──────────────────────────────────────────────

// DiagnosisResult contains the complete diagnosis of a failed pipeline step.
type DiagnosisResult struct {
	StepName       string                `json:"step_name"`
	Classification pipeline.Classification `json:"classification"`
	RootCause      string                `json:"root_cause"`
	FixPlan        string                `json:"fix_plan"`
	Confidence     float64               `json:"confidence"`
	Category       string                `json:"category"`
	MatchedSeeds   []SeedMatch           `json:"matched_seeds,omitempty"`
	SuggestedFix   *pipeline.StepFix     `json:"suggested_fix,omitempty"`
	LLMUsage       *llm.Usage            `json:"llm_usage,omitempty"`
	IsDegraded     bool                  `json:"is_degraded"`
	Error          string                `json:"error,omitempty"`
	DurationMs     int64                 `json:"duration_ms"`
}

// ─── Orchestrator ────────────────────────────────────────

// llmResponse is the JSON structure returned by the LLM.
type llmResponse struct {
	RootCause    string            `json:"root_cause"`
	FixPlan      string            `json:"fix_plan"`
	Confidence   float64           `json:"confidence"`
	Category     string            `json:"category,omitempty"`
	SuggestedFix *pipeline.StepFix `json:"suggested_fix,omitempty"`
}

// Diagnose analyzes a failed step's log and produces a diagnosis.
//
// Flow: Sanitize → Classify → RAG Match → LLM Call → Parse → Result
// Degradation: If LLM is unavailable, falls back to RAG-only result.
func Diagnose(ctx context.Context, cfg DiagnoseConfig, stepName, rawLog string) *DiagnosisResult {
	startedAt := time.Now()
	result := &DiagnosisResult{
		StepName: stepName,
	}

	defer func() {
		result.DurationMs = time.Since(startedAt).Milliseconds()
	}()

	if rawLog == "" {
		result.Error = "empty log, nothing to diagnose"
		result.Classification = pipeline.Classification{
			Type:   pipeline.Unknown,
			Reason: "empty log",
		}
		return result
	}

	slog.Info("diagnosing step failure",
		"step", stepName,
		"log_length", len(rawLog),
	)

	// 1. Sanitize
	sanitizedLog := cfg.Sanitizer.Sanitize(rawLog)
	slog.Debug("log sanitized", "original", len(rawLog), "sanitized", len(sanitizedLog))

	// 2. Classify
	classification := cfg.Classifier.Classify(sanitizedLog)
	result.Classification = classification
	slog.Info("log classified", "type", classification.Type, "reason", classification.Reason)

	// 3. RAG Match
	result.MatchedSeeds = cfg.SeedEngine.Match(sanitizedLog, 3)
	if len(result.MatchedSeeds) > 0 {
		slog.Info("seed matches found", "count", len(result.MatchedSeeds),
			"best", result.MatchedSeeds[0].Seed.Title)
	} else {
		slog.Debug("no seed matches found")
	}

	// 4. If AppError → skip LLM (read-only diagnosis)
	if classification.Type == pipeline.AppError {
		if len(result.MatchedSeeds) > 0 {
			best := result.MatchedSeeds[0]
			result.RootCause = best.Seed.RootCause
			result.FixPlan = best.Seed.FixSuggestion.Description
			result.Confidence = best.Score * 0.8
			result.Category = best.Seed.Category
		} else {
			result.RootCause = "Application code error detected. Review the source code for bugs."
			result.FixPlan = "Check the application code and fix the underlying issue."
			result.Confidence = 0.3
			result.Category = "app_code"
		}
		result.IsDegraded = true
		slog.Info("app_error detected, skipping LLM call")
		return result
	}

	// 5. Build LLM prompt
	matchedContext := cfg.SeedEngine.BuildContext(sanitizedLog, 3)

	promptMessages := []llm.Message{
		{Role: llm.RoleSystem, Content: llm.SystemDiagnosisPrompt},
		{Role: llm.RoleUser, Content: llm.BuildDiagnosisUserPrompt(
			stepName,
			string(classification.Type),
			classification.Reason,
			sanitizedLog,
			matchedContext,
		)},
	}

	// 6. Call LLM
	llmResp, err := cfg.LLM.Chat(ctx, llm.ChatRequest{
		Messages:  promptMessages,
		MaxTokens: 2000,
		Schema:    llm.DiagnosisSchema(),
	})

	if err != nil {
		// Degrade gracefully
		slog.Warn("LLM diagnosis failed, degrading to RAG-only", "error", err)
		result.IsDegraded = true
		result.Error = fmt.Sprintf("LLM unavailable: %v", err)

		// Fill from best matched seed
		if len(result.MatchedSeeds) > 0 {
			best := result.MatchedSeeds[0]
			result.RootCause = best.Seed.RootCause
			result.FixPlan = best.Seed.FixSuggestion.Description
			result.Confidence = best.Score * 0.6
			result.Category = best.Seed.Category
			if best.Seed.FixSuggestion.ConfigOverride != nil {
				result.SuggestedFix = &pipeline.StepFix{
					Description:    best.Seed.FixSuggestion.Description,
					ConfigOverride: best.Seed.FixSuggestion.ConfigOverride,
				}
			}
		} else {
			result.RootCause = "Unknown error. Unable to determine root cause."
			result.FixPlan = "Review the step configuration and logs manually."
			result.Confidence = 0.1
			result.Category = "unknown"
		}
		return result
	}

	// Track LLM usage
	result.LLMUsage = &llmResp.Usage

	// 7. Parse LLM response
	parsed, parseErr := parseLLMResponse(llmResp.Content)
	if parseErr != nil {
		slog.Warn("failed to parse LLM response as JSON, using raw text", "error", parseErr)
		// Use raw text as root cause
		result.RootCause = strings.TrimSpace(llmResp.Content)
		result.FixPlan = "Review the analysis above."
		result.Confidence = 0.4
		result.Category = "unknown"
		result.Error = fmt.Sprintf("LLM response parse error: %v", parseErr)
		return result
	}

	result.RootCause = parsed.RootCause
	result.FixPlan = parsed.FixPlan
	result.Confidence = parsed.Confidence
	result.Category = parsed.Category
	result.SuggestedFix = parsed.SuggestedFix

	slog.Info("diagnosis complete",
		"category", result.Category,
		"confidence", result.Confidence,
		"has_fix", result.SuggestedFix != nil,
	)

	return result
}

// ─── JSON Parsing ────────────────────────────────────────

// parseLLMResponse attempts to parse an LLM response as JSON.
// Handles both raw JSON and JSON fenced in markdown code blocks.
func parseLLMResponse(content string) (*llmResponse, error) {
	content = strings.TrimSpace(content)

	// Try extracting JSON from code fence first
	if strings.HasPrefix(content, "```") {
		content = extractJSONFromFence(content)
	}

	var resp llmResponse
	if err := json.Unmarshal([]byte(content), &resp); err != nil {
		return nil, fmt.Errorf("parse JSON: %w", err)
	}

	if resp.RootCause == "" {
		return nil, fmt.Errorf("missing root_cause in LLM response")
	}

	return &resp, nil
}

// extractJSONFromFence extracts JSON content from a markdown code fence.
func extractJSONFromFence(content string) string {
	lines := strings.Split(content, "\n")
	var inFence bool
	var jsonLines []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			jsonLines = append(jsonLines, line)
		}
	}

	if len(jsonLines) > 0 {
		return strings.Join(jsonLines, "\n")
	}

	// No fence found, try to find JSON object directly
	return content
}

// ─── Convenience ─────────────────────────────────────────

// DiagnoseStep is a convenience wrapper that diagnoses a single StepResult.
// It extracts the step name and raw log from the result.
func DiagnoseStep(ctx context.Context, cfg DiagnoseConfig, result pipeline.StepResult) *DiagnosisResult {
	log := result.RawLog
	if result.Sanitized != "" {
		log = result.Sanitized
	}
	return Diagnose(ctx, cfg, result.Name, log)
}
