# miniflow — Progress & Roadmap

<!-- Auto-update: each significant change should update this file -->

> **Phase**: 1 (MVP — Serial CI/CD Execution Engine)
> **Version**: 0.1.0-alpha
> **Go**: 1.23+ (CGO-free)

---

## 1. Project Overview

miniflow is an **AI-native lightweight CI/CD execution engine** written in Go. It parses DAG pipeline definitions from JSON, schedules ephemeral Docker containers to execute steps serially, collects logs, and provides an AI-powered failure diagnosis engine with RAG-based error matching.

**Three-phase roadmap:**

| Phase | Focus | Status |
|-------|-------|--------|
| **1** | Serial Docker execution + CLI + diagnosis foundation | ✅ **Complete** |
| **1B** | AI diagnosis engine refinement (RAG, LLM structured output, fix suggestions) | ✅ **Complete** (with optimization headroom) |
| **2** | Parallel execution, worker daemon, REST API, Web UI, control plane | 🚧 **In design** |

---

## 2. Architecture

### Two-Entry-Point Design

```
cmd/
├── miniflow/          # CLI entry point (cobra) — Phase 1 complete
└── worker/            # Worker daemon entry point — Phase 1 skeleton
```

### Package Layout

```
internal/
├── pipeline/          # DAG model, validation (Kahn), serial executor
├── container/         # Docker SDK wrapper, socket auto-detect, workspace mgmt
├── log/               # Log collector, regex sanitizer (8 rules), deterministic classifier
├── llm/               # LLM abstraction layer + OpenAI-compatible client
├── fixer/             # AI diagnosis engine + RAG seed matching
├── api/               # REST API skeleton (Phase 2)
├── config/            # Config loading (JSON file, env vars, CLI flags)
└── db/                # Store interface + SQLite implementation (pure Go)

pkg/pipeline/          # Public PipelineSpec types (shared by CLI and API)

seeds/                 # RAG seed cases for error classification (YAML, not yet loaded)
examples/              # Sample pipeline JSON files
```

### Execution Flow (Phase 1)

```
JSON → PipelineSpec → Validate(DAG) → TopologicalSort(Kahn)
    → CreateWorkspace → chown → [serial step loop]
        → ensureImage → CreateContainer → Start → Wait
        → CollectLogs → CleanupContainer
    → Sanitize(optional) → Classify(optional)
    → Diagnose(optional, AI) → Persist(SQLite)
```

---

## 3. Module-by-Module Status

### ✅ `pkg/pipeline/` — Public Spec Types

**Files**: `spec.go`, `errors.go`

- `PipelineSpec` with `Validate()` — step name, image, commands, DAG
- Sentine errors (`ErrEmptyName`, `ErrNoSteps`, etc.)

**Status**: Stable. Minimal API surface, no changes expected.

---

### ✅ `internal/pipeline/` — DAG Core & Execution

**Files**: `types.go`, `validate.go`, `execute.go`

Core types:
- `Pipeline`, `Step`, `Cache`, `Status` (6 states)
- `PipelineResult`, `StepResult`, `Classification`, `FixSuggestion`
- `ExecContext`

Validation:
- `ValidateDAG()` — unique names, valid `depends_on` references, **Kahn cycle detection**, entry node check
- `TopologicalSort()` — produces linear execution order

Executor:
- `Executor.ExecutePipeline()` — serial step execution with cancellation
- `executeStep()` — wraps Docker container config and dispatches to container manager

**Status**: Stable. Phase 2 will add parallel step groups.

**Known gaps**:
- `executeStep` has unused parameter `_ string` (pipelineID, kept for Phase 2 parallel groups)
- No timeout per step (only context cancellation)

---

### ✅ `internal/container/` — Docker Lifecycle

**Files**: `manager.go`, `docker.go`, `workspace.go`

Manager interface + DockerManager:
- Socket auto-detection: DOCKER_HOST → /var/run/docker.sock → OrbStack → Docker Desktop (rootless) → rootless Docker
- `RunContainer()` — ensure image → create → start → wait → collect logs → cleanup
- `PullImage()`, `ImageExists()`, `Close()`

WorkspaceManager:
- `CreateWorkspace()`, `RemoveWorkspace()`, `WorkspacePath()`
- `EnsureWorkspacePermissions()` — chown via ephemeral Alpine container (root)
- `EnsureCacheDir()`, `CachePath()`

**Key design decisions**:
- `AutoRemove: false` — manual cleanup after log collection ensures log readability
- All containers run as `--user 1000:1000` — UID unification
- Commands wrapped in `/bin/sh -c` for multi-line shell support

**Status**: Stable. Integration tests need Docker runtime.

---

### ✅ `internal/log/` — Log Sanitizer & Classifier

**Files**: `sanitizer.go`, `classifier.go`, `collector.go`

Sanitizer (8 rules, two modes):
| Pattern | Standard | Semantic |
|---------|----------|----------|
| JWT Bearer token | `***JWT***` | `***JWT_TOKEN_REDACTED***` |
| AWS Access Key | `***AWS_KEY***` | `***AWS_ACCESS_KEY_REDACTED***` |
| URL credentials | `***CREDENTIALS@` | `***CREDENTIALS_REDACTED@` |
| Private key header | `***PRIVATE_KEY***` | `***PRIVATE_KEY_REDACTED***` |
| GitHub token | `***GH_TOKEN***` | `***GITHUB_TOKEN_REDACTED***` |
| Docker auth JSON | `***DOCKER_AUTH***` | `***DOCKER_AUTH_REDACTED***` |
| NPM auth token | `***NPM_TOKEN***` | `***NPM_TOKEN_REDACTED***` |
| High-entropy (40+ chars) | `***HIGH_ENTROPY***` | `***HIGH_ENTROPY_STRING_REDACTED***` |

Classifier (deterministic):
- **app_error**: panic, JS/TS errors, Python traceback, NullPointer, Go fatal/panic
- **infra_error**: auth, image pull, network, permission, disk, cert, config not found
- **unknown**: fallback if exit code 1 with no signal match

Collector:
- Concurrent-safe log line buffer with streaming callback support
- `MultiReader` for merging multiple log streams

**Status**: Stable. Additional rules welcome (database connection strings, etc.)

---

### ✅ `internal/llm/` — LLM Abstraction Layer

**Files**: `client.go`, `openai.go`, `prompt.go`, `prompt_test.go`

- `LLMClient` interface: Chat (sync) + ChatStream (SSE)
- `OpenAIClient` — supports OpenAI + compatible APIs (DeepSeek, Qwen, etc.)
  - JSON Schema for structured output (`response_format: json_schema`)
  - Streaming (SSE with `[DONE]` termination)
  - Configurable model, base URL, API key
- Diagnosis prompt: system prompt + user prompt builder + JSON Schema
  - System: CI/CD failure diagnosis expert role, classification context, rules
  - Schema: `root_cause`, `fix_plan`, `confidence`, `category`, `suggested_fix`

**Status**: Stable. Good abstraction for multi-provider support.

---

### ✅ `internal/fixer/` — AI Diagnosis Engine

**Files**: `diagnose.go`, `rag.go`, `seeds.go`, `rag_test.go`

Diagnosis pipeline:
```
Sanitize → Classify → RAG Match → [AppError: skip LLM] → LLM Call → Parse JSON → Result
```

Key behaviors:
- **AppError fast-path**: skips LLM, returns RAG-matched or fallback message
- **Degradation**: LLM unavailable → RAG-only fallback (graceful)
- **LLM parse fallback**: JSON parse failure → raw text used as root cause
- Token usage tracking

Seed engine:
- 17 built-in seed cases across 7 categories (auth, image_pull, network, permission, resource, app_code, configuration)
- Simple keyword matching with scoring (`matched/total` ratio)
- `BuildContext()` for few-shot prompt context

**Status**: Complete. Seed YAML files exist but are not loaded at runtime.

---

### ✅ `internal/config/` — Configuration Loading

**Files**: `config.go` (with tests)

Priority: CLI flags > Env vars > Config file > Code defaults

Config search paths:
1. `--config <path>` (explicit flag)
2. `./.miniflow.json` (project root)
3. `~/.miniflow.json` (user home)

LLM config resolution:
- `LLM_API_KEY` → fallback `OPENAI_API_KEY`
- `LLM_BASE_URL` → default `https://api.openai.com/v1`
- `LLM_MODEL` → default `gpt-4o-mini`

**Status**: Stable. Good test coverage (8 tests).

---

### ✅ `internal/db/` — Persistence Layer

**Files**: `store.go`, `sqlite.go`

`Store` interface with SQLite implementation:
- `SavePipelineResult` / `GetPipelineResult` / `ListPipelineResults`
- `SaveExecContext` / `GetExecContext`
- `SaveDiagnosis` / `ListDiagnoses`
- `Ping` / `Close`

Tables:
- `pipeline_results` — execution history
- `exec_contexts` — interrupt recovery (keyed by pipeline_id)
- `diagnosis_history` — AI diagnosis records (with indices)

**Status**: Stable. No migrations needed for Phase 1.

---

### 🚧 `internal/api/` — REST API Skeleton

**Files**: `handler.go`, `router.go`

Routes:
| Method | Path | Handler |
|--------|------|---------|
| GET | `/healthz` | Health check |
| POST | `/api/v1/pipelines` | Accept pipeline for execution (stub) |
| GET | `/api/v1/pipelines/{id}` | Get pipeline result |
| GET | `/api/v1/pipelines` | List recent results |
| POST | `/api/v1/fix/suggest` | Log sanitize + classify |
| POST | `/api/v1/diagnose` | Full AI diagnosis |

**Status**: Skeleton. Phase 2 will add CORS, auth, async execution, middleware.

---

### ✅ `cmd/miniflow/` — CLI

**Files**: `main.go`

Commands:
| Command | Description | Flags |
|---------|-------------|-------|
| `miniflow` (default) | Execute a pipeline | `-f`, `-v`, `-d`, `-c` |
| `miniflow validate` | Validate pipeline JSON | `-f` |
| `miniflow diagnose` | AI diagnose a log | `--step`, `--log`, `--log-file` |
| `miniflow version` | Print version | — |

Auto-diagnose (`-d` flag): on pipeline failure, runs `fixer.Diagnose()` on each failed step.

**Status**: Stable. Good UX with ANSI symbols and structured output.

### 🚧 `cmd/worker/` — Worker Daemon

**Files**: `main.go`

- Skeleton: initializes Docker + workspace managers, waits for signal
- TODO Phase 2: gRPC/REST listener, image warm-up queue, task concurrency

**Status**: Skeleton only.

---

### ✅ Docker & Deployment

**Dockerfile**: Multi-stage build (golang:1.23-alpine → alpine:3.20), 2 binaries.

**docker-compose.yaml**: Single service (`miniflow-worker`), persistent volumes (workspaces, data), health check, environment variable configuration.

**Status**: Deployable as worker container. CLI mode available via `docker compose exec`.

---

## 4. Test Coverage

### Unit Tests

| Package | Files | Tests | Status |
|---------|-------|-------|--------|
| `internal/config` | `config_test.go` | 8 tests | ✅ Strong |
| `internal/log` | `sanitizer_test.go` | 8 tests | ✅ Sanitizer only |
| `internal/fixer` | `rag_test.go` | 5 tests | ✅ Seed engine only |
| `internal/llm` | `prompt_test.go` | 3 tests | ✅ Prompt builder only |

**Missing test coverage**:
- `internal/log/classifier.go` — no tests
- `internal/log/collector.go` — no tests
- `internal/pipeline/validate.go` — no tests (TopologicalSort, ValidateDAG)
- `internal/pipeline/execute.go` — no tests (Executor)
- `internal/container/docker.go` — no unit tests (integration-tagged only)
- `internal/container/workspace.go` — no tests
- `internal/container/manager.go` — no tests
- `internal/fixer/diagnose.go` — no tests (requires LLM mock)
- `internal/api/handler.go` — no tests
- `internal/api/router.go` — no tests
- `internal/db/sqlite.go` — no tests
- `internal/llm/openai.go` — no tests
- `internal/config/config.go` — `LoadDefault()` untested

### Integration Tests

- Tag-based: `go test -tags=integration ./internal/container/...`

---

## 5. Completed Features

- [x] Pipeline JSON parsing and validation
- [x] DAG topological sort (Kahn algorithm) with cycle detection
- [x] Serial Docker container execution with workspace bind-mounts
- [x] UID unification (1000:1000) with automatic chown
- [x] Docker socket auto-detection (OrbStack, Docker Desktop, rootless)
- [x] Cache mount support
- [x] Log sanitizer (8 regex rules, standard + semantic modes)
- [x] Deterministic log classifier (app_error / infra_error / unknown)
- [x] Log collector with streaming callback
- [x] AI diagnosis engine with structured output (JSON Schema)
- [x] RAG seed matching engine (17 built-in cases)
- [x] Graceful degradation: LLM unavailable → RAG-only fallback
- [x] AppError fast-path: skip LLM for application code errors
- [x] SQLite persistence (pipeline results, execution contexts, diagnosis history)
- [x] Config loading (CLI flags → env vars → config file → defaults)
- [x] CLI with 4 subcommands (run, validate, diagnose, version)
- [x] CLI auto-diagnose flag (`-d`) for failed steps
- [x] REST API skeleton (6 routes)
- [x] Multi-stage Docker build
- [x] Docker Compose deployment with persistent volumes
- [x] Signal handling (SIGINT/SIGTERM) for graceful cancellation

---

## 6. In Progress / Immediate Next Steps

### 🔄 RAG Enhancement (highest priority)

Seeds YAML files exist in `seeds/` but are never loaded at runtime.
- [ ] Load seed files at startup via `os.ReadFile` + `yaml.Unmarshal`
- [ ] Extend `SeedEngine.LoadFromYAML()` method
- [ ] Document YAML schema for user-contributed seeds

### 🔄 CI/CD

- [ ] GitHub Actions workflow (verify no regressions)
- [ ] Add Go lint + vet + test steps

### 🔄 Test Coverage

- [ ] Add unit tests for `internal/pipeline/validate.go` — `ValidateDAG`, `TopologicalSort`
- [ ] Add unit tests for `internal/log/classifier.go` — all signal rules
- [ ] Add unit tests for `internal/log/collector.go` — line collection, streaming callback
- [ ] Add unit tests for `internal/container/workspace.go` — workspace lifecycle
- [ ] Add unit tests for `internal/db/sqlite.go` — CRUD + migration
- [ ] Add unit tests for `internal/api/handler.go` / `router.go` — HTTP handlers
- [ ] Add mock for `llm.LLMClient` to test `fixer/diagnose.go`

---

## 7. Phase 2 Roadmap (Design Phase)

### Parallel Execution Engine

- [ ] Concurrent step groups (parallel stages within topologically-sorted levels)
- [ ] Worker pool with configurable concurrency
- [ ] Step timeout (configurable per step)
- [ ] Retry policies for transient failures

### Worker Daemon

- [ ] gRPC service for receiving tasks from control plane
- [ ] REST API completion (CORS, auth middleware, request logging)
- [ ] Image warm-up queue
- [ ] Task queue with concurrent limit
- [ ] Heartbeat / health reporting to control plane

### Web UI (WebSocket-based)

- [ ] Real-time pipeline execution dashboard
- [ ] Step log streaming per-step
- [ ] Diagnosis result visualization
- [ ] Pipeline configuration editor (JSON)

### Control Plane

- [ ] Multi-worker orchestration
- [ ] Centralized pipeline scheduling
- [ ] Execution history dashboard
- [ ] Credential management

### AI & RAG

- [ ] Dynamic RAG: feed successful diagnoses back into seed library (via `diagnosis_history`)
- [ ] Auto-fix execution: apply `config_override` from LLM suggestions to step config
- [ ] Multi-step diagnosis: analyze logs across multiple failed steps
- [ ] Confidence-based escalation: low-confidence → request human review
- [ ] Prompt caching support (system prompt reuse)

---

## 8. Known Technical Debt

| Area | Issue | Impact |
|------|-------|--------|
| `pipeline/execute.go:153` | Unused `_ string` parameter (pipelineID) | Minor — kept for Phase 2 parallel groups |
| `internal/fixer/seeds.go` | Hardcoded seed cases, YAML files not loaded | Seeds cannot be customized without recompilation |
| `internal/llm/openai.go` | No HTTP client timeout configured | Potential resource leak on hung connections |
| `internal/api/router.go` | Uses `http.ServeMux` (Go 1.22+) | Adequate for now, chi/gin planned for Phase 2 |
| `internal/container/docker.go` | `AutoRemove: false` with manual cleanup | Works but creates container lifecycle states |
| Tests | Only 24 tests across 4 test files | Low coverage in core execution paths |
| Documentation | No `example_test.go` files | Harder for new contributors to onboard |

---

## 9. Integration Points

| System | Method | Status |
|--------|--------|--------|
| Docker Engine | UNIX socket auto-detection | ✅ |
| OpenAI API | HTTP REST (chat/completions) | ✅ |
| OpenAI-compatible APIs | Configurable base URL | ✅ |
| SQLite | Pure Go via `modernc.org/sqlite` | ✅ |

---

*Last updated: 2026-06-19*
