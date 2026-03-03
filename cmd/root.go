package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var version = "v0.1.0-dev"

var rootCmd = &cobra.Command{
	Use:     "mctl",
	Short:   "Mycelium — reproducible dependency context for AI coding agents",
	Version: version,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
