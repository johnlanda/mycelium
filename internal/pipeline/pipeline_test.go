package pipeline

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/johnlanda/mycelium/internal/chunker"
	"github.com/johnlanda/mycelium/internal/fetchers"
	"github.com/johnlanda/mycelium/internal/hasher"
	"github.com/johnlanda/mycelium/internal/lockfile"
	"github.com/johnlanda/mycelium/internal/manifest"
	"github.com/johnlanda/mycelium/internal/store"
)

// --- Mock implementations ---

type mockStore struct {
	upserted  map[string][]store.StoredChunk
	keys      map[string]bool
	hasKeyErr error
}

func newMockStore() *mockStore {
	return &mockStore{
		upserted: make(map[string][]store.StoredChunk),
		keys:     make(map[string]bool),
	}
}

func (s *mockStore) Upsert(_ context.Context, storeKey string, chunks []store.StoredChunk) error {
	s.upserted[storeKey] = chunks
	s.keys[storeKey] = true
	return nil
}

func (s *mockStore) Search(_ context.Context, _ []float32, _ store.SearchOpts) ([]store.SearchResult, error) {
	return nil, nil
}

func (s *mockStore) Delete(_ context.Context, _ string) error { return nil }

func (s *mockStore) HasKey(_ context.Context, storeKey string) (bool, error) {
	if s.hasKeyErr != nil {
		return false, s.hasKeyErr
	}
	return s.keys[storeKey], nil
}

func (s *mockStore) ListSources(_ context.Context) ([]store.SourceInfo, error) { return nil, nil }
func (s *mockStore) Close() error                                               { return nil }

type mockEmbedder struct {
	dims int
}

func (e *mockEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	vecs := make([][]float32, len(texts))
	for i := range texts {
		vecs[i] = make([]float32, e.dims)
	}
	return vecs, nil
}

func (e *mockEmbedder) ModelID() string { return "mock-model" }
func (e *mockEmbedder) Dimensions() int { return e.dims }

type mockFetchResult struct {
	commitSHA string
	files     []hasher.FileContent
}

type mockFetcherImpl struct {
	result *mockFetchResult
	err    error
}

func (f *mockFetcherImpl) Fetch(_ context.Context, _ manifest.Dependency) (*fetchers.FetchResult, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &fetchers.FetchResult{
		CommitSHA: f.result.commitSHA,
		Files:     f.result.files,
	}, nil
}

type multiFetcher struct {
	results map[string]*mockFetchResult
}

func (f *multiFetcher) Fetch(_ context.Context, dep manifest.Dependency) (*fetchers.FetchResult, error) {
	r, ok := f.results[dep.ID]
	if !ok {
		return nil, fmt.Errorf("no mock result for %s", dep.ID)
	}
	return &fetchers.FetchResult{
		CommitSHA: r.commitSHA,
		Files:     r.files,
	}, nil
}

// --- Tests ---

func TestSyncDependency_FullPipeline(t *testing.T) {
	ctx := context.Background()
	st := newMockStore()
	emb := &mockEmbedder{dims: 4}
	mdChunker := chunker.NewMarkdownChunker(chunker.DefaultOptions())
	codeChunker := chunker.NewLineChunker(chunker.DefaultOptions())

	dep := manifest.Dependency{
		ID:     "test/repo",
		Source: "github.com/test/repo",
		Ref:    "v1.0.0",
	}

	fr := &mockFetchResult{
		commitSHA: "abc123",
		files: []hasher.FileContent{
			{Path: "README.md", Content: []byte("# Hello\n\nThis is a test document with enough words to be meaningful.")},
			{Path: "main.go", Content: []byte("package main\n\nfunc main() {}")},
		},
	}
	fetcher := &mockFetcherImpl{result: fr}

	sl, skipped, err := syncDependency(ctx, dep, fetcher, st, emb, mdChunker, codeChunker)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if skipped {
		t.Error("expected synced, got skipped")
	}
	if sl.Version != "v1.0.0" {
		t.Errorf("Version = %q, want %q", sl.Version, "v1.0.0")
	}
	if sl.Commit != "abc123" {
		t.Errorf("Commit = %q, want %q", sl.Commit, "abc123")
	}
	if sl.ContentHash == "" {
		t.Error("ContentHash should not be empty")
	}
	if sl.StoreKey == "" {
		t.Error("StoreKey should not be empty")
	}
	if sl.IngestionType != "built" {
		t.Errorf("IngestionType = %q, want %q", sl.IngestionType, "built")
	}

	// Verify Upsert was called.
	if len(st.upserted) == 0 {
		t.Error("expected Upsert to be called")
	}
	for _, chunks := range st.upserted {
		for _, c := range chunks {
			if len(c.Vector) != 4 {
				t.Errorf("vector dims = %d, want 4", len(c.Vector))
			}
		}
	}
}

func TestSyncDependency_Skip(t *testing.T) {
	ctx := context.Background()
	st := newMockStore()
	emb := &mockEmbedder{dims: 4}
	mdChunker := chunker.NewMarkdownChunker(chunker.DefaultOptions())
	codeChunker := chunker.NewLineChunker(chunker.DefaultOptions())

	dep := manifest.Dependency{
		ID:     "test/repo",
		Source: "github.com/test/repo",
		Ref:    "v1.0.0",
	}

	fr := &mockFetchResult{
		commitSHA: "abc123",
		files: []hasher.FileContent{
			{Path: "main.go", Content: []byte("package main")},
		},
	}
	fetcher := &mockFetcherImpl{result: fr}

	// Compute the expected store key so we can pre-populate the store.
	contentHash := hasher.ContentHash(fr.files)
	sk := hasher.StoreKey(contentHash, emb.ModelID(), "", hasher.ChunkingConfig{
		ChunkerType: "mixed",
		TargetSize:  chunker.DefaultOptions().TargetSize,
		Overlap:     chunker.DefaultOptions().Overlap,
	})
	st.keys[sk] = true

	sl, skipped, err := syncDependency(ctx, dep, fetcher, st, emb, mdChunker, codeChunker)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !skipped {
		t.Error("expected skipped, got synced")
	}
	if sl.StoreKey != sk {
		t.Errorf("StoreKey = %q, want %q", sl.StoreKey, sk)
	}

	// Upsert should NOT have been called since the key already existed.
	if len(st.upserted) != 0 {
		t.Error("Upsert should not be called when key already exists")
	}
}

func TestFileClassification(t *testing.T) {
	tests := []struct {
		path string
		want bool // true = markdown
	}{
		{"docs/guide.md", true},
		{"docs/guide.mdx", true},
		{"main.go", false},
		{"app.py", false},
		{"index.ts", false},
		{"README.MD", true},
	}
	for _, tt := range tests {
		got := isMarkdown(tt.path)
		if got != tt.want {
			t.Errorf("isMarkdown(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestLanguageFromExt(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"main.go", "go"},
		{"app.py", "python"},
		{"index.ts", "typescript"},
		{"component.tsx", "typescript"},
		{"script.js", "javascript"},
		{"component.jsx", "javascript"},
		{"Main.java", "java"},
		{"lib.rs", "rust"},
		{"file.txt", ""},
	}
	for _, tt := range tests {
		got := languageFromExt(tt.path)
		if got != tt.want {
			t.Errorf("languageFromExt(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestSyncDependency_OneFailureDoesNotAbortOthers(t *testing.T) {
	ctx := context.Background()
	st := newMockStore()
	emb := &mockEmbedder{dims: 4}
	mdChunker := chunker.NewMarkdownChunker(chunker.DefaultOptions())
	codeChunker := chunker.NewLineChunker(chunker.DefaultOptions())

	failFetcher := &mockFetcherImpl{err: fmt.Errorf("network error")}
	okFetcher := &mockFetcherImpl{result: &mockFetchResult{
		commitSHA: "def456",
		files:     []hasher.FileContent{{Path: "lib.go", Content: []byte("package lib")}},
	}}

	dep1 := manifest.Dependency{ID: "fail/repo", Source: "github.com/fail/repo", Ref: "v1"}
	dep2 := manifest.Dependency{ID: "ok/repo", Source: "github.com/ok/repo", Ref: "v2"}

	_, _, err1 := syncDependency(ctx, dep1, failFetcher, st, emb, mdChunker, codeChunker)
	if err1 == nil {
		t.Error("expected error for failing dep")
	}

	sl2, _, err2 := syncDependency(ctx, dep2, okFetcher, st, emb, mdChunker, codeChunker)
	if err2 != nil {
		t.Fatalf("second dep should succeed: %v", err2)
	}
	if sl2 == nil {
		t.Fatal("expected non-nil SourceLock for second dep")
	}
}

func TestSyncAll_Integration(t *testing.T) {
	ctx := context.Background()
	st := newMockStore()
	emb := &mockEmbedder{dims: 4}
	mdChunker := chunker.NewMarkdownChunker(chunker.DefaultOptions())
	codeChunker := chunker.NewLineChunker(chunker.DefaultOptions())

	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, "mycelium.lock")

	m := &manifest.Manifest{
		Config: manifest.Config{EmbeddingModel: "mock-model"},
		Dependencies: []manifest.Dependency{
			{ID: "dep1", Source: "github.com/test/dep1", Ref: "v1.0.0"},
		},
	}
	lf := lockfile.New()

	fetcher := &multiFetcher{
		results: map[string]*mockFetchResult{
			"dep1": {commitSHA: "sha1", files: []hasher.FileContent{
				{Path: "main.go", Content: []byte("package main\n\nfunc main() {}")},
			}},
		},
	}

	opts := SyncOptions{
		LockfilePath: lockPath,
		Output:       io.Discard,
	}

	results, err := syncAll(ctx, m, lf, st, emb, fetcher, mdChunker, codeChunker, opts)
	if err != nil {
		t.Fatalf("syncAll error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Status != "synced" {
		t.Errorf("status = %q, want %q", results[0].Status, "synced")
	}

	// Verify lockfile was written.
	if _, err := os.Stat(lockPath); os.IsNotExist(err) {
		t.Error("lockfile was not written")
	}
}
