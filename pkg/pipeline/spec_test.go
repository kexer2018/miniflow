package pipeline

import (
	"testing"
)

func TestPipelineSpecValidateWithSource(t *testing.T) {
	spec := &PipelineSpec{
		Version: "1.1",
		Name:    "test-pipeline",
		Source: &SourceSpec{
			Repository: "github.com/user/repo",
			Ref:        "develop",
		},
		Steps: []StepSpec{
			{Name: "build", Image: "golang:alpine", Commands: []string{"go build"}},
		},
	}

	if err := spec.Validate(); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if spec.Source.Ref != "develop" {
		t.Errorf("expected Ref 'develop', got %q", spec.Source.Ref)
	}
}

func TestPipelineSpecValidateSourceMissingRepo(t *testing.T) {
	spec := &PipelineSpec{
		Version: "1.1",
		Name:    "test-pipeline",
		Source:  &SourceSpec{Ref: "main"},
		Steps: []StepSpec{
			{Name: "build", Image: "golang:alpine", Commands: []string{"go build"}},
		},
	}

	if err := spec.Validate(); err != ErrSourceNoRepo {
		t.Fatalf("expected ErrSourceNoRepo, got: %v", err)
	}
}

func TestPipelineSpecValidateSourceDefaults(t *testing.T) {
	spec := &PipelineSpec{
		Version: "1.1",
		Name:    "test-pipeline",
		Source:  &SourceSpec{Repository: "github.com/user/repo"},
		Steps: []StepSpec{
			{Name: "build", Image: "golang:alpine", Commands: []string{"go build"}},
		},
	}

	if err := spec.Validate(); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if spec.Source.Ref != "main" {
		t.Errorf("expected default Ref 'main', got %q", spec.Source.Ref)
	}
	if spec.Source.Shallow == nil || !*spec.Source.Shallow {
		t.Error("expected Shallow default true")
	}
	if spec.Source.Depth != 50 {
		t.Errorf("expected default Depth 50, got %d", spec.Source.Depth)
	}
}

func TestPipelineSpecValidateBackwardCompat(t *testing.T) {
	// 没有 Source 的旧格式应该仍然校验通过
	spec := &PipelineSpec{
		Version: "1.0",
		Name:    "test-pipeline",
		Steps: []StepSpec{
			{Name: "build", Image: "golang:alpine", Commands: []string{"go build"}},
		},
	}

	if err := spec.Validate(); err != nil {
		t.Fatalf("expected no error for backward compat, got: %v", err)
	}
}

func TestPipelineSpecValidateTypedScriptRun(t *testing.T) {
	spec := &PipelineSpec{
		Version: "1.1",
		Name:    "typed-pipeline",
		Steps: []StepSpec{
			{
				Name:  "test",
				Type:  "script.run",
				Image: "golang:1.25",
				With: map[string]any{
					"script": "go test ./...",
				},
			},
		},
	}

	if err := spec.Validate(); err != nil {
		t.Fatalf("expected typed script.run step to validate, got: %v", err)
	}
}

func TestPipelineSpecValidateTypedScriptRunRequiresScript(t *testing.T) {
	spec := &PipelineSpec{
		Version: "1.1",
		Name:    "typed-pipeline",
		Steps: []StepSpec{
			{
				Name:  "test",
				Type:  "script.run",
				Image: "golang:1.25",
				With:  map[string]any{},
			},
		},
	}

	if err := spec.Validate(); err == nil {
		t.Fatal("expected script.run without with.script to fail validation")
	}
}
