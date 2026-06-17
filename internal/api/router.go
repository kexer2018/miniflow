// Package api 提供 REST API 接口，为后期 Web UI 预留。
package api

import (
	"net/http"
)

// ─── 路由 ─────────────────────────────────────────────────

// Router 封装 HTTP 路由。
type Router struct {
	mux *http.ServeMux
	// TODO: Phase 2 替换为 chi/gin 等更强大的路由库
}

// NewRouter 创建 API 路由器。
func NewRouter(handler *Handler) *Router {
	r := &Router{mux: http.NewServeMux()}

	// ─── 健康检查 ──────────────────────────────────
	r.mux.HandleFunc("GET /healthz", handler.HealthCheck)

	// ─── 流水线执行 ────────────────────────────────
	r.mux.HandleFunc("POST /api/v1/pipelines", handler.RunPipeline)
	r.mux.HandleFunc("GET /api/v1/pipelines/{id}", handler.GetPipelineResult)
	r.mux.HandleFunc("GET /api/v1/pipelines", handler.ListPipelineResults)

	// ─── 修复建议 ─────────────────────────────────
	r.mux.HandleFunc("POST /api/v1/fix/suggest", handler.SuggestFix)

	return r
}

// ServeHTTP 实现 http.Handler 接口。
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	// TODO: Phase 2 添加 CORS、鉴权、请求日志中间件
	r.mux.ServeHTTP(w, req)
}
