// Package api 提供 REST API 接口，为后期 Web UI 预留。
package api

import (
	"crypto/subtle"
	"log/slog"
	"net/http"
	"time"
)

const defaultMaxRequestBodyBytes int64 = 1 << 20

// RouterOptions configures the deployment boundary around the local API.
type RouterOptions struct {
	BearerToken         string
	MaxRequestBodyBytes int64
}

// ─── 路由 ─────────────────────────────────────────────────

// Router 封装 HTTP 路由。
type Router struct {
	mux *http.ServeMux
	// TODO: Phase 2 替换为 chi/gin 等更强大的路由库
}

// NewRouter 创建 API 路由器。
func NewRouter(handler *Handler) *Router {
	return NewRouterWithOptions(handler, RouterOptions{})
}

// NewRouterWithOptions builds a router with optional bearer authentication and
// a bounded request body. The zero value preserves existing local callers.
func NewRouterWithOptions(handler *Handler, options RouterOptions) *Router {
	r := &Router{mux: http.NewServeMux()}

	// ─── 健康检查 ──────────────────────────────────
	r.mux.HandleFunc("GET /healthz", handler.HealthCheck)

	// ─── Step 类型发现 / 校验 ───────────────────────
	r.mux.HandleFunc("GET /api/v1/step-types", handler.ListStepTypes)
	r.mux.HandleFunc("GET /api/v1/pipeline-definitions", handler.ListPipelineDefinitions)
	r.mux.HandleFunc("POST /api/v1/pipelines/validate", handler.ValidatePipeline)

	// ─── 流水线执行 ────────────────────────────────
	r.mux.HandleFunc("POST /api/v1/runs", handler.StartRun)
	r.mux.HandleFunc("GET /api/v1/runs", handler.ListRuns)
	r.mux.HandleFunc("GET /api/v1/runs/{id}", handler.GetRun)
	r.mux.HandleFunc("GET /api/v1/runs/{id}/steps", handler.ListRunSteps)
	r.mux.HandleFunc("GET /api/v1/runs/{id}/artifacts", handler.ListArtifacts)
	r.mux.HandleFunc("GET /api/v1/runs/{id}/artifacts/{name}/download", handler.DownloadArtifact)
	r.mux.HandleFunc("GET /api/v1/runs/{id}/events", handler.StreamRunEvents)
	r.mux.HandleFunc("POST /api/v1/runs/{id}/cancel", handler.CancelRun)
	r.mux.HandleFunc("POST /api/v1/pipelines", handler.RunPipeline)
	r.mux.HandleFunc("GET /api/v1/pipelines/{id}", handler.GetPipelineResult)
	r.mux.HandleFunc("GET /api/v1/pipelines", handler.ListPipelineResults)

	// ─── 修复建议 ─────────────────────────────────
	r.mux.HandleFunc("POST /api/v1/fix/suggest", handler.SuggestFix)

	// ─── AI 智能诊断 ───────────────────────────────
	r.mux.HandleFunc("POST /api/v1/diagnose", handler.Diagnose)

	return r.withSecurity(options)
}

// ServeHTTP 实现 http.Handler 接口。
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	startedAt := time.Now()
	recorder := &responseRecorder{ResponseWriter: w}
	r.mux.ServeHTTP(recorder, req)

	slog.Info("api request completed",
		"method", req.Method,
		"path", req.URL.Path,
		"status", recorder.statusCode(),
		"duration_ms", time.Since(startedAt).Milliseconds(),
		"remote_addr", req.RemoteAddr,
	)
}

func (r *Router) withSecurity(options RouterOptions) *Router {
	maxBytes := options.MaxRequestBodyBytes
	if maxBytes <= 0 {
		maxBytes = defaultMaxRequestBodyBytes
	}
	inner := r.mux
	r.mux = http.NewServeMux()
	r.mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if options.BearerToken != "" {
			const prefix = "Bearer "
			header := req.Header.Get("Authorization")
			if len(header) < len(prefix) || header[:len(prefix)] != prefix || subtle.ConstantTimeCompare([]byte(header[len(prefix):]), []byte(options.BearerToken)) != 1 {
				w.Header().Set("WWW-Authenticate", "Bearer")
				writeError(w, http.StatusUnauthorized, "authentication required")
				return
			}
		}
		if req.Body != nil {
			req.Body = http.MaxBytesReader(w, req.Body, maxBytes)
		}
		inner.ServeHTTP(w, req)
	}))
	return r
}

// responseRecorder captures the status code while preserving SSE flushing.
type responseRecorder struct {
	http.ResponseWriter
	status int
}

func (w *responseRecorder) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *responseRecorder) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(data)
}

func (w *responseRecorder) Flush() {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *responseRecorder) statusCode() int {
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}
