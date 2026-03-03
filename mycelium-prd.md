# Product Requirements Document
## Mycelium — Reproducible Dependency Context for AI Coding Agents

**Version:** 1.0-MVP  
**Status:** Draft  
**Last Updated:** 2026-03-02

---

## 1. Overview

### 1.1 Purpose

This document defines the requirements for the minimum viable product (MVP) of **Mycelium** (`mctl`) — a CLI tool that gives AI coding agents accurate, version-pinned knowledge of the libraries and frameworks a project depends on, by indexing both their **documentation and source code**.

It reads a project manifest (`mycelium.toml`), resolves dependencies against a lockfile (`mycelium.lock`), and ensures a local vector store contains exactly the context the lockfile declares. An MCP server exposes that context to any agent that supports the Model Context Protocol — Claude Code, Cursor, Windsurf, or any future MCP-compatible tool.

The result: a developer clones a repo, runs `mctl up`, and their coding agent immediately knows the current APIs, configuration schemas, idiomatic patterns, function signatures, type definitions, and usage examples of every dependency — not the stale or absent knowledge baked into its training data.

### 1.2 Problem Statement

AI coding agents are trained on documentation snapshots that are months or years old. When a developer asks their agent to generate configuration for Envoy Gateway v1.4, Kubernetes v1.31, or any actively evolving project, the agent confidently produces YAML referencing fields that were renamed, options that were deprecated, or patterns that are no longer idiomatic.

This problem has a second, more durable dimension: **private libraries that will never appear in any model's training data.** Large enterprises routinely have deep stacks of internal libraries — a platform SDK, an internal service mesh wrapper, a payments framework, a compliance library — that underpin production systems and are depended on by hundreds of services. No amount of improved model training will ever teach an agent about these libraries. Every engineer working on a service that depends on `internal-platform-sdk` is currently on their own: they paste Confluence pages into context windows, maintain sprawling instruction files, or simply accept that their agent is useless for anything touching internal dependencies.

Making this worse: for many internal libraries, the documentation is sparse, outdated, or nonexistent. The real source of truth is the code itself — function signatures, type definitions, interface contracts, inline comments, and the test files that demonstrate idiomatic usage. An agent that can search a dependency's documentation but not its public API surface is only getting half the picture. For poorly documented internal libraries, it's getting almost nothing.

Today, developers compensate by manually curating context per-session. None of this is reproducible. When a second developer clones the same repo, or when CI runs an agent-assisted workflow, the context is different — or absent entirely. In enterprises, this problem scales with organizational complexity: a service depending on five internal libraries and three OSS frameworks means eight sets of documentation that need to be manually loaded, every session, by every developer.

The gap: **no tool makes a project's agent context reproducible and portable across developers and CI, the way a lockfile makes dependency resolution reproducible.** This is true for fast-moving OSS projects, and it is permanently true for private libraries.

### 1.3 Design Principle

The core insight is borrowed from package managers: **if two developers have the same lockfile, they should get the same result.** Applied to dependency context, this means an embedding artifact is identified not by a name and version tag, but by a content-addressed hash of what was embedded, which model embedded it, and how it was chunked. A fresh clone always produces agent context functionally identical to the original author's.

The tool also implements **transparent source/binary deployment**: it knows how to build embeddings from source (clone, chunk, embed), but always prefers fetching a pre-built artifact when one exists. The developer doesn't choose the path; the tool resolves it automatically.

### 1.4 Target Audience

The primary audience is **any developer using an MCP-compatible AI coding agent** (Claude Code, Cursor, Windsurf, etc.) on a project whose dependencies include libraries — public or private — that the agent doesn't know well enough to be useful.

This spans two distinct segments that experience the same underlying problem for different structural reasons:

**Open-source developers** working with fast-moving frameworks and infrastructure projects (Kubernetes ecosystem, service meshes, IaC tools, emerging language frameworks). Their agent's training data is stale. The tool gives the agent current documentation and API surface knowledge.

**Enterprise developers** building on internal library stacks — platform SDKs, service frameworks, shared middleware, compliance libraries — that will never appear in any model's training data. For these developers, the agent isn't stale; it's entirely blind. The tool gives the agent knowledge it cannot acquire any other way — both the documentation (where it exists) and the actual code: exported functions, type definitions, interface contracts, and usage examples from tests.

Within these segments, the MVP focuses on developers working on projects with **complex configuration surfaces or deep API contracts** — where incorrect agent output isn't just unhelpful but actively costly (silent misconfigurations, integration failures, compliance violations). These developers already feel the pain daily and are willing to add a tool to their workflow to fix it.

The secondary audience is **library maintainers** — both OSS project maintainers and internal platform teams — who want to publish embedding artifacts so that every downstream consumer of their library gets accurate agent context automatically.

See **§10 (Audience Positioning)** for a deeper discussion of go-to-market positioning.

---

## 2. Goals and Non-Goals

### 2.1 MVP Goals

- **Reproducible agent context.** Two developers with the same `mycelium.lock` always get functionally identical vector stores and query results.
- **One-command setup for downstream developers.** Clone a repo, run `mctl up`, and the coding agent is immediately context-aware.
- **Lockfile-pinned dependencies.** Every source is pinned by content hash, not just version tag.
- **Transparent artifact resolution.** Prefer fetching pre-built embedding artifacts; fall back to building from source.
- **Documentation and code indexing.** Index both Markdown documentation and source code from dependencies. Documentation is chunked by heading structure; code is chunked by semantic structure (functions, types, classes) using tree-sitter AST parsing.
- **MCP integration.** Expose indexed context to any MCP-compatible coding agent.
- **Local-first data plane.** No indexed content leaves the developer's machine at query time.
- **GitHub source support.** Clone a repo at a pinned ref and index its documentation and code. Works with github.com, GitHub Enterprise, and any git host accessible via HTTPS + token auth.
- **Pre-built artifact ingestion.** Fetch and verify a published `.jsonl.gz` embedding artifact without calling any embedding API.

### 2.2 MVP Non-Goals

- Community registry of shared sources (see §9, Extension: Community Registry)
- Web crawling, Docusaurus site indexing, or PDF ingestion (see §9, Extension: Additional Source Types)
- Garbage collection across projects
- `direnv` integration or shell activation hooks
- Multi-model support within a single project (one model per project in MVP)
- A UI for browsing indexed documentation
- Remote or multi-user hosting of the runtime

---

## 3. Users and Use Cases

### 3.1 MVP Use Cases

**UC-1: Bootstrapping a new project's agent context.**
A developer runs `mctl init`, adds upstream documentation dependencies, runs `mctl up`, and commits the manifest and lockfile. Their coding agent now has accurate, current knowledge of every declared dependency.

**UC-2: Onboarding to an existing project.**
A new developer clones a repo that already has `mycelium.toml` and `mycelium.lock`. They run `mctl up`. All declared dependencies are fetched (pre-built artifacts where available, built from source otherwise). The coding agent is immediately context-aware, identically to the maintainer's setup.

**UC-3: Upgrading a dependency.**
A maintainer runs `mctl upgrade envoy-gateway@v1.4`. The tool resolves the new version, fetches or builds new embeddings, loads them atomically, evicts the old version's vectors, and updates the lockfile. The developer commits the updated `mycelium.lock`.

**UC-4: Publishing embedding artifacts.**
On a release, CI runs `mctl publish --tag v1.2.0`. The project's documentation embeddings are uploaded as a GitHub release asset. Any downstream project that depends on this library can now fetch the artifact instead of building from source.

**UC-5: Checking context status.**
A developer runs `mctl status` to see whether their local store matches the lockfile and whether any upstream dependencies have newer versions available.

**UC-6: Enterprise internal library stack.**
A payments service depends on three internal libraries (`platform-sdk`, `payments-core`, `compliance-lib`) and two OSS frameworks. The team lead adds all five as dependencies in `mycelium.toml`, pointing the internal libraries at their GitHub Enterprise repositories — indexing both their documentation and their public API source code. The platform team publishes embedding artifacts for `platform-sdk` as part of their release CI. When a new engineer joins the payments team, they clone the repo, run `mctl up`, and their agent immediately understands the internal platform SDK's function signatures and types, the compliance library's validation rules, and the current Envoy Gateway configuration schema. No Confluence spelunking required.

**UC-7: Platform team publishing internal library context.**
An internal platform team maintains `platform-sdk`, used by 40+ services across the organization. They add `mctl publish` to their release pipeline. The published artifact includes embeddings of both their Markdown documentation and their exported Go API surface (function signatures, type definitions, interface contracts). Every downstream service that declares a dependency on `platform-sdk` in its `mycelium.toml` gets pre-built embeddings on upgrade — no API calls, no indexing, identical context for every consumer. When the platform team ships a breaking change, every downstream agent learns the new API surface as soon as the team runs `mctl upgrade`.

**UC-8: Indexing a poorly documented dependency.**
An internal library has a sparse README and no other documentation, but 8,000 lines of well-structured Go with clear function names, typed interfaces, and comprehensive test files. The team configures `mycelium.toml` to index the library's `pkg/` and `test/` directories. The agent can now answer questions like "how do I create a new service client?" by retrieving the `NewClient` constructor signature, its option types, and the test cases that demonstrate initialization patterns — context that would otherwise require reading the source code manually or asking a senior engineer.

---

## 4. The Manifest and Lockfile

### 4.1 `mycelium.toml` — The Project Manifest

`mycelium.toml` is committed to the repository root. It is the authoritative, human-editable declaration of what dependency context this project requires. Each dependency can declare both documentation paths and code paths; the tool infers the chunking strategy from file type.

```toml
[config]
embedding_model = "voyage-code-2"
publish = "github-releases"

[local]
index = ["./docs", "./README.md"]

# Local-only sources, never included in published artifacts
private = ["./notes"]

# Public OSS dependencies
[[dependencies]]
id = "envoy-gateway"
source = "github.com/envoyproxy/gateway"
ref = "v1.3.0"
docs = ["site/content"]                    # Markdown documentation
code = ["api/v1alpha1"]                    # Source code (Go types, CRD definitions)

[[dependencies]]
id = "envoy-ai-gateway"
source = "github.com/envoyproxy/ai-gateway"
ref = "main"
docs = ["docs/"]

[[dependencies]]
id = "kubernetes-api"
source = "github.com/kubernetes/website"
ref = "main"
docs = ["content/en/docs/reference/kubernetes-api"]

# Internal / private dependencies (GitHub Enterprise or private repos)
[[dependencies]]
id = "platform-sdk"
source = "github.example.com/platform/sdk"
ref = "v4.2.0"
docs = ["docs/", "api-reference/"]
code = ["pkg/client", "pkg/types"]         # Public API surface
code_extensions = [".go"]                  # Default: language-appropriate extensions

[[dependencies]]
id = "compliance-lib"
source = "github.example.com/infra/compliance"
ref = "v2.1.0"
docs = ["docs/rules", "docs/integration"]
code = ["pkg/"]
```

### 4.2 `mycelium.lock` — The Lockfile

`mycelium.lock` is committed to the repository root and never edited by hand. It pins every dependency to exact content hashes and artifact checksums.

```toml
[meta]
mycelium_version = "1.0.0"
embedding_model = "voyage-code-2"
embedding_model_version = "2024-05"
locked_at = "2026-02-28T14:30:00Z"

[sources.envoy-gateway]
version = "v1.3.0"
commit = "8f3a2b1c9d4e..."
content_hash = "sha256:3f8a..."
artifact_url = "https://github.com/envoyproxy/gateway/releases/download/v1.3.0/mycelium-voyage-code-2.jsonl.gz"
artifact_hash = "sha256:9d2c..."
store_key = "sha256:a1b2..."
ingestion_type = "artifact"

[sources.envoy-ai-gateway]
version = "main"
commit = "4b7c1d2e3f5a..."
content_hash = "sha256:7e1f..."
store_key = "sha256:b3c4..."
ingestion_type = "built"

[sources.local]
content_hash = "sha256:4d9a..."
store_key = "sha256:c5d6..."
```

The `store_key` is the critical field. It is a hash of `(content_hash + embedding_model + model_version + chunking_config)`. The `chunking_config` component includes the chunker type (markdown or tree-sitter), language grammars used, and chunking parameters. Two projects that compute the same `store_key` reference identical data. This is what gets used as the vector store partition identifier.

---

## 5. Functional Requirements

### 5.1 CLI Commands (MVP)

**`mctl init`**
Initializes `mycelium.toml` in the current directory with sensible defaults. Prompts for embedding model preference.

**`mctl add <source>`**
Adds a dependency to `mycelium.toml`. Resolves the source and stages the change. Does not update the lockfile or vector store. Examples:
- `mctl add github.com/envoyproxy/gateway@v1.3 --docs site/content`
- `mctl add github.example.com/platform/sdk@v4.2 --docs docs/ --code pkg/`
- `mctl add github.example.com/infra/compliance@v2.1 --code pkg/` (code-only, no docs)

**`mctl up`**
The primary command. Ensures the local runtime (vector store + MCP server) is running, then converges the vector store to match the manifest and lockfile. For each declared source:
1. If `store_key` is already present in the local store, skip.
2. If an artifact URL is declared in the lockfile, fetch, verify checksum, and ingest.
3. If no artifact exists, build from source: clone at pinned ref, chunk documentation (heading-aware) and code (tree-sitter AST), call embedding API, and load vectors.
Updates `mycelium.lock` if any source was newly resolved. Idempotent: running twice is always safe. Starts the MCP server if it isn't already running.

**`mctl upgrade <source>[@version]`**
Upgrades a dependency to a new version. Resolves the new version, fetches or builds new embeddings, loads them into the store, then evicts the old version's vectors. Updates `mycelium.lock`. Eviction happens only after successful load — the store is never degraded mid-upgrade.

**`mctl publish --tag <version>`**
Generates embedding artifacts for all `index` paths (documentation) and any `code` paths declared in `mycelium.toml`, then publishes them to the configured target (GitHub releases). The artifact contains both documentation and code embeddings. Publishes a companion `.sha256` file. Updates `mycelium.lock` with the artifact URL and hash.

**`mctl status`**
Reports the state of all declared sources relative to the lockfile and local store. Shows whether each dependency's local vectors match the lockfile, and whether newer upstream versions are available.

### 5.2 Source Types (MVP)

The MVP supports two source types:

- **`github`** — Clone a repository at a specific git ref. Index Markdown documentation and/or source code from configurable paths. The tool infers chunking strategy from file type: Markdown files (`.md`, `.mdx`) are chunked by heading structure; source code files are chunked by AST structure using tree-sitter. Works with github.com, GitHub Enterprise, and any git host accessible via HTTPS + token auth.
- **`artifact`** — Fetch and ingest a pre-built embedding artifact from a URL. No embedding API calls required. Artifacts can contain embeddings from both documentation and code. This is the fast path when an upstream project (public or internal) publishes artifacts as part of its release process.

### 5.3 Ingestion and Chunking

The ingestion pipeline uses two chunking strategies, selected automatically by file type.

**Documentation chunking (Markdown).** Markdown files are parsed into chunks that preserve heading hierarchy as breadcrumb metadata. Chunk boundaries align with heading structure (h1, h2, h3), ensuring each chunk is a self-contained section. Chunk size targets 512–1024 tokens with configurable overlap.

**Code chunking (source files).** Source code files are parsed into an abstract syntax tree (AST) using **tree-sitter**, the industry-standard incremental parsing library used by Cursor, Neovim, Zed, and GitHub's own code search. Tree-sitter grammars are available for all major languages (Go, Python, TypeScript, Java, Rust, C/C++, etc.), giving the tool broad language coverage from day one.

AST-aware chunking splits code along semantically meaningful boundaries — function definitions, type declarations, interface definitions, method implementations, and struct/class definitions — rather than splitting at arbitrary token counts. Each code chunk represents a complete, self-contained code construct. Adjacent small constructs (e.g., a sequence of short type definitions) are grouped into a single chunk to stay within the target token range.

Every chunk, whether from documentation or code, carries the same metadata: `source`, `source_version`, `commit`, `path`, `breadcrumb`, `embedding_model`, `store_key`, `chunk_index`, and `chunk_type` (`doc` or `code`). For code chunks, the `breadcrumb` field records the structural path (e.g., `pkg/client > ClientOptions > WithTimeout`) rather than heading hierarchy. For documentation chunks, it records the heading hierarchy as before.

The `chunk_type` field enables the MCP server to filter or weight results by type — for example, preferring code results when the agent's query looks like a function lookup, or documentation results when the query is conceptual.

Ingestion is resumable: a failed `mctl up` checkpoints progress and can be continued without re-embedding already-processed chunks.

**Supported languages (MVP):** Go, Python, TypeScript/JavaScript, Java, Rust. Additional tree-sitter grammars can be added with minimal effort — each requires only a grammar file and a mapping of AST node types to chunk boundaries.

### 5.4 Vector Store

The MVP uses an embedded vector store (LanceDB or ChromaDB) rather than a client-server database like Qdrant. This eliminates the Docker dependency for the vector store itself, reducing the infrastructure tax on developers. The store persists to a local directory (`.mycelium/store/` in the project root or `~/.mycelium/store/` for shared cross-project data).

The store uses `store_key` as the partition identifier. Sources sharing a `store_key` (same content, same model, same chunking config) share a single copy of their vectors — no duplication across projects that depend on the same library at the same version.

Different embedding models use separate collections. Mixing vector dimensions within a collection is never permitted.

### 5.5 Embedding Providers (MVP)

Configurable in `mycelium.toml`. The MVP supports:

| Provider | Model | Notes |
|---|---|---|
| Voyage AI | `voyage-code-2` (default) | Optimized for both code and technical documentation |
| OpenAI | `text-embedding-3-small` | Widely available alternative; less code-optimized |
| Ollama | Any locally-served model | Fully offline, no API key required |

The default model, `voyage-code-2`, is specifically trained on code and technical text, making it a natural fit for a tool that indexes both documentation and source code. Projects that index a significant amount of code should prefer `voyage-code-2` or a similarly code-aware model.

Embedding requests are batched during ingestion. Rate limit failures use exponential backoff. The lockfile records the specific model version string to detect when a provider update would invalidate cached embeddings.

### 5.6 MCP Server

The MCP server exposes three tools:

**`search(query: str, source: str | null, type: str | null, top_k: int = 5) -> str`**
Semantic search across all indexed content — documentation and code. If `source` is provided, results are scoped to that dependency. If `type` is provided (`"doc"`, `"code"`, or `null` for both), results are filtered by chunk type. When `type` is null, results from both documentation and code are merged and ranked by relevance score. Returns top-k chunks with breadcrumb path, source name, version, chunk type, and relevance score.

**`search_code(query: str, source: str | null, language: str | null, top_k: int = 5) -> str`**
Convenience tool equivalent to `search` with `type="code"`. Designed for agent workflows that specifically need function signatures, type definitions, or implementation patterns. The optional `language` filter scopes results to a specific programming language.

**`list_sources() -> str`**
Returns all indexed sources with version, chunk count (docs and code separately), embedding model, last-synced timestamp, and ingestion type (artifact or built).

The MCP server runs as a lightweight local process started by `mctl up`. It reads from the local vector store directly (no network hop to a database container). Claude Code and other MCP clients connect via stdio. Tool responses are plain text formatted for LLM consumption.

### 5.7 Runtime

`mctl up` is the single entry point. It starts the MCP server process (and Docker containers if needed for any future services), verifies vector store health, and runs a sync. The MCP client configuration (`.mcp.json` or equivalent) is committed to the repo so that Claude Code and other MCP clients discover the server automatically.

No manual sequencing of services is required. A developer's workflow is: clone, `mctl up`, start coding.

---

## 6. Embedding Artifact Standard

### 6.1 Format

A gzipped JSONL file (`mycelium-{model-slug}.jsonl.gz`) where each line is a chunk embedding. Artifacts contain both documentation and code chunks, distinguished by the `chunk_type` field.

Documentation chunk example:
```json
{
  "id": "envoy-gateway::site/content/tasks/http-routing.md::3",
  "vector": [0.023, -0.117, "..."],
  "payload": {
    "source": "envoy-gateway",
    "source_version": "v1.3.0",
    "commit": "8f3a2b1c9d4e...",
    "path": "site/content/tasks/http-routing.md",
    "breadcrumb": "tasks > http-routing > timeout configuration",
    "text": "The HTTPRoute timeout field accepts...",
    "chunk_type": "doc",
    "embedding_model": "voyage-code-2",
    "embedding_model_version": "2024-05",
    "chunk_index": 3,
    "store_key": "sha256:a1b2..."
  }
}
```

Code chunk example:
```json
{
  "id": "platform-sdk::pkg/client/client.go::fn::NewClient",
  "vector": [0.041, -0.089, "..."],
  "payload": {
    "source": "platform-sdk",
    "source_version": "v4.2.0",
    "commit": "7c2d4e6f8a1b...",
    "path": "pkg/client/client.go",
    "breadcrumb": "pkg/client > NewClient",
    "text": "// NewClient creates a new platform client with the given options.\n// It establishes a connection to the control plane and validates credentials.\nfunc NewClient(ctx context.Context, opts ...ClientOption) (*Client, error) {",
    "chunk_type": "code",
    "language": "go",
    "node_type": "function_declaration",
    "embedding_model": "voyage-code-2",
    "embedding_model_version": "2024-05",
    "chunk_index": 7,
    "store_key": "sha256:a1b2..."
  }
}
```

A companion `.sha256` file is published alongside every artifact. Artifacts failing checksum verification are rejected and the existing store is left unchanged.

### 6.2 Publication Standard

This format is designed as an open standard for any OSS project or internal platform team to adopt. A reference GitHub Actions workflow for generating and publishing artifacts at release time will be included in the project documentation.

The goal is for projects to publish embedding artifacts alongside container images and release binaries, so that downstream developers get accurate agent context — covering both documentation and public API source code — without running any embedding API calls.

---

## 7. Non-Functional Requirements

**NFR-1: Reproducibility.** Two developers with the same `mycelium.lock` must produce functionally identical local vector stores, regardless of whether they fetched artifacts or built from source.

**NFR-2: Content-addressing.** The local store must never contain duplicate vectors for the same `store_key`.

**NFR-3: Atomic upgrades.** `mctl upgrade` must load new vectors before evicting old ones. The store is never in a degraded state mid-operation.

**NFR-4: Single-command startup.** `mctl up` is the only command a downstream developer needs to run. No manual service orchestration.

**NFR-5: Local-first data plane.** No indexed content leaves the local machine at query time. Embedding API calls only occur during `mctl up` (build-from-source path) or `mctl publish`.

**NFR-6: Retrieval latency.** `search_docs` must return results in under 2 seconds for typical queries.

**NFR-7: Artifact integrity.** Pre-built artifacts are checksum-verified before ingestion. Verification failures leave the existing store unchanged.

**NFR-8: Resumable ingestion.** A failed `mctl up` can be resumed without re-embedding already-processed chunks.

**NFR-9: Onboarding speed.** A developer cloning an existing repo with pre-built artifacts available should have a working agent context within 3 minutes.

---

## 8. Success Criteria

The MVP is successful if:

1. A developer cloning a repo with `mycelium.toml` and `mycelium.lock` has a working agent context after running `mctl up` with no additional configuration.
2. Two developers with the same `mycelium.lock` get identical query results from `search` for the same input.
3. A coding agent with Mycelium context produces correct, version-accurate API references (field names, configuration options, types) in >80% of cases during representative coding sessions, compared to <40% without it.
4. For a dependency with indexed source code, the agent can retrieve correct function signatures, type definitions, and constructor patterns when asked — even for internal libraries with no documentation.
5. `mctl upgrade` completes atomically — the store is never degraded mid-operation.
6. A new developer can go from zero to productive with the tool in under 15 minutes using only the README.
7. At least one OSS project publishes embedding artifacts using the standard defined in this document.
8. The tool works with private GitHub repositories and GitHub Enterprise instances using standard token authentication, with no additional configuration beyond `GITHUB_TOKEN`.
9. A project with a mix of public and private dependencies, and a mix of documentation and code sources, resolves all sources correctly in a single `mctl up` invocation.

---

## 9. Extension Roadmap

The following capabilities are explicitly out of scope for the MVP but represent natural extensions once the core value is validated. Each is described with enough detail to inform architectural decisions in the MVP without committing to implementation.

### 9.1 Additional Source Types

**What:** Support for web crawling (Docusaurus sites, rendered documentation), single-page fetch, and PDF ingestion.

**Why it matters:** Many projects publish documentation as rendered websites rather than raw Markdown in a repo (e.g., React docs, Terraform docs). PDF specs (RFCs, protocol specifications) are common in infrastructure work.

**MVP implication:** The ingestion pipeline is designed with a `Fetcher` interface (MVP implements `github` and `artifact`) and a `Chunker` interface (MVP implements `markdown` and `code`). Web and PDF fetchers slot cleanly into this architecture.

**Dependencies:** Web crawling requires a headless browser runtime (Crawl4AI or similar). PDF extraction requires Docling or equivalent. Both add significant binary dependencies, which is why they are deferred.

### 9.2 Community Registry

**What:** A shared, versioned registry (likely a GitHub repository) of pre-built embedding artifacts for well-known documentation sources that no single project "owns" — Effective Go, Kubernetes API reference, SPIFFE spec, common RFCs, language standard libraries.

**Why it matters:** Without a registry, every team independently builds embeddings for the same popular sources, wasting compute and API costs. A registry makes `mctl add community:effective-go@latest` a one-line operation that fetches a verified, pre-built artifact.

**MVP implication:** The `mycelium.toml` dependency schema should reserve the `registry` field even if the MVP doesn't resolve it. The artifact format (§6) is already registry-compatible — the registry is essentially a curated index of artifact URLs.

**Open questions:** Governance (who can publish), quality control, update cadence, multi-model artifact publishing.

### 9.3 Garbage Collection

**What:** A `mctl gc` command that identifies and removes vectors in the local store that are no longer referenced by any project's lockfile.

**Why it matters:** As developers upgrade dependencies, old vectors accumulate. Over months, a developer working on multiple projects could have gigabytes of stale embeddings.

**MVP implication:** The content-addressed store design (§5.4) already enables this — every vector is tagged with a `store_key`, so identifying unreferenced data is a set-difference operation. The MVP should ensure `store_key` tagging is complete and correct, even if `gc` itself is deferred.

**Design consideration:** GC requires knowledge of all active projects on the machine. This could be a scan of known directories, an explicit registry, or simply per-project gc (remove vectors for dependencies no longer in this project's lockfile). Per-project gc is simpler and avoids the global-state problem.

### 9.4 Rollback

**What:** A `mctl rollback <source>` command that reverts a dependency to its previous lockfile-pinned version.

**Why it matters:** An upgrade might introduce a regression in context quality (e.g., a project restructured its docs and the new chunking is worse). Rollback lets a developer revert to the previous known-good state without manually editing the lockfile.

**MVP implication:** The lockfile should include enough information to reconstruct the previous state (previous `store_key`, previous `artifact_url`). The simplest implementation: `mctl rollback` is `git checkout HEAD~1 -- mycelium.lock && mctl up`.

### 9.5 Shell Integration (`mctl dev`)

**What:** A `mctl dev` command or `direnv` hook that activates a project's Mycelium context when entering its directory — similar to `nix develop` activating a dev shell.

**Why it matters:** Developers working across multiple projects shouldn't need to remember to run `mctl up` in each one. Automatic activation makes context switching seamless.

**MVP implication:** No architectural impact. This is purely a UX convenience layer on top of `mctl up`.

### 9.6 Multi-Model Artifact Publishing

**What:** Publish embedding artifacts for multiple models simultaneously (e.g., `voyage-code-2` and `text-embedding-3-small`) so downstream projects using different models can still consume pre-built artifacts.

**Why it matters:** The artifact standard is only valuable if the model the publisher chose matches what the consumer configured. Multi-model publishing maximizes artifact hit rates.

**MVP implication:** The artifact naming convention (`mycelium-{model-slug}.jsonl.gz`) already supports this. The `mctl publish` command could accept a `--models` flag in a future version.

### 9.7 CI/CD Integration

**What:** First-class support for running `mctl up` and `search` in CI pipelines — for agent-assisted code review, automated documentation validation, or agent-driven test generation.

**Why it matters:** If agent context is reproducible, it can be used in CI the same way it's used locally. This unlocks workflows like "agent reviews PR against current API docs" or "agent generates test cases from spec documentation."

**MVP implication:** The MVP's design (lockfile + deterministic resolution + single-command startup) already makes CI use feasible. Explicit CI support means optimizing for headless operation: non-interactive `mctl up`, machine-readable `mctl status`, and documented cache strategies for artifact layers.

### 9.8 Private Artifact Registry

**What:** Support for fetching and publishing embedding artifacts to private registries — GitHub Enterprise release assets, internal Artifactory/Nexus instances, S3 buckets, or other corporate artifact stores — in addition to public GitHub releases.

**Why it matters:** In enterprise environments, embedding artifacts for internal libraries can't be published to public GitHub. Internal platform teams need to publish artifacts to the same infrastructure they use for container images and release binaries. Without this, every downstream team builds embeddings from source on every clone, which is slow and wasteful at scale.

**MVP implication:** The `publish` field in `mycelium.toml` and the `artifact_url` field in `mycelium.lock` should be treated as opaque URLs from the start, not hard-coded to GitHub's release API. The MVP can implement only the GitHub releases backend, but the interface should accept any HTTPS URL with optional token auth. This makes adding Artifactory, S3, or GHE backends a configuration change rather than an architectural one.

### 9.9 Organization-Wide Configuration

**What:** A shared, organization-level configuration that sets defaults for all projects within a company — default embedding model, default artifact registry URL, approved embedding providers, internal library catalog.

**Why it matters:** When 200 services across an organization adopt the tool, per-project configuration becomes tedious and error-prone. An org-level config (e.g., published to an internal repo or fetched from a URL) ensures consistency: every project uses the same embedding model, publishes to the same artifact store, and has access to the same catalog of internal library documentation.

**MVP implication:** No direct architectural impact, but the `mycelium.toml` schema should be designed so that fields like `embedding_model` and `publish` can eventually support inheritance from a parent config. The simplest future implementation: `mctl init --org github.example.com/eng/mycelium-config` seeds a project's `mycelium.toml` from an org template.

### 9.10 Dependency Graph Awareness

**What:** Automatic discovery of documentation dependencies based on the project's code dependencies (go.mod, package.json, requirements.txt, etc.).

**Why it matters:** In a large enterprise, a service might depend on 15 internal libraries transitively. Manually adding each one to `mycelium.toml` is tedious and easy to get wrong. If the tool can read `go.mod` and suggest (or auto-populate) documentation dependencies for known internal libraries, adoption friction drops significantly.

**MVP implication:** No architectural impact. This is a UX layer on top of `mctl add`. The key prerequisite is a catalog mapping code packages to their documentation sources — which the org-wide configuration (§9.9) could provide.

---

## 10. Audience Positioning

### 10.1 Two Problems, One Tool

The tool addresses two related but structurally different problems, and this distinction matters for positioning:

**The stale-knowledge problem (OSS).** Agents are trained on old documentation. This problem is real today but will diminish over time as models improve, context windows grow, and tools like Context7 cover more libraries. The tool's value here is in reproducibility and version-pinning — ensuring the agent has the *right* version of the docs, not just *some* version.

**The zero-knowledge problem (private libraries).** Agents have never seen internal library documentation and never will. No amount of improved training data, longer context windows, or better web search will teach an agent about a company's `platform-sdk` or `payments-core`. This problem is **structurally permanent**, which makes the tool's value proposition durable in a way that the OSS use case alone is not. Code indexing makes this even stronger: for the many internal libraries where documentation is sparse or absent, the tool indexes the actual source code — function signatures, type definitions, interface contracts — giving the agent useful knowledge even when documentation doesn't exist.

The strongest positioning leads with the problem that won't be solved by anything else: *Your agent will never know your internal libraries. This tool fixes that.* The OSS version-pinning story is the natural second act: *And while we're at it, it also makes sure the agent knows the right version of every public library you depend on.*

### 10.2 Who Adopts First

Adoption will follow a predictable curve from developers who feel the pain most acutely to those who benefit more casually.

**Tier 1: Enterprise platform and infrastructure teams.** Large organizations (Fortune 500, scale-ups with 500+ engineers) where services depend on layers of internal libraries that have been built up over years. These teams already struggle with onboarding — new engineers take weeks or months to become productive because the internal stack is undocumented, poorly documented, or documented across scattered Confluence pages. The tool gives their coding agents the same institutional knowledge that currently lives only in senior engineers' heads. The value is immediate: faster onboarding, fewer integration mistakes, less time spent answering "how do I use the X library" questions.

**Tier 2: Infrastructure and platform engineers (OSS).** Developers working with complex configuration surfaces (Kubernetes, Envoy, Terraform, Pulumi, Crossplane) where a wrong field name produces a silent misconfiguration. They already distrust their agent's knowledge and have developed manual workarounds. They will adopt because the tool replaces a workflow they already perform by hand.

**Tier 3: Backend developers on fast-moving frameworks.** Developers building on frameworks with frequent breaking changes. They encounter stale agent knowledge weekly but haven't systematized workarounds. They adopt when they see Tier 1 and 2 usage and recognize their own pain.

**Tier 4: Library and platform team maintainers.** Internal platform teams and OSS maintainers who want downstream consumers to get accurate agent context automatically. They adopt by publishing embedding artifacts as part of their release process. This is the supply-side adoption that makes the ecosystem self-sustaining, and it follows demand-side adoption (Tiers 1–3 asking for it).

### 10.3 The Enterprise Wedge

The enterprise use case deserves specific attention because it has several properties that make it an unusually strong adoption path:

**The pain scales with organizational complexity.** A solo developer working on a small project with two OSS dependencies might not bother. A team of 15 engineers building on a stack of five internal libraries and three OSS frameworks — where every new joiner spends their first month learning the internal platform — will see immediate ROI.

**Code indexing eliminates the documentation prerequisite.** Most enterprise tools that promise "AI-powered internal knowledge" require well-maintained documentation as input. In practice, internal library documentation is sparse, outdated, or nonexistent. By indexing source code directly — function signatures, type definitions, interface contracts, test files — the tool delivers value even when documentation doesn't exist. The code *is* the documentation. This makes the tool adoptable today, not after a documentation sprint that will never happen.

**The value is defensible.** Better model training will eventually erode the stale-OSS-knowledge problem. It will never erode the private-library problem. A tool that solves the private-library problem has a durable moat.

**Platform teams are a natural distribution channel.** When an internal platform team adds `mctl publish` to their release pipeline, every downstream service team that consumes the library is a potential adopter. Adoption spreads horizontally through the dependency graph within an organization.

**The ROI is measurable in enterprise terms.** Reduced onboarding time, fewer integration-related bugs, less senior-engineer time spent answering questions about internal APIs — these are metrics that engineering leaders already care about.

### 10.4 Positioning Recommendation

Lead with the pain, not the mechanism. The pitch is not "a content-addressed RAG context manager with lockfile reproducibility." The pitch is:

> **Your coding agent doesn't know your libraries. `mctl up` fixes that.**
>
> Pin your project's dependency knowledge — docs and code, public and private — the same way you pin your code dependencies. One command gives every developer and every CI run identical, accurate context.

For OSS-focused audiences, the before/after is:
- **Before:** Claude generates Envoy Gateway config with fields from v1.1. You spend 20 minutes fixing it.
- **After:** `mctl up` gives Claude the v1.4 docs. It generates correct config on the first try.

For enterprise audiences, the before/after is:
- **Before:** A new engineer joins the team. Their coding agent is useless for anything touching the internal platform SDK. They spend three weeks reading Confluence and asking questions in Slack.
- **After:** They clone the repo, run `mctl up`, and their agent understands the platform SDK, the compliance library, and the service framework — from the docs where they exist, and from the actual source code where they don't. They ship their first PR in two days.

### 10.5 Naming: Mycelium / `mctl`

The project is called **Mycelium**. The CLI command is **`mctl`**.

**The metaphor.** In a forest, mycelium is the hidden fungal network that connects trees to nutrients they can't reach on their own. A tree doesn't need to know where the phosphorus is — the mycelial network finds it and delivers it. In a codebase, this tool is the hidden network that connects a coding agent to knowledge it can't reach on its own — the agent doesn't need to know where the API docs or function signatures are, the tool finds them and delivers them.

The analogy extends to how the tool operates within an organization. A mycelial network connects many organisms to many nutrient sources through a shared substrate. The dependency graph in `mycelium.toml` connects one project to many sources of knowledge, and when a platform team publishes artifacts, those nutrients flow through the network to every downstream consumer. The more projects that adopt the tool, the richer the network becomes — the same way a mycelial network becomes more effective as it grows.

**The CLI name.** `mctl` follows the `*ctl` naming convention established in the Kubernetes ecosystem: `kubectl`, `istioctl`, `argoctl`, `etcdctl`, `systemctl`. This is a deliberate choice for three reasons:

First, **it signals the target audience.** The `ctl` suffix is a tribal marker for infrastructure and platform engineers — the tool's Tier 1 adoption segment. An engineer who sees `mctl` in a README or CI pipeline immediately recognizes it as a control plane tool in the same category as the rest of their infrastructure tooling. It says "this is for you" before they've read a word of documentation.

Second, **it fits the enterprise deployment context.** When an engineering director reviews a platform team's CI pipeline and sees `mctl` alongside `kubectl` and `istioctl`, it looks like it belongs. The name communicates that this is production infrastructure tooling, not a weekend experiment. This matters for enterprise adoption, where tool choices are scrutinized.

Third, **it avoids the jargon problem.** The working name `rag` was technically precise but required the user to know what retrieval-augmented generation is. Most developers — and certainly most engineering leaders evaluating tools — don't. "Mycelium" is evocative and memorable without requiring AI/ML vocabulary. The name gets people to look; the tagline tells them what it is.

**Practical considerations.** `mctl` is four characters, fast to type, and tab-completes well. It doesn't collide with any widely-used CLI tool. The pronunciation follows the established `*ctl` convention: "M-control" or spelled out "M-C-T-L." The project name "Mycelium" is googleable, distinctive, and doesn't carry negative associations. (Note: the abbreviation `myc` was considered and rejected due to collision with the MYC oncogene, a prominent term in cancer biology that would harm searchability and carry undesirable connotations.)

---

## 11. Architecture (MVP)

```
┌──────────────────────────────────────────────────────────────┐
│                      Developer Machine                        │
│                                                              │
│  ┌──────────────┐         ┌────────────────────────────────┐ │
│  │ Claude Code / │◄───────►│       MCP Server               │ │
│  │ Cursor / etc. │  stdio  │  search, search_code           │ │
│  └──────────────┘         │  list_sources                   │ │
│                           └──────────────┬─────────────────┘ │
│                                          │                    │
│                           ┌──────────────▼─────────────────┐ │
│                           │  Embedded Vector Store          │ │
│                           │  (LanceDB / ChromaDB)           │ │
│                           │                                 │ │
│                           │  collection: voyage-code-2      │ │
│                           │    store_key: sha256:a1b2 (docs)│ │
│                           │    store_key: sha256:b3c4 (code)│ │
│                           └────────────────────────────────┘ │
│                                                              │
│  ┌────────────────────────────────────────────────────────┐  │
│  │  mctl CLI                                              │  │
│  │                                                        │  │
│  │  mycelium.toml + mycelium.lock                                   │  │
│  │    ├─ artifact → fetch → verify → ingest               │  │
│  │    └─ github   → clone ─┬─ .md  → heading chunker ──┐ │  │
│  │                         └─ code → tree-sitter AST ───┤ │  │
│  │                                                      │ │  │
│  │                              embed ◄─────────────────┘ │  │
│  │                                │                       │  │
│  │                  ┌─────────────▼──────────────┐        │  │
│  │                  │  Embedding API              │        │  │
│  │                  │  Voyage / OpenAI / Ollama   │        │  │
│  │                  └────────────────────────────┘        │  │
│  └────────────────────────────────────────────────────────┘  │
│                                                              │
└──────────────────────────────────────────────────────────────┘

  ┌──────────────────────────────────────────────────────┐
  │  Upstream: GitHub Release Assets                      │
  │  envoyproxy/gateway@v1.3.0                           │
  │    mycelium-voyage-code-2.jsonl.gz (docs + code)          │
  │    mycelium-voyage-code-2.jsonl.gz.sha256                 │
  └──────────────────────────────────────────────────────┘

  ┌──────────────────────────────────────────────────────┐
  │  Internal: GitHub Enterprise Release Assets           │
  │  github.example.com/platform/sdk@v4.2.0              │
  │    mycelium-voyage-code-2.jsonl.gz (docs + code)          │
  │    mycelium-voyage-code-2.jsonl.gz.sha256                 │
  └──────────────────────────────────────────────────────┘
```

---

## 12. Project Structure

```
mycelium/
├── cmd/
│   ├── init.go
│   ├── add.go
│   ├── up.go
│   ├── upgrade.go
│   ├── publish.go
│   ├── status.go
│   └── serve.go          # MCP server (embedded, not separate Python process)
├── internal/
│   ├── manifest/          # mycelium.toml parsing and validation
│   ├── lockfile/          # mycelium.lock read/write
│   ├── store/             # Vector store abstraction (LanceDB / ChromaDB)
│   ├── fetchers/
│   │   ├── github.go      # Clone + walk docs and code paths
│   │   └── artifact.go    # Fetch + verify + ingest pre-built artifact
│   ├── chunker/
│   │   ├── markdown.go    # Heading-aware documentation chunking
│   │   ├── code.go        # AST-aware code chunking via tree-sitter
│   │   └── grammars/      # Tree-sitter grammar files per language
│   ├── embedder/          # Embedding provider abstraction
│   └── hasher/            # content_hash and store_key computation
└── go.mod

# Per-repository files (committed)
mycelium.toml                   # Project manifest
mycelium.lock                   # Lockfile (never hand-edited)
.mcp.json                  # MCP client configuration
```

---

## 13. Configuration Reference

### 13.1 `mycelium.toml` Full Schema

```toml
[config]
embedding_model = "voyage-code-2"     # Required
publish = "github-releases"            # Optional: where mctl publish uploads

[local]
index = ["./docs", "./README.md"]      # Paths to index, included in published artifacts
private = ["./notes"]                  # Local-only, never published

# OSS dependency: documentation only
[[dependencies]]
id = "envoy-gateway"                   # Unique identifier
source = "github.com/envoyproxy/gateway"
ref = "v1.3.0"                         # Git ref (tag, branch, or commit)
docs = ["site/content"]                # Directories with Markdown documentation
code = ["api/v1alpha1"]                # Directories with source code to index
code_extensions = [".go"]              # File types for code indexing (default: inferred from language)

# Internal dependency: documentation + code
[[dependencies]]
id = "platform-sdk"
source = "github.example.com/platform/sdk"
ref = "v4.2.0"
docs = ["docs/", "api-reference/"]
code = ["pkg/client", "pkg/types"]
code_extensions = [".go"]

# Internal dependency: code only (no documentation)
[[dependencies]]
id = "compliance-lib"
source = "github.example.com/infra/compliance"
ref = "v2.1.0"
code = ["pkg/"]                        # Code-only — the source is the documentation

# Future: registry-resolved dependency (reserved, not implemented in MVP)
# [[dependencies]]
# id = "effective-go"
# registry = "community"
# version = "latest"
```

### 13.2 Environment Variables

| Variable | Description | Default |
|---|---|---|
| `VOYAGE_API_KEY` | Voyage AI API key | Required if model is Voyage |
| `OPENAI_API_KEY` | OpenAI API key | Required if model is OpenAI |
| `OLLAMA_URL` | Ollama base URL | `http://localhost:11434` |
| `GITHUB_TOKEN` | Token for GitHub.com (public and private repos) | Optional for public repos |
| `GHE_TOKEN` | Token for GitHub Enterprise instances | Optional |
| `GHE_URL` | GitHub Enterprise base URL | None |
| `MYCELIUM_STORE_DIR` | Vector store location | `~/.mycelium/store` |

---

## 14. Open Questions

- **Embedded vs. client-server vector store:** The MVP specifies an embedded store (LanceDB/ChromaDB) to minimize infrastructure. If cross-project deduplication or concurrent access becomes important, this may need to evolve to a local Qdrant instance. The `store_key` abstraction insulates the rest of the system from this choice.
- **Single binary vs. Go + Python:** The MVP targets a single Go binary for both CLI and MCP server. If the MCP ecosystem strongly favors Python (FastMCP), a thin Python wrapper calling into Go may be pragmatic. The priority is: developer installs one thing, runs one command.
- **Tree-sitter grammar bundling:** Tree-sitter grammars are language-specific shared libraries. The MVP can bundle grammars for the five supported languages (Go, Python, TypeScript, Java, Rust) in the binary. As language coverage grows, a grammar download mechanism (similar to how Neovim manages tree-sitter parsers) may be needed to avoid bloating the binary. Go bindings for tree-sitter exist (e.g., `go-tree-sitter`) and should be evaluated for maturity.
- **Code chunk granularity:** The right chunking granularity for code is not always obvious. A single large function might need to be split, while a cluster of small type definitions should be grouped. The MVP should start with a conservative approach (one chunk per top-level declaration, split if over token limit) and tune based on retrieval quality feedback.
- **Model version lifecycle:** When Voyage ships a new version of `voyage-code-2`, all existing artifacts become incompatible. The MVP should warn when a model version mismatch is detected. A future version should support migration tooling.
- **Lockfile format stability:** The lockfile format must be stable enough that older CLI versions can read newer lockfiles (or fail gracefully). Semantic versioning of the lockfile schema should be established before the first public release.
- **Enterprise git hosting diversity:** The MVP assumes GitHub (github.com and GHE) as the git host. Enterprise environments may also use GitLab, Bitbucket Server, or Azure DevOps. The git-clone path should be abstracted enough that adding new hosts is a configuration change, not a code change. Token auth patterns vary across hosts.
- **Air-gapped environments:** Some enterprise environments have no outbound internet access. The build-from-source path works (clone from internal git, embed via internal Ollama), but the Voyage/OpenAI embedding providers won't. Ollama support is the MVP's answer, but the documentation should explicitly address this deployment model.
- **Embedding cost at enterprise scale:** An organization with 50 internal libraries, each with documentation and code, could face substantial embedding API costs during initial indexing. Code generates more chunks than documentation for the same library. The artifact publication model (build once, fetch everywhere) mitigates this, but the cold-start cost for the first team to index each library should be estimated and documented.
