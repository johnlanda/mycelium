package cmd

import (
	"fmt"
	"os"

	"github.com/johnlanda/mycelium/internal/pipeline"
	"github.com/spf13/cobra"
)

var upCmd = &cobra.Command{
	Use:   "up",
	Short: "Sync the local vector store with resolved dependencies",
	RunE: func(cmd *cobra.Command, args []string) error {
		storePath := os.Getenv("MYCELIUM_STORE_DIR")

		results, err := pipeline.Sync(cmd.Context(), pipeline.SyncOptions{
			StorePath: storePath,
			Output:    cmd.OutOrStdout(),
		})
		if err != nil {
			return err
		}

		var synced, skipped, failed int
		for _, r := range results {
			switch r.Status {
			case "synced":
				synced++
			case "skipped":
				skipped++
			case "error":
				failed++
			}
		}

		fmt.Fprintf(cmd.OutOrStdout(), "\nDone: %d synced, %d skipped, %d failed\n", synced, skipped, failed)

		if failed > 0 {
			return fmt.Errorf("%d dependencies failed to sync", failed)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(upCmd)
}
