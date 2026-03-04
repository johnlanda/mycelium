package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/johnlanda/mycelium/internal/lockfile"
	"github.com/johnlanda/mycelium/internal/manifest"
	"github.com/johnlanda/mycelium/internal/pipeline"
	"github.com/spf13/cobra"
)

var upgradeCmd = &cobra.Command{
	Use:   "upgrade <id[@version]>",
	Short: "Upgrade a dependency to a newer version",
	Args:  cobra.ExactArgs(1),
	RunE:  runUpgrade,
}

func init() {
	rootCmd.AddCommand(upgradeCmd)
}

func runUpgrade(cmd *cobra.Command, args []string) error {
	raw := args[0]

	// Split on '@' to get dep ID and optional new version.
	var depID, newVersion string
	if idx := strings.Index(raw, "@"); idx >= 0 {
		depID = raw[:idx]
		newVersion = raw[idx+1:]
	} else {
		depID = raw
	}

	m, err := manifest.ParseFile("mycelium.toml")
	if err != nil {
		return fmt.Errorf("read manifest: %w", err)
	}

	// Find the dependency by ID.
	depIdx := -1
	for i, dep := range m.Dependencies {
		if dep.ID == depID {
			depIdx = i
			break
		}
	}
	if depIdx < 0 {
		return fmt.Errorf("dependency %q not found in manifest", depID)
	}

	oldRef := m.Dependencies[depIdx].Ref

	// If a new version was specified, update the ref.
	if newVersion != "" {
		m.Dependencies[depIdx].Ref = newVersion
	}

	// Read lockfile (or create empty if missing).
	var lf *lockfile.Lockfile
	if _, statErr := os.Stat("mycelium.lock"); os.IsNotExist(statErr) {
		lf = lockfile.New()
	} else {
		lf, err = lockfile.ReadFile("mycelium.lock")
		if err != nil {
			return fmt.Errorf("read lockfile: %w", err)
		}
	}

	oldStoreKey := ""
	if sl, ok := lf.Sources[depID]; ok {
		oldStoreKey = sl.StoreKey
	}

	storePath := os.Getenv("MYCELIUM_STORE_DIR")

	newLock, err := pipeline.UpgradeDependency(cmd.Context(), m.Dependencies[depIdx], m.Config.EmbeddingModel, oldStoreKey, pipeline.SyncOptions{
		ManifestPath: "mycelium.toml",
		LockfilePath: "mycelium.lock",
		StorePath:    storePath,
		Output:       cmd.OutOrStdout(),
	})
	if err != nil {
		return fmt.Errorf("upgrade %s: %w", depID, err)
	}

	// Update lockfile with new lock.
	lf.SetSource(depID, *newLock)
	if err := lf.WriteFile("mycelium.lock"); err != nil {
		return fmt.Errorf("write lockfile: %w", err)
	}

	// Update manifest if ref changed.
	if err := m.WriteFile("mycelium.toml"); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}

	newRef := m.Dependencies[depIdx].Ref
	if oldRef != newRef {
		cmd.Printf("Upgraded %s: %s → %s\n", depID, oldRef, newRef)
	} else {
		cmd.Printf("Upgraded %s: %s (re-synced)\n", depID, newRef)
	}
	return nil
}
