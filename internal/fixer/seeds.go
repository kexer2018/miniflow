// Package fixer provides AI-powered error analysis and fix suggestion generation.
package fixer

// ─── Seed Case Type ──────────────────────────────────────

// SeedCase represents a known failure pattern with root cause and fix suggestion.
type SeedCase struct {
	ID            string            // Unique identifier, e.g. "auth-001"
	Category      string            // Error category: authentication, image_pull, network, etc.
	Title         string            // Human-readable title
	MatchPatterns []string          // Keywords/patterns to match against log text (OR logic)
	RawLogExample string            // Example raw log (for reference only)
	SanitizedLog  string            // Example sanitized log
	RootCause     string            // Known root cause explanation
	FixSuggestion FixSuggestion     // Suggested fix
}

// FixSuggestion describes a recommended fix action.
type FixSuggestion struct {
	Description    string         `json:"description"`
	ConfigOverride map[string]any `json:"config_override,omitempty"`
}

// ─── Built-in Default Seeds ──────────────────────────────

// DefaultSeeds returns the built-in seed case library.
// These cover common CI/CD failure patterns for the MVP.
func DefaultSeeds() []SeedCase {
	return []SeedCase{
		// ── Authentication ─────────────────────────────
		{
			ID:            "auth-001",
			Category:      "authentication",
			Title:         "401 Unauthorized",
			MatchPatterns: []string{"401 Unauthorized", "status 401"},
			RootCause:     "API authentication credentials are missing, invalid, or expired.",
			FixSuggestion: FixSuggestion{
				Description: "Check and update API authentication credentials",
				ConfigOverride: map[string]any{
					"credential_id": "api-token-prod",
				},
			},
		},
		{
			ID:            "auth-002",
			Category:      "authentication",
			Title:         "Certificate Expired",
			MatchPatterns: []string{"certificate has expired", "x509: certificate has expired", "SSL certificate expired"},
			RootCause:     "SSL/TLS certificate has expired. The certificate needs to be renewed or updated.",
			FixSuggestion: FixSuggestion{
				Description: "Renew SSL/TLS certificate or contact operations team for renewal",
				ConfigOverride: map[string]any{
					"env": []any{"NODE_TLS_REJECT_UNAUTHORIZED=0"},
				},
			},
		},
		{
			ID:            "auth-003",
			Category:      "authentication",
			Title:         "Self-Signed Certificate / Unknown CA",
			MatchPatterns: []string{"x509: certificate signed by unknown authority", "self-signed certificate"},
			RootCause:     "The server uses a self-signed certificate or a certificate issued by an internal CA that is not in the system trust store.",
			FixSuggestion: FixSuggestion{
				Description: "Add the CA certificate to the system trust chain, or configure the environment to skip certificate verification for development",
				ConfigOverride: map[string]any{
					"env": []any{
						"SSL_CERT_FILE=/etc/ssl/certs/ca-certificates.crt",
						"NODE_EXTRA_CA_CERTS=/workspace/certs/ca.pem",
					},
				},
			},
		},

		// ── Image Pull ─────────────────────────────────
		{
			ID:            "img-001",
			Category:      "image_pull",
			Title:         "Image Not Found in Registry",
			MatchPatterns: []string{"no such image", "manifest for", "not found: manifest unknown", "manifest unknown"},
			RootCause:     "The specified container image tag does not exist in the target registry. This may be due to a typo, a renamed tag, or the image only exists in a private registry.",
			FixSuggestion: FixSuggestion{
				Description: "Correct the image tag to an existing version",
				ConfigOverride: map[string]any{
					"image": "golang:1.22",
				},
			},
		},
		{
			ID:            "img-002",
			Category:      "image_pull",
			Title:         "Private Registry Authentication Failure",
			MatchPatterns: []string{"unauthorized: access denied", "unauthorized: authentication required", "no basic auth credentials", "pull access denied for"},
			RootCause:     "Docker client is not authenticated to the private image registry, or credentials have expired.",
			FixSuggestion: FixSuggestion{
				Description: "Configure private registry authentication credentials",
				ConfigOverride: map[string]any{
					"registry_auth": map[string]any{
						"server":        "registry.company.com",
						"credential_id": "harbor-prod-token",
					},
				},
			},
		},
		{
			ID:            "img-003",
			Category:      "image_pull",
			Title:         "Docker Hub Rate Limited",
			MatchPatterns: []string{"toomanyrequests: too many requests", "rate limit", "you have reached your pull rate limit"},
			RootCause:     "Docker Hub has a pull rate limit for anonymous users (typically 100-200 pulls per hour). Authenticated users have higher limits.",
			FixSuggestion: FixSuggestion{
				Description: "Configure Docker Hub authentication credentials to increase pull rate limit",
				ConfigOverride: map[string]any{
					"registry_auth": map[string]any{
						"server":        "https://index.docker.io/v1/",
						"credential_id": "docker-hub-token",
					},
				},
			},
		},

		// ── Network ────────────────────────────────────
		{
			ID:            "net-001",
			Category:      "network",
			Title:         "DNS Resolution Failure",
			MatchPatterns: []string{"dial tcp: lookup", "Temporary failure in name resolution", "cannot resolve host", "no such host"},
			RootCause:     "DNS cannot resolve the target hostname. Possible causes: hostname typo, incorrect DNS server configuration, or the internal domain is not registered in public DNS.",
			FixSuggestion: FixSuggestion{
				Description: "Check the hostname spelling and DNS configuration. Verify whether an internal DNS server should be used.",
				ConfigOverride: map[string]any{
					"extra_hosts": []any{"registry.company.com:10.0.1.100"},
				},
			},
		},
		{
			ID:            "net-002",
			Category:      "network",
			Title:         "Connection Refused",
			MatchPatterns: []string{"connection refused", "connect: connection refused"},
			RootCause:     "The target port is not open or the service is not running. The target service may not be ready yet, a firewall may be blocking the connection, or the port mapping is incorrect.",
			FixSuggestion: FixSuggestion{
				Description: "Verify that the target service is running and the port is correct. Check network security groups and firewall rules.",
				ConfigOverride: map[string]any{
					"network": "build-network",
				},
			},
		},
		{
			ID:            "net-003",
			Category:      "network",
			Title:         "TLS Handshake Timeout",
			MatchPatterns: []string{"TLS handshake timeout", "tls: first record does not look like a TLS handshake", "remote error: tls: bad certificate"},
			RootCause:     "TLS handshake timed out, possibly due to high network latency, TLS version incompatibility, or proxy configuration issues.",
			FixSuggestion: FixSuggestion{
				Description: "Check network connectivity and proxy configuration. Try lowering the TLS version requirement.",
				ConfigOverride: map[string]any{
					"env": []any{
						"HTTP_PROXY=http://proxy.company.com:8080",
						"HTTPS_PROXY=http://proxy.company.com:8080",
					},
				},
			},
		},

		// ── File Permission ────────────────────────────
		{
			ID:            "perm-001",
			Category:      "permission",
			Title:         "Permission Denied",
			MatchPatterns: []string{"permission denied", "cannot open file", "access denied"},
			RootCause:     "The running process does not have sufficient file system permissions. The container user (UID 1000) may not own the workspace files, or the target file/directory has restrictive permissions.",
			FixSuggestion: FixSuggestion{
				Description: "Ensure the workspace is chowned to the container user. This is typically handled automatically by the engine. If the issue persists, check that the base image supports --user 1000.",
			},
		},

		// ── Resource ───────────────────────────────────
		{
			ID:            "res-001",
			Category:      "resource",
			Title:         "Disk Space Exhausted",
			MatchPatterns: []string{"no space left on device", "disk quota exceeded"},
			RootCause:     "The container or host filesystem has run out of disk space. This can happen when build artifacts or cache directories grow too large.",
			FixSuggestion: FixSuggestion{
				Description: "Free up disk space on the host. Consider increasing cache cleanup frequency or adding disk space monitoring.",
			},
		},
		{
			ID:            "res-002",
			Category:      "resource",
			Title:         "Memory Allocation Failure",
			MatchPatterns: []string{"cannot allocate memory", "out of memory", "OOM killed", "memory allocation failed"},
			RootCause:     "The container has exhausted its memory limit. The process was killed by the OOM killer or could not allocate the required memory.",
			FixSuggestion: FixSuggestion{
				Description: "Increase the container's memory limit, or optimize the build step to consume less memory",
				ConfigOverride: map[string]any{
					"memory": "4g",
					"memory_swap": "4g",
				},
			},
		},
		{
			ID:            "res-003",
			Category:      "resource",
			Title:         "Operation Timed Out",
			MatchPatterns: []string{"timeout", "timed out", "deadline exceeded", "context deadline exceeded"},
			RootCause:     "The operation exceeded its configured timeout. This could be due to a slow network, a large download, or an inefficient build step.",
			FixSuggestion: FixSuggestion{
				Description: "Increase the step timeout, or optimize the operation to complete faster",
				ConfigOverride: map[string]any{
					"timeout": "600",
				},
			},
		},

		// ── Application Code ───────────────────────────
		{
			ID:            "app-001",
			Category:      "app_code",
			Title:         "Application Panic",
			MatchPatterns: []string{"panic:", "fatal error:", "goroutine "},
			RootCause:     "The application code panicked or encountered a fatal error. This is an application-level bug that needs to be fixed in the source code.",
			FixSuggestion: FixSuggestion{
				Description: "Review the source code around the panic location. Check for nil pointer dereferences, out-of-bounds access, or assertion failures.",
			},
		},
		{
			ID:            "app-002",
			Category:      "app_code",
			Title:         "Test Failure",
			MatchPatterns: []string{"FAIL", "tests failed", "test failed", "exit status 1"},
			RootCause:     "One or more tests failed during the test step. The test output should indicate which tests failed and why.",
			FixSuggestion: FixSuggestion{
				Description: "Review the test output to identify failing tests. Check for assertion failures, compilation errors, or dependency issues.",
			},
		},

		// ── Configuration ──────────────────────────────
		{
			ID:            "cfg-001",
			Category:      "configuration",
			Title:         "Configuration File Not Found",
			MatchPatterns: []string{"could not find", "not found", "config file not found", "no such file"},
			RootCause:     "A required configuration file or resource could not be found at the expected path. The step may be missing a required file from the workspace or a previous step.",
			FixSuggestion: FixSuggestion{
				Description: "Verify that the required file exists in the workspace. Check the step's dependencies to ensure the file is generated by a previous step.",
			},
		},
	}
}
