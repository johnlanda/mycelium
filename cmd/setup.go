package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Configure Claude Code integration (skill, hook, MCP server)",
	Args:  cobra.NoArgs,
	RunE:  runSetup,
}

func init() {
	setupCmd.Flags().String("mctl-path", "mctl", "path to the mctl binary")
	rootCmd.AddCommand(setupCmd)
}

func runSetup(cmd *cobra.Command, args []string) error {
	mctlPath, _ := cmd.Flags().GetString("mctl-path")

	// Step 1: Write skill file (always overwrite — mctl owns this content).
	action, err := writeSkillFile(mctlPath)
	if err != nil {
		return fmt.Errorf("write skill file: %w", err)
	}
	cmd.Printf("  %s .claude/skills/mycelium/SKILL.md\n", action)

	// Step 2: Merge .claude/settings.json — add hook if not present.
	action, err = mergeSettingsJSON()
	if err != nil {
		return fmt.Errorf("merge settings.json: %w", err)
	}
	cmd.Printf("  %s .claude/settings.json\n", action)

	// Step 3: Merge .mcp.json — add/overwrite mycelium server entry.
	action, err = mergeMCPJSON(mctlPath)
	if err != nil {
		return fmt.Errorf("merge .mcp.json: %w", err)
	}
	cmd.Printf("  %s .mcp.json\n", action)

	cmd.Println("\nClaude Code integration configured. Run 'mctl up' to sync your store, then start a Claude Code session.")
	return nil
}

const hookCommand = `echo 'You have access to a Mycelium MCP server with semantic search over indexed documentation and code. Use the search and search_code tools to find relevant information before making changes. Call list_sources to see what is available.'`

func skillContent(mctlPath string) string {
	return fmt.Sprintf(`---
name: mycelium
description: Search indexed documentation and code via the Mycelium MCP server and CLI.
argument-hint: "[search <query>|status|help]"
allowed-tools: mcp__mycelium__search, mcp__mycelium__search_code, mcp__mycelium__list_sources, Bash(%s *)
---

# Mycelium — Dependency Context for AI Agents

Mycelium indexes documentation and source code into a local vector store.
Use the MCP tools for semantic search and the CLI for lifecycle management.

## MCP Tools

| Tool | Purpose |
|------|---------|
| search | Semantic search across all indexed documentation |
| search_code | Semantic search scoped to indexed source code |
| list_sources | List all indexed sources with their versions and status |

**Always call list_sources first** to see what is available, then use search or search_code with specific queries.

## CLI Commands

| Command | Description |
|---------|-------------|
| %s init | Initialize mycelium.toml in the current directory |
| %s add <source> | Add a dependency to the manifest |
| %s up | Fetch, chunk, embed, and store all dependencies |
| %s status | Show sync status of all dependencies |
| %s upgrade [dep] | Upgrade dependencies to latest compatible versions |
| %s serve | Start the MCP server (stdio) |
| %s setup | Configure Claude Code integration |

## Manifest Format (mycelium.toml)

` + "```toml" + `
[config]
embedding_model = "voyage-code-2"

[[sources]]
name = "my-lib"
source = "github:org/repo"
version = "v1.0.0"
paths = ["docs/**/*.md", "src/**/*.go"]
` + "```" + `

## Common Workflows

1. **Before making changes** — search for relevant docs:
   - Call list_sources to see indexed dependencies
   - Use search with a description of what you want to change
   - Use search_code for implementation patterns

2. **Add a new dependency**:
   ` + "```bash" + `
   %s add github:org/repo --version v1.0.0 --paths "docs/**/*.md"
   %s up
   ` + "```" + `

3. **Check sync status**:
   ` + "```bash" + `
   %s status
   ` + "```" + `
`, mctlPath,
		mctlPath, mctlPath, mctlPath, mctlPath, mctlPath, mctlPath, mctlPath,
		mctlPath, mctlPath,
		mctlPath)
}

func writeSkillFile(mctlPath string) (string, error) {
	dir := filepath.Join(".claude", "skills", "mycelium")
	path := filepath.Join(dir, "SKILL.md")

	action := "created"
	if _, err := os.Stat(path); err == nil {
		action = "updated"
	}

	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", dir, err)
	}

	if err := os.WriteFile(path, []byte(skillContent(mctlPath)), 0644); err != nil {
		return "", fmt.Errorf("write %s: %w", path, err)
	}
	return action, nil
}

func mergeSettingsJSON() (string, error) {
	const path = ".claude/settings.json"

	action := "created"
	data := make(map[string]any)

	existing, err := os.ReadFile(path)
	if err == nil {
		action = "updated"
		if err := json.Unmarshal(existing, &data); err != nil {
			return "", fmt.Errorf("parse existing %s: %w", path, err)
		}
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("read %s: %w", path, err)
	}

	// Ensure hooks.UserPromptSubmit exists and contains our entry.
	hooks, _ := data["hooks"].(map[string]any)
	if hooks == nil {
		hooks = make(map[string]any)
	}

	entries, _ := hooks["UserPromptSubmit"].([]any)

	// Check if our hook is already present.
	found := false
	for _, e := range entries {
		entry, ok := e.(map[string]any)
		if !ok {
			continue
		}
		if cmd, _ := entry["command"].(string); cmd == hookCommand {
			found = true
			break
		}
	}

	if !found {
		entries = append(entries, map[string]any{
			"matcher": "",
			"command": hookCommand,
		})
		hooks["UserPromptSubmit"] = entries
		data["hooks"] = hooks
	}

	if err := os.MkdirAll(".claude", 0755); err != nil {
		return "", fmt.Errorf("mkdir .claude: %w", err)
	}

	out, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal settings: %w", err)
	}

	if err := os.WriteFile(path, append(out, '\n'), 0644); err != nil {
		return "", fmt.Errorf("write %s: %w", path, err)
	}
	return action, nil
}

func mergeMCPJSON(mctlPath string) (string, error) {
	const path = ".mcp.json"

	action := "created"
	data := make(map[string]any)

	existing, err := os.ReadFile(path)
	if err == nil {
		action = "updated"
		if err := json.Unmarshal(existing, &data); err != nil {
			return "", fmt.Errorf("parse existing %s: %w", path, err)
		}
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("read %s: %w", path, err)
	}

	servers, _ := data["mcpServers"].(map[string]any)
	if servers == nil {
		servers = make(map[string]any)
	}

	servers["mycelium"] = map[string]any{
		"command": mctlPath,
		"args":    []any{"serve"},
	}
	data["mcpServers"] = servers

	out, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal .mcp.json: %w", err)
	}

	if err := os.WriteFile(path, append(out, '\n'), 0644); err != nil {
		return "", fmt.Errorf("write %s: %w", path, err)
	}
	return action, nil
}
