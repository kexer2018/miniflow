package llm

import (
	"strings"
	"testing"
)

func TestBuildDiagnosisUserPrompt(t *testing.T) {
	tests := []struct {
		name           string
		stepName       string
		classification string
		reason         string
		sanitizedLog   string
		similarCases   string
		wantStep       string
		wantFallback   bool // true if similarCases should be the fallback text
	}{
		{
			name:           "basic diagnosis prompt",
			stepName:       "build",
			classification: "infra_error",
			reason:         "network connectivity",
			sanitizedLog:   "Error: connection refused to host:443",
			similarCases:   "Connection Refused (network)",
			wantStep:       "build",
			wantFallback:   false,
		},
		{
			name:           "empty similar cases uses fallback",
			stepName:       "test",
			classification: "app_error",
			reason:         "test failure",
			sanitizedLog:   "FAIL: TestFoo failed",
			similarCases:   "",
			wantStep:       "test",
			wantFallback:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prompt := BuildDiagnosisUserPrompt(
				tt.stepName,
				tt.classification,
				tt.reason,
				tt.sanitizedLog,
				tt.similarCases,
			)

			if !strings.Contains(prompt, tt.stepName) {
				t.Errorf("prompt should contain step name %q", tt.stepName)
			}
			if !strings.Contains(prompt, tt.classification) {
				t.Errorf("prompt should contain classification %q", tt.classification)
			}
			if !strings.Contains(prompt, tt.sanitizedLog) {
				t.Errorf("prompt should contain sanitized log")
			}

			if tt.wantFallback {
				if !strings.Contains(prompt, "No similar past incidents found.") {
					t.Error("prompt should contain fallback text when similarCases is empty")
				}
			} else {
				if !strings.Contains(prompt, tt.similarCases) {
					t.Errorf("prompt should contain similar cases text %q", tt.similarCases)
				}
			}
		})
	}
}

func TestSystemDiagnosisPrompt(t *testing.T) {
	if SystemDiagnosisPrompt == "" {
		t.Fatal("SystemDiagnosisPrompt should not be empty")
	}

	// Must contain key sections
	required := []string{
		"root cause",
		"fix plan",
		"confidence",
		"app_error",
		"infra_error",
	}

	for _, r := range required {
		if !strings.Contains(strings.ToLower(SystemDiagnosisPrompt), r) {
			t.Errorf("SystemDiagnosisPrompt should contain %q", r)
		}
	}
}

func TestDiagnosisSchema(t *testing.T) {
	schema := DiagnosisSchema()

	// Check top-level structure
	if schema == nil {
		t.Fatal("DiagnosisSchema() should not return nil")
	}

	if schema["type"] != "object" {
		t.Errorf("schema type should be 'object', got %v", schema["type"])
	}

	// Check required fields
	required, ok := schema["required"].([]any)
	if !ok {
		t.Fatal("schema should have 'required' array")
	}

	requiredFields := make(map[string]bool)
	for _, f := range required {
		requiredFields[f.(string)] = true
	}

	for _, field := range []string{"root_cause", "fix_plan", "confidence"} {
		if !requiredFields[field] {
			t.Errorf("schema should require %q", field)
		}
	}

	// Check properties
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("schema should have 'properties'")
	}

	for _, field := range []string{"root_cause", "fix_plan", "confidence", "category"} {
		if _, exists := props[field]; !exists {
			t.Errorf("schema should have property %q", field)
		}
	}
}
