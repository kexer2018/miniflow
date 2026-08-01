// Package stepops executes local-only platform primitives for typed Steps.
package stepops

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/kexer2018/miniflow/internal/artifact"
	"github.com/kexer2018/miniflow/internal/container"
	internalpipeline "github.com/kexer2018/miniflow/internal/pipeline"
	"github.com/kexer2018/miniflow/internal/source"
	pipelinespec "github.com/kexer2018/miniflow/pkg/pipeline"
)

type Manager struct {
	workspace *container.WorkspaceManager
	source    *source.Manager
	artifacts *artifact.Manager
}

func NewManager(workspace *container.WorkspaceManager, sourceManager *source.Manager, artifacts *artifact.Manager) *Manager {
	return &Manager{workspace: workspace, source: sourceManager, artifacts: artifacts}
}

func (m *Manager) ExecuteOperation(ctx context.Context, runID, workspace string, operation internalpipeline.Operation) (string, error) {
	switch operation.Type {
	case "git.checkout":
		repository, err := requiredString(operation.With, "repository")
		if err != nil {
			return "", err
		}
		target, err := localPath(workspace, stringValue(operation.With, "target_dir", "."))
		if err != nil {
			return "", err
		}
		if err := os.MkdirAll(target, 0o755); err != nil {
			return "", err
		}
		spec := pipelineSourceSpec(operation.With, repository)
		result, err := m.source.PrepareWorkspace(ctx, &spec, target)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("checked out %s at %s", result.RepoURL, result.CommitSHA), nil
	case "cache.restore":
		key, err := requiredString(operation.With, "key")
		if err != nil {
			return "", err
		}
		target, err := localPath(workspace, stringValue(operation.With, "path", ""))
		if err != nil {
			return "", err
		}
		cachePath := m.workspace.CachePath(key)
		if _, err := os.Stat(cachePath); os.IsNotExist(err) {
			return fmt.Sprintf("cache miss: %s", key), nil
		} else if err != nil {
			return "", err
		}
		if err := copyTree(cachePath, target); err != nil {
			return "", fmt.Errorf("restore cache: %w", err)
		}
		return fmt.Sprintf("cache restored: %s", key), nil
	case "cache.save":
		key, err := requiredString(operation.With, "key")
		if err != nil {
			return "", err
		}
		sourcePath, err := localPath(workspace, stringValue(operation.With, "path", ""))
		if err != nil {
			return "", err
		}
		if _, err := os.Stat(sourcePath); err != nil {
			return "", fmt.Errorf("cache source: %w", err)
		}
		cachePath := m.workspace.CachePath(key)
		if err := os.RemoveAll(cachePath); err != nil {
			return "", err
		}
		if err := copyTree(sourcePath, cachePath); err != nil {
			return "", fmt.Errorf("save cache: %w", err)
		}
		return fmt.Sprintf("cache saved: %s", key), nil
	case "artifact.save":
		if m.artifacts == nil {
			return "", fmt.Errorf("artifact manager is not configured")
		}
		name, err := requiredString(operation.With, "name")
		if err != nil {
			return "", err
		}
		path, err := requiredString(operation.With, "path")
		if err != nil {
			return "", err
		}
		record, err := m.artifacts.Save(ctx, runID, name, workspace, path, stringValue(operation.With, "if_no_files", "error"))
		if err != nil {
			return "", err
		}
		if record.Name == "" {
			return fmt.Sprintf("artifact %s not created: no files", name), nil
		}
		return fmt.Sprintf("artifact saved: %s (%d bytes)", record.Name, record.Size), nil
	case "artifact.restore":
		if m.artifacts == nil {
			return "", fmt.Errorf("artifact manager is not configured")
		}
		name, err := requiredString(operation.With, "name")
		if err != nil {
			return "", err
		}
		sourceRun := stringValue(operation.With, "run_id", runID)
		record, err := m.artifacts.Restore(ctx, sourceRun, name, workspace, stringValue(operation.With, "target_dir", "."))
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("artifact restored: %s from run %s", record.Name, record.RunID), nil
	default:
		return "", fmt.Errorf("unsupported operation %q", operation.Type)
	}
}

func pipelineSourceSpec(values map[string]any, repository string) pipelinespec.SourceSpec {
	shallow := true
	spec := pipelinespec.SourceSpec{
		Repository: repository,
		Ref:        stringValue(values, "ref", "main"),
		Credential: stringValue(values, "credential", ""),
		Shallow:    &shallow,
		Depth:      50,
	}
	if shallow, ok := values["shallow"].(bool); ok {
		spec.Shallow = &shallow
		if !shallow {
			spec.Depth = 0
		}
	}
	if depth, ok := values["depth"].(float64); ok {
		spec.Depth = int(depth)
	}
	if submodules, ok := values["submodules"].(bool); ok {
		spec.Submodules = submodules
	}
	return spec
}

func requiredString(values map[string]any, key string) (string, error) {
	value := stringValue(values, key, "")
	if value == "" {
		return "", fmt.Errorf("operation requires %s", key)
	}
	return value, nil
}

func stringValue(values map[string]any, key, fallback string) string {
	if value, ok := values[key].(string); ok && value != "" {
		return value
	}
	return fallback
}

func localPath(workspace, value string) (string, error) {
	if value == "." {
		return workspace, nil
	}
	if value == "" || filepath.IsAbs(value) {
		return "", fmt.Errorf("path must be relative to the workspace")
	}
	clean := filepath.Clean(value)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes workspace: %q", value)
	}
	return filepath.Join(workspace, clean), nil
}

func copyTree(source, target string) error {
	return filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		destination := target
		if rel != "." {
			destination = filepath.Join(target, rel)
		}
		if info.IsDir() {
			return os.MkdirAll(destination, info.Mode())
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return err
		}
		input, err := os.Open(path)
		if err != nil {
			return err
		}
		defer input.Close()
		output, err := os.OpenFile(destination, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
		if err != nil {
			return err
		}
		_, err = io.Copy(output, input)
		closeErr := output.Close()
		if err != nil {
			return err
		}
		return closeErr
	})
}
