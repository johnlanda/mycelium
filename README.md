# Mycelium

**Reproducible dependency context for AI coding agents.**

Your coding agent doesn't know your libraries. `mctl up` fixes that.

Mycelium pins your project's dependency knowledge — docs and code, public and private — the same way you pin your code dependencies. One command gives every developer and every CI run identical, accurate context via the [Model Context Protocol](https://modelcontextprotocol.io/).

## The Problem

AI coding agents are trained on documentation snapshots that are months or years old. Ask your agent to generate config for Envoy Gateway v1.4 and it confidently produces YAML referencing fields from v1.1. Ask it about your company's internal platform SDK and it has nothing — because that code will never appear in any model's training data.

Developers compensate by pasting docs into context windows, maintaining sprawling instruction files, or accepting that their agent is useless for anything touching internal dependencies. None of this is reproducible. When a second developer clones the same repo, the context is different — or absent entirely.

## How It Works

```
mycelium.toml    →    mctl up    →    Vector Store    →    MCP Server    →    Claude / Cursor / etc.
  (manifest)          (sync)         (local, fast)       (search, search_code, list_sources)
```

1. **Declare** dependencies in `mycelium.toml` — point at GitHub repos (public or private), specify which docs and code paths to index.
2. **Sync** with `mctl up` — fetches pre-built embedding artifacts when available, builds from source otherwise. Updates the lockfile.
3. **Query** — the MCP server exposes semantic search over all indexed content. Your coding agent calls `search` or `search_code` and gets accurate, version-pinned results.

The lockfile (`mycelium.lock`) guarantees that two developers with the same lockfile get functionally identical vector stores — the same way a package lockfile guarantees identical dependency trees.

## Quick Start

### Prerequisites

- Go 1.25+
- An embedding provider: Voyage AI API key, OpenAI API key, or a local [Ollama](https://ollama.com/) instance

### Install

Build from source:

```bash
git clone https://github.com/johnlanda/mycelium.git
cd mycelium
make setup-lancedb   # Download native LanceDB libraries (one-time)
make build           # Build the mctl binary
```

### Initialize a Project

```bash
mctl init
```

This creates `mycelium.toml` with sensible defaults.

### Add Dependencies

```bash
# OSS documentation + code
mctl add github.com/envoyproxy/gateway@v1.3.0 --docs site/content --code api/v1alpha1

# Internal library (GitHub Enterprise)
mctl add github.example.com/platform/sdk@v4.2.0 --docs docs/ --code pkg/client,pkg/types

# Code-only (the source is the documentation)
mctl add github.example.com/infra/compliance@v2.1.0 --code pkg/
```

### Sync

```bash
export VOYAGE_API_KEY=your-key    # or OPENAI_API_KEY, or use Ollama
export GITHUB_TOKEN=ghp_...       # for private repos
mctl up
```

This fetches each dependency, chunks its content (heading-aware for Markdown, AST-aware via tree-sitter for code), embeds it, and loads it into the local vector store. If a pre-built artifact exists for a dependency, it's downloaded directly — no embedding API calls needed.

### Connect Your Agent

Start the MCP server:

```bash
mctl serve
```

The server communicates over stdio. Configure your MCP client (Claude Code, Cursor, etc.) to connect to it. Example `.mcp.json`:

```json
{
  "mcpServers": {
    "mycelium": {
      "command": "mctl",
      "args": ["serve"]
    }
  }
}
```

Your agent now has access to three tools:

| Tool | Description |
|------|-------------|
| `search` | Semantic search across all indexed docs and code. Filter by source or chunk type. |
| `search_code` | Convenience tool for code-specific queries. Filter by language. |
| `list_sources` | List all indexed sources with version and chunk count. |

## Commands

| Command | Description |
|---------|-------------|
| `mctl init` | Initialize `mycelium.toml` in the current directory |
| `mctl add <source@ref>` | Add a dependency to the manifest |
| `mctl up` | Sync the local vector store with all declared dependencies |
| `mctl upgrade <id[@version]>` | Upgrade a dependency to a new version |
| `mctl status` | Show sync status of all dependencies |
| `mctl publish --tag <version>` | Publish pre-built embedding artifacts |
| `mctl serve` | Start the MCP server over stdio |

## Manifest

`mycelium.toml` is the project manifest. It declares which dependencies to index and how.

```toml
[config]
embedding_model = "voyage-code-2"   # Required: voyage-code-2, text-embedding-3-small, or ollama/<model>
embedding_dimensions = 0             # Optional: override vector dimensions (0 = auto-detect, useful for Ollama)
publish = "github-releases"          # Optional: where mctl publish uploads artifacts

[local]
index = ["./docs", "./README.md"]   # Local paths to index (included in published artifacts)
private = ["./notes"]                # Local-only paths (never published)

[[dependencies]]
id = "envoy-gateway"
source = "github.com/envoyproxy/gateway"
ref = "v1.3.0"
docs = ["site/content"]              # Markdown documentation paths
code = ["api/v1alpha1"]              # Source code paths
code_extensions = [".go"]            # File types to index (default: .go, .py, .ts, .tsx, .js, .jsx, .java, .rs)
```

## Lockfile

`mycelium.lock` is auto-generated and committed to the repo. It pins every dependency to exact content hashes and (when available) artifact checksums.

```toml
[meta]
mycelium_version = "0.1.0"
embedding_model = "ollama/qwen3-embedding"
embedding_model_version = ""
locked_at = "2026-03-05T15:43:51Z"
schema_version = 1

[sources.envoy-gateway]
version = "v1.3.0"
commit = "76e714e12b75cc20a0de5edd6e89fcfea231444d"
content_hash = "sha256:953a00be..."
store_key = "sha256:4a8b8979..."
ingestion_type = "built"
```

When a pre-built artifact is available, the lockfile also includes `artifact_url` and `artifact_hash` fields, and `ingestion_type` is `"artifact"` instead of `"built"`.

The `store_key` is a content-addressed hash of `(content_hash + embedding_model + chunking_config)`. Two projects that compute the same `store_key` reference identical data.

## Chunking

Mycelium uses two chunking strategies, selected automatically by file type:

**Markdown** (`.md`, `.mdx`) — Heading-aware chunking that preserves heading hierarchy as breadcrumb metadata. Chunk boundaries align with heading structure (h1, h2, h3), keeping each chunk a self-contained section.

**Source code** — AST-aware chunking via [tree-sitter](https://tree-sitter.github.io/tree-sitter/). Code is split along semantically meaningful boundaries — function definitions, type declarations, interface definitions, method implementations — rather than arbitrary token counts.

Supported languages: Go, Python, TypeScript/TSX, JavaScript/JSX, Java, Rust.

## Embedding Providers

| Provider | Model | Dimensions | Notes |
|----------|-------|------------|-------|
| Voyage AI | `voyage-code-2` | 1536 | Optimized for code and technical documentation. |
| OpenAI | `text-embedding-3-small` | 1536 | Widely available alternative. |
| Ollama | `ollama/<model>` | Auto-detected | Fully offline, no API key required. Set `embedding_dimensions` in manifest to override. |

## Publishing Artifacts

Library maintainers can publish pre-built embedding artifacts so downstream consumers skip the fetch-chunk-embed pipeline entirely.

```bash
# Publish to GitHub releases
mctl publish --tag v1.2.0

# Or write to a local directory
mctl publish --tag v1.2.0 --output ./artifacts/
```

This produces a gzipped JSONL file (`mycelium-{model-slug}.jsonl.gz`) and a companion `.sha256` checksum file. When a downstream project runs `mctl up`, it automatically detects and fetches the artifact instead of building from source.

The artifact format is an open standard — any CI pipeline can generate and publish them. See the [PRD](mycelium-prd.md) for the full specification.

## Artifact Resolution

`mctl up` resolves each dependency in this order:

1. **Store check** — If the `store_key` from the lockfile already exists in the local store, skip entirely.
2. **Artifact fetch** — If an artifact URL is available (from the lockfile or by probing the GitHub release), download it, verify the SHA-256 checksum, and ingest directly. No embedding API calls.
3. **Build from source** — Clone the repo at the pinned ref, chunk the content, call the embedding API, and load vectors.

This is transparent to the user. Pre-built artifacts are always preferred when available.

## Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `VOYAGE_API_KEY` | Voyage AI API key | Required if model is Voyage |
| `OPENAI_API_KEY` | OpenAI API key | Required if model is OpenAI |
| `OLLAMA_URL` | Ollama base URL | `http://localhost:11434` |
| `GITHUB_TOKEN` | Token for GitHub.com repos | Optional for public repos |
| `GHE_TOKEN` | Token for GitHub Enterprise | - |
| `GHE_URL` | GitHub Enterprise base URL | - |
| `MYCELIUM_STORE_DIR` | LanceDB store directory | `~/.mycelium/store` |

## Example Workflow

**Before Mycelium:** Claude generates Envoy Gateway config with fields from v1.1. You spend 20 minutes fixing it.

**After Mycelium:**

```bash
mctl up            # gives Claude the v1.4 docs and API types
mctl serve         # start the MCP server
# Claude generates correct config on the first try
```

**For internal libraries:**

```bash
# A new engineer joins the team
git clone git@github.example.com:payments/service.git
cd service
mctl up            # agent now understands platform-sdk, compliance-lib, and every other dependency
```

No Confluence spelunking required.

## Development

```bash
# One-time setup: download LanceDB native libraries
make setup-lancedb

# Build
make build

# Test
make test

# End-to-end tests (requires Ollama)
make test-e2e

# Vet
make vet

# Tidy modules
make tidy
```

## Architecture

```
mctl CLI
  mycelium.toml + mycelium.lock
    ├─ artifact → fetch → verify checksum → ingest (fast path)
    └─ github   → clone ─┬─ .md/.mdx → heading chunker ──┐
                          └─ code     → tree-sitter AST ───┤
                                                           │
                               embed ◄─────────────────────┘
                                 │
                   ┌─────────────▼──────────────┐
                   │  Embedding API              │
                   │  Voyage / OpenAI / Ollama   │
                   └─────────────┬──────────────┘
                                 │
                   ┌─────────────▼──────────────┐
                   │  Vector Store (LanceDB)     │
                   │  Partitioned by store_key   │
                   └─────────────┬──────────────┘
                                 │
                   ┌─────────────▼──────────────┐
                   │  MCP Server (stdio)         │
                   │  search · search_code       │
                   │  list_sources                │
                   └────────────────────────────┘
```

## Project Structure

```
mycelium/
├── cmd/                      # CLI commands (init, add, up, upgrade, publish, status, serve)
├── internal/
│   ├── artifact/             # Gzipped JSONL artifact format, checksum, HTTP fetcher
│   ├── chunker/              # Markdown heading chunker + tree-sitter code chunker
│   ├── embedder/             # Voyage AI, OpenAI, Ollama providers
│   ├── fetchers/             # GitHub repo cloner
│   ├── hasher/               # Content hash and store key computation
│   ├── lockfile/             # mycelium.lock read/write
│   ├── manifest/             # mycelium.toml parsing and validation
│   ├── mcp/                  # MCP server (search, search_code, list_sources)
│   ├── pipeline/             # Orchestrates fetch → chunk → embed → upsert
│   └── store/                # Vector store abstraction (LanceDB embedded)
├── e2e/                      # End-to-end tests (build tag: e2e)
├── demo/                     # Benchmark and demo projects
├── mycelium.toml             # Project manifest (committed)
├── mycelium.lock             # Lockfile (committed, never hand-edited)
└── go.mod
```

## License

[MIT](LICENSE)
