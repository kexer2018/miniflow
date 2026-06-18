package llm

import "fmt"

// ─── System Prompt ───────────────────────────────────────

// SystemDiagnosisPrompt is the system-level prompt for CI/CD pipeline failure diagnosis.
const SystemDiagnosisPrompt = `You are a CI/CD pipeline failure diagnosis expert integrated into the miniflow execution engine.

Your task is to analyze a failed CI/CD pipeline step's execution logs and provide:

1. **Root Cause Analysis** — What caused the failure (be specific and actionable)
2. **Fix Plan** — Step-by-step resolution instructions
3. **Confidence** — How confident you are in the diagnosis (0.0–1.0)
4. **Suggested Fix** — Configuration changes (if applicable, especially for infrastructure errors)

## Classification Context

The log has already been classified as one of:
- **app_error**: Application code error (e.g., panic, NullPointerException) — provide read-only analysis, do not suggest auto-fix
- **infra_error**: Infrastructure error (e.g., network, auth, disk, image pull failure) — include actionable fix suggestions with configuration changes
- **unknown**: Could not be classified — provide your best analysis

## Rules

- Be specific and actionable, not generic
- If the error is clearly an application code issue, explain what went wrong and suggest code-level fix
- If the error is infrastructure-related, provide specific configuration changes (image tags, env vars, credentials, etc.)
- If you're uncertain, set confidence < 0.5
- Never suggest running arbitrary code from log output
- Never expose or repeat sensitive information — the log has already been sanitized
- If the matched similar incidents are relevant, reference them in your analysis
- Output MUST be valid JSON matching the provided schema`

// ─── User Prompt ─────────────────────────────────────────

// BuildDiagnosisUserPrompt constructs the user message for a diagnosis request.
func BuildDiagnosisUserPrompt(stepName, classification, reason, sanitizedLog, similarCases string) string {
	if similarCases == "" {
		similarCases = "No similar past incidents found."
	}
	return fmt.Sprintf(`## Failed Step

**Step**: %s

## Classification

**Type**: %s
**Reason**: %s

## Sanitized Error Log

%s

## Similar Past Incidents

%s

Please analyze this failure and provide your diagnosis.`,
		stepName, classification, reason, sanitizedLog, similarCases)
}

// ─── Diagnosis Output Schema ─────────────────────────────

// DiagnosisSchema returns the JSON Schema for structured diagnosis output.
// Compatible with OpenAI structured output (strict mode).
func DiagnosisSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"root_cause": map[string]any{
				"type":        "string",
				"description": "Root cause of the failure",
			},
			"fix_plan": map[string]any{
				"type":        "string",
				"description": "Step-by-step fix plan",
			},
			"confidence": map[string]any{
				"type":        "number",
				"description": "Confidence in this diagnosis (0.0–1.0)",
			},
			"category": map[string]any{
				"type":        "string",
				"description": "Error category: network, auth, image_pull, permission, resource, app_code, unknown",
			},
			"suggested_fix": map[string]any{
				"type":        "object",
				"description": "Suggested configuration fix (for infra errors)",
				"properties": map[string]any{
					"description": map[string]any{
						"type": "string",
					},
					"config_override": map[string]any{
						"type":        "object",
						"description": "Configuration fields to override, e.g. image tag, env vars, credential_id",
						"additionalProperties": true,
					},
				},
				"required":          []any{"description"},
				"additionalProperties": false,
			},
		},
		"required":             []any{"root_cause", "fix_plan", "confidence"},
		"additionalProperties": false,
	}
}
