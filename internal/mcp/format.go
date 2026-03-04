// Package mcp implements the MCP server exposing semantic search over the vector store.
package mcp

import (
	"fmt"
	"strings"

	"github.com/johnlanda/mycelium/internal/store"
)

// formatSearchResults formats search results for LLM consumption.
func formatSearchResults(results []store.SearchResult) string {
	if len(results) == 0 {
		return "No results found."
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Found %d results:\n", len(results))

	for i, r := range results {
		fmt.Fprintf(&sb, "\n--- Result %d (score: %.3f) ---\n", i+1, r.Score)
		if r.Chunk.SourceVersion != "" {
			fmt.Fprintf(&sb, "Source: %s @ %s\n", r.Chunk.Source, r.Chunk.SourceVersion)
		} else {
			fmt.Fprintf(&sb, "Source: %s\n", r.Chunk.Source)
		}
		fmt.Fprintf(&sb, "Path: %s\n", r.Chunk.Path)
		if r.Chunk.Breadcrumb != "" {
			fmt.Fprintf(&sb, "Section: %s\n", r.Chunk.Breadcrumb)
		}
		fmt.Fprintf(&sb, "Type: %s\n", r.Chunk.ChunkType)
		if r.Chunk.Language != "" {
			fmt.Fprintf(&sb, "Language: %s\n", r.Chunk.Language)
		}
		fmt.Fprintf(&sb, "\n%s\n", r.Chunk.Text)
	}

	return sb.String()
}

// formatSourceList formats the indexed source list for LLM consumption.
func formatSourceList(sources []store.SourceInfo) string {
	if len(sources) == 0 {
		return "No indexed sources."
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Indexed sources (%d):\n", len(sources))

	for _, s := range sources {
		if s.SourceVersion != "" {
			fmt.Fprintf(&sb, "\n  %s @ %s\n", s.Source, s.SourceVersion)
		} else {
			fmt.Fprintf(&sb, "\n  %s\n", s.Source)
		}
		fmt.Fprintf(&sb, "    Chunks: %d\n", s.ChunkCount)
		fmt.Fprintf(&sb, "    Store Key: %s\n", s.StoreKey)
	}

	return sb.String()
}
