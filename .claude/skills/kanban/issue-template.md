# Issue Body Template

Use this template when creating GitHub issues. Fill in every section — do NOT leave placeholders or TBD items. If you don't have enough information for a section, research the codebase first.

## Template

```markdown
## Summary

[1-2 sentences: What needs to happen and why. Be specific — reference the exact behavior change, not vague goals.]

## Context

[What exists today. Reference specific files, functions, or packages. Include relevant code paths so a future session can pick this up without re-exploring the codebase.]

**Key files:**
- `path/to/file.go` — [what this file does and how it relates]
- `path/to/other.go` — [what this file does and how it relates]

## Acceptance Criteria

- [ ] [Concrete, testable criterion — e.g., "`mctl init` creates a valid `mycelium.toml` with default embedding model"]
- [ ] [Another criterion — e.g., "Lockfile parser round-trips without data loss"]
- [ ] [Test criterion — e.g., "Unit tests cover all chunk boundary cases in `markdown.go`"]
- [ ] [Integration/E2E criterion if applicable]

## Technical Approach

[Recommended implementation approach. Reference the project structure from the PRD if applicable. Include which packages need changes.]

1. [Step 1 — e.g., "Define `Manifest` struct in `internal/manifest/manifest.go`"]
2. [Step 2 — e.g., "Add TOML parsing with `github.com/BurntSushi/toml`"]
3. [Step 3 ...]

## Dependencies

[Other issues that must be completed first, or upstream features this depends on. Write "None" if standalone.]

## Out of Scope

[What this issue explicitly does NOT cover. Helps prevent scope creep in future sessions.]
```

## Guidelines

- **Be specific**: "Add chunking" is bad. "Implement heading-aware Markdown chunker that preserves breadcrumb hierarchy and targets 512-1024 token chunks" is good.
- **Reference real paths**: Always include actual file paths from the codebase, not hypothetical ones.
- **Testable criteria**: Every acceptance criterion should be verifiable by running a test or checking a specific behavior.
- **Size the work**: If an issue has more than 8 acceptance criteria, it's probably too large — split it.
