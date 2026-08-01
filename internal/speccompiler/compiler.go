// Package speccompiler converts public PipelineSpec values into the internal
// Docker-backed execution model used by the CLI and API run service.
package speccompiler

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	internalpipeline "github.com/kexer2018/miniflow/internal/pipeline"
	"github.com/kexer2018/miniflow/internal/secret"
	"github.com/kexer2018/miniflow/internal/stepregistry"
	pipelinespec "github.com/kexer2018/miniflow/pkg/pipeline"
)

// BuildSteps compiles the public pipeline spec into internal executable steps.
// It is the shared CLI/API path so PipelineSpec stays the stable protocol.
func BuildSteps(spec *pipelinespec.PipelineSpec, credStore *secret.CredentialStore) ([]internalpipeline.Step, error) {
	pipelineEnv, err := PipelineEnv(spec)
	if err != nil {
		return nil, err
	}

	steps := make([]internalpipeline.Step, len(spec.Steps))
	for i, s := range spec.Steps {
		step, err := stepregistry.Compile(s)
		if err != nil {
			return nil, err
		}

		env := make([]string, len(pipelineEnv), len(pipelineEnv)+len(s.Env)+len(s.Secrets))
		copy(env, pipelineEnv)
		env = append(env, s.Env...)

		for _, secName := range s.Secrets {
			if val, ok := credStore.ResolveSecretEnv(secName); ok {
				env = append(env, val)
			} else {
				slog.Warn("secret not found, skipping", "secret", secName, "step", s.Name)
			}
		}

		step.Env = env
		step.Timeout = time.Duration(s.Timeout) * time.Second
		if s.Cache != nil {
			step.Cache = &internalpipeline.Cache{
				Path: s.Cache.Path,
				Key:  s.Cache.Key,
			}
		}
		steps[i] = step
	}
	return steps, nil
}

// PipelineEnv returns sorted pipeline-level KEY=VALUE values, with Env
// overriding entries read from EnvFile.
func PipelineEnv(spec *pipelinespec.PipelineSpec) ([]string, error) {
	envMap := make(map[string]string, len(spec.Env))

	if spec.EnvFile != "" {
		envFilePairs, err := ParseEnvFile(spec.EnvFile)
		if err != nil {
			return nil, err
		}
		for _, pair := range envFilePairs {
			if k, v, ok := strings.Cut(pair, "="); ok {
				envMap[k] = v
			}
		}
	}

	for k, v := range spec.Env {
		envMap[k] = v
	}

	keys := make([]string, 0, len(envMap))
	for k := range envMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	env := make([]string, 0, len(keys))
	for _, k := range keys {
		env = append(env, k+"="+envMap[k])
	}
	return env, nil
}

// ParseEnvFile reads a .env file and returns KEY=VALUE entries.
// It supports blank lines and # comments.
func ParseEnvFile(path string) ([]string, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("read env_file: %w", err)
	}

	var result []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !strings.Contains(line, "=") {
			slog.Warn("env_file: skipping line without '='", "line", line)
			continue
		}
		result = append(result, line)
	}
	return result, nil
}
