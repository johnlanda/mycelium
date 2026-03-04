package cmd

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/johnlanda/mycelium/internal/lockfile"
	"github.com/johnlanda/mycelium/internal/manifest"
	"github.com/johnlanda/mycelium/internal/store"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show sync status of dependencies and vector store",
	Args:  cobra.NoArgs,
	RunE:  runStatus,
}

func init() {
	rootCmd.AddCommand(statusCmd)
}

func runStatus(cmd *cobra.Command, args []string) error {
	m, err := manifest.ParseFile("mycelium.toml")
	if err != nil {
		return fmt.Errorf("read manifest: %w", err)
	}

	var lf *lockfile.Lockfile
	if _, statErr := os.Stat("mycelium.lock"); os.IsNotExist(statErr) {
		lf = lockfile.New()
	} else {
		lf, err = lockfile.ReadFile("mycelium.lock")
		if err != nil {
			return fmt.Errorf("read lockfile: %w", err)
		}
	}

	// Try connecting to the store; if unavailable, we'll show "unknown" for store status.
	var st store.Store
	st, storeErr := store.NewLanceDBStore(cmd.Context(), store.DefaultStorePath(), 0)
	if storeErr == nil {
		defer st.Close()
	}

	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "ID\tVersion\tStatus")

	for _, dep := range m.Dependencies {
		sl, inLock := lf.Sources[dep.ID]
		status := depStatus(cmd.Context(), inLock, sl, st)
		version := dep.Ref
		if inLock && sl.Version != "" {
			version = sl.Version
		}
		fmt.Fprintf(w, "%s\t%s\t%s\n", dep.ID, version, status)
	}

	w.Flush()
	return nil
}

func depStatus(ctx context.Context, inLock bool, sl lockfile.SourceLock, st store.Store) string {
	if !inLock {
		return "not synced"
	}
	if st == nil {
		return "unknown"
	}
	has, err := st.HasKey(ctx, sl.StoreKey)
	if err != nil {
		return "unknown"
	}
	if !has {
		return "store missing"
	}
	return "synced"
}
