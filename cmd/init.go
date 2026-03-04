package cmd

import (
	"fmt"
	"os"

	"github.com/johnlanda/mycelium/internal/manifest"
	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize mycelium.toml in the current directory",
	Args:  cobra.NoArgs,
	RunE:  runInit,
}

func init() {
	initCmd.Flags().String("model", "voyage-code-2", "embedding model to use")
	rootCmd.AddCommand(initCmd)
}

func runInit(cmd *cobra.Command, args []string) error {
	const path = "mycelium.toml"

	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("mycelium.toml already exists")
	}

	model, _ := cmd.Flags().GetString("model")

	m := &manifest.Manifest{
		Config: manifest.Config{
			EmbeddingModel: model,
		},
	}

	if err := m.WriteFile(path); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}

	cmd.Println("Initialized mycelium.toml")
	return nil
}
