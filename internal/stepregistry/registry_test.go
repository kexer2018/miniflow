package stepregistry

import (
	"os"
	"path/filepath"
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

func TestRegistryLoadsTrustedExtension(t *testing.T) {
	bundle := t.TempDir()
	manifest := `{
  "apiVersion":"miniflow.dev/v1",
  "kind":"ScriptStepExtension",
  "metadata":{"id":"com.acme.node.build","version":"1.0.0","name":"Acme Node Build","description":"Build"},
  "spec":{"runtime":{"image":"node:22-alpine","script":"run.sh"},"inputs":{"type":"object","required":["build_mode"],"properties":{"build_mode":{"type":"string","enum":["production"]}},"additionalProperties":false}}
}`
	if err := os.WriteFile(filepath.Join(bundle, "step.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "run.sh"), []byte("#!/bin/sh\necho build\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	registry, err := New(bundle)
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	step, err := registry.Compile(pipelinespec.StepSpec{Name: "build", Type: "com.acme.node.build", With: map[string]any{"build_mode": "production"}})
	if err != nil {
		t.Fatalf("compile extension: %v", err)
	}
	if step.Image != "node:22-alpine" || step.Extension == nil {
		t.Fatalf("unexpected extension step: %#v", step)
	}
	if got := step.Env; len(got) != 1 || got[0] != "MINIFLOW_INPUT_BUILD_MODE=production" {
		t.Fatalf("unexpected inputs: %#v", got)
	}
	if _, err := registry.Compile(pipelinespec.StepSpec{Name: "invalid", Type: "com.acme.node.build", With: map[string]any{"unknown": "value"}}); err == nil {
		t.Fatal("expected schema validation error")
	}
}

func TestRegistryRejectsEscapingScript(t *testing.T) {
	bundle := t.TempDir()
	manifest := `{"apiVersion":"miniflow.dev/v1","kind":"ScriptStepExtension","metadata":{"id":"com.acme.invalid","version":"1.0.0","name":"Invalid"},"spec":{"runtime":{"image":"alpine","script":"../run.sh"},"inputs":{"type":"object"}}}`
	if err := os.WriteFile(filepath.Join(bundle, "step.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := New(bundle); err == nil {
		t.Fatal("expected invalid script path to fail")
	}
}

func TestRegistryRejectsUnsafeBundleFiles(t *testing.T) {
	bundle := t.TempDir()
	manifest := `{"apiVersion":"miniflow.dev/v1","kind":"ScriptStepExtension","metadata":{"id":"com.acme.unsafe","version":"1.0.0","name":"Unsafe"},"spec":{"runtime":{"image":"alpine","script":"run.sh"},"inputs":{"type":"object"}}}`
	if err := os.WriteFile(filepath.Join(bundle, "step.json"), []byte(manifest), 0o666); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "run.sh"), []byte("echo unsafe"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(bundle, "step.json"), 0o666); err != nil {
		t.Fatal(err)
	}
	if _, err := New(bundle); err == nil {
		t.Fatal("expected group/world-writable manifest to fail")
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
