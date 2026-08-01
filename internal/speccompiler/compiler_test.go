package speccompiler

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	pipelinespec "github.com/kexer2018/miniflow/pkg/pipeline"
)

func TestBuildStepsMergesEnvFilePipelineEnvAndStepEnv(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	if err := os.WriteFile(envPath, []byte("FROM_FILE=one\nOVERRIDE=file\n# comment\n"), 0644); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	spec := &pipelinespec.PipelineSpec{
		Version: "1.1",
		Name:    "env-merge",
		EnvFile: envPath,
		Env: map[string]string{
			"OVERRIDE": "spec",
			"PIPE":     "two",
		},
		Steps: []pipelinespec.StepSpec{
			{
				Name:  "test",
				Type:  "script.run",
				Image: "alpine:latest",
				With:  map[string]any{"script": "env"},
				Env:   []string{"STEP=three"},
			},
		},
	}

	steps, err := BuildSteps(spec, nil)
	if err != nil {
		t.Fatalf("build steps: %v", err)
	}

	want := []string{
		"FROM_FILE=one",
		"OVERRIDE=spec",
		"PIPE=two",
		"STEP=three",
	}
	if !reflect.DeepEqual(steps[0].Env, want) {
		t.Fatalf("env mismatch\nwant %#v\n got %#v", want, steps[0].Env)
	}
}

func TestBuildStepsReturnsEnvFileError(t *testing.T) {
	spec := &pipelinespec.PipelineSpec{
		Version: "1.1",
		Name:    "missing-env",
		EnvFile: filepath.Join(t.TempDir(), "missing.env"),
		Steps: []pipelinespec.StepSpec{
			{
				Name:  "test",
				Type:  "script.run",
				Image: "alpine:latest",
				With:  map[string]any{"script": "echo ok"},
			},
		},
	}

	if _, err := BuildSteps(spec, nil); err == nil {
		t.Fatalf("expected missing env_file error")
	}
}
