package run

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/kexer2018/miniflow/internal/container"
	"github.com/kexer2018/miniflow/internal/db"
	internalpipeline "github.com/kexer2018/miniflow/internal/pipeline"
	"github.com/kexer2018/miniflow/internal/stepregistry"
	pipelinespec "github.com/kexer2018/miniflow/pkg/pipeline"
)

type Service struct {
	store     db.Store
	mgr       container.Manager
	wsManager *container.WorkspaceManager

	mu          sync.RWMutex
	runs        map[string]*Run
	steps       map[string][]StepRun
	history     map[string][]Event
	subscribers map[string]map[chan Event]struct{}
}

func NewService(store db.Store, mgr container.Manager, ws *container.WorkspaceManager) *Service {
	if ws == nil {
		ws = container.NewWorkspaceManager("")
	}
	return &Service{
		store:       store,
		mgr:         mgr,
		wsManager:   ws,
		runs:        make(map[string]*Run),
		steps:       make(map[string][]StepRun),
		history:     make(map[string][]Event),
		subscribers: make(map[string]map[chan Event]struct{}),
	}
}

func (s *Service) Start(ctx context.Context, spec pipelinespec.PipelineSpec) (*Run, error) {
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	if s.mgr == nil {
		return nil, fmt.Errorf("container manager is required")
	}

	id := uuid.New().String()
	now := time.Now()
	run := &Run{
		ID:        id,
		Name:      spec.Name,
		Status:    StatusQueued,
		Spec:      spec,
		StartedAt: now,
	}
	stepRuns := make([]StepRun, len(spec.Steps))
	for i, step := range spec.Steps {
		stepRuns[i] = StepRun{Name: step.Name, Status: StatusQueued}
	}

	s.mu.Lock()
	s.runs[id] = run
	s.steps[id] = stepRuns
	s.mu.Unlock()

	created := cloneRun(run)
	s.publish(Event{Type: EventRunStatus, RunID: id, Status: StatusQueued, Time: now})

	go s.execute(context.WithoutCancel(ctx), id, spec)

	return created, nil
}

func (s *Service) Get(id string) (*Run, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	run, ok := s.runs[id]
	if !ok {
		return nil, false
	}
	return cloneRun(run), true
}

func (s *Service) Steps(id string) ([]StepRun, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	steps, ok := s.steps[id]
	if !ok {
		return nil, false
	}
	copied := make([]StepRun, len(steps))
	copy(copied, steps)
	return copied, true
}

func (s *Service) Subscribe(id string) (<-chan Event, func(), bool) {
	s.mu.Lock()
	if _, ok := s.runs[id]; !ok {
		s.mu.Unlock()
		return nil, nil, false
	}
	ch := make(chan Event, 64)
	for _, event := range s.history[id] {
		ch <- event
	}
	if s.subscribers[id] == nil {
		s.subscribers[id] = make(map[chan Event]struct{})
	}
	s.subscribers[id][ch] = struct{}{}
	s.mu.Unlock()

	unsubscribe := func() {
		s.mu.Lock()
		if subs := s.subscribers[id]; subs != nil {
			delete(subs, ch)
		}
		s.mu.Unlock()
	}
	return ch, unsubscribe, true
}

func (s *Service) execute(ctx context.Context, runID string, spec pipelinespec.PipelineSpec) {
	s.updateRun(runID, StatusRunning, "")
	s.publish(Event{Type: EventRunStatus, RunID: runID, Status: StatusRunning, Time: time.Now()})

	steps, err := compileSteps(spec)
	if err != nil {
		s.finishRun(runID, StatusFailed, err.Error(), nil)
		return
	}

	p := &internalpipeline.Pipeline{
		ID:        runID,
		Name:      spec.Name,
		Version:   spec.Version,
		Workspace: spec.Workspace,
		Status:    internalpipeline.StatusPending,
		Steps:     steps,
	}

	executor := internalpipeline.NewExecutor(s.mgr, s.wsManager)
	result := executor.ExecutePipelineWithObserver(ctx, p, &observer{service: s, runID: runID})

	finalStatus := convertStatus(result.Status)
	if s.store != nil {
		_ = s.store.SavePipelineResult(ctx, result)
	}
	s.finishRun(runID, finalStatus, "", result)
}

func compileSteps(spec pipelinespec.PipelineSpec) ([]internalpipeline.Step, error) {
	steps := make([]internalpipeline.Step, len(spec.Steps))
	for i, stepSpec := range spec.Steps {
		step, err := stepregistry.Compile(stepSpec)
		if err != nil {
			return nil, err
		}
		step.Timeout = time.Duration(stepSpec.Timeout) * time.Second
		if stepSpec.Cache != nil {
			step.Cache = &internalpipeline.Cache{
				Path: stepSpec.Cache.Path,
				Key:  stepSpec.Cache.Key,
			}
		}
		steps[i] = step
	}
	return steps, nil
}

type observer struct {
	service *Service
	runID   string
}

func (o *observer) StepStarted(step internalpipeline.Step) {
	o.service.updateStep(o.runID, step.Name, StatusRunning, 0, 0, "")
	o.service.publish(Event{
		Type:   EventStepStatus,
		RunID:  o.runID,
		Step:   step.Name,
		Status: StatusRunning,
		Time:   time.Now(),
	})
}

func (o *observer) StepFinished(result internalpipeline.StepResult) {
	status := convertStatus(result.Status)
	o.service.updateStep(o.runID, result.Name, status, result.ExitCode, result.DurationMs, result.Error)
	o.service.publish(Event{
		Type:       EventStepStatus,
		RunID:      o.runID,
		Step:       result.Name,
		Status:     status,
		ExitCode:   result.ExitCode,
		DurationMs: result.DurationMs,
		Message:    result.Error,
		Time:       time.Now(),
	})
	if result.RawLog != "" {
		o.service.publish(Event{
			Type:    EventLog,
			RunID:   o.runID,
			Step:    result.Name,
			Message: result.RawLog,
			Time:    time.Now(),
		})
	}
}

func (s *Service) updateRun(runID string, status Status, errText string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if run := s.runs[runID]; run != nil {
		run.Status = status
		run.Error = errText
	}
}

func (s *Service) finishRun(runID string, status Status, errText string, result *internalpipeline.PipelineResult) {
	now := time.Now()
	s.mu.Lock()
	run := s.runs[runID]
	if run != nil {
		run.Status = status
		run.Error = errText
		run.FinishedAt = now
		if result != nil {
			run.DurationMs = result.DurationMs
		} else {
			run.DurationMs = now.Sub(run.StartedAt).Milliseconds()
		}
	}
	s.mu.Unlock()

	event := Event{Type: EventRunDone, RunID: runID, Status: status, Message: errText, Time: now}
	if run != nil {
		event.DurationMs = run.DurationMs
	}
	s.publish(event)
}

func (s *Service) updateStep(runID, name string, status Status, exitCode int, durationMs int64, errText string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	steps := s.steps[runID]
	for i := range steps {
		if steps[i].Name == name {
			steps[i].Status = status
			steps[i].ExitCode = exitCode
			steps[i].DurationMs = durationMs
			steps[i].Error = errText
			break
		}
	}
	s.steps[runID] = steps
}

func (s *Service) publish(event Event) {
	if event.Time.IsZero() {
		event.Time = time.Now()
	}
	s.mu.Lock()
	s.history[event.RunID] = append(s.history[event.RunID], event)
	subs := make([]chan Event, 0, len(s.subscribers[event.RunID]))
	for ch := range s.subscribers[event.RunID] {
		subs = append(subs, ch)
	}
	s.mu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- event:
		default:
		}
	}
}

func convertStatus(status internalpipeline.Status) Status {
	switch status {
	case internalpipeline.StatusSuccess:
		return StatusSuccess
	case internalpipeline.StatusFailed:
		return StatusFailed
	case internalpipeline.StatusCancelled:
		return StatusCancelled
	case internalpipeline.StatusRunning:
		return StatusRunning
	case internalpipeline.StatusSkipped:
		return StatusSkipped
	default:
		return StatusQueued
	}
}

func cloneRun(run *Run) *Run {
	if run == nil {
		return nil
	}
	copied := *run
	return &copied
}
