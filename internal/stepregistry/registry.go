// Package stepregistry defines product-level typed steps and compiles them to
// the current internal pipeline execution model.
package stepregistry

import (
	"fmt"

	internalpipeline "github.com/kexer2018/miniflow/internal/pipeline"
	pipelinespec "github.com/kexer2018/miniflow/pkg/pipeline"
)

const (
	TypeScriptRun = "script.run"
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
	}
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
	default:
		return internalpipeline.Step{}, fmt.Errorf("step %q has unsupported type %q", spec.Name, spec.Type)
	}
}
