package cmd

import "github.com/spf13/cobra"

var addCmd = &cobra.Command{
	Use:   "add",
	Short: "Add a dependency to mycelium.toml",
	RunE: func(cmd *cobra.Command, args []string) error {
		cmd.Println("not yet implemented")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(addCmd)
}
