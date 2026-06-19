package log

import (
	"testing"

	"github.com/kexer2018/miniflow/internal/pipeline"
)

func TestClassifier_EmptyLog(t *testing.T) {
	c := NewClassifier()
	result := c.Classify("")

	if result.Type != pipeline.Unknown {
		t.Errorf("expected Unknown, got %s", result.Type)
	}
	if result.Reason != "empty log" {
		t.Errorf("expected 'empty log', got %q", result.Reason)
	}
}

func TestClassifier_AppErrors(t *testing.T) {
	c := NewClassifier()

	tests := []struct {
		name   string
		log    string
		reason string // expected classification reason
	}{
		{
			name:   "panic",
			log:    "panic: runtime error: invalid memory address or nil pointer dereference",
			reason: "panic",
		},
		{
			name:   "JavaScript SyntaxError",
			log:    "SyntaxError: unexpected token",
			reason: "JavaScript/TypeScript error",
		},
		{
			name:   "JavaScript TypeError",
			log:    "TypeError: undefined is not a function",
			reason: "JavaScript/TypeScript error",
		},
		{
			name:   "JavaScript ReferenceError",
			log:    "ReferenceError: x is not defined",
			reason: "JavaScript/TypeScript error",
		},
		{
			name:   "Python traceback",
			log:    "Traceback (most recent call last):\n  File \"test.py\", line 10, in <module>\n    foo()\nValueError: invalid literal",
			reason: "Python exception",
		},
		{
			name:   "Java NullPointerException",
			log:    "Exception in thread \"main\" java.lang.NullPointerException",
			reason: "NullPointer (Java/Kotlin)",
		},
		{
			name:   "NullPointer short form",
			log:    "nullpointer: cannot invoke method on null object",
			reason: "NullPointer (Java/Kotlin)",
		},
		{
			name:   "Go fatal error stack overflow",
			log:    "fatal error: stack overflow",
			reason: "Go panic/fatal",
		},
		{
			name:   "Go goroutine trace",
			log:    "goroutine 1 [running]:\nmain.main()\n\t/app/main.go:42 +0x123",
			reason: "Go panic/fatal",
		},
		{
			name:   "Go fatal error concurrent map writes",
			log:    "fatal error: concurrent map writes",
			reason: "Go panic/fatal",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := c.Classify(tt.log)
			if result.Type != pipeline.AppError {
				t.Errorf("expected AppError, got %s (reason: %s)", result.Type, result.Reason)
			}
			if result.Reason != tt.reason {
				t.Errorf("expected reason %q, got %q", tt.reason, result.Reason)
			}
			if len(result.Signals) == 0 {
				t.Error("expected at least one signal")
			}
		})
	}
}

func TestClassifier_InfraErrors(t *testing.T) {
	c := NewClassifier()

	tests := []struct {
		name   string
		log    string
		reason string
	}{
		{
			name:   "401 Unauthorized",
			log:    "error: 401 Unauthorized",
			reason: "authentication (401/403)",
		},
		{
			name:   "403 Forbidden",
			log:    "Received 403 Forbidden from server",
			reason: "authentication (401/403)",
		},
		{
			name:   "authentication required",
			log:    "Authentication required. Please provide credentials.",
			reason: "authentication (401/403)",
		},
		{
			name:   "authentication failed",
			log:    "ssh: authentication failed",
			reason: "authentication (401/403)",
		},
		{
			name:   "image not found",
			log:    "manifest for golang:1.99 not found: manifest unknown",
			reason: "image pull failure",
		},
		{
			name:   "connection refused",
			log:    "dial tcp 10.0.1.100:443: connect: connection refused",
			reason: "network connectivity",
		},
		{
			name:   "TLS handshake timeout",
			log:    "TLS handshake timeout after 30s",
			reason: "network connectivity",
		},
		{
			name:   "permission denied",
			log:    "mkdir: cannot create directory '/x': Permission denied",
			reason: "file permission",
		},
		{
			name:   "no space left on device",
			log:    "write error: no space left on device",
			reason: "disk space",
		},
		{
			name:   "certificate expired",
			log:    "x509: certificate has expired",
			reason: "credential/certificate",
		},
		{
			name:   "configuration not found",
			log:    "could not find config file at /etc/app/config.yaml",
			reason: "configuration not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := c.Classify(tt.log)
			if result.Type != pipeline.InfraError {
				t.Errorf("expected InfraError, got %s (reason: %s)", result.Type, result.Reason)
			}
			if result.Reason != tt.reason {
				t.Errorf("expected reason %q, got %q", tt.reason, result.Reason)
			}
			if len(result.Signals) == 0 {
				t.Error("expected at least one signal")
			}
		})
	}
}

func TestClassifier_CaseInsensitive(t *testing.T) {
	c := NewClassifier()

	tests := []struct {
		name   string
		log    string
		rtype pipeline.LogType
		reason string
	}{
		{
			name:   "uppercase PANIC",
			log:    "PANIC: out of memory",
			rtype:  pipeline.AppError,
			reason: "panic",
		},
		{
			name:   "uppercase 401 UNAUTHORIZED",
			log:    "error: 401 UNAUTHORIZED from registry",
			rtype:  pipeline.InfraError,
			reason: "authentication (401/403)",
		},
		{
			name:   "mixed case Traceback",
			log:    "Traceback (most recent call last):",
			rtype:  pipeline.AppError,
			reason: "Python exception",
		},
		{
			name:   "lowercase goroutine",
			log:    "goroutine 5 [running]:",
			rtype:  pipeline.AppError,
			reason: "Go panic/fatal",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := c.Classify(tt.log)
			if result.Type != tt.rtype {
				t.Errorf("expected %s, got %s (reason: %s)", tt.rtype, result.Type, result.Reason)
			}
			if result.Reason != tt.reason {
				t.Errorf("expected reason %q, got %q", tt.reason, result.Reason)
			}
		})
	}
}

func TestClassifier_AppErrorPriority(t *testing.T) {
	// When both app_error and infra_error signals are present,
	// app_error should win (checked first).
	c := NewClassifier()

	log := "panic: runtime error\n401 Unauthorized from registry"
	result := c.Classify(log)

	if result.Type != pipeline.AppError {
		t.Errorf("expected AppError (priority), got %s", result.Type)
	}
	if result.Reason != "panic" {
		t.Errorf("expected reason 'panic', got %q", result.Reason)
	}
}

func TestClassifier_ExitCodeFallback(t *testing.T) {
	c := NewClassifier()

	log := "some build step failed with exit code 1"
	result := c.Classify(log)

	if result.Type != pipeline.Unknown {
		t.Errorf("expected Unknown (exit code fallback), got %s", result.Type)
	}
	if result.Reason != "exit code 1 with no other signals" {
		t.Errorf("expected 'exit code 1 with no other signals', got %q", result.Reason)
	}
}

func TestClassifier_NoMatch(t *testing.T) {
	c := NewClassifier()

	log := "everything is fine\nbuild succeeded"
	result := c.Classify(log)

	if result.Type != pipeline.Unknown {
		t.Errorf("expected Unknown, got %s", result.Type)
	}
	if result.Reason != "no matching signals" {
		t.Errorf("expected 'no matching signals', got %q", result.Reason)
	}
}

func TestClassifier_SignalsArePopulated(t *testing.T) {
	c := NewClassifier()

	// AppError should populate signals
	appResult := c.Classify("panic: runtime error")
	if len(appResult.Signals) == 0 {
		t.Error("AppError should have signals populated")
	}

	// InfraError should populate signals
	infraResult := c.Classify("401 Unauthorized")
	if len(infraResult.Signals) == 0 {
		t.Error("InfraError should have signals populated")
	}

	// Unknown (no match) should have empty signals
	noMatch := c.Classify("normal output")
	if len(noMatch.Signals) != 0 {
		t.Errorf("Unknown with no match should have empty signals, got %v", noMatch.Signals)
	}
}

func TestClassifier_MultiLineLog(t *testing.T) {
	c := NewClassifier()

	// Python traceback is multi-line
	log := "Traceback (most recent call last):\n  File \"app.py\", line 23, in run\n    result = process(data)\n  File \"app.py\", line 45, in process\n    return data['key']\nKeyError: 'key'"

	result := c.Classify(log)
	if result.Type != pipeline.AppError {
		t.Errorf("expected AppError for multi-line traceback, got %s", result.Type)
	}
	if result.Reason != "Python exception" {
		t.Errorf("expected 'Python exception', got %q", result.Reason)
	}
}

func TestClassifier_RegexPatterns(t *testing.T) {
	c := NewClassifier()

	tests := []struct {
		name   string
		log    string
		rtype pipeline.LogType
		reason string
	}{
		{
			name:   "dial tcp with port",
			log:    "dial tcp 10.0.1.100:443: i/o timeout",
			rtype:  pipeline.InfraError,
			reason: "network connectivity",
		},
		{
			name:   "config not found pattern",
			log:    "could not find credential file at /etc/secrets/db.json",
			rtype:  pipeline.InfraError,
			reason: "configuration not found",
		},
		{
			name:   "token missing pattern",
			log:    "API token not found in environment",
			rtype:  pipeline.InfraError,
			reason: "configuration not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := c.Classify(tt.log)
			if result.Type != tt.rtype {
				t.Errorf("expected %s, got %s (reason: %s)", tt.rtype, result.Type, result.Reason)
			}
			if result.Reason != tt.reason {
				t.Errorf("expected reason %q, got %q", tt.reason, result.Reason)
			}
		})
	}
}
