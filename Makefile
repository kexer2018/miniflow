.PHONY: all build build-miniflow build-worker clean test lint run help

# ─── 变量 ───────────────────────────────────────────────
BIN_DIR        := bin
CMD_MINIFLOW   := ./cmd/miniflow
CMD_WORKER     := ./cmd/worker
BIN_MINIFLOW   := $(BIN_DIR)/miniflow
BIN_WORKER     := $(BIN_DIR)/miniflow-worker

GO             ?= go
GOFLAGS        ?= -ldflags="-s -w"
CGO_ENABLED    ?= 0

# ─── 默认目标 ────────────────────────────────────────────
all: build

# ─── 构建 ────────────────────────────────────────────────
build: build-miniflow build-worker

build-miniflow:
	@echo "🔨 Building miniflow CLI..."
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=$(CGO_ENABLED) $(GO) build $(GOFLAGS) -o $(BIN_MINIFLOW) $(CMD_MINIFLOW)
	@echo "✅  $(BIN_MINIFLOW)"

build-worker:
	@echo "🔨 Building miniflow worker..."
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=$(CGO_ENABLED) $(GO) build $(GOFLAGS) -o $(BIN_WORKER) $(CMD_WORKER)
	@echo "✅  $(BIN_WORKER)"


# ─── 运行 / 调试 ─────────────────────────────────────────
run: build-miniflow
	@echo "▶️  Running miniflow..."
	@$(BIN_MINIFLOW)

# ─── 测试 ────────────────────────────────────────────────
test:
	@echo "🧪 Running tests..."
	@$(GO) test ./... -v -count=1 -race -timeout=120s

test-short:
	@echo "🧪 Running short tests..."
	@$(GO) test ./... -short -count=1 -race -timeout=60s

test-coverage:
	@echo "🧪 Running tests with coverage..."
	@$(GO) test ./... -count=1 -race -coverprofile=coverage.out -timeout=120s
	@$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "📊  coverage.html"

test-integration:
	@echo "🧪 Running integration tests (requires Docker)..."
	@$(GO) test ./internal/container/... -tags=integration -count=1 -v -timeout=300s

# ─── 代码质量 ────────────────────────────────────────────
lint:
	@echo "🔍 Running linters..."
	@golangci-lint run ./... --timeout=5m || true

vet:
	@echo "🔍 Running go vet..."
	@$(GO) vet ./...

fmt:
	@echo "📝 Formatting code..."
	@$(GO) fmt ./...

# ─── 清理 ────────────────────────────────────────────────
clean:
	@echo "🧹 Cleaning..."
	@rm -rf $(BIN_DIR) coverage.out coverage.html
	@echo "✅  Clean"

# ─── 依赖管理 ────────────────────────────────────────────
tidy:
	@echo "📦 Tidying dependencies..."
	@$(GO) mod tidy

deps:
	@echo "📦 Downloading dependencies..."
	@$(GO) mod download

vendor:
	@echo "📦 Vendoring dependencies..."
	@$(GO) mod vendor

# ─── 帮助 ────────────────────────────────────────────────
help:
	@echo "Usage: make <target>"
	@echo ""
	@echo "Build targets:"
	@echo "  all              Build all binaries (default)"
	@echo "  build             Build all binaries"
	@echo "  build-miniflow    Build miniflow CLI"
	@echo "  build-worker      Build miniflow worker"
	@echo ""
	@echo "Run / Debug:"
	@echo "  run               Build and run miniflow CLI"
	@echo ""
	@echo "Test:"
	@echo "  test              Run all tests"
	@echo "  test-short        Run short tests only"
	@echo "  test-coverage     Run tests with coverage report"
	@echo "  test-integration  Run integration tests (requires Docker)"
	@echo ""
	@echo "Code quality:"
	@echo "  lint              Run golangci-lint"
	@echo "  vet               Run go vet"
	@echo "  fmt               Format code"
	@echo ""
	@echo "Cleanup:"
	@echo "  clean             Remove build artifacts"
	@echo ""
	@echo "Dependencies:"
	@echo "  tidy              Tidy go.mod/go.sum"
	@echo "  deps              Download dependencies"
	@echo "  vendor            Vendor dependencies"
