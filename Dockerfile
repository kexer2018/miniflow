# ─── 构建阶段 ─────────────────────────────────────────────
FROM golang:1.23-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /src

# 依赖缓存层（利用 Docker layer caching）
COPY go.mod go.sum ./
RUN go mod download

# 源码复制与构建
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /build/miniflow ./cmd/miniflow && \
    CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /build/miniflow-worker ./cmd/worker

# ─── 运行阶段 ─────────────────────────────────────────────
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata

# 创建运行时目录
RUN mkdir -p /tmp/miniflow/workspaces /tmp/miniflow/data

# 复制二进制
COPY --from=builder /build/miniflow /usr/local/bin/miniflow
COPY --from=builder /build/miniflow-worker /usr/local/bin/miniflow-worker

# 默认配置
ENV LLM_MODEL=gpt-4o-mini
ENV LLM_BASE_URL=https://api.openai.com/v1

# 健康检查
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD miniflow version || exit 1

EXPOSE 9090

# 默认入口：worker 守护进程
# 可通过 docker run --entrypoint miniflow 切换为 CLI 模式
ENTRYPOINT ["miniflow-worker"]
