package mcp

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/johnlanda/mycelium/internal/store"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// mockEmbedder returns fixed vectors and tracks received texts.
type mockEmbedder struct {
	vectors   [][]float32
	err       error
	texts     []string
	embedCalls int
}

func (m *mockEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	m.embedCalls++
	m.texts = append(m.texts, texts...)
	if m.err != nil {
		return nil, m.err
	}
	return m.vectors, nil
}

func (m *mockEmbedder) ModelID() string { return "mock" }
func (m *mockEmbedder) Dimensions() int { return 3 }

// mockStore returns canned results and tracks received options.
type mockStore struct {
	searchResults []store.SearchResult
	searchErr     error
	searchOpts    store.SearchOpts
	searchCalls   int

	sources      []store.SourceInfo
	sourcesErr   error
	sourcesCalls int
}

func (m *mockStore) Search(_ context.Context, _ []float32, opts store.SearchOpts) ([]store.SearchResult, error) {
	m.searchCalls++
	m.searchOpts = opts
	if m.searchErr != nil {
		return nil, m.searchErr
	}
	return m.searchResults, nil
}

func (m *mockStore) ListSources(_ context.Context) ([]store.SourceInfo, error) {
	m.sourcesCalls++
	if m.sourcesErr != nil {
		return nil, m.sourcesErr
	}
	return m.sources, nil
}

func (m *mockStore) Upsert(_ context.Context, _ string, _ []store.StoredChunk) error { return nil }
func (m *mockStore) Delete(_ context.Context, _ string) error                        { return nil }
func (m *mockStore) HasKey(_ context.Context, _ string) (bool, error)                { return false, nil }
func (m *mockStore) Close() error                                                    { return nil }

func TestHandleSearch(t *testing.T) {
	ctx := context.Background()
	ms := &mockStore{
		searchResults: []store.SearchResult{
			{
				Chunk: store.StoredChunk{
					Text:          "Install with brew",
					Breadcrumb:    "Guide > Install",
					ChunkType:     "doc",
					Path:          "docs/guide.md",
					Source:        "envoy",
					SourceVersion: "v1.0",
				},
				Score: 0.9,
			},
		},
	}
	me := &mockEmbedder{vectors: [][]float32{{0.1, 0.2, 0.3}}}
	srv := NewServer(ms, me)

	t.Run("basic search", func(t *testing.T) {
		result, _, err := srv.handleSearch(ctx, &sdkmcp.CallToolRequest{}, SearchInput{Query: "install"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		text := extractText(t, result)
		if !strings.Contains(text, "Install with brew") {
			t.Errorf("expected result text in output, got: %s", text)
		}
		if !strings.Contains(text, "envoy @ v1.0") {
			t.Errorf("expected source in output, got: %s", text)
		}
		if me.texts[0] != "install" {
			t.Errorf("expected embed text 'install', got %q", me.texts[0])
		}
	})

	t.Run("default topK", func(t *testing.T) {
		srv.handleSearch(ctx, &sdkmcp.CallToolRequest{}, SearchInput{Query: "test"})
		if ms.searchOpts.TopK != defaultTopK {
			t.Errorf("expected TopK=%d, got %d", defaultTopK, ms.searchOpts.TopK)
		}
	})

	t.Run("custom topK", func(t *testing.T) {
		k := 10
		srv.handleSearch(ctx, &sdkmcp.CallToolRequest{}, SearchInput{Query: "test", TopK: &k})
		if ms.searchOpts.TopK != 10 {
			t.Errorf("expected TopK=10, got %d", ms.searchOpts.TopK)
		}
	})

	t.Run("source filter", func(t *testing.T) {
		src := "envoy"
		srv.handleSearch(ctx, &sdkmcp.CallToolRequest{}, SearchInput{Query: "test", Source: &src})
		if ms.searchOpts.Source != "envoy" {
			t.Errorf("expected Source='envoy', got %q", ms.searchOpts.Source)
		}
	})

	t.Run("type filter maps to ChunkType", func(t *testing.T) {
		typ := "doc"
		srv.handleSearch(ctx, &sdkmcp.CallToolRequest{}, SearchInput{Query: "test", Type: &typ})
		if ms.searchOpts.ChunkType != "doc" {
			t.Errorf("expected ChunkType='doc', got %q", ms.searchOpts.ChunkType)
		}
	})
}

func TestHandleSearchCode(t *testing.T) {
	ctx := context.Background()
	ms := &mockStore{
		searchResults: []store.SearchResult{
			{
				Chunk: store.StoredChunk{
					Text:      "func main() {}",
					ChunkType: "code",
					Path:      "main.go",
					Source:    "repo",
					Language:  "go",
				},
				Score: 0.85,
			},
		},
	}
	me := &mockEmbedder{vectors: [][]float32{{0.1, 0.2, 0.3}}}
	srv := NewServer(ms, me)

	t.Run("always sets ChunkType to code", func(t *testing.T) {
		srv.handleSearchCode(ctx, &sdkmcp.CallToolRequest{}, SearchCodeInput{Query: "main"})
		if ms.searchOpts.ChunkType != "code" {
			t.Errorf("expected ChunkType='code', got %q", ms.searchOpts.ChunkType)
		}
	})

	t.Run("passes language filter", func(t *testing.T) {
		lang := "go"
		srv.handleSearchCode(ctx, &sdkmcp.CallToolRequest{}, SearchCodeInput{Query: "main", Language: &lang})
		if ms.searchOpts.Language != "go" {
			t.Errorf("expected Language='go', got %q", ms.searchOpts.Language)
		}
	})

	t.Run("result includes language", func(t *testing.T) {
		result, _, err := srv.handleSearchCode(ctx, &sdkmcp.CallToolRequest{}, SearchCodeInput{Query: "main"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		text := extractText(t, result)
		if !strings.Contains(text, "Language: go") {
			t.Errorf("expected Language in output, got: %s", text)
		}
	})
}

func TestHandleListSources(t *testing.T) {
	ctx := context.Background()

	t.Run("returns formatted sources", func(t *testing.T) {
		ms := &mockStore{
			sources: []store.SourceInfo{
				{Source: "envoy", SourceVersion: "v1.3.0", StoreKey: "sha256:abc", ChunkCount: 45},
			},
		}
		srv := NewServer(ms, &mockEmbedder{vectors: [][]float32{{0.1, 0.2, 0.3}}})

		result, _, err := srv.handleListSources(ctx, &sdkmcp.CallToolRequest{}, ListSourcesInput{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		text := extractText(t, result)
		if !strings.Contains(text, "envoy @ v1.3.0") {
			t.Errorf("expected source in output, got: %s", text)
		}
		if !strings.Contains(text, "Chunks: 45") {
			t.Errorf("expected chunk count in output, got: %s", text)
		}
	})

	t.Run("empty sources", func(t *testing.T) {
		ms := &mockStore{}
		srv := NewServer(ms, &mockEmbedder{vectors: [][]float32{{0.1, 0.2, 0.3}}})

		result, _, err := srv.handleListSources(ctx, &sdkmcp.CallToolRequest{}, ListSourcesInput{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		text := extractText(t, result)
		if text != "No indexed sources." {
			t.Errorf("expected 'No indexed sources.', got: %s", text)
		}
	})
}

func TestHandleSearchEmbedError(t *testing.T) {
	ctx := context.Background()
	ms := &mockStore{}
	me := &mockEmbedder{err: fmt.Errorf("API key invalid")}
	srv := NewServer(ms, me)

	result, _, err := srv.handleSearch(ctx, &sdkmcp.CallToolRequest{}, SearchInput{Query: "test"})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError=true for embed failure")
	}
	text := extractText(t, result)
	if !strings.Contains(text, "API key invalid") {
		t.Errorf("expected error message in output, got: %s", text)
	}
}

func TestHandleSearchStoreError(t *testing.T) {
	ctx := context.Background()
	ms := &mockStore{searchErr: fmt.Errorf("connection refused")}
	me := &mockEmbedder{vectors: [][]float32{{0.1, 0.2, 0.3}}}
	srv := NewServer(ms, me)

	result, _, err := srv.handleSearch(ctx, &sdkmcp.CallToolRequest{}, SearchInput{Query: "test"})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError=true for store error")
	}
	text := extractText(t, result)
	if !strings.Contains(text, "connection refused") {
		t.Errorf("expected error message in output, got: %s", text)
	}
}

func TestHandleListSourcesStoreError(t *testing.T) {
	ctx := context.Background()
	ms := &mockStore{sourcesErr: fmt.Errorf("store unavailable")}
	srv := NewServer(ms, &mockEmbedder{vectors: [][]float32{{0.1, 0.2, 0.3}}})

	result, _, err := srv.handleListSources(ctx, &sdkmcp.CallToolRequest{}, ListSourcesInput{})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError=true for store error")
	}
	text := extractText(t, result)
	if !strings.Contains(text, "store unavailable") {
		t.Errorf("expected error message in output, got: %s", text)
	}
}

// extractText extracts the text from the first TextContent in a CallToolResult.
func extractText(t *testing.T, result *sdkmcp.CallToolResult) string {
	t.Helper()
	if len(result.Content) == 0 {
		t.Fatal("result has no content")
	}
	tc, ok := result.Content[0].(*sdkmcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}
	return tc.Text
}

func TestCacheSearchHit(t *testing.T) {
	ctx := context.Background()
	ms := &mockStore{
		searchResults: []store.SearchResult{
			{Chunk: store.StoredChunk{Text: "cached result", Source: "src"}, Score: 0.9},
		},
	}
	me := &mockEmbedder{vectors: [][]float32{{0.1, 0.2, 0.3}}}
	srv := NewServer(ms, me, WithCache(CacheConfig{}))

	input := SearchInput{Query: "install"}
	// First call — cache miss.
	r1, _, _ := srv.handleSearch(ctx, &sdkmcp.CallToolRequest{}, input)
	// Second call — cache hit.
	r2, _, _ := srv.handleSearch(ctx, &sdkmcp.CallToolRequest{}, input)

	if me.embedCalls != 1 {
		t.Errorf("expected embedder called once, got %d", me.embedCalls)
	}
	if ms.searchCalls != 1 {
		t.Errorf("expected store.Search called once, got %d", ms.searchCalls)
	}
	t1 := extractText(t, r1)
	t2 := extractText(t, r2)
	if t1 != t2 {
		t.Errorf("cached result differs:\n  first:  %q\n  second: %q", t1, t2)
	}
}

func TestCacheSearchMissOnDifferentQuery(t *testing.T) {
	ctx := context.Background()
	ms := &mockStore{
		searchResults: []store.SearchResult{
			{Chunk: store.StoredChunk{Text: "result", Source: "src"}, Score: 0.9},
		},
	}
	me := &mockEmbedder{vectors: [][]float32{{0.1, 0.2, 0.3}}}
	srv := NewServer(ms, me, WithCache(CacheConfig{}))

	srv.handleSearch(ctx, &sdkmcp.CallToolRequest{}, SearchInput{Query: "install"})
	srv.handleSearch(ctx, &sdkmcp.CallToolRequest{}, SearchInput{Query: "upgrade"})

	if me.embedCalls != 2 {
		t.Errorf("expected 2 embed calls for different queries, got %d", me.embedCalls)
	}
}

func TestCacheSearchMissOnDifferentTopK(t *testing.T) {
	ctx := context.Background()
	ms := &mockStore{
		searchResults: []store.SearchResult{
			{Chunk: store.StoredChunk{Text: "result", Source: "src"}, Score: 0.9},
		},
	}
	me := &mockEmbedder{vectors: [][]float32{{0.1, 0.2, 0.3}}}
	srv := NewServer(ms, me, WithCache(CacheConfig{}))

	srv.handleSearch(ctx, &sdkmcp.CallToolRequest{}, SearchInput{Query: "install"})
	k := 10
	srv.handleSearch(ctx, &sdkmcp.CallToolRequest{}, SearchInput{Query: "install", TopK: &k})

	if me.embedCalls != 2 {
		t.Errorf("expected 2 embed calls for different topK, got %d", me.embedCalls)
	}
}

func TestCacheSearchMissOnDifferentSource(t *testing.T) {
	ctx := context.Background()
	ms := &mockStore{
		searchResults: []store.SearchResult{
			{Chunk: store.StoredChunk{Text: "result", Source: "src"}, Score: 0.9},
		},
	}
	me := &mockEmbedder{vectors: [][]float32{{0.1, 0.2, 0.3}}}
	srv := NewServer(ms, me, WithCache(CacheConfig{}))

	srv.handleSearch(ctx, &sdkmcp.CallToolRequest{}, SearchInput{Query: "install"})
	src := "envoy"
	srv.handleSearch(ctx, &sdkmcp.CallToolRequest{}, SearchInput{Query: "install", Source: &src})

	if me.embedCalls != 2 {
		t.Errorf("expected 2 embed calls for different source, got %d", me.embedCalls)
	}
}

func TestCacheEmbedErrorNotCached(t *testing.T) {
	ctx := context.Background()
	ms := &mockStore{}
	me := &mockEmbedder{err: fmt.Errorf("transient error")}
	srv := NewServer(ms, me, WithCache(CacheConfig{}))

	// First call — embed error.
	r1, _, _ := srv.handleSearch(ctx, &sdkmcp.CallToolRequest{}, SearchInput{Query: "test"})
	if !r1.IsError {
		t.Fatal("expected error result on first call")
	}

	// Fix the error and retry — should call embedder again.
	me.err = nil
	me.vectors = [][]float32{{0.1, 0.2, 0.3}}
	r2, _, _ := srv.handleSearch(ctx, &sdkmcp.CallToolRequest{}, SearchInput{Query: "test"})
	if r2.IsError {
		t.Fatal("expected successful result after error fix")
	}
	if me.embedCalls != 2 {
		t.Errorf("expected 2 embed calls (error not cached), got %d", me.embedCalls)
	}
}

func TestCacheStoreErrorNotCached(t *testing.T) {
	ctx := context.Background()
	ms := &mockStore{searchErr: fmt.Errorf("store down")}
	me := &mockEmbedder{vectors: [][]float32{{0.1, 0.2, 0.3}}}
	srv := NewServer(ms, me, WithCache(CacheConfig{}))

	// First call — store error.
	r1, _, _ := srv.handleSearch(ctx, &sdkmcp.CallToolRequest{}, SearchInput{Query: "test"})
	if !r1.IsError {
		t.Fatal("expected error result on first call")
	}

	// Fix the error and retry — should call embedder again.
	ms.searchErr = nil
	ms.searchResults = []store.SearchResult{
		{Chunk: store.StoredChunk{Text: "ok", Source: "s"}, Score: 0.9},
	}
	r2, _, _ := srv.handleSearch(ctx, &sdkmcp.CallToolRequest{}, SearchInput{Query: "test"})
	if r2.IsError {
		t.Fatal("expected successful result after store fix")
	}
	if me.embedCalls != 2 {
		t.Errorf("expected 2 embed calls (store error not cached), got %d", me.embedCalls)
	}
}

func TestCacheSearchCodeHit(t *testing.T) {
	ctx := context.Background()
	ms := &mockStore{
		searchResults: []store.SearchResult{
			{Chunk: store.StoredChunk{Text: "func main()", ChunkType: "code", Source: "r"}, Score: 0.8},
		},
	}
	me := &mockEmbedder{vectors: [][]float32{{0.1, 0.2, 0.3}}}
	srv := NewServer(ms, me, WithCache(CacheConfig{}))

	input := SearchCodeInput{Query: "main"}
	srv.handleSearchCode(ctx, &sdkmcp.CallToolRequest{}, input)
	srv.handleSearchCode(ctx, &sdkmcp.CallToolRequest{}, input)

	if me.embedCalls != 1 {
		t.Errorf("expected 1 embed call for search_code cache hit, got %d", me.embedCalls)
	}
}

func TestCacheSearchVsSearchCodeDistinct(t *testing.T) {
	ctx := context.Background()
	ms := &mockStore{
		searchResults: []store.SearchResult{
			{Chunk: store.StoredChunk{Text: "data", Source: "s"}, Score: 0.9},
		},
	}
	me := &mockEmbedder{vectors: [][]float32{{0.1, 0.2, 0.3}}}
	srv := NewServer(ms, me, WithCache(CacheConfig{}))

	// Same query text via search and search_code should be distinct cache entries.
	srv.handleSearch(ctx, &sdkmcp.CallToolRequest{}, SearchInput{Query: "main"})
	srv.handleSearchCode(ctx, &sdkmcp.CallToolRequest{}, SearchCodeInput{Query: "main"})

	if me.embedCalls != 2 {
		t.Errorf("expected 2 embed calls (search vs search_code distinct), got %d", me.embedCalls)
	}
}

func TestCacheListSourcesHit(t *testing.T) {
	ctx := context.Background()
	ms := &mockStore{
		sources: []store.SourceInfo{
			{Source: "envoy", SourceVersion: "v1.0", ChunkCount: 10},
		},
	}
	srv := NewServer(ms, &mockEmbedder{}, WithCache(CacheConfig{}))

	srv.handleListSources(ctx, &sdkmcp.CallToolRequest{}, ListSourcesInput{})
	srv.handleListSources(ctx, &sdkmcp.CallToolRequest{}, ListSourcesInput{})

	if ms.sourcesCalls != 1 {
		t.Errorf("expected store.ListSources called once, got %d", ms.sourcesCalls)
	}
}

func TestNoCacheByDefault(t *testing.T) {
	ctx := context.Background()
	ms := &mockStore{
		searchResults: []store.SearchResult{
			{Chunk: store.StoredChunk{Text: "data", Source: "s"}, Score: 0.9},
		},
	}
	me := &mockEmbedder{vectors: [][]float32{{0.1, 0.2, 0.3}}}
	srv := NewServer(ms, me) // No WithCache option.

	input := SearchInput{Query: "install"}
	srv.handleSearch(ctx, &sdkmcp.CallToolRequest{}, input)
	srv.handleSearch(ctx, &sdkmcp.CallToolRequest{}, input)

	if me.embedCalls != 2 {
		t.Errorf("without cache, expected embedder called every time, got %d", me.embedCalls)
	}
}

func TestBuildToolDescriptionsWithSources(t *testing.T) {
	sources := []store.SourceInfo{
		{Source: "envoy-gateway", SourceVersion: "v1.3.0", ChunkCount: 423},
	}

	search, searchCode, listSources := buildToolDescriptions(sources)

	if !strings.Contains(search, "envoy-gateway @ v1.3.0 (423 chunks)") {
		t.Errorf("search description missing source info: %s", search)
	}
	if !strings.Contains(search, "CRD specifications") {
		t.Errorf("search description missing usage hints: %s", search)
	}

	if !strings.Contains(searchCode, "envoy-gateway @ v1.3.0 (423 chunks)") {
		t.Errorf("search_code description missing source info: %s", searchCode)
	}
	if !strings.Contains(searchCode, "Go type definitions") {
		t.Errorf("search_code description missing usage hints: %s", searchCode)
	}

	if !strings.Contains(listSources, "versions and chunk counts") {
		t.Errorf("list_sources description missing enrichment: %s", listSources)
	}
}

func TestBuildToolDescriptionsWithoutSources(t *testing.T) {
	search, searchCode, listSources := buildToolDescriptions(nil)

	if search != defaultSearchDesc {
		t.Errorf("expected default search desc, got: %s", search)
	}
	if searchCode != defaultSearchCodeDesc {
		t.Errorf("expected default search_code desc, got: %s", searchCode)
	}
	if listSources != defaultListSourceDesc {
		t.Errorf("expected default list_sources desc, got: %s", listSources)
	}
}

func TestBuildToolDescriptionsMultipleSources(t *testing.T) {
	sources := []store.SourceInfo{
		{Source: "envoy-gateway", SourceVersion: "v1.3.0", ChunkCount: 423},
		{Source: "istio", SourceVersion: "v1.20.0", ChunkCount: 150},
	}

	search, _, _ := buildToolDescriptions(sources)

	if !strings.Contains(search, "envoy-gateway @ v1.3.0 (423 chunks)") {
		t.Errorf("search description missing first source: %s", search)
	}
	if !strings.Contains(search, "istio @ v1.20.0 (150 chunks)") {
		t.Errorf("search description missing second source: %s", search)
	}
}

func TestWithSourceContextOption(t *testing.T) {
	sources := []store.SourceInfo{
		{Source: "envoy-gateway", SourceVersion: "v1.3.0", ChunkCount: 423},
	}
	ms := &mockStore{}
	me := &mockEmbedder{vectors: [][]float32{{0.1, 0.2, 0.3}}}
	srv := NewServer(ms, me, WithSourceContext(sources))

	if len(srv.sources) != 1 {
		t.Fatalf("expected 1 source, got %d", len(srv.sources))
	}
	if srv.sources[0].Source != "envoy-gateway" {
		t.Errorf("expected source 'envoy-gateway', got %q", srv.sources[0].Source)
	}
}

func TestCacheConcurrentAccess(t *testing.T) {
	ctx := context.Background()
	ms := &mockStore{
		searchResults: []store.SearchResult{
			{Chunk: store.StoredChunk{Text: "data", Source: "s"}, Score: 0.9},
		},
	}
	me := &mockEmbedder{vectors: [][]float32{{0.1, 0.2, 0.3}}}
	srv := NewServer(ms, me, WithCache(CacheConfig{}))

	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			srv.handleSearch(ctx, &sdkmcp.CallToolRequest{}, SearchInput{Query: "concurrent"})
		}()
	}
	for i := 0; i < 10; i++ {
		<-done
	}
	// If we get here without a race/panic, the test passes.
}
