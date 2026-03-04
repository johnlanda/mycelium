# Mycelium

## Project Overview

Mycelium (`mctl`) is a Go CLI tool and MCP server that gives AI coding agents reproducible, version-pinned dependency context. It reads a project manifest (`mycelium.toml`), resolves dependencies against a lockfile (`mycelium.lock`), and maintains a local vector store of indexed documentation and source code. An MCP server exposes semantic search over that store.

## Repository

- **GitHub:** `github.com/johnlanda/mycelium`
- **Go module:** `github.com/johnlanda/mycelium`
- **License:** TBD
- **CLI binary:** `mctl`

## Project Structure

```
mycelium/
├── cmd/                    # CLI command implementations
│   ├── init.go
│   ├── add.go
│   ├── up.go
│   ├── upgrade.go
│   ├── publish.go
│   ├── status.go
│   └── serve.go            # MCP server entry point
├── internal/
│   ├── manifest/            # mycelium.toml parsing and validation
│   ├── lockfile/            # mycelium.lock read/write
│   ├── store/               # Vector store abstraction (LanceDB embedded)
│   ├── fetchers/            # Source fetchers (github, artifact)
│   ├── chunker/             # Chunking strategies (markdown, code/tree-sitter)
│   ├── embedder/            # Embedding provider abstraction
│   └── hasher/              # content_hash and store_key computation
├── mycelium.toml            # Project manifest (committed)
├── mycelium.lock            # Lockfile (committed, never hand-edited)
└── go.mod
```

## Architecture

CLI commands orchestrate a pipeline: fetchers clone/download sources, chunkers split content into semantic units (heading-aware for Markdown, AST-aware via tree-sitter for code), embedders produce vectors, and the store persists them keyed by content-addressed `store_key`. The MCP server exposes `search`, `search_code`, and `list_sources` tools over the local store via stdio.

## Key Documents

- **PRD:** [`mycelium-prd.md`](mycelium-prd.md) — source of truth for all requirements
- **Project Board:** https://github.com/users/johnlanda/projects/3/views/1

## Task Tracking

**Every implementation task MUST have a GitHub issue on the project board.** Use `/kanban` to manage the board:

1. `/kanban` — check the board before starting work
2. `/kanban create "Title" P1 M` — create an issue for new work
3. `/kanban move #N in progress` — claim the issue
4. Work on the task
5. `/kanban move #N done` — close when finished

Do not start implementation work without a tracked issue.

## Development Conventions

- **Language:** Go (latest stable)
- **Build target:** Single binary (`mctl`)
- **Module path:** `github.com/johnlanda/mycelium`
- **Test command:** `go test ./...`
- **Formatting:** `gofmt` (standard)
- **Linting:** `go vet ./...`
- **Error handling:** Return errors, don't panic. Wrap with `fmt.Errorf("context: %w", err)`.
- **Naming:** Follow Go conventions — exported names are PascalCase, unexported are camelCase.
- **Packages:** Keep packages focused. `internal/` for non-exported packages.
- **Tests:** Table-driven tests preferred. Test files live alongside source (`foo_test.go`).

## Quick Reference

```bash
# One-time setup: download LanceDB native libraries
make setup-lancedb

# Build
make build

# Test
make test

# Vet
make vet

# Tidy modules
make tidy
```
