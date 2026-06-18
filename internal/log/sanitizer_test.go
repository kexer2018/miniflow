package log

import (
	"strings"
	"testing"
)

func TestNewSanitizerWithSemantic(t *testing.T) {
	s := NewSanitizerWithSemantic()
	if s == nil {
		t.Fatal("NewSanitizerWithSemantic() should not return nil")
	}
	if !s.IsSemantic() {
		t.Error("NewSanitizerWithSemantic() should set semantic mode to true")
	}
}

func TestSanitize_SemanticMode_JWT(t *testing.T) {
	s := NewSanitizerWithSemantic()

	input := "Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8"
	output := s.Sanitize(input)

	if strings.Contains(output, "eyJhbGciOiJIUzI1NiJ9") {
		t.Error("JWT token should be redacted")
	}
	if !strings.Contains(output, "***JWT_TOKEN_REDACTED***") {
		t.Errorf("semantic mode should use ***JWT_TOKEN_REDACTED***, got: %q", output)
	}
}

func TestSanitize_SemanticMode_AWSKey(t *testing.T) {
	s := NewSanitizerWithSemantic()

	input := "aws_access_key_id = AKIAIOSFODNN7EXAMPLE"
	output := s.Sanitize(input)

	if strings.Contains(output, "AKIAIOSFODNN7EXAMPLE") {
		t.Error("AWS key should be redacted")
	}
	if !strings.Contains(output, "***AWS_ACCESS_KEY_REDACTED***") {
		t.Errorf("semantic mode should use ***AWS_ACCESS_KEY_REDACTED***, got: %q", output)
	}
}

func TestSanitize_SemanticMode_PrivateKey(t *testing.T) {
	s := NewSanitizerWithSemantic()

	input := "-----BEGIN RSA PRIVATE KEY-----\nMIIEpAIBAAKCAQEA..."
	output := s.Sanitize(input)

	if strings.Contains(output, "BEGIN RSA PRIVATE KEY") {
		t.Error("private key header should be redacted")
	}
	if !strings.Contains(output, "***PRIVATE_KEY_REDACTED***") {
		t.Errorf("semantic mode should use ***PRIVATE_KEY_REDACTED***, got: %q", output)
	}
}

func TestSanitize_StandardMode_UsesShortTags(t *testing.T) {
	s := NewSanitizer()

	if s.IsSemantic() {
		t.Error("default sanitizer should not be in semantic mode")
	}

	input := "Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8"
	output := s.Sanitize(input)

	if !strings.Contains(output, "***JWT***") {
		t.Errorf("standard mode should use ***JWT***, got: %q", output)
	}
}

func TestSanitize_EmptyInput(t *testing.T) {
	s := NewSanitizerWithSemantic()

	if output := s.Sanitize(""); output != "" {
		t.Errorf("empty input should return empty string, got: %q", output)
	}
}

func TestSanitize_CredentialInURL(t *testing.T) {
	s := NewSanitizerWithSemantic()

	input := "https://user:password@registry.company.com/v2/"
	output := s.Sanitize(input)

	if strings.Contains(output, "user:password") {
		t.Error("credentials in URL should be redacted")
	}
	if !strings.Contains(output, "***CREDENTIALS_REDACTED@") {
		t.Errorf("semantic mode should redact URL credentials, got: %q", output)
	}
}

func TestSanitize_GitHubToken(t *testing.T) {
	s := NewSanitizerWithSemantic()

	input := "git clone https://ghp_abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMN@github.com/org/repo.git"
	output := s.Sanitize(input)

	if strings.Contains(output, "ghp_") {
		t.Error("GitHub token should be redacted")
	}
	if !strings.Contains(output, "***GITHUB_TOKEN_REDACTED***") {
		t.Errorf("semantic mode should use ***GITHUB_TOKEN_REDACTED***, got: %q", output)
	}
}

func TestSanitize_NoFalsePositiveOnNormalText(t *testing.T) {
	s := NewSanitizerWithSemantic()

	// Normal build log with no credentials
	input := "Step 1/4 : FROM golang:1.22\nResolving deltas: 100% (42/42), done.\nSuccessfully built abcdef123456"
	output := s.Sanitize(input)

	// Should not change normal text
	if !strings.Contains(output, "FROM golang") {
		t.Error("normal build log content should be preserved")
	}
}
