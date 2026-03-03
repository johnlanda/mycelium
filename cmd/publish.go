package cmd

import "github.com/spf13/cobra"

var publishCmd = &cobra.Command{
	Use:   "publish",
	Short: "Publish embedding artifacts to a registry",
	RunE: func(cmd *cobra.Command, args []string) error {
		cmd.Println("not yet implemented")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(publishCmd)
}
