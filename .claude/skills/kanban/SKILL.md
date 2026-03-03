---
name: kanban
description: Manage the Mycelium project board — create issues, move between columns, list items.
argument-hint: "[list|create|move|update|close] [args...]"
allowed-tools: Bash(gh *), Read, Grep, Glob
---

# Kanban Board Management

Manage the Mycelium project board using the GitHub CLI (`gh`).

## Project Configuration

- **Owner:** johnlanda
- **Project number:** 3
- **Project ID:** PVT_kwHOAGPp9s4BQtJg
- **Repository:** johnlanda/mycelium
- **Board URL:** https://github.com/users/johnlanda/projects/3/views/1

### Field IDs

**Status** (field: `PVTSSF_lAHOAGPp9s4BQtJgzg-v3zc`):
| Column | Option ID |
|--------|-----------|
| Backlog | `f75ad846` |
| Ready | `61e4505c` |
| In progress | `47fc9ee4` |
| In review | `df73e18b` |
| Done | `98236657` |

**Priority** (field: `PVTSSF_lAHOAGPp9s4BQtJgzg-v31A`):
| Level | Option ID |
|-------|-----------|
| P0 | `79628723` |
| P1 | `0a877460` |
| P2 | `da944a9c` |

**Size** (field: `PVTSSF_lAHOAGPp9s4BQtJgzg-v31E`):
| Size | Option ID |
|------|-----------|
| XS | `6c6483d2` |
| S | `f784b110` |
| M | `7515a9f1` |
| L | `817d0097` |
| XL | `db339eb2` |

## Instructions

Parse the user's request from `$ARGUMENTS` and perform the appropriate operation below.

If no arguments are provided, list all board items grouped by status.

## Operations

### 1. Create Issue

Create a GitHub issue and add it to the project board. **Every issue MUST have a well-structured body.** Use the issue template in [issue-template.md](issue-template.md) to generate the body.

Before creating, you MUST:
1. Read relevant source files to understand the current state of the code
2. Identify the specific files that will need changes
3. Write concrete acceptance criteria (not vague descriptions)
4. Include technical context: what exists today, what needs to change, and why

```bash
# Create the issue with a structured body
gh issue create --repo johnlanda/mycelium --title "<title>" --body "<body>" [--label "<label>"]

# Add to project board
gh project item-add 3 --owner johnlanda --url <issue_url>

# Set fields (use item ID from previous command)
gh project item-edit --project-id PVT_kwHOAGPp9s4BQtJg --id <item_id> --field-id PVTSSF_lAHOAGPp9s4BQtJgzg-v3zc --single-select-option-id <status_option_id>
gh project item-edit --project-id PVT_kwHOAGPp9s4BQtJg --id <item_id> --field-id PVTSSF_lAHOAGPp9s4BQtJgzg-v31A --single-select-option-id <priority_option_id>
gh project item-edit --project-id PVT_kwHOAGPp9s4BQtJg --id <item_id> --field-id PVTSSF_lAHOAGPp9s4BQtJgzg-v31E --single-select-option-id <size_option_id>
```

Default status is **Backlog** unless specified. Always ask for priority if not provided.

### 2. List Items

```bash
gh project item-list 3 --owner johnlanda --format json
```

Display results as a formatted table grouped by status column. Show: title, priority, size, issue number.

### 3. Move Item

```bash
# Find the item ID
gh project item-list 3 --owner johnlanda --format json
# Update status
gh project item-edit --project-id PVT_kwHOAGPp9s4BQtJg --id <item_id> --field-id PVTSSF_lAHOAGPp9s4BQtJgzg-v3zc --single-select-option-id <status_option_id>
```

When moving to **Done**, also close the GitHub issue.

### 4. Update Item Fields

```bash
gh project item-edit --project-id PVT_kwHOAGPp9s4BQtJg --id <item_id> --field-id <field_id> --single-select-option-id <option_id>
```

### 5. Close Issue

```bash
gh issue close <number> --repo johnlanda/mycelium
gh project item-edit --project-id PVT_kwHOAGPp9s4BQtJg --id <item_id> --field-id PVTSSF_lAHOAGPp9s4BQtJgzg-v3zc --single-select-option-id 98236657
```

## Usage Patterns

| User says | Action |
|-----------|--------|
| `/kanban` | List all items grouped by status |
| `/kanban list` | List all items grouped by status |
| `/kanban list ready` | List items in Ready column |
| `/kanban create <title>` | Create issue with full context, ask for priority/size |
| `/kanban create <title> P0 M` | Create issue with priority P0, size M |
| `/kanban move #123 in progress` | Move issue #123 to In Progress |
| `/kanban move #123 done` | Move to Done and close the issue |
| `/kanban update #123 P0` | Update priority to P0 |
| `/kanban close #123` | Close issue and move to Done |
