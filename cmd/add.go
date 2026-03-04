package cmd

import (
	"fmt"
	"strings"

	"github.com/johnlanda/mycelium/internal/manifest"
	"github.com/spf13/cobra"
)

var addCmd = &cobra.Command{
	Use:   "add <source@ref>",
	Short: "Add a dependency to mycelium.toml",
	Args:  cobra.ExactArgs(1),
	RunE:  runAdd,
}

func init() {
	addCmd.Flags().StringSlice("docs", nil, "documentation paths to index")
	addCmd.Flags().StringSlice("code", nil, "code paths to index")
	addCmd.Flags().String("id", "", "dependency ID (default: last path segment of source)")
	rootCmd.AddCommand(addCmd)
}

func runAdd(cmd *cobra.Command, args []string) error {
	raw := args[0]

	// Split on last '@' to separate source and ref.
	idx := strings.LastIndex(raw, "@")
	if idx < 0 || idx == len(raw)-1 {
		return fmt.Errorf("source must include a ref: <source>@<ref>")
	}
	source := raw[:idx]
	ref := raw[idx+1:]

	id, _ := cmd.Flags().GetString("id")
	if id == "" {
		// Auto-generate from last path segment.
		parts := strings.Split(strings.TrimSuffix(source, "/"), "/")
		id = parts[len(parts)-1]
	}

	docs, _ := cmd.Flags().GetStringSlice("docs")
	code, _ := cmd.Flags().GetStringSlice("code")

	const path = "mycelium.toml"
	m, err := manifest.ParseFile(path)
	if err != nil {
		return fmt.Errorf("read manifest: %w", err)
	}

	// Check for duplicate ID.
	for _, dep := range m.Dependencies {
		if dep.ID == id {
			return fmt.Errorf("dependency %q already exists", id)
		}
	}

	dep := manifest.Dependency{
		ID:     id,
		Source: source,
		Ref:    ref,
		Docs:   docs,
		Code:   code,
	}
	m.Dependencies = append(m.Dependencies, dep)

	if err := m.WriteFile(path); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}

	cmd.Printf("Added %s (%s@%s)\n", id, source, ref)
	return nil
}
