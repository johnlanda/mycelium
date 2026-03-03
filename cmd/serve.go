package cmd

import "github.com/spf13/cobra"

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the MCP server over stdio",
	RunE: func(cmd *cobra.Command, args []string) error {
		cmd.Println("not yet implemented")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(serveCmd)
}
