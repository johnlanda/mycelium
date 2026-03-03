package cmd

import "github.com/spf13/cobra"

var upgradeCmd = &cobra.Command{
	Use:   "upgrade",
	Short: "Upgrade a dependency to a newer version",
	RunE: func(cmd *cobra.Command, args []string) error {
		cmd.Println("not yet implemented")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(upgradeCmd)
}
