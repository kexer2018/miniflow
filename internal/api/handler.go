// Package api 提供 REST API 接口，为后期 Web UI 预留。
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/kexer2018/miniflow/internal/db"
	"github.com/kexer2018/miniflow/internal/fixer"
	"github.com/kexer2018/miniflow/internal/log"
	runservice "github.com/kexer2018/miniflow/internal/run"
	"github.com/kexer2018/miniflow/internal/stepregistry"
	pipelinespec "github.com/kexer2018/miniflow/pkg/pipeline"
)

// ─── Handler ──────────────────────────────────────────────

// Handler 封装 API 请求处理函数。
type Handler struct {
	store       db.Store
	runSvc      *runservice.Service
	classifier  *log.Classifier
	sanitizer   *log.Sanitizer
	diagnoseCfg *fixer.DiagnoseConfig // 可选的诊断配置
	docker      healthChecker
}

type healthChecker interface {
	HealthCheck(ctx context.Context) error
}

// NewHandler 创建 API 处理器。
func NewHandler(store db.Store) *Handler {
	return &Handler{
		store:      store,
		classifier: log.NewClassifier(),
		sanitizer:  log.NewSanitizer(),
	}
}

// SetDiagnoseConfig 设置诊断引擎配置（启用 AI 诊断能力）。
func (h *Handler) SetDiagnoseConfig(cfg *fixer.DiagnoseConfig) {
	h.diagnoseCfg = cfg
}

// SetRunService 设置异步流水线执行服务。
func (h *Handler) SetRunService(svc *runservice.Service) {
	h.runSvc = svc
}

// SetDockerHealthChecker 设置 /healthz 使用的 Docker 可达性检查器。
func (h *Handler) SetDockerHealthChecker(checker healthChecker) {
	h.docker = checker
}

// ─── 健康检查 ─────────────────────────────────────────────

func (h *Handler) HealthCheck(w http.ResponseWriter, r *http.Request) {
	dockerStatus := map[string]any{
		"configured": h.docker != nil,
		"reachable":  false,
	}
	status := "ok"
	httpStatus := http.StatusOK

	if h.docker != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := h.docker.HealthCheck(ctx); err != nil {
			status = "degraded"
			httpStatus = http.StatusServiceUnavailable
			dockerStatus["error"] = err.Error()
		} else {
			dockerStatus["reachable"] = true
		}
	}

	writeJSON(w, httpStatus, map[string]any{
		"status":  status,
		"version": "0.1.0-alpha",
		"docker":  dockerStatus,
	})
}

// ─── Step Type API ────────────────────────────────────────

func (h *Handler) ListStepTypes(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, stepregistry.Builtins())
}

// ─── 流水线 API ───────────────────────────────────────────

// RunPipelineRequest 是运行流水线的请求体。
type RunPipelineRequest struct {
	Spec pipelinespec.PipelineSpec `json:"spec"`
}

func (h *Handler) ValidatePipeline(w http.ResponseWriter, r *http.Request) {
	var req RunPipelineRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if err := req.Spec.Validate(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"valid": false,
			"error": err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"valid":   true,
		"name":    req.Spec.Name,
		"version": req.Spec.Version,
		"steps":   len(req.Spec.Steps),
	})
}

func (h *Handler) RunPipeline(w http.ResponseWriter, r *http.Request) {
	h.StartRun(w, r)
}

func (h *Handler) StartRun(w http.ResponseWriter, r *http.Request) {
	if h.runSvc == nil {
		writeError(w, http.StatusServiceUnavailable, "run service not configured")
		return
	}

	var req RunPipelineRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if err := req.Spec.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid pipeline spec: "+err.Error())
		return
	}

	run, err := h.runSvc.Start(r.Context(), req.Spec)
	if err != nil {
		writeError(w, http.StatusBadRequest, "start run: "+err.Error())
		return
	}

	writeJSON(w, http.StatusAccepted, run)
}

func (h *Handler) GetRun(w http.ResponseWriter, r *http.Request) {
	if h.runSvc == nil {
		writeError(w, http.StatusServiceUnavailable, "run service not configured")
		return
	}

	run, ok := h.runSvc.Get(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "run not found")
		return
	}

	writeJSON(w, http.StatusOK, run)
}

func (h *Handler) ListRunSteps(w http.ResponseWriter, r *http.Request) {
	if h.runSvc == nil {
		writeError(w, http.StatusServiceUnavailable, "run service not configured")
		return
	}

	steps, ok := h.runSvc.Steps(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "run not found")
		return
	}

	writeJSON(w, http.StatusOK, steps)
}

func (h *Handler) CancelRun(w http.ResponseWriter, r *http.Request) {
	if h.runSvc == nil {
		writeError(w, http.StatusServiceUnavailable, "run service not configured")
		return
	}

	run, ok := h.runSvc.Cancel(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "run not found")
		return
	}

	writeJSON(w, http.StatusOK, run)
}

func (h *Handler) StreamRunEvents(w http.ResponseWriter, r *http.Request) {
	if h.runSvc == nil {
		writeError(w, http.StatusServiceUnavailable, "run service not configured")
		return
	}

	events, unsubscribe, ok := h.runSvc.Subscribe(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "run not found")
		return
	}
	defer unsubscribe()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	flusher, _ := w.(http.Flusher)
	if flusher != nil {
		flusher.Flush()
	}

	for {
		select {
		case event := <-events:
			if err := writeSSEEvent(w, event); err != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		case <-r.Context().Done():
			return
		}
	}
}

func (h *Handler) GetPipelineResult(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "pipeline ID is required")
		return
	}

	result, err := h.store.GetPipelineResult(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) ListPipelineResults(w http.ResponseWriter, r *http.Request) {
	results, err := h.store.ListPipelineResults(r.Context(), 20, 0)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, results)
}

// ─── 修复建议 API ─────────────────────────────────────────

// SuggestFixRequest 是获取修复建议的请求体。
type SuggestFixRequest struct {
	StepName string `json:"step_name"`
	LogText  string `json:"log_text"`
}

func (h *Handler) SuggestFix(w http.ResponseWriter, r *http.Request) {
	var req SuggestFixRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	// 1. 脱敏
	sanitized := h.sanitizer.Sanitize(req.LogText)

	// 2. 分类
	classification := h.classifier.Classify(sanitized)

	// 3. 根据分类结果，返回基础分析
	response := map[string]any{
		"step_name":      req.StepName,
		"classification": classification,
	}

	writeJSON(w, http.StatusOK, response)
}

// ─── AI 诊断 API ──────────────────────────────────────────

// DiagnoseRequest 是 AI 诊断的请求体。
type DiagnoseRequest struct {
	StepName string `json:"step_name"`
	LogText  string `json:"log_text"`
}

// Diagnose 执行 AI 诊断并返回结果。
// 需要先通过 SetDiagnoseConfig 注册诊断引擎。
func (h *Handler) Diagnose(w http.ResponseWriter, r *http.Request) {
	if h.diagnoseCfg == nil {
		writeError(w, http.StatusServiceUnavailable, "AI diagnosis not configured (set LLM_API_KEY)")
		return
	}

	var req DiagnoseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if req.LogText == "" {
		writeError(w, http.StatusBadRequest, "log_text is required")
		return
	}

	if req.StepName == "" {
		req.StepName = "unknown"
	}

	result := fixer.Diagnose(r.Context(), *h.diagnoseCfg, req.StepName, req.LogText)
	writeJSON(w, http.StatusOK, result)
}

// ─── 辅助函数 ─────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeSSEEvent(w http.ResponseWriter, event runservice.Event) error {
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if _, err := w.Write([]byte("event: " + string(event.Type) + "\n")); err != nil {
		return err
	}
	if _, err := w.Write([]byte("data: " + string(data) + "\n\n")); err != nil {
		return err
	}
	return nil
}
