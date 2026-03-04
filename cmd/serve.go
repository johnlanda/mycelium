package cmd

import (
	"fmt"
	"os"

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

		emb, err := embedder.NewEmbedder(m.Config.EmbeddingModel, embedder.Config{})
		if err != nil {
			return fmt.Errorf("create embedder: %w", err)
		}

		host := os.Getenv("WEAVIATE_URL")
		if host == "" {
			host = "localhost:8080"
		}

		st, err := store.NewWeaviateStore(cmd.Context(), host)
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
