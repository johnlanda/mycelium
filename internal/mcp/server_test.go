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
	vectors [][]float32
	err     error
	texts   []string
}

func (m *mockEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
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

	sources    []store.SourceInfo
	sourcesErr error
}

func (m *mockStore) Search(_ context.Context, _ []float32, opts store.SearchOpts) ([]store.SearchResult, error) {
	m.searchOpts = opts
	if m.searchErr != nil {
		return nil, m.searchErr
	}
	return m.searchResults, nil
}

func (m *mockStore) ListSources(_ context.Context) ([]store.SourceInfo, error) {
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
