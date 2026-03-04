package store

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"
)

func weaviateHost(t *testing.T) string {
	t.Helper()
	host := os.Getenv("WEAVIATE_URL")
	if host == "" {
		host = "localhost:8080"
	}
	return host
}

func skipIfNoWeaviate(t *testing.T) {
	t.Helper()
	host := weaviateHost(t)
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://%s/v1/.well-known/ready", host))
	if err != nil {
		t.Skipf("Weaviate not reachable at %s: %v", host, err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Skipf("Weaviate not ready at %s: status %d", host, resp.StatusCode)
	}
}

func newTestStore(t *testing.T) *WeaviateStore {
	t.Helper()
	skipIfNoWeaviate(t)
	ctx := context.Background()
	ws, err := NewWeaviateStore(ctx, weaviateHost(t))
	if err != nil {
		t.Fatalf("NewWeaviateStore: %v", err)
	}
	return ws
}

func testStoreKey(t *testing.T, suffix string) string {
	t.Helper()
	return fmt.Sprintf("test_%s_%d_%s", t.Name(), time.Now().UnixNano(), suffix)
}

func cleanup(t *testing.T, ws *WeaviateStore, keys ...string) {
	t.Helper()
	ctx := context.Background()
	for _, key := range keys {
		if err := ws.Delete(ctx, key); err != nil {
			t.Logf("cleanup delete %s: %v", key, err)
		}
	}
}

func makeChunks(storeKey string, n int, chunkType, source, sourceVersion, language string) []StoredChunk {
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
	ws := newTestStore(t)
	ctx := context.Background()
	key := testStoreKey(t, "upsert_search")
	t.Cleanup(func() { cleanup(t, ws, key) })

	chunks := makeChunks(key, 3, "doc", "my-dep", "v1.0.0", "")
	if err := ws.Upsert(ctx, key, chunks); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	// Search with a vector that matches chunk 0 exactly.
	query := []float32{1, 0, 0, 0}
	results, err := ws.Search(ctx, query, SearchOpts{TopK: 5})
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
	ws := newTestStore(t)
	ctx := context.Background()
	key := testStoreKey(t, "delete")
	t.Cleanup(func() { cleanup(t, ws, key) })

	chunks := makeChunks(key, 2, "doc", "dep-a", "v1.0.0", "")
	if err := ws.Upsert(ctx, key, chunks); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	has, err := ws.HasKey(ctx, key)
	if err != nil {
		t.Fatalf("HasKey before delete: %v", err)
	}
	if !has {
		t.Fatal("expected HasKey true after Upsert")
	}

	if err := ws.Delete(ctx, key); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	has, err = ws.HasKey(ctx, key)
	if err != nil {
		t.Fatalf("HasKey after delete: %v", err)
	}
	if has {
		t.Fatal("expected HasKey false after Delete")
	}
}

func TestHasKey(t *testing.T) {
	ws := newTestStore(t)
	ctx := context.Background()
	key := testStoreKey(t, "haskey")
	t.Cleanup(func() { cleanup(t, ws, key) })

	has, err := ws.HasKey(ctx, key)
	if err != nil {
		t.Fatalf("HasKey unknown: %v", err)
	}
	if has {
		t.Fatal("expected HasKey false for unknown key")
	}

	chunks := makeChunks(key, 1, "doc", "dep", "v1.0.0", "")
	if err := ws.Upsert(ctx, key, chunks); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	has, err = ws.HasKey(ctx, key)
	if err != nil {
		t.Fatalf("HasKey after upsert: %v", err)
	}
	if !has {
		t.Fatal("expected HasKey true after Upsert")
	}
}

func TestSourceFilter(t *testing.T) {
	ws := newTestStore(t)
	ctx := context.Background()
	keyA := testStoreKey(t, "srcA")
	keyB := testStoreKey(t, "srcB")
	t.Cleanup(func() { cleanup(t, ws, keyA, keyB) })

	chunksA := makeChunks(keyA, 2, "doc", "source-a", "v1.0.0", "")
	chunksB := makeChunks(keyB, 2, "doc", "source-b", "v2.0.0", "")
	if err := ws.Upsert(ctx, keyA, chunksA); err != nil {
		t.Fatalf("Upsert A: %v", err)
	}
	if err := ws.Upsert(ctx, keyB, chunksB); err != nil {
		t.Fatalf("Upsert B: %v", err)
	}

	query := []float32{1, 0, 0, 0}
	results, err := ws.Search(ctx, query, SearchOpts{TopK: 10, Source: "source-a"})
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
	ws := newTestStore(t)
	ctx := context.Background()
	keyDoc := testStoreKey(t, "doc")
	keyCode := testStoreKey(t, "code")
	t.Cleanup(func() { cleanup(t, ws, keyDoc, keyCode) })

	docChunks := makeChunks(keyDoc, 2, "doc", "dep", "v1.0.0", "")
	codeChunks := makeChunks(keyCode, 2, "code", "dep", "v1.0.0", "go")
	if err := ws.Upsert(ctx, keyDoc, docChunks); err != nil {
		t.Fatalf("Upsert doc: %v", err)
	}
	if err := ws.Upsert(ctx, keyCode, codeChunks); err != nil {
		t.Fatalf("Upsert code: %v", err)
	}

	query := []float32{1, 0, 0, 0}
	results, err := ws.Search(ctx, query, SearchOpts{TopK: 10, ChunkType: "doc"})
	if err != nil {
		t.Fatalf("Search with chunkType filter: %v", err)
	}
	for _, r := range results {
		if r.Chunk.ChunkType != "doc" {
			t.Errorf("expected chunkType 'doc', got %q", r.Chunk.ChunkType)
		}
	}

	results, err = ws.Search(ctx, query, SearchOpts{TopK: 10, ChunkType: "code"})
	if err != nil {
		t.Fatalf("Search with chunkType filter code: %v", err)
	}
	for _, r := range results {
		if r.Chunk.ChunkType != "code" {
			t.Errorf("expected chunkType 'code', got %q", r.Chunk.ChunkType)
		}
	}
}

func TestListSources(t *testing.T) {
	ws := newTestStore(t)
	ctx := context.Background()
	keyA := testStoreKey(t, "listA")
	keyB := testStoreKey(t, "listB")
	t.Cleanup(func() { cleanup(t, ws, keyA, keyB) })

	chunksA := makeChunks(keyA, 3, "doc", "list-src-a", "v1.0.0", "")
	chunksB := makeChunks(keyB, 2, "code", "list-src-b", "v2.0.0", "go")
	if err := ws.Upsert(ctx, keyA, chunksA); err != nil {
		t.Fatalf("Upsert A: %v", err)
	}
	if err := ws.Upsert(ctx, keyB, chunksB); err != nil {
		t.Fatalf("Upsert B: %v", err)
	}

	sources, err := ws.ListSources(ctx)
	if err != nil {
		t.Fatalf("ListSources: %v", err)
	}

	found := make(map[string]SourceInfo)
	for _, s := range sources {
		found[s.StoreKey] = s
	}

	if info, ok := found[keyA]; !ok {
		t.Errorf("missing source info for %s", keyA)
	} else {
		if info.ChunkCount != 3 {
			t.Errorf("expected 3 chunks for keyA, got %d", info.ChunkCount)
		}
		if info.Source != "list-src-a" {
			t.Errorf("expected source 'list-src-a', got %q", info.Source)
		}
	}

	if info, ok := found[keyB]; !ok {
		t.Errorf("missing source info for %s", keyB)
	} else {
		if info.ChunkCount != 2 {
			t.Errorf("expected 2 chunks for keyB, got %d", info.ChunkCount)
		}
	}
}

func TestUpsertIdempotent(t *testing.T) {
	ws := newTestStore(t)
	ctx := context.Background()
	key := testStoreKey(t, "idempotent")
	t.Cleanup(func() { cleanup(t, ws, key) })

	chunks1 := makeChunks(key, 3, "doc", "dep", "v1.0.0", "")
	if err := ws.Upsert(ctx, key, chunks1); err != nil {
		t.Fatalf("Upsert first: %v", err)
	}

	// Upsert again with fewer chunks — old ones should be replaced.
	chunks2 := makeChunks(key, 1, "doc", "dep", "v2.0.0", "")
	if err := ws.Upsert(ctx, key, chunks2); err != nil {
		t.Fatalf("Upsert second: %v", err)
	}

	query := []float32{1, 0, 0, 0}
	results, err := ws.Search(ctx, query, SearchOpts{TopK: 10, Source: "dep"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	// Only the new chunk set should remain.
	var storeKeyMatches int
	for _, r := range results {
		if r.Chunk.StoreKey == key {
			storeKeyMatches++
		}
	}
	if storeKeyMatches != 1 {
		t.Errorf("expected 1 chunk after idempotent upsert, got %d", storeKeyMatches)
	}
}

func TestEmptyStore(t *testing.T) {
	ws := newTestStore(t)
	ctx := context.Background()

	query := []float32{1, 0, 0, 0}
	results, err := ws.Search(ctx, query, SearchOpts{TopK: 5})
	if err != nil {
		t.Fatalf("Search empty: %v", err)
	}
	// The store may contain data from other tests, but the search itself should not error.
	_ = results

	sources, err := ws.ListSources(ctx)
	if err != nil {
		t.Fatalf("ListSources empty: %v", err)
	}
	_ = sources
}
