// Package run provides asynchronous API-facing pipeline execution state.
package run

import (
	"time"

	pipelinespec "github.com/kexer2018/miniflow/pkg/pipeline"
)

type Status string

const (
	StatusQueued      Status = "queued"
	StatusRunning     Status = "running"
	StatusSuccess     Status = "success"
	StatusFailed      Status = "failed"
	StatusCancelled   Status = "cancelled"
	StatusInterrupted Status = "interrupted"
	StatusSkipped     Status = "skipped"
)

type EventType string

const (
	EventRunStatus  EventType = "run_status"
	EventStepStatus EventType = "step_status"
	EventLog        EventType = "log"
	EventRunDone    EventType = "run_done"
)

type Run struct {
	ID         string                    `json:"id"`
	Name       string                    `json:"name"`
	Status     Status                    `json:"status"`
	Spec       pipelinespec.PipelineSpec `json:"spec,omitempty"`
	StartedAt  time.Time                 `json:"started_at,omitempty"`
	FinishedAt time.Time                 `json:"finished_at,omitempty"`
	DurationMs int64                     `json:"duration_ms,omitempty"`
	Error      string                    `json:"error,omitempty"`
}

type PipelineDefinition struct {
	Name      string                    `json:"name"`
	Spec      pipelinespec.PipelineSpec `json:"spec"`
	UpdatedAt time.Time                 `json:"updated_at"`
}

type StepRun struct {
	Name       string `json:"name"`
	Status     Status `json:"status"`
	ExitCode   int    `json:"exit_code"`
	DurationMs int64  `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
}

type Event struct {
	Type       EventType `json:"type"`
	RunID      string    `json:"run_id"`
	Step       string    `json:"step,omitempty"`
	Status     Status    `json:"status,omitempty"`
	Message    string    `json:"message,omitempty"`
	ExitCode   int       `json:"exit_code,omitempty"`
	DurationMs int64     `json:"duration_ms,omitempty"`
	Time       time.Time `json:"time"`
}
