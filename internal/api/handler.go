// Package api 提供 REST API 接口，为后期 Web UI 预留。
package api

import (
	"encoding/json"
	"net/http"

	"github.com/kexer2018/miniflow/internal/db"
	"github.com/kexer2018/miniflow/internal/fixer"
	"github.com/kexer2018/miniflow/internal/log"
	"github.com/kexer2018/miniflow/internal/stepregistry"
	pipelinespec "github.com/kexer2018/miniflow/pkg/pipeline"
)

// ─── Handler ──────────────────────────────────────────────

// Handler 封装 API 请求处理函数。
type Handler struct {
	store       db.Store
	classifier  *log.Classifier
	sanitizer   *log.Sanitizer
	diagnoseCfg *fixer.DiagnoseConfig // 可选的诊断配置
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

// ─── 健康检查 ─────────────────────────────────────────────

func (h *Handler) HealthCheck(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"version": "0.1.0-alpha",
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
	var req RunPipelineRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if err := req.Spec.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid pipeline spec: "+err.Error())
		return
	}

	// TODO: Phase 2 - 异步执行流水线并返回 202 Accepted
	// 当前仅为骨架，返回成功
	writeJSON(w, http.StatusAccepted, map[string]string{
		"status":  "accepted",
		"message": "pipeline execution not yet implemented in API mode (use CLI)",
	})
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
