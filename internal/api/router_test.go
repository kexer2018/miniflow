package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kexer2018/miniflow/internal/container"
	runservice "github.com/kexer2018/miniflow/internal/run"
	pipelinespec "github.com/kexer2018/miniflow/pkg/pipeline"
)

type fakeRunContainerManager struct {
	outputs       []container.Result
	respectCancel bool
	calls         int
	blockAfter    int
}

func (m *fakeRunContainerManager) RunContainer(ctx context.Context, cfg container.Config) (container.Result, error) {
	m.calls++
	if m.blockAfter > 0 && m.calls >= m.blockAfter {
		<-ctx.Done()
		return container.Result{ExitCode: -1, Output: ctx.Err().Error()}, ctx.Err()
	}
	if m.respectCancel {
		select {
		case <-ctx.Done():
			return container.Result{ExitCode: -1, Output: ctx.Err().Error()}, ctx.Err()
		default:
		}
	}
	if len(m.outputs) == 0 {
		return container.Result{Output: "ok", ExitCode: 0}, nil
	}
	result := m.outputs[0]
	m.outputs = m.outputs[1:]
	return result, nil
}

func (m *fakeRunContainerManager) PullImage(ctx context.Context, image string) error {
	return nil
}

func (m *fakeRunContainerManager) ImageExists(ctx context.Context, image string) (bool, error) {
	return true, nil
}

func (m *fakeRunContainerManager) Close() error {
	return nil
}

type fakeDockerHealth struct {
	err error
}

func (h fakeDockerHealth) HealthCheck(ctx context.Context) error {
	return h.err
}

func TestRouterListsStepTypes(t *testing.T) {
	router := NewRouter(NewHandler(nil))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/step-types", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var body []map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	found := false
	for _, def := range body {
		if def["id"] == "script.run" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected script.run in step type list, got %#v", body)
	}
}

func TestHealthCheckIncludesDockerStatus(t *testing.T) {
	handler := NewHandler(nil)
	handler.SetDockerHealthChecker(fakeDockerHealth{})
	router := NewRouter(handler)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	docker, ok := body["docker"].(map[string]any)
	if !ok {
		t.Fatalf("expected docker object, got %#v", body)
	}
	if docker["reachable"] != true {
		t.Fatalf("expected docker reachable true, got %#v", docker)
	}
}

func TestHealthCheckReportsDockerFailure(t *testing.T) {
	handler := NewHandler(nil)
	handler.SetDockerHealthChecker(fakeDockerHealth{err: errors.New("docker unavailable")})
	router := NewRouter(handler)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status 503, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "docker unavailable") {
		t.Fatalf("expected docker error in response, got %q", rec.Body.String())
	}
}

func TestRouterValidatesTypedPipeline(t *testing.T) {
	router := NewRouter(NewHandler(nil))
	payload := []byte(`{
		"spec": {
			"version": "1.1",
			"name": "typed-api",
			"steps": [
				{
					"name": "test",
					"type": "script.run",
					"image": "golang:1.25",
					"with": { "script": "go test ./..." }
				}
			]
		}
	}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pipelines/validate", bytes.NewReader(payload))
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["valid"] != true {
		t.Fatalf("expected valid true, got %#v", body)
	}
}

func TestRouterStartsAndReadsAsyncRun(t *testing.T) {
	handler := NewHandler(nil)
	handler.SetRunService(runservice.NewService(nil, &fakeRunContainerManager{
		outputs: []container.Result{
			{Output: "", ExitCode: 0},
			{Output: "api log line", ExitCode: 0},
		},
	}, container.NewWorkspaceManager(t.TempDir())))
	router := NewRouter(handler)

	runID := startTestRun(t, router)
	waitForRunStatus(t, router, runID, string(runservice.StatusSuccess))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/runs/"+runID+"/steps", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var steps []runservice.StepRun
	if err := json.NewDecoder(rec.Body).Decode(&steps); err != nil {
		t.Fatalf("decode steps: %v", err)
	}
	if len(steps) != 1 || steps[0].Status != runservice.StatusSuccess {
		t.Fatalf("expected one successful step, got %#v", steps)
	}
}

func TestRouterStreamsRunEventsAsSSE(t *testing.T) {
	handler := NewHandler(nil)
	handler.SetRunService(runservice.NewService(nil, &fakeRunContainerManager{
		outputs: []container.Result{
			{Output: "", ExitCode: 0},
			{Output: "streamed log", ExitCode: 0},
		},
	}, container.NewWorkspaceManager(t.TempDir())))
	router := NewRouter(handler)

	runID := startTestRun(t, router)
	waitForRunStatus(t, router, runID, string(runservice.StatusSuccess))

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/runs/"+runID+"/events", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("expected text/event-stream, got %q", got)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "event: run_done") {
		t.Fatalf("expected run_done SSE event, got %q", body)
	}
	if !strings.Contains(body, "streamed log") {
		t.Fatalf("expected log payload in SSE stream, got %q", body)
	}
}

func TestRouterStartRunDetachesFromRequestContext(t *testing.T) {
	handler := NewHandler(nil)
	handler.SetRunService(runservice.NewService(nil, &fakeRunContainerManager{
		respectCancel: true,
		outputs: []container.Result{
			{Output: "", ExitCode: 0},
			{Output: "detached log", ExitCode: 0},
		},
	}, container.NewWorkspaceManager(t.TempDir())))
	router := NewRouter(handler)

	payload, err := json.Marshal(RunPipelineRequest{Spec: testPipelineSpec()})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/api/v1/runs", bytes.NewReader(payload))
	cancel()
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected status 202, got %d: %s", rec.Code, rec.Body.String())
	}

	var body runservice.Run
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode start response: %v", err)
	}
	waitForRunStatus(t, router, body.ID, string(runservice.StatusSuccess))
}

func TestRouterCancelsRun(t *testing.T) {
	handler := NewHandler(nil)
	handler.SetRunService(runservice.NewService(nil, &fakeRunContainerManager{
		outputs: []container.Result{
			{Output: "", ExitCode: 0},
		},
		blockAfter: 2,
	}, container.NewWorkspaceManager(t.TempDir())))
	router := NewRouter(handler)

	runID := startTestRun(t, router)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/runs/"+runID+"/cancel", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	waitForRunStatus(t, router, runID, string(runservice.StatusCancelled))
}

func startTestRun(t *testing.T, router http.Handler) string {
	t.Helper()

	payload, err := json.Marshal(RunPipelineRequest{Spec: testPipelineSpec()})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/runs", bytes.NewReader(payload))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected status 202, got %d: %s", rec.Code, rec.Body.String())
	}

	var body runservice.Run
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode start response: %v", err)
	}
	if body.ID == "" {
		t.Fatalf("expected run id, got %#v", body)
	}
	return body.ID
}

func testPipelineSpec() pipelinespec.PipelineSpec {
	return pipelinespec.PipelineSpec{
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
}

func waitForRunStatus(t *testing.T, router http.Handler, runID, want string) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/runs/"+runID, nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var body runservice.Run
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatalf("decode run response: %v", err)
		}
		if string(body.Status) == want {
			return
		}

		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for run %s status %s", runID, want)
}
