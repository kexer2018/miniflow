// Package stepregistry defines product-level typed steps and compiles them to
// the current internal pipeline execution model.
package stepregistry

import (
	"fmt"
	"strings"

	internalpipeline "github.com/kexer2018/miniflow/internal/pipeline"
	pipelinespec "github.com/kexer2018/miniflow/pkg/pipeline"
)

const (
	TypeScriptRun       = "script.run"
	TypeGitCheckout     = "git.checkout"
	TypeFileOperation   = "file.operation"
	TypeCacheRestore    = "cache.restore"
	TypeCacheSave       = "cache.save"
	TypeArtifactSave    = "artifact.save"
	TypeArtifactRestore = "artifact.restore"
)

// Definition describes one built-in Step type for API/UI consumers.
type Definition struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Group       string         `json:"group"`
	Description string         `json:"description"`
	Defaults    map[string]any `json:"defaults,omitempty"`
	Schema      map[string]any `json:"schema"`
}

// Builtins returns the MVP Step types known by this backend.
func Builtins() []Definition {
	return []Definition{
		{
			ID:          TypeScriptRun,
			Name:        "Shell Script",
			Group:       "Script",
			Description: "Run a user-provided script in an ephemeral Docker container.",
			Schema: map[string]any{
				"type": "object",
				"required": []string{
					"script",
				},
				"properties": map[string]any{
					"script": map[string]any{
						"type":        "string",
						"description": "Script content to execute.",
					},
				},
			},
		},
		operationDefinition(TypeGitCheckout, "Git Checkout", "Source", "Clone a Git repository into the shared workspace.", []string{"repository"}),
		operationDefinition(TypeFileOperation, "File Operation", "Workspace", "Copy, move, delete, create, archive, or extract workspace files.", []string{"operation"}),
		operationDefinition(TypeCacheRestore, "Cache Restore", "Storage", "Restore a local cache into the workspace.", []string{"key", "path"}),
		operationDefinition(TypeCacheSave, "Cache Save", "Storage", "Save a workspace path into the local cache.", []string{"key", "path"}),
		operationDefinition(TypeArtifactSave, "Artifact Save", "Storage", "Archive workspace files for this run.", []string{"name", "path"}),
		operationDefinition(TypeArtifactRestore, "Artifact Restore", "Storage", "Restore an artifact archive into the workspace.", []string{"name"}),
	}
}

func operationDefinition(id, name, group, description string, required []string) Definition {
	return Definition{ID: id, Name: name, Group: group, Description: description, Schema: map[string]any{
		"type": "object", "required": required, "properties": map[string]any{},
	}}
}

// Compile converts a public StepSpec into the current internal Step model.
func Compile(spec pipelinespec.StepSpec) (internalpipeline.Step, error) {
	step := internalpipeline.Step{
		Name:       spec.Name,
		Image:      spec.Image,
		Commands:   spec.Commands,
		DependsOn:  spec.DependsOn,
		Env:        spec.Env,
		Entrypoint: spec.Entrypoint,
		SSHAgent:   spec.SSHAgent,
	}

	switch spec.Type {
	case "":
		return step, nil
	case TypeScriptRun:
		script, ok := spec.With["script"].(string)
		if !ok || script == "" {
			return internalpipeline.Step{}, fmt.Errorf("step %q requires with.script", spec.Name)
		}
		step.Commands = []string{script}
		return step, nil
	case TypeGitCheckout, TypeCacheRestore, TypeCacheSave, TypeArtifactSave, TypeArtifactRestore:
		step.Operation = &internalpipeline.Operation{Type: spec.Type, With: spec.With}
		return step, nil
	case TypeFileOperation:
		command, err := compileFileOperation(spec.With)
		if err != nil {
			return internalpipeline.Step{}, fmt.Errorf("step %q: %w", spec.Name, err)
		}
		if step.Image == "" {
			step.Image = "alpine:latest"
		}
		step.Commands = []string{command}
		return step, nil
	default:
		return internalpipeline.Step{}, fmt.Errorf("step %q has unsupported type %q", spec.Name, spec.Type)
	}
}

func compileFileOperation(values map[string]any) (string, error) {
	operation, _ := values["operation"].(string)
	source, _ := values["source"].(string)
	target, _ := values["target"].(string)
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }
	switch operation {
	case "mkdir":
		if target == "" {
			return "", fmt.Errorf("mkdir requires with.target")
		}
		return "mkdir -p " + quote(target), nil
	case "copy":
		if source == "" || target == "" {
			return "", fmt.Errorf("copy requires with.source and with.target")
		}
		return "cp -a " + quote(source) + " " + quote(target), nil
	case "move":
		if source == "" || target == "" {
			return "", fmt.Errorf("move requires with.source and with.target")
		}
		return "mv " + quote(source) + " " + quote(target), nil
	case "delete":
		if source == "" {
			return "", fmt.Errorf("delete requires with.source")
		}
		return "rm -rf " + quote(source), nil
	case "archive":
		if source == "" || target == "" {
			return "", fmt.Errorf("archive requires with.source and with.target")
		}
		return "tar -czf " + quote(target) + " " + quote(source), nil
	case "extract":
		if source == "" || target == "" {
			return "", fmt.Errorf("extract requires with.source and with.target")
		}
		return "mkdir -p " + quote(target) + " && tar -xzf " + quote(source) + " -C " + quote(target), nil
	default:
		return "", fmt.Errorf("unsupported file operation %q", operation)
	}
}
