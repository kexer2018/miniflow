package stepregistry

import (
	"testing"

	pipelinespec "github.com/kexer2018/miniflow/pkg/pipeline"
)

func TestCompileCoreOperationStep(t *testing.T) {
	step, err := Compile(pipelinespec.StepSpec{Name: "save", Type: TypeArtifactSave, With: map[string]any{"name": "build", "path": "dist"}})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if step.Operation == nil || step.Operation.Type != TypeArtifactSave {
		t.Fatalf("expected artifact operation, got %#v", step.Operation)
	}
}

func TestCompileFileOperationUsesContainer(t *testing.T) {
	step, err := Compile(pipelinespec.StepSpec{Name: "mkdir", Type: TypeFileOperation, With: map[string]any{"operation": "mkdir", "target": "dist"}})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if step.Image != "alpine:latest" || len(step.Commands) != 1 {
		t.Fatalf("unexpected file step: %#v", step)
	}
}
