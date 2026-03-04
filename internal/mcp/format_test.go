package mcp

import (
	"strings"
	"testing"

	"github.com/johnlanda/mycelium/internal/store"
)

func TestFormatSearchResults(t *testing.T) {
	tests := []struct {
		name     string
		results  []store.SearchResult
		contains []string
	}{
		{
			name:     "empty results",
			results:  nil,
			contains: []string{"No results found."},
		},
		{
			name: "single doc result",
			results: []store.SearchResult{
				{
					Chunk: store.StoredChunk{
						Text:          "Install with brew install foo",
						Breadcrumb:    "Guide > Installation > macOS",
						ChunkType:     "doc",
						Path:          "docs/guide.md",
						Source:        "envoy-gateway",
						SourceVersion: "v1.3.0",
					},
					Score: 0.923,
				},
			},
			contains: []string{
				"Found 1 results:",
				"Result 1 (score: 0.923)",
				"Source: envoy-gateway @ v1.3.0",
				"Path: docs/guide.md",
				"Section: Guide > Installation > macOS",
				"Type: doc",
				"Install with brew install foo",
			},
		},
		{
			name: "code result with language",
			results: []store.SearchResult{
				{
					Chunk: store.StoredChunk{
						Text:          "func main() {}",
						Breadcrumb:    "main.go",
						ChunkType:     "code",
						Path:          "main.go",
						Source:        "test/repo",
						SourceVersion: "v2.0.0",
						Language:      "go",
					},
					Score: 0.85,
				},
			},
			contains: []string{
				"Type: code",
				"Language: go",
				"func main() {}",
			},
		},
		{
			name: "multiple results",
			results: []store.SearchResult{
				{
					Chunk: store.StoredChunk{Text: "first", ChunkType: "doc", Source: "a", Path: "a.md"},
					Score: 0.9,
				},
				{
					Chunk: store.StoredChunk{Text: "second", ChunkType: "code", Source: "b", Path: "b.go"},
					Score: 0.8,
				},
			},
			contains: []string{
				"Found 2 results:",
				"Result 1",
				"Result 2",
			},
		},
		{
			name: "source without version",
			results: []store.SearchResult{
				{
					Chunk: store.StoredChunk{Text: "content", ChunkType: "doc", Source: "local", Path: "README.md"},
					Score: 0.7,
				},
			},
			contains: []string{"Source: local\n"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatSearchResults(tt.results)
			for _, want := range tt.contains {
				if !strings.Contains(got, want) {
					t.Errorf("output missing %q\ngot:\n%s", want, got)
				}
			}
		})
	}
}

func TestFormatSourceList(t *testing.T) {
	tests := []struct {
		name     string
		sources  []store.SourceInfo
		contains []string
	}{
		{
			name:     "empty sources",
			sources:  nil,
			contains: []string{"No indexed sources."},
		},
		{
			name: "single source",
			sources: []store.SourceInfo{
				{
					Source:        "envoy-gateway",
					SourceVersion: "v1.3.0",
					StoreKey:      "sha256:abc123",
					ChunkCount:   45,
				},
			},
			contains: []string{
				"Indexed sources (1):",
				"envoy-gateway @ v1.3.0",
				"Chunks: 45",
				"Store Key: sha256:abc123",
			},
		},
		{
			name: "multiple sources",
			sources: []store.SourceInfo{
				{Source: "repo-a", SourceVersion: "v1.0", StoreKey: "sha256:aaa", ChunkCount: 10},
				{Source: "repo-b", SourceVersion: "v2.0", StoreKey: "sha256:bbb", ChunkCount: 20},
			},
			contains: []string{
				"Indexed sources (2):",
				"repo-a @ v1.0",
				"repo-b @ v2.0",
			},
		},
		{
			name: "source without version",
			sources: []store.SourceInfo{
				{Source: "local", StoreKey: "sha256:xxx", ChunkCount: 5},
			},
			contains: []string{"  local\n"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatSourceList(tt.sources)
			for _, want := range tt.contains {
				if !strings.Contains(got, want) {
					t.Errorf("output missing %q\ngot:\n%s", want, got)
				}
			}
		})
	}
}
