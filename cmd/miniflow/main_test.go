package main

import (
	"os"
	"path/filepath"
	"testing"

	pipelinespec "github.com/kexer2018/miniflow/pkg/pipeline"
)

func TestValidateCommandAcceptsFileFlag(t *testing.T) {
	oldJSONInput := jsonInput
	t.Cleanup(func() {
		jsonInput = oldJSONInput
	})
	jsonInput = ""

	dir := t.TempDir()
	specPath := filepath.Join(dir, "pipeline.json")
	specJSON := `{
		"version": "1.1",
		"name": "validate-file-flag",
		"steps": [
			{
				"name": "test",
				"type": "script.run",
				"image": "golang:1.25",
				"with": { "script": "go test ./..." }
			}
		]
	}`
	if err := os.WriteFile(specPath, []byte(specJSON), 0644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	cmd := newValidateCmd()
	cmd.SetArgs([]string{"-f", specPath})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected validate -f to succeed, got: %v", err)
	}
}

func TestBuildStepsCompilesTypedScriptRun(t *testing.T) {
	spec := &pipelinespec.PipelineSpec{
		Version: "1.1",
		Name:    "typed",
		Steps: []pipelinespec.StepSpec{
			{
				Name:      "test",
				Type:      "script.run",
				Image:     "golang:1.25",
				DependsOn: []string{"checkout"},
				With: map[string]any{
					"script": "go test ./...\ngo build ./cmd/miniflow",
				},
			},
		},
	}

	steps := buildSteps(spec, nil)

	if len(steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(steps))
	}
	if steps[0].Name != "test" {
		t.Fatalf("expected step name test, got %q", steps[0].Name)
	}
	if steps[0].Image != "golang:1.25" {
		t.Fatalf("expected image golang:1.25, got %q", steps[0].Image)
	}
	if got := steps[0].Commands; len(got) != 1 || got[0] != "go test ./...\ngo build ./cmd/miniflow" {
		t.Fatalf("expected compiled script command, got %#v", got)
	}
	if steps[0].DependsOn[0] != "checkout" {
		t.Fatalf("expected depends_on checkout, got %#v", steps[0].DependsOn)
	}
}
