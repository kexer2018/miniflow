package run

import (
	"context"
	"testing"
	"time"

	"github.com/kexer2018/miniflow/internal/container"
	pipelinespec "github.com/kexer2018/miniflow/pkg/pipeline"
)

type fakeContainerManager struct {
	outputs []container.Result
	calls   int
}

func (m *fakeContainerManager) RunContainer(ctx context.Context, cfg container.Config) (container.Result, error) {
	m.calls++
	if len(m.outputs) == 0 {
		return container.Result{Output: "ok", ExitCode: 0}, nil
	}
	result := m.outputs[0]
	m.outputs = m.outputs[1:]
	return result, nil
}

func (m *fakeContainerManager) PullImage(ctx context.Context, image string) error {
	return nil
}

func (m *fakeContainerManager) ImageExists(ctx context.Context, image string) (bool, error) {
	return true, nil
}

func (m *fakeContainerManager) Close() error {
	return nil
}

func TestServiceStartPublishesRunEvents(t *testing.T) {
	mgr := &fakeContainerManager{
		outputs: []container.Result{
			{Output: "", ExitCode: 0},                // workspace chown
			{Output: "hello from step", ExitCode: 0}, // actual step
		},
	}
	service := NewService(nil, mgr, container.NewWorkspaceManager(t.TempDir()))
	spec := pipelinespec.PipelineSpec{
		Version: "1.1",
		Name:    "api-run",
		Steps: []pipelinespec.StepSpec{
			{
				Name:  "test",
				Type:  "script.run",
				Image: "alpine:latest",
				With: map[string]any{
					"script": "echo hello",
				},
			},
		},
	}

	run, err := service.Start(context.Background(), spec)
	if err != nil {
		t.Fatalf("start run: %v", err)
	}

	events, unsubscribe, ok := service.Subscribe(run.ID)
	if !ok {
		t.Fatalf("expected subscription for run %s", run.ID)
	}
	defer unsubscribe()

	seen := map[EventType]bool{}
	deadline := time.After(2 * time.Second)
	for !seen[EventRunDone] {
		select {
		case event := <-events:
			seen[event.Type] = true
		case <-deadline:
			t.Fatalf("timed out waiting for run_done; seen=%#v", seen)
		}
	}

	if !seen[EventRunStatus] {
		t.Fatalf("expected run_status event, seen=%#v", seen)
	}
	if !seen[EventStepStatus] {
		t.Fatalf("expected step_status event, seen=%#v", seen)
	}
	if !seen[EventLog] {
		t.Fatalf("expected log event, seen=%#v", seen)
	}

	stored, ok := service.Get(run.ID)
	if !ok {
		t.Fatalf("expected stored run %s", run.ID)
	}
	if stored.Status != StatusSuccess {
		t.Fatalf("expected success, got %q", stored.Status)
	}

	steps, ok := service.Steps(run.ID)
	if !ok {
		t.Fatalf("expected step state for run %s", run.ID)
	}
	if len(steps) != 1 || steps[0].Status != StatusSuccess {
		t.Fatalf("expected one successful step, got %#v", steps)
	}
}

func TestServicePublishesSanitizedLogEvents(t *testing.T) {
	mgr := &fakeContainerManager{
		outputs: []container.Result{
			{Output: "", ExitCode: 0}, // workspace chown
			{Output: "token=ghp_abcdefghijklmnopqrstuvwxyz1234567890AB", ExitCode: 0},
		},
	}
	service := NewService(nil, mgr, container.NewWorkspaceManager(t.TempDir()))
	spec := pipelinespec.PipelineSpec{
		Version: "1.1",
		Name:    "api-run",
		Steps: []pipelinespec.StepSpec{
			{
				Name:  "test",
				Type:  "script.run",
				Image: "alpine:latest",
				With: map[string]any{
					"script": "echo secret",
				},
			},
		},
	}

	run, err := service.Start(context.Background(), spec)
	if err != nil {
		t.Fatalf("start run: %v", err)
	}

	events, unsubscribe, ok := service.Subscribe(run.ID)
	if !ok {
		t.Fatalf("expected subscription for run %s", run.ID)
	}
	defer unsubscribe()

	deadline := time.After(2 * time.Second)
	for {
		select {
		case event := <-events:
			if event.Type == EventLog {
				if event.Message == "token=ghp_abcdefghijklmnopqrstuvwxyz1234567890AB" {
					t.Fatalf("expected sanitized log event, got raw message")
				}
				if event.Message != "token=***GH_TOKEN***" {
					t.Fatalf("expected GitHub token redaction, got %q", event.Message)
				}
				return
			}
		case <-deadline:
			t.Fatalf("timed out waiting for log event")
		}
	}
}
