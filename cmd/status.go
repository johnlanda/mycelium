package cmd

import "github.com/spf13/cobra"

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show sync status of dependencies and vector store",
	RunE: func(cmd *cobra.Command, args []string) error {
		cmd.Println("not yet implemented")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(statusCmd)
}
