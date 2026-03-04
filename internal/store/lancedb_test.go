package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestLanceStore(t *testing.T) *LanceDBStore {
	t.Helper()
	ctx := context.Background()
	dbPath := t.TempDir()
	ls, err := NewLanceDBStore(ctx, dbPath, 4)
	if err != nil {
		t.Fatalf("NewLanceDBStore: %v", err)
	}
	t.Cleanup(func() { ls.Close() })
	return ls
}

func makeLanceChunks(storeKey string, n int, chunkType, source, sourceVersion, language string) []StoredChunk {
	chunks := make([]StoredChunk, n)
	for i := range n {
		vec := make([]float32, 4)
		vec[i%4] = 1.0
		chunks[i] = StoredChunk{
			Text:          fmt.Sprintf("chunk %d text", i),
			Breadcrumb:    fmt.Sprintf("path > chunk%d", i),
			ChunkType:     chunkType,
			ChunkIndex:    i,
			Path:          fmt.Sprintf("docs/chunk%d.md", i),
			Source:        source,
			SourceVersion: sourceVersion,
			StoreKey:      storeKey,
			Language:      language,
			Vector:        vec,
		}
	}
	return chunks
}

func TestUpsertAndSearch(t *testing.T) {
	ls := newTestLanceStore(t)
	ctx := context.Background()

	chunks := makeLanceChunks("key-upsert", 3, "doc", "my-dep", "v1.0.0", "")
	if err := ls.Upsert(ctx, "key-upsert", chunks); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	query := []float32{1, 0, 0, 0}
	results, err := ls.Search(ctx, query, SearchOpts{TopK: 5})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("Search returned no results")
	}
	if results[0].Chunk.Text != "chunk 0 text" {
		t.Errorf("expected first result to be chunk 0, got %q", results[0].Chunk.Text)
	}
	if results[0].Score <= 0 {
		t.Errorf("expected positive score, got %f", results[0].Score)
	}
}

func TestDelete(t *testing.T) {
	ls := newTestLanceStore(t)
	ctx := context.Background()

	chunks := makeLanceChunks("key-delete", 2, "doc", "dep-a", "v1.0.0", "")
	if err := ls.Upsert(ctx, "key-delete", chunks); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	has, err := ls.HasKey(ctx, "key-delete")
	if err != nil {
		t.Fatalf("HasKey before delete: %v", err)
	}
	if !has {
		t.Fatal("expected HasKey true after Upsert")
	}

	if err := ls.Delete(ctx, "key-delete"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	has, err = ls.HasKey(ctx, "key-delete")
	if err != nil {
		t.Fatalf("HasKey after delete: %v", err)
	}
	if has {
		t.Fatal("expected HasKey false after Delete")
	}
}

func TestHasKey(t *testing.T) {
	ls := newTestLanceStore(t)
	ctx := context.Background()

	has, err := ls.HasKey(ctx, "nonexistent-key")
	if err != nil {
		t.Fatalf("HasKey unknown: %v", err)
	}
	if has {
		t.Fatal("expected HasKey false for unknown key")
	}

	chunks := makeLanceChunks("key-haskey", 1, "doc", "dep", "v1.0.0", "")
	if err := ls.Upsert(ctx, "key-haskey", chunks); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	has, err = ls.HasKey(ctx, "key-haskey")
	if err != nil {
		t.Fatalf("HasKey after upsert: %v", err)
	}
	if !has {
		t.Fatal("expected HasKey true after Upsert")
	}
}

func TestSourceFilter(t *testing.T) {
	ls := newTestLanceStore(t)
	ctx := context.Background()

	chunksA := makeLanceChunks("key-srcA", 2, "doc", "source-a", "v1.0.0", "")
	chunksB := makeLanceChunks("key-srcB", 2, "doc", "source-b", "v2.0.0", "")
	if err := ls.Upsert(ctx, "key-srcA", chunksA); err != nil {
		t.Fatalf("Upsert A: %v", err)
	}
	if err := ls.Upsert(ctx, "key-srcB", chunksB); err != nil {
		t.Fatalf("Upsert B: %v", err)
	}

	query := []float32{1, 0, 0, 0}
	results, err := ls.Search(ctx, query, SearchOpts{TopK: 10, Source: "source-a"})
	if err != nil {
		t.Fatalf("Search with source filter: %v", err)
	}
	for _, r := range results {
		if r.Chunk.Source != "source-a" {
			t.Errorf("expected source 'source-a', got %q", r.Chunk.Source)
		}
	}
}

func TestChunkTypeFilter(t *testing.T) {
	ls := newTestLanceStore(t)
	ctx := context.Background()

	docChunks := makeLanceChunks("key-doc", 2, "doc", "dep", "v1.0.0", "")
	codeChunks := makeLanceChunks("key-code", 2, "code", "dep", "v1.0.0", "go")
	if err := ls.Upsert(ctx, "key-doc", docChunks); err != nil {
		t.Fatalf("Upsert doc: %v", err)
	}
	if err := ls.Upsert(ctx, "key-code", codeChunks); err != nil {
		t.Fatalf("Upsert code: %v", err)
	}

	query := []float32{1, 0, 0, 0}
	results, err := ls.Search(ctx, query, SearchOpts{TopK: 10, ChunkType: "doc"})
	if err != nil {
		t.Fatalf("Search with chunkType doc: %v", err)
	}
	for _, r := range results {
		if r.Chunk.ChunkType != "doc" {
			t.Errorf("expected chunkType 'doc', got %q", r.Chunk.ChunkType)
		}
	}

	results, err = ls.Search(ctx, query, SearchOpts{TopK: 10, ChunkType: "code"})
	if err != nil {
		t.Fatalf("Search with chunkType code: %v", err)
	}
	for _, r := range results {
		if r.Chunk.ChunkType != "code" {
			t.Errorf("expected chunkType 'code', got %q", r.Chunk.ChunkType)
		}
	}
}

func TestLanguageFilter(t *testing.T) {
	ls := newTestLanceStore(t)
	ctx := context.Background()

	goChunks := makeLanceChunks("key-go", 2, "code", "dep", "v1.0.0", "go")
	pyChunks := makeLanceChunks("key-py", 2, "code", "dep", "v1.0.0", "python")
	if err := ls.Upsert(ctx, "key-go", goChunks); err != nil {
		t.Fatalf("Upsert go: %v", err)
	}
	if err := ls.Upsert(ctx, "key-py", pyChunks); err != nil {
		t.Fatalf("Upsert python: %v", err)
	}

	query := []float32{1, 0, 0, 0}
	results, err := ls.Search(ctx, query, SearchOpts{TopK: 10, Language: "go"})
	if err != nil {
		t.Fatalf("Search with language filter: %v", err)
	}
	for _, r := range results {
		if r.Chunk.Language != "go" {
			t.Errorf("expected language 'go', got %q", r.Chunk.Language)
		}
	}
}

func TestListSources(t *testing.T) {
	ls := newTestLanceStore(t)
	ctx := context.Background()

	chunksA := makeLanceChunks("key-listA", 3, "doc", "list-src-a", "v1.0.0", "")
	chunksB := makeLanceChunks("key-listB", 2, "code", "list-src-b", "v2.0.0", "go")
	if err := ls.Upsert(ctx, "key-listA", chunksA); err != nil {
		t.Fatalf("Upsert A: %v", err)
	}
	if err := ls.Upsert(ctx, "key-listB", chunksB); err != nil {
		t.Fatalf("Upsert B: %v", err)
	}

	sources, err := ls.ListSources(ctx)
	if err != nil {
		t.Fatalf("ListSources: %v", err)
	}

	found := make(map[string]SourceInfo)
	for _, s := range sources {
		found[s.StoreKey] = s
	}

	if info, ok := found["key-listA"]; !ok {
		t.Errorf("missing source info for key-listA")
	} else {
		if info.ChunkCount != 3 {
			t.Errorf("expected 3 chunks for keyA, got %d", info.ChunkCount)
		}
		if info.Source != "list-src-a" {
			t.Errorf("expected source 'list-src-a', got %q", info.Source)
		}
	}

	if info, ok := found["key-listB"]; !ok {
		t.Errorf("missing source info for key-listB")
	} else {
		if info.ChunkCount != 2 {
			t.Errorf("expected 2 chunks for keyB, got %d", info.ChunkCount)
		}
	}
}

func TestUpsertIdempotent(t *testing.T) {
	ls := newTestLanceStore(t)
	ctx := context.Background()

	chunks1 := makeLanceChunks("key-idem", 3, "doc", "dep", "v1.0.0", "")
	if err := ls.Upsert(ctx, "key-idem", chunks1); err != nil {
		t.Fatalf("Upsert first: %v", err)
	}

	// Upsert again with fewer chunks — old ones should be replaced.
	chunks2 := makeLanceChunks("key-idem", 1, "doc", "dep", "v2.0.0", "")
	if err := ls.Upsert(ctx, "key-idem", chunks2); err != nil {
		t.Fatalf("Upsert second: %v", err)
	}

	query := []float32{1, 0, 0, 0}
	results, err := ls.Search(ctx, query, SearchOpts{TopK: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	var matches int
	for _, r := range results {
		if r.Chunk.StoreKey == "key-idem" {
			matches++
		}
	}
	if matches != 1 {
		t.Errorf("expected 1 chunk after idempotent upsert, got %d", matches)
	}
}

func TestEmptyStore(t *testing.T) {
	ls := newTestLanceStore(t)
	ctx := context.Background()

	query := []float32{1, 0, 0, 0}
	results, err := ls.Search(ctx, query, SearchOpts{TopK: 5})
	if err != nil {
		t.Fatalf("Search empty: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results from empty store, got %d", len(results))
	}

	sources, err := ls.ListSources(ctx)
	if err != nil {
		t.Fatalf("ListSources empty: %v", err)
	}
	if len(sources) != 0 {
		t.Errorf("expected 0 sources from empty store, got %d", len(sources))
	}
}

func TestCombinedFilters(t *testing.T) {
	ls := newTestLanceStore(t)
	ctx := context.Background()

	docA := makeLanceChunks("key-comb-docA", 2, "doc", "source-a", "v1.0.0", "")
	codeA := makeLanceChunks("key-comb-codeA", 2, "code", "source-a", "v1.0.0", "go")
	docB := makeLanceChunks("key-comb-docB", 2, "doc", "source-b", "v1.0.0", "")
	for _, pair := range []struct {
		key    string
		chunks []StoredChunk
	}{
		{"key-comb-docA", docA},
		{"key-comb-codeA", codeA},
		{"key-comb-docB", docB},
	} {
		if err := ls.Upsert(ctx, pair.key, pair.chunks); err != nil {
			t.Fatalf("Upsert %s: %v", pair.key, err)
		}
	}

	query := []float32{1, 0, 0, 0}
	results, err := ls.Search(ctx, query, SearchOpts{
		TopK:      10,
		Source:    "source-a",
		ChunkType: "code",
	})
	if err != nil {
		t.Fatalf("Search combined: %v", err)
	}
	for _, r := range results {
		if r.Chunk.Source != "source-a" {
			t.Errorf("expected source 'source-a', got %q", r.Chunk.Source)
		}
		if r.Chunk.ChunkType != "code" {
			t.Errorf("expected chunkType 'code', got %q", r.Chunk.ChunkType)
		}
	}
}

func TestDefaultStorePath(t *testing.T) {
	t.Run("env var override", func(t *testing.T) {
		t.Setenv("MYCELIUM_STORE_DIR", "/custom/path")
		got := DefaultStorePath()
		if got != "/custom/path" {
			t.Errorf("expected /custom/path, got %q", got)
		}
	})

	t.Run("default without env var", func(t *testing.T) {
		t.Setenv("MYCELIUM_STORE_DIR", "")
		got := DefaultStorePath()
		home, err := os.UserHomeDir()
		if err != nil {
			t.Fatalf("UserHomeDir: %v", err)
		}
		want := filepath.Join(home, ".mycelium", "store")
		if got != want {
			t.Errorf("expected %q, got %q", want, got)
		}
	})
}

func TestNewLanceDBStore_ZeroDimensions(t *testing.T) {
	ctx := context.Background()
	dbPath := t.TempDir()

	_, err := NewLanceDBStore(ctx, dbPath, 0)
	if err == nil {
		t.Fatal("expected error when dimensions=0 and table does not exist")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("expected error containing 'does not exist', got %q", err.Error())
	}
}

func TestNewLanceDBStore_OpenExisting(t *testing.T) {
	ctx := context.Background()
	dbPath := t.TempDir()

	// Create the store with known dimensions.
	store1, err := NewLanceDBStore(ctx, dbPath, 4)
	if err != nil {
		t.Fatalf("first NewLanceDBStore: %v", err)
	}
	// Insert a chunk so the table is non-empty.
	chunks := makeLanceChunks("key-open", 1, "doc", "dep", "v1.0.0", "")
	if err := store1.Upsert(ctx, "key-open", chunks); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if err := store1.Close(); err != nil {
		t.Fatalf("Close first store: %v", err)
	}

	// Reopen with dimensions=0 — should succeed because the table already exists.
	store2, err := NewLanceDBStore(ctx, dbPath, 0)
	if err != nil {
		t.Fatalf("second NewLanceDBStore with dims=0: %v", err)
	}
	defer store2.Close()

	has, err := store2.HasKey(ctx, "key-open")
	if err != nil {
		t.Fatalf("HasKey on reopened store: %v", err)
	}
	if !has {
		t.Error("expected HasKey true on reopened store")
	}
}

func TestMapInt_Types(t *testing.T) {
	tests := []struct {
		name string
		val  interface{}
		want int
	}{
		{"int", int(42), 42},
		{"int32", int32(32), 32},
		{"int64", int64(64), 64},
		{"float64", float64(3.14), 3},
		{"float32", float32(2.71), 2},
		{"nil", nil, 0},
		{"string", "notanumber", 0},
		{"missing key", nil, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := map[string]interface{}{"k": tc.val}
			got := mapInt(m, "k")
			if got != tc.want {
				t.Errorf("mapInt(%T(%v)) = %d, want %d", tc.val, tc.val, got, tc.want)
			}
		})
	}

	// Test missing key separately.
	t.Run("key absent", func(t *testing.T) {
		m := map[string]interface{}{"other": 99}
		got := mapInt(m, "missing")
		if got != 0 {
			t.Errorf("mapInt missing key = %d, want 0", got)
		}
	})
}

func TestToFloat32_Types(t *testing.T) {
	tests := []struct {
		name string
		val  interface{}
		want float32
	}{
		{"float32", float32(1.5), 1.5},
		{"float64", float64(2.5), 2.5},
		{"string", "notafloat", 0},
		{"nil", nil, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := toFloat32(tc.val)
			if got != tc.want {
				t.Errorf("toFloat32(%T(%v)) = %f, want %f", tc.val, tc.val, got, tc.want)
			}
		})
	}
}

func TestUpsertEmpty(t *testing.T) {
	ls := newTestLanceStore(t)
	ctx := context.Background()

	// Upsert with an empty slice should not error.
	if err := ls.Upsert(ctx, "key-empty", nil); err != nil {
		t.Fatalf("Upsert empty: %v", err)
	}

	has, err := ls.HasKey(ctx, "key-empty")
	if err != nil {
		t.Fatalf("HasKey after empty upsert: %v", err)
	}
	if has {
		t.Error("expected HasKey false after empty upsert")
	}

	// Also test with a non-nil empty slice.
	if err := ls.Upsert(ctx, "key-empty2", []StoredChunk{}); err != nil {
		t.Fatalf("Upsert empty slice: %v", err)
	}

	has, err = ls.HasKey(ctx, "key-empty2")
	if err != nil {
		t.Fatalf("HasKey after empty slice upsert: %v", err)
	}
	if has {
		t.Error("expected HasKey false after empty slice upsert")
	}
}

func TestClose_NilFields(t *testing.T) {
	// Test Close with nil table and nil conn — should not panic and return nil.
	ls := &LanceDBStore{
		conn:  nil,
		table: nil,
	}
	if err := ls.Close(); err != nil {
		t.Errorf("Close on nil fields returned error: %v", err)
	}
}
