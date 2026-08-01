# AGENTS.md

This file provides guidance to Codex (Codex.ai/code) when working with code in this repository.

## Build & Test Commands

```bash
# Build everything
make build

# Build CLI only
make build-miniflow
# or: go build -o bin/miniflow ./cmd/miniflow

# Run a pipeline from a JSON spec
./bin/miniflow -f examples/go-ci.json

# Validate a pipeline JSON without running
./bin/miniflow validate -f examples/go-ci.json

# Run with verbose debug logging
./bin/miniflow -v -f examples/go-ci.json

# Tests
go test ./... -v -count=1 -race -timeout=120s
go test ./internal/pipeline/...             # unit tests for DAG logic
go test ./internal/log/...                  # unit tests for classifier/sanitizer
go test -tags=integration ./internal/container/...  # integration tests (requires Docker)

# Code quality
make vet
make fmt
make lint          # requires golangci-lint

# Dependencies
go mod tidy
go mod download
```

## Project Overview

`miniflow` is an AI-native lightweight CI/CD execution engine written in Go. It parses DAG pipeline definitions from JSON, schedules ephemeral Docker containers to execute steps serially (Phase 1), collects logs, and provides a foundation for AI-powered error analysis and auto-fix (Phase 1B+).

**Go 1.23+ required.** No CGO needed (pure Go SQLite via `modernc.org/sqlite`).

## Architecture

### Two-Entry-Point Design

- **`cmd/miniflow/main.go`** — CLI tool that reads a pipeline JSON file, validates the DAG, and executes steps via Docker
- **`cmd/worker/main.go`** — Worker daemon skeleton (Phase 2: receives tasks from control plane)

### Package Layout

```
cmd/
├── miniflow/        # CLI entry point (cobra)
└── worker/          # Worker daemon entry point (Phase 2)

internal/
├── pipeline/        # DAG model, validation (Kahn cycle detection), execution engine
├── container/       # Docker SDK wrapper, workspace management, UID unification
├── log/             # Log collector, regex sanitizer (8 rules), deterministic classifier
├── llm/             # LLM abstraction layer (Phase 1B)
├── fixer/           # AI fix engine + RAG (Phase 1B)
├── api/             # REST API skeleton (Phase 2 Web UI)
└── db/              # Store interface + SQLite implementation

pkg/pipeline/        # Public PipelineSpec types (shared by CLI and API)

seeds/               # RAG seed cases for error classification (YAML)
examples/            # Sample pipeline JSON files
```

### Execution Flow (Phase 1)

1. CLI reads JSON → `PipelineSpec` validation
2. Docker client auto-detects socket (OrbStack, Docker Desktop, or default)
3. Workspace created at `/tmp/miniflow/workspaces/{pipeline-id}`
4. DAG topologically sorted (Kahn algorithm)
5. Steps executed serially in ephemeral containers:
   - Each step gets a fresh container (`--user 1000:1000`, workspace bind-mounted)
   - Commands wrapped in `/bin/sh -c` for multi-line shell support
   - Container logs collected, then container cleaned up
6. Results printed to terminal (step status, exit code, output on failure)

### Key Design Decisions

- **Phase 1: Serial-only execution** — one step at a time, no parallelism
- **UID unification** — all containers run as `1000:1000`; workspace `chown`'d before use
- **Log sanitizer** — conservative regex-based redaction (JWT, AWS keys, SSH keys, etc.)
- **Log classifier** — deterministic keyword matching, not LLM-based, separates `app_error` from `infra_error`
- **Container lifecycle** — `AutoRemove: false`, manual cleanup after log collection to ensure logs are readable
- **OrbStack support** — auto-detects `~/.orbstack/run/docker.sock` if default socket is missing

### Pipeline JSON Format

```json
{
  "version": "1.0",
  "name": "my-pipeline",
  "workspace": "/workspace",
  "steps": [
    {
      "name": "step-1",
      "image": "alpine:latest",
      "commands": ["echo hello"],
      "depends_on": [],
      "cache": { "path": "/cache/path", "key": "cache-key" }
    }
  ]
}
```
