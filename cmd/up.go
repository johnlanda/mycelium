package cmd

import "github.com/spf13/cobra"

var upCmd = &cobra.Command{
	Use:   "up",
	Short: "Sync the local vector store with resolved dependencies",
	RunE: func(cmd *cobra.Command, args []string) error {
		cmd.Println("not yet implemented")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(upCmd)
}
