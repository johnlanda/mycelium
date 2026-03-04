package cmd

import (
	"fmt"

	"github.com/johnlanda/mycelium/internal/embedder"
	"github.com/johnlanda/mycelium/internal/manifest"
	mcpserver "github.com/johnlanda/mycelium/internal/mcp"
	"github.com/johnlanda/mycelium/internal/store"
	"github.com/spf13/cobra"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the MCP server over stdio",
	RunE: func(cmd *cobra.Command, args []string) error {
		m, err := manifest.ParseFile("mycelium.toml")
		if err != nil {
			return fmt.Errorf("parse manifest: %w", err)
		}

		emb, err := embedder.NewEmbedder(m.Config.EmbeddingModel, embedder.Config{
			EmbeddingDimensions: m.Config.EmbeddingDimensions,
		})
		if err != nil {
			return fmt.Errorf("create embedder: %w", err)
		}

		st, err := store.NewLanceDBStore(cmd.Context(), store.DefaultStorePath(), emb.Dimensions())
		if err != nil {
			return fmt.Errorf("connect store: %w", err)
		}
		defer st.Close()

		srv := mcpserver.NewServer(st, emb)
		return srv.Serve(cmd.Context())
	},
}

func init() {
	rootCmd.AddCommand(serveCmd)
}
