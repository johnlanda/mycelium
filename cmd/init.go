package cmd

import "github.com/spf13/cobra"

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize mycelium.toml in the current directory",
	RunE: func(cmd *cobra.Command, args []string) error {
		cmd.Println("not yet implemented")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(initCmd)
}
