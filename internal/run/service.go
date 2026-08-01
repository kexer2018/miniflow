package run

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/kexer2018/miniflow/internal/artifact"
	"github.com/kexer2018/miniflow/internal/container"
	"github.com/kexer2018/miniflow/internal/db"
	logutil "github.com/kexer2018/miniflow/internal/log"
	internalpipeline "github.com/kexer2018/miniflow/internal/pipeline"
	"github.com/kexer2018/miniflow/internal/secret"
	"github.com/kexer2018/miniflow/internal/source"
	"github.com/kexer2018/miniflow/internal/speccompiler"
	"github.com/kexer2018/miniflow/internal/stepops"
	pipelinespec "github.com/kexer2018/miniflow/pkg/pipeline"
)

type Service struct {
	store     db.Store
	mgr       container.Manager
	wsManager *container.WorkspaceManager
	credStore *secret.CredentialStore
	artifacts *artifact.Manager

	mu          sync.RWMutex
	runs        map[string]*Run
	steps       map[string][]StepRun
	history     map[string][]Event
	subscribers map[string]map[chan Event]struct{}
	cancels     map[string]context.CancelFunc
}

func NewService(store db.Store, mgr container.Manager, ws *container.WorkspaceManager) *Service {
	if ws == nil {
		ws = container.NewWorkspaceManager("")
	}
	var artifactStore db.ArtifactStore
	if indexedStore, ok := store.(db.ArtifactStore); ok {
		artifactStore = indexedStore
	}
	return &Service{
		store:       store,
		mgr:         mgr,
		wsManager:   ws,
		credStore:   secret.NewCredentialStore(),
		artifacts:   artifact.NewManager(filepath.Join(filepath.Dir(ws.BaseDir), "artifacts"), artifactStore),
		runs:        make(map[string]*Run),
		steps:       make(map[string][]StepRun),
		history:     make(map[string][]Event),
		subscribers: make(map[string]map[chan Event]struct{}),
		cancels:     make(map[string]context.CancelFunc),
	}
}

// ArtifactManager exposes the local artifact reader used by the API layer.
func (s *Service) ArtifactManager() *artifact.Manager { return s.artifacts }

// SetCredentialStore sets the secret/source credential resolver used by API runs.
func (s *Service) SetCredentialStore(store *secret.CredentialStore) {
	if store == nil {
		store = secret.NewCredentialStore()
	}
	s.credStore = store
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

	execCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	s.mu.Lock()
	s.cancels[id] = cancel
	s.mu.Unlock()

	go s.execute(execCtx, id, spec)

	return created, nil
}

func (s *Service) Cancel(id string) (*Run, bool) {
	s.mu.Lock()
	run, ok := s.runs[id]
	if !ok {
		s.mu.Unlock()
		return nil, false
	}
	cancel := s.cancels[id]
	active := run.Status == StatusQueued || run.Status == StatusRunning
	if active {
		run.Status = StatusCancelled
	}
	copied := cloneRun(run)
	s.mu.Unlock()

	if active && cancel != nil {
		cancel()
	}
	if active {
		s.publish(Event{Type: EventRunStatus, RunID: id, Status: StatusCancelled, Time: time.Now()})
	}
	return copied, true
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

	steps, err := speccompiler.BuildSteps(&spec, s.credStore)
	if err != nil {
		s.finishRun(runID, StatusFailed, err.Error(), nil)
		return
	}

	if spec.Source != nil {
		wsPath, err := s.wsManager.CreateWorkspace(runID)
		if err != nil {
			s.finishRun(runID, StatusFailed, fmt.Sprintf("create workspace: %v", err), nil)
			return
		}
		sourceMgr := source.NewManager(s.credStore)
		if _, err := sourceMgr.PrepareWorkspace(ctx, spec.Source, wsPath); err != nil {
			s.finishRun(runID, StatusFailed, fmt.Sprintf("source checkout: %v", err), nil)
			return
		}
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
	executor.SetOperationHandler(stepops.NewManager(s.wsManager, source.NewManager(s.credStore), s.artifacts))
	result := executor.ExecutePipelineWithObserver(ctx, p, &observer{service: s, runID: runID})
	sanitizePipelineResult(result)

	finalStatus := convertStatus(result.Status)
	if s.store != nil {
		_ = s.store.SavePipelineResult(ctx, result)
	}
	s.finishRun(runID, finalStatus, "", result)
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

func (o *observer) StepLog(step internalpipeline.Step, line string) {
	if line == "" {
		return
	}
	message := logutil.SanitizeString(line)
	slog.Info("step log",
		"run_id", o.runID,
		"step", step.Name,
		"message", message,
	)
	o.service.publish(Event{
		Type:    EventLog,
		RunID:   o.runID,
		Step:    step.Name,
		Message: message,
		Time:    time.Now(),
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
}

func sanitizePipelineResult(result *internalpipeline.PipelineResult) {
	if result == nil {
		return
	}
	for i := range result.StepResults {
		if result.StepResults[i].RawLog != "" && result.StepResults[i].Sanitized == "" {
			result.StepResults[i].Sanitized = logutil.SanitizeString(result.StepResults[i].RawLog)
		}
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
	delete(s.cancels, runID)
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
