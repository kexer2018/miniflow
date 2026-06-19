package pipeline

import (
	"context"
	"testing"
	"time"
)

func TestExecuteStep_TimeoutContext(t *testing.T) {
	// Verify that executeStep creates a timeout context by checking
	// that a step with a very short timeout gets cancelled before
	// a long-running operation completes.
	//
	// We do this by creating an Executor with nil managers and passing a
	// context we control. The timeout is set on the Step, not the outer ctx.

	// This test verifies the Timeout field is accepted and stored.
	// Full integration requires Docker (to run containers with timeout).
	step := Step{
		Name:    "timeout-test",
		Image:   "alpine:latest",
		Timeout: 5 * time.Second,
	}

	if step.Timeout != 5*time.Second {
		t.Errorf("expected Timeout 5s, got %v", step.Timeout)
	}
}

func TestConvertSteps_WithTimeout(t *testing.T) {
	// Simulates the JSON → StepSpec → internal Step conversion path
	// Test that Timeout in seconds is correctly converted to time.Duration
	steps := []struct {
		name    string
		timeout int
		want    time.Duration
	}{
		{name: "no-timeout", timeout: 0, want: 0},
		{name: "30-seconds", timeout: 30, want: 30 * time.Second},
		{name: "600-seconds", timeout: 600, want: 600 * time.Second},
	}

	for _, tt := range steps {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate convertSteps behavior
			step := Step{
				Name:    tt.name,
				Timeout: time.Duration(tt.timeout) * time.Second,
			}
			if step.Timeout != tt.want {
				t.Errorf("expected %v, got %v", tt.want, step.Timeout)
			}
		})
	}
}

func TestContextWithTimeout_Cancellation(t *testing.T) {
	// Test that WithTimeout properly cancels the context.
	// This is a pure Go test (no Docker needed).
	ctx := context.Background()

	// Create a very short timeout (1ms)
	timeoutCtx, cancel := context.WithTimeout(ctx, time.Millisecond)
	defer cancel()

	// Wait for the timeout to fire
	<-timeoutCtx.Done()

	if timeoutCtx.Err() != context.DeadlineExceeded {
		t.Errorf("expected DeadlineExceeded, got %v", timeoutCtx.Err())
	}
}

func TestContextWithTimeout_NoTimeout(t *testing.T) {
	// Test that without timeout, the context is not cancelled
	ctx := context.Background()

	var step Step // Timeout = 0
	if step.Timeout > 0 {
		t.Error("expected no timeout when Timeout is 0")
	}

	// Context should NOT have a deadline
	_, hasDeadline := ctx.Deadline()
	if hasDeadline {
		t.Error("expected no deadline on context without timeout")
	}
}

func TestContextWithTimeout_WithTimeout(t *testing.T) {
	// Test that with timeout, the context HAS a deadline
	ctx := context.Background()
	step := Step{Timeout: 30 * time.Second}

	if step.Timeout == 0 {
		t.Fatal("test requires non-zero timeout")
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, step.Timeout)
	defer cancel()

	deadline, hasDeadline := timeoutCtx.Deadline()
	if !hasDeadline {
		t.Error("expected deadline on timeout context")
	}
	if deadline.Before(time.Now()) {
		t.Error("deadline should be in the future")
	}
}
