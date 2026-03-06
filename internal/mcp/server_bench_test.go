package mcp

import (
	"context"
	"fmt"
	"testing"

	"github.com/johnlanda/mycelium/internal/store"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func newBenchServer(withCache bool) (*Server, *mockEmbedder, *mockStore) {
	ms := &mockStore{
		searchResults: []store.SearchResult{
			{
				Chunk: store.StoredChunk{
					Text:          "Install with brew install envoy-gateway",
					Breadcrumb:    "Guide > Install",
					ChunkType:     "doc",
					Path:          "docs/guide.md",
					Source:        "envoy",
					SourceVersion: "v1.0",
				},
				Score: 0.92,
			},
			{
				Chunk: store.StoredChunk{
					Text:          "Configure the gateway with GatewayClass resource",
					Breadcrumb:    "Guide > Configure",
					ChunkType:     "doc",
					Path:          "docs/configure.md",
					Source:        "envoy",
					SourceVersion: "v1.0",
				},
				Score: 0.85,
			},
			{
				Chunk: store.StoredChunk{
					Text:          "func main() {\n\tgateway.Run()\n}",
					ChunkType:     "code",
					Path:          "cmd/main.go",
					Source:        "envoy",
					SourceVersion: "v1.0",
					Language:      "go",
				},
				Score: 0.78,
			},
		},
		sources: []store.SourceInfo{
			{Source: "envoy", SourceVersion: "v1.0", StoreKey: "sha256:abc123", ChunkCount: 450},
			{Source: "istio", SourceVersion: "v1.2", StoreKey: "sha256:def456", ChunkCount: 320},
		},
	}
	me := &mockEmbedder{vectors: [][]float32{{0.1, 0.2, 0.3}}}

	var opts []ServerOption
	if withCache {
		opts = append(opts, WithCache(CacheConfig{}))
	}
	srv := NewServer(ms, me, opts...)
	return srv, me, ms
}

// BenchmarkHandleSearch_ColdCache measures the full pipeline: embed + search + format.
// No result cache is configured.
func BenchmarkHandleSearch_ColdCache(b *testing.B) {
	srv, _, _ := newBenchServer(false)
	ctx := context.Background()
	req := &sdkmcp.CallToolRequest{}
	input := SearchInput{Query: "install envoy gateway"}

	b.ResetTimer()
	for b.Loop() {
		srv.handleSearch(ctx, req, input)
	}
}

// BenchmarkHandleSearch_ResultCacheHit measures a result cache hit.
// Skips embedding, vector search, and formatting entirely.
func BenchmarkHandleSearch_ResultCacheHit(b *testing.B) {
	srv, _, _ := newBenchServer(true)
	ctx := context.Background()
	req := &sdkmcp.CallToolRequest{}
	input := SearchInput{Query: "install envoy gateway"}

	// Prime the cache.
	srv.handleSearch(ctx, req, input)

	b.ResetTimer()
	for b.Loop() {
		srv.handleSearch(ctx, req, input)
	}
}

// BenchmarkHandleSearch_ColdCacheWithCache measures the full pipeline with cache enabled
// but every query is unique (cache miss every time). This shows the overhead of cache
// key computation + failed lookup + cache store on the hot path.
func BenchmarkHandleSearch_ColdCacheWithCache(b *testing.B) {
	srv, _, _ := newBenchServer(true)
	ctx := context.Background()
	req := &sdkmcp.CallToolRequest{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		input := SearchInput{Query: fmt.Sprintf("unique query %d", i)}
		srv.handleSearch(ctx, req, input)
	}
}

// BenchmarkHandleSearchCode_ResultCacheHit measures a result cache hit for search_code.
func BenchmarkHandleSearchCode_ResultCacheHit(b *testing.B) {
	srv, _, _ := newBenchServer(true)
	ctx := context.Background()
	req := &sdkmcp.CallToolRequest{}
	input := SearchCodeInput{Query: "main function"}

	// Prime the cache.
	srv.handleSearchCode(ctx, req, input)

	b.ResetTimer()
	for b.Loop() {
		srv.handleSearchCode(ctx, req, input)
	}
}

// BenchmarkHandleSearchCode_ColdCache measures the full pipeline for search_code
// without any caching.
func BenchmarkHandleSearchCode_ColdCache(b *testing.B) {
	srv, _, _ := newBenchServer(false)
	ctx := context.Background()
	req := &sdkmcp.CallToolRequest{}
	input := SearchCodeInput{Query: "main function"}

	b.ResetTimer()
	for b.Loop() {
		srv.handleSearchCode(ctx, req, input)
	}
}

// BenchmarkHandleListSources_ResultCacheHit measures a result cache hit for list_sources.
func BenchmarkHandleListSources_ResultCacheHit(b *testing.B) {
	srv, _, _ := newBenchServer(true)
	ctx := context.Background()
	req := &sdkmcp.CallToolRequest{}

	// Prime the cache.
	srv.handleListSources(ctx, req, ListSourcesInput{})

	b.ResetTimer()
	for b.Loop() {
		srv.handleListSources(ctx, req, ListSourcesInput{})
	}
}

// BenchmarkHandleListSources_ColdCache measures list_sources without caching.
func BenchmarkHandleListSources_ColdCache(b *testing.B) {
	srv, _, _ := newBenchServer(false)
	ctx := context.Background()
	req := &sdkmcp.CallToolRequest{}

	b.ResetTimer()
	for b.Loop() {
		srv.handleListSources(ctx, req, ListSourcesInput{})
	}
}

// BenchmarkSearchKeyComputation measures the overhead of cache key generation.
func BenchmarkSearchKeyComputation(b *testing.B) {
	for b.Loop() {
		searchKey("install envoy gateway", "envoy", "doc", 5)
	}
}

// BenchmarkSearchCodeKeyComputation measures cache key generation for search_code.
func BenchmarkSearchCodeKeyComputation(b *testing.B) {
	for b.Loop() {
		searchCodeKey("main function", "envoy", "go", 5)
	}
}
