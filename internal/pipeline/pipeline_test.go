package pipeline

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/johnlanda/mycelium/internal/artifact"
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

// --- New tests for syncLocal, ProcessFiles, and extended syncAll scenarios ---

func TestSyncLocal(t *testing.T) {
	ctx := context.Background()
	st := newMockStore()
	emb := &mockEmbedder{dims: 4}
	mdChunker := chunker.NewMarkdownChunker(chunker.DefaultOptions())
	codeChunker := chunker.NewLineChunker(chunker.DefaultOptions())

	tmpDir := t.TempDir()
	// Create a markdown file.
	mdPath := filepath.Join(tmpDir, "README.md")
	if err := os.WriteFile(mdPath, []byte("# Guide\n\nThis is a local markdown file with enough content to chunk."), 0644); err != nil {
		t.Fatalf("write md file: %v", err)
	}
	// Create a Go file.
	goPath := filepath.Join(tmpDir, "main.go")
	if err := os.WriteFile(goPath, []byte("package main\n\nfunc main() {\n\tprintln(\"hello\")\n}"), 0644); err != nil {
		t.Fatalf("write go file: %v", err)
	}

	sl, skipped, err := syncLocal(ctx, []string{tmpDir}, st, emb, mdChunker, codeChunker)
	if err != nil {
		t.Fatalf("syncLocal error: %v", err)
	}
	if skipped {
		t.Error("expected synced on first call, got skipped")
	}
	if sl == nil {
		t.Fatal("expected non-nil SourceLock")
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

	// Verify store received upserted chunks.
	if len(st.upserted) == 0 {
		t.Error("expected Upsert to be called")
	}
	found := false
	for key := range st.upserted {
		if key == sl.StoreKey {
			found = true
			break
		}
	}
	if !found {
		t.Error("store was not upserted under the expected store key")
	}
}

func TestSyncLocal_Skip(t *testing.T) {
	ctx := context.Background()
	st := newMockStore()
	emb := &mockEmbedder{dims: 4}
	mdChunker := chunker.NewMarkdownChunker(chunker.DefaultOptions())
	codeChunker := chunker.NewLineChunker(chunker.DefaultOptions())

	tmpDir := t.TempDir()
	goPath := filepath.Join(tmpDir, "lib.go")
	if err := os.WriteFile(goPath, []byte("package lib\n\nfunc Foo() {}"), 0644); err != nil {
		t.Fatalf("write go file: %v", err)
	}

	// Compute the expected store key by reading the files the same way syncLocal does.
	files := []hasher.FileContent{
		{Path: goPath, Content: []byte("package lib\n\nfunc Foo() {}")},
	}
	contentHash := hasher.ContentHash(files)
	sk := hasher.StoreKey(contentHash, emb.ModelID(), "", hasher.ChunkingConfig{
		ChunkerType: "mixed",
		TargetSize:  chunker.DefaultOptions().TargetSize,
		Overlap:     chunker.DefaultOptions().Overlap,
	})
	st.keys[sk] = true

	sl, skipped, err := syncLocal(ctx, []string{tmpDir}, st, emb, mdChunker, codeChunker)
	if err != nil {
		t.Fatalf("syncLocal error: %v", err)
	}
	if !skipped {
		t.Error("expected skipped, got synced")
	}
	if sl.StoreKey != sk {
		t.Errorf("StoreKey = %q, want %q", sl.StoreKey, sk)
	}

	// Upsert should NOT have been called.
	if len(st.upserted) != 0 {
		t.Error("Upsert should not be called when key already exists")
	}
}

func TestSyncLocal_EmptyDir(t *testing.T) {
	ctx := context.Background()
	st := newMockStore()
	emb := &mockEmbedder{dims: 4}
	mdChunker := chunker.NewMarkdownChunker(chunker.DefaultOptions())
	codeChunker := chunker.NewLineChunker(chunker.DefaultOptions())

	tmpDir := t.TempDir()

	sl, _, err := syncLocal(ctx, []string{tmpDir}, st, emb, mdChunker, codeChunker)
	if err != nil {
		t.Fatalf("syncLocal on empty dir should not error: %v", err)
	}
	if sl == nil {
		t.Fatal("expected non-nil SourceLock even for empty dir")
	}
	// With no files, ContentHash is still computed (just over zero files).
	if sl.ContentHash == "" {
		t.Error("ContentHash should not be empty even for empty dir")
	}
	if sl.StoreKey == "" {
		t.Error("StoreKey should not be empty even for empty dir")
	}
}

func TestProcessFiles(t *testing.T) {
	ctx := context.Background()
	emb := &mockEmbedder{dims: 8}
	mdChunker := chunker.NewMarkdownChunker(chunker.DefaultOptions())
	codeChunker := chunker.NewLineChunker(chunker.DefaultOptions())

	files := []hasher.FileContent{
		{Path: "docs/guide.md", Content: []byte("# Guide\n\nThis is a markdown guide with enough words to produce a chunk.")},
		{Path: "src/main.go", Content: []byte("package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}")},
		{Path: "src/lib.py", Content: []byte("def hello():\n    print('hello world')\n")},
	}

	source := "test/dep"
	sourceVersion := "v2.0.0"
	storeKey := "sha256:teststorekey"

	stored, err := ProcessFiles(ctx, files, source, sourceVersion, emb, mdChunker, codeChunker, storeKey)
	if err != nil {
		t.Fatalf("ProcessFiles error: %v", err)
	}

	if len(stored) == 0 {
		t.Fatal("expected non-empty stored chunks")
	}

	// Verify all chunks have the right vector dimension.
	for i, c := range stored {
		if len(c.Vector) != 8 {
			t.Errorf("chunk[%d]: vector dims = %d, want 8", i, len(c.Vector))
		}
	}

	// Verify chunk types: markdown files should produce "doc" chunks, code files "code" chunks.
	hasDoc := false
	hasCode := false
	for _, c := range stored {
		if c.ChunkType == "doc" {
			hasDoc = true
		}
		if c.ChunkType == "code" {
			hasCode = true
		}
		// Every chunk should have Source and StoreKey set.
		if c.Source != source {
			t.Errorf("chunk Source = %q, want %q", c.Source, source)
		}
		if c.SourceVersion != sourceVersion {
			t.Errorf("chunk SourceVersion = %q, want %q", c.SourceVersion, sourceVersion)
		}
		if c.StoreKey != storeKey {
			t.Errorf("chunk StoreKey = %q, want %q", c.StoreKey, storeKey)
		}
	}

	if !hasDoc {
		t.Error("expected at least one 'doc' chunk from markdown file")
	}
	if !hasCode {
		t.Error("expected at least one 'code' chunk from code files")
	}

	// Verify Language is set for code files.
	for _, c := range stored {
		if c.Path == "src/main.go" && c.Language != "go" {
			t.Errorf("go file chunk Language = %q, want %q", c.Language, "go")
		}
		if c.Path == "src/lib.py" && c.Language != "python" {
			t.Errorf("python file chunk Language = %q, want %q", c.Language, "python")
		}
	}
}

func TestProcessFiles_EmptyInput(t *testing.T) {
	ctx := context.Background()
	emb := &mockEmbedder{dims: 4}
	mdChunker := chunker.NewMarkdownChunker(chunker.DefaultOptions())
	codeChunker := chunker.NewLineChunker(chunker.DefaultOptions())

	stored, err := ProcessFiles(ctx, nil, "test", "v1", emb, mdChunker, codeChunker, "key")
	if err != nil {
		t.Fatalf("ProcessFiles with nil files should not error: %v", err)
	}
	if stored != nil {
		t.Errorf("expected nil stored chunks for empty input, got %d", len(stored))
	}
}

func TestSyncAll_MultipleDepsMixedStatus(t *testing.T) {
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
			{ID: "dep2", Source: "github.com/test/dep2", Ref: "v2.0.0"},
		},
	}
	lf := lockfile.New()

	dep1Files := []hasher.FileContent{
		{Path: "main.go", Content: []byte("package dep1")},
	}
	dep2Files := []hasher.FileContent{
		{Path: "lib.go", Content: []byte("package dep2\n\nfunc Lib() {}")},
	}

	// Pre-compute dep1's store key and mark it as existing so it gets skipped.
	contentHash1 := hasher.ContentHash(dep1Files)
	sk1 := hasher.StoreKey(contentHash1, emb.ModelID(), "", hasher.ChunkingConfig{
		ChunkerType: "mixed",
		TargetSize:  chunker.DefaultOptions().TargetSize,
		Overlap:     chunker.DefaultOptions().Overlap,
	})
	st.keys[sk1] = true

	fetcher := &multiFetcher{
		results: map[string]*mockFetchResult{
			"dep1": {commitSHA: "sha1", files: dep1Files},
			"dep2": {commitSHA: "sha2", files: dep2Files},
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
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// dep1 should be skipped.
	var dep1Result, dep2Result *SyncResult
	for i := range results {
		switch results[i].ID {
		case "dep1":
			dep1Result = &results[i]
		case "dep2":
			dep2Result = &results[i]
		}
	}

	if dep1Result == nil {
		t.Fatal("missing result for dep1")
	}
	if dep1Result.Status != "skipped" {
		t.Errorf("dep1 status = %q, want %q", dep1Result.Status, "skipped")
	}

	if dep2Result == nil {
		t.Fatal("missing result for dep2")
	}
	if dep2Result.Status != "synced" {
		t.Errorf("dep2 status = %q, want %q", dep2Result.Status, "synced")
	}

	// Verify lockfile was written with both entries.
	if _, err := os.Stat(lockPath); os.IsNotExist(err) {
		t.Fatal("lockfile was not written")
	}
	writtenLf, err := lockfile.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("reading lockfile: %v", err)
	}
	if _, ok := writtenLf.Sources["dep1"]; !ok {
		t.Error("lockfile missing entry for dep1")
	}
	if _, ok := writtenLf.Sources["dep2"]; !ok {
		t.Error("lockfile missing entry for dep2")
	}
}

func TestSyncAll_WithLocal(t *testing.T) {
	ctx := context.Background()
	st := newMockStore()
	emb := &mockEmbedder{dims: 4}
	mdChunker := chunker.NewMarkdownChunker(chunker.DefaultOptions())
	codeChunker := chunker.NewLineChunker(chunker.DefaultOptions())

	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, "mycelium.lock")

	// Create a local directory with files to index.
	localDir := filepath.Join(tmpDir, "local-docs")
	if err := os.MkdirAll(localDir, 0755); err != nil {
		t.Fatalf("mkdir local-docs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(localDir, "notes.md"), []byte("# Notes\n\nSome local documentation that should be indexed."), 0644); err != nil {
		t.Fatalf("write notes.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(localDir, "util.go"), []byte("package util\n\nfunc Helper() string { return \"help\" }"), 0644); err != nil {
		t.Fatalf("write util.go: %v", err)
	}

	m := &manifest.Manifest{
		Config: manifest.Config{EmbeddingModel: "mock-model"},
		Local: manifest.Local{
			Index: []string{localDir},
		},
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

	// Should have results for dep1 + local.
	if len(results) != 2 {
		t.Fatalf("expected 2 results (dep + local), got %d", len(results))
	}

	var depResult, localResult *SyncResult
	for i := range results {
		switch results[i].ID {
		case "dep1":
			depResult = &results[i]
		case "local":
			localResult = &results[i]
		}
	}

	if depResult == nil {
		t.Fatal("missing result for dep1")
	}
	if depResult.Status != "synced" {
		t.Errorf("dep1 status = %q, want %q", depResult.Status, "synced")
	}

	if localResult == nil {
		t.Fatal("missing result for local")
	}
	if localResult.Status != "synced" {
		t.Errorf("local status = %q, want %q", localResult.Status, "synced")
	}

	// Verify lockfile has entries for both the dependency and local.
	writtenLf, err := lockfile.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("reading lockfile: %v", err)
	}
	if _, ok := writtenLf.Sources["dep1"]; !ok {
		t.Error("lockfile missing entry for dep1")
	}
	if localLock, ok := writtenLf.Sources["local"]; !ok {
		t.Error("lockfile missing entry for local")
	} else {
		if localLock.ContentHash == "" {
			t.Error("local entry should have a non-empty ContentHash")
		}
		if localLock.StoreKey == "" {
			t.Error("local entry should have a non-empty StoreKey")
		}
		if localLock.IngestionType != "built" {
			t.Errorf("local IngestionType = %q, want %q", localLock.IngestionType, "built")
		}
	}

	// Verify the store has chunks from both the dep and local files.
	if len(st.upserted) < 2 {
		t.Errorf("expected at least 2 upsert calls (dep + local), got %d", len(st.upserted))
	}
}

// --- Error-path tests for higher coverage ---

// mockEmbedderErr is an embedder that returns an error on Embed.
type mockEmbedderErr struct {
	dims int
	err  error
}

func (e *mockEmbedderErr) Embed(_ context.Context, _ []string) ([][]float32, error) {
	return nil, e.err
}
func (e *mockEmbedderErr) ModelID() string { return "mock-model-err" }
func (e *mockEmbedderErr) Dimensions() int { return e.dims }

// mockStoreUpsertErr is a store that returns an error on Upsert.
type mockStoreUpsertErr struct {
	mockStore
	upsertErr error
}

func (s *mockStoreUpsertErr) Upsert(_ context.Context, _ string, _ []store.StoredChunk) error {
	return s.upsertErr
}

// mockChunkerErr is a chunker that always returns an error.
type mockChunkerErr struct {
	err error
}

func (c *mockChunkerErr) Chunk(_ []byte, _ chunker.ChunkMetadata) ([]chunker.Chunk, error) {
	return nil, c.err
}

func TestSyncDependency_HasKeyError(t *testing.T) {
	ctx := context.Background()
	st := newMockStore()
	st.hasKeyErr = fmt.Errorf("store connection failed")
	emb := &mockEmbedder{dims: 4}
	mdChunker := chunker.NewMarkdownChunker(chunker.DefaultOptions())
	codeChunker := chunker.NewLineChunker(chunker.DefaultOptions())

	dep := manifest.Dependency{
		ID:     "test/repo",
		Source: "github.com/test/repo",
		Ref:    "v1.0.0",
	}
	fetcher := &mockFetcherImpl{result: &mockFetchResult{
		commitSHA: "abc123",
		files:     []hasher.FileContent{{Path: "main.go", Content: []byte("package main")}},
	}}

	_, _, err := syncDependency(ctx, dep, fetcher, st, emb, mdChunker, codeChunker)
	if err == nil {
		t.Fatal("expected error from HasKey failure")
	}
	if !strings.Contains(err.Error(), "check store key") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "check store key")
	}
}

func TestSyncLocal_HasKeyError(t *testing.T) {
	ctx := context.Background()
	st := newMockStore()
	st.hasKeyErr = fmt.Errorf("has key broken")
	emb := &mockEmbedder{dims: 4}
	mdChunker := chunker.NewMarkdownChunker(chunker.DefaultOptions())
	codeChunker := chunker.NewLineChunker(chunker.DefaultOptions())

	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "f.go"), []byte("package f"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	_, _, err := syncLocal(ctx, []string{tmpDir}, st, emb, mdChunker, codeChunker)
	if err == nil {
		t.Fatal("expected error from HasKey failure")
	}
	if !strings.Contains(err.Error(), "check store key") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "check store key")
	}
}

func TestSyncLocal_WalkError(t *testing.T) {
	ctx := context.Background()
	st := newMockStore()
	emb := &mockEmbedder{dims: 4}
	mdChunker := chunker.NewMarkdownChunker(chunker.DefaultOptions())
	codeChunker := chunker.NewLineChunker(chunker.DefaultOptions())

	// Pass a path that does not exist.
	_, _, err := syncLocal(ctx, []string{"/nonexistent/path/that/should/not/exist"}, st, emb, mdChunker, codeChunker)
	if err == nil {
		t.Fatal("expected error from walking nonexistent path")
	}
	if !strings.Contains(err.Error(), "walk local path") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "walk local path")
	}
}

func TestSyncLocal_UpsertError(t *testing.T) {
	ctx := context.Background()
	st := &mockStoreUpsertErr{
		mockStore: *newMockStore(),
		upsertErr: fmt.Errorf("upsert broken"),
	}
	emb := &mockEmbedder{dims: 4}
	mdChunker := chunker.NewMarkdownChunker(chunker.DefaultOptions())
	codeChunker := chunker.NewLineChunker(chunker.DefaultOptions())

	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "f.go"), []byte("package f\n\nfunc F() {}"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	_, _, err := syncLocal(ctx, []string{tmpDir}, st, emb, mdChunker, codeChunker)
	if err == nil {
		t.Fatal("expected error from Upsert failure")
	}
	if !strings.Contains(err.Error(), "upsert local") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "upsert local")
	}
}

func TestProcessFiles_EmbedError(t *testing.T) {
	ctx := context.Background()
	emb := &mockEmbedderErr{dims: 4, err: fmt.Errorf("embedding service down")}
	mdChunker := chunker.NewMarkdownChunker(chunker.DefaultOptions())
	codeChunker := chunker.NewLineChunker(chunker.DefaultOptions())

	files := []hasher.FileContent{
		{Path: "main.go", Content: []byte("package main\n\nfunc main() {}")},
	}

	_, err := ProcessFiles(ctx, files, "test", "v1", emb, mdChunker, codeChunker, "key")
	if err == nil {
		t.Fatal("expected error from embed failure")
	}
	if !strings.Contains(err.Error(), "embed") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "embed")
	}
}

func TestProcessFiles_ChunkError(t *testing.T) {
	ctx := context.Background()
	emb := &mockEmbedder{dims: 4}
	badChunker := &mockChunkerErr{err: fmt.Errorf("chunking failed")}
	goodChunker := chunker.NewLineChunker(chunker.DefaultOptions())

	// Use a .md file so it hits mdChunker (the bad one).
	files := []hasher.FileContent{
		{Path: "README.md", Content: []byte("# Hello\n\nSome content.")},
	}

	_, err := ProcessFiles(ctx, files, "test", "v1", emb, badChunker, goodChunker, "key")
	if err == nil {
		t.Fatal("expected error from chunking failure")
	}
	if !strings.Contains(err.Error(), "chunk") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "chunk")
	}
}

func TestSyncAll_DepError_ContinuesToOthers(t *testing.T) {
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
			{ID: "fail-dep", Source: "github.com/test/fail", Ref: "v1.0.0"},
			{ID: "ok-dep", Source: "github.com/test/ok", Ref: "v2.0.0"},
		},
	}
	lf := lockfile.New()

	// fail-dep has no entry in multiFetcher, so it will error.
	fetcher := &multiFetcher{
		results: map[string]*mockFetchResult{
			"ok-dep": {commitSHA: "sha2", files: []hasher.FileContent{
				{Path: "lib.go", Content: []byte("package lib\n\nfunc Lib() {}")},
			}},
		},
	}

	opts := SyncOptions{
		LockfilePath: lockPath,
		Output:       io.Discard,
	}

	results, err := syncAll(ctx, m, lf, st, emb, fetcher, mdChunker, codeChunker, opts)
	if err != nil {
		t.Fatalf("syncAll should not return top-level error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	var failResult, okResult *SyncResult
	for i := range results {
		switch results[i].ID {
		case "fail-dep":
			failResult = &results[i]
		case "ok-dep":
			okResult = &results[i]
		}
	}

	if failResult == nil || failResult.Status != "error" {
		t.Errorf("fail-dep should have status 'error', got %v", failResult)
	}
	if failResult != nil && failResult.Err == nil {
		t.Error("fail-dep should have a non-nil error")
	}
	if okResult == nil || okResult.Status != "synced" {
		t.Errorf("ok-dep should have status 'synced', got %v", okResult)
	}
}

func TestSyncAll_LocalSkipped(t *testing.T) {
	ctx := context.Background()
	st := newMockStore()
	emb := &mockEmbedder{dims: 4}
	mdChunker := chunker.NewMarkdownChunker(chunker.DefaultOptions())
	codeChunker := chunker.NewLineChunker(chunker.DefaultOptions())

	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, "mycelium.lock")

	// Create a local directory with a file.
	localDir := filepath.Join(tmpDir, "docs")
	if err := os.MkdirAll(localDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	localFilePath := filepath.Join(localDir, "guide.md")
	if err := os.WriteFile(localFilePath, []byte("# Guide\n\nSome content."), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	// Pre-compute the local store key so it will be skipped.
	localFiles := []hasher.FileContent{
		{Path: localFilePath, Content: []byte("# Guide\n\nSome content.")},
	}
	contentHash := hasher.ContentHash(localFiles)
	sk := hasher.StoreKey(contentHash, emb.ModelID(), "", hasher.ChunkingConfig{
		ChunkerType: "mixed",
		TargetSize:  chunker.DefaultOptions().TargetSize,
		Overlap:     chunker.DefaultOptions().Overlap,
	})
	st.keys[sk] = true

	m := &manifest.Manifest{
		Config: manifest.Config{EmbeddingModel: "mock-model"},
		Local: manifest.Local{
			Index: []string{localDir},
		},
	}
	lf := lockfile.New()
	fetcher := &multiFetcher{results: map[string]*mockFetchResult{}}

	opts := SyncOptions{
		LockfilePath: lockPath,
		Output:       io.Discard,
	}

	results, err := syncAll(ctx, m, lf, st, emb, fetcher, mdChunker, codeChunker, opts)
	if err != nil {
		t.Fatalf("syncAll error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result (local), got %d", len(results))
	}
	if results[0].ID != "local" {
		t.Errorf("result ID = %q, want %q", results[0].ID, "local")
	}
	if results[0].Status != "skipped" {
		t.Errorf("local status = %q, want %q", results[0].Status, "skipped")
	}
}

func TestSyncAll_LocalError(t *testing.T) {
	ctx := context.Background()
	st := newMockStore()
	st.hasKeyErr = fmt.Errorf("store broken")
	emb := &mockEmbedder{dims: 4}
	mdChunker := chunker.NewMarkdownChunker(chunker.DefaultOptions())
	codeChunker := chunker.NewLineChunker(chunker.DefaultOptions())

	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, "mycelium.lock")

	localDir := filepath.Join(tmpDir, "docs")
	if err := os.MkdirAll(localDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(localDir, "f.go"), []byte("package f"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	m := &manifest.Manifest{
		Config: manifest.Config{EmbeddingModel: "mock-model"},
		Local: manifest.Local{
			Index: []string{localDir},
		},
	}
	lf := lockfile.New()
	fetcher := &multiFetcher{results: map[string]*mockFetchResult{}}

	opts := SyncOptions{
		LockfilePath: lockPath,
		Output:       io.Discard,
	}

	results, err := syncAll(ctx, m, lf, st, emb, fetcher, mdChunker, codeChunker, opts)
	if err != nil {
		t.Fatalf("syncAll should not return top-level error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result (local), got %d", len(results))
	}
	if results[0].Status != "error" {
		t.Errorf("local status = %q, want %q", results[0].Status, "error")
	}
	if results[0].Err == nil {
		t.Error("local result should have a non-nil error")
	}
}

func TestSyncDependency_UpsertError(t *testing.T) {
	ctx := context.Background()
	st := &mockStoreUpsertErr{
		mockStore: *newMockStore(),
		upsertErr: fmt.Errorf("upsert disk full"),
	}
	emb := &mockEmbedder{dims: 4}
	mdChunker := chunker.NewMarkdownChunker(chunker.DefaultOptions())
	codeChunker := chunker.NewLineChunker(chunker.DefaultOptions())

	dep := manifest.Dependency{
		ID:     "test/repo",
		Source: "github.com/test/repo",
		Ref:    "v1.0.0",
	}
	fetcher := &mockFetcherImpl{result: &mockFetchResult{
		commitSHA: "abc123",
		files:     []hasher.FileContent{{Path: "main.go", Content: []byte("package main\n\nfunc main() {}")}},
	}}

	_, _, err := syncDependency(ctx, dep, fetcher, st, emb, mdChunker, codeChunker)
	if err == nil {
		t.Fatal("expected error from Upsert failure")
	}
	if !strings.Contains(err.Error(), "upsert") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "upsert")
	}
}

func TestSyncDependency_ProcessFilesError(t *testing.T) {
	ctx := context.Background()
	st := newMockStore()
	emb := &mockEmbedderErr{dims: 4, err: fmt.Errorf("embedding broken")}
	mdChunker := chunker.NewMarkdownChunker(chunker.DefaultOptions())
	codeChunker := chunker.NewLineChunker(chunker.DefaultOptions())

	dep := manifest.Dependency{
		ID:     "test/repo",
		Source: "github.com/test/repo",
		Ref:    "v1.0.0",
	}
	fetcher := &mockFetcherImpl{result: &mockFetchResult{
		commitSHA: "abc123",
		files:     []hasher.FileContent{{Path: "main.go", Content: []byte("package main\n\nfunc main() {}")}},
	}}

	_, _, err := syncDependency(ctx, dep, fetcher, st, emb, mdChunker, codeChunker)
	if err == nil {
		t.Fatal("expected error from ProcessFiles (embed) failure")
	}
	if !strings.Contains(err.Error(), "embed") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "embed")
	}
}

func TestSyncLocal_ProcessFilesError(t *testing.T) {
	ctx := context.Background()
	st := newMockStore()
	emb := &mockEmbedderErr{dims: 4, err: fmt.Errorf("embedding broken")}
	mdChunker := chunker.NewMarkdownChunker(chunker.DefaultOptions())
	codeChunker := chunker.NewLineChunker(chunker.DefaultOptions())

	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte("package main\n\nfunc main() {}"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	_, _, err := syncLocal(ctx, []string{tmpDir}, st, emb, mdChunker, codeChunker)
	if err == nil {
		t.Fatal("expected error from ProcessFiles (embed) failure in syncLocal")
	}
	if !strings.Contains(err.Error(), "embed") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "embed")
	}
}

func TestSyncLocal_FileReadError(t *testing.T) {
	ctx := context.Background()
	st := newMockStore()
	emb := &mockEmbedder{dims: 4}
	mdChunker := chunker.NewMarkdownChunker(chunker.DefaultOptions())
	codeChunker := chunker.NewLineChunker(chunker.DefaultOptions())

	tmpDir := t.TempDir()
	// Create a file that cannot be read (write-only permissions).
	unreadable := filepath.Join(tmpDir, "secret.go")
	if err := os.WriteFile(unreadable, []byte("package secret"), 0000); err != nil {
		t.Fatalf("write file: %v", err)
	}

	_, _, err := syncLocal(ctx, []string{tmpDir}, st, emb, mdChunker, codeChunker)
	if err == nil {
		t.Fatal("expected error from unreadable file")
	}
	if !strings.Contains(err.Error(), "walk local path") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "walk local path")
	}
}

func TestSyncAll_ArtifactError_FallsBackToSource(t *testing.T) {
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

	// Pre-populate lockfile with an artifact URL that will fail to download,
	// forcing the artifact error path (artErr != nil) in syncAll.
	lf := lockfile.New()
	lf.SetSource("dep1", lockfile.SourceLock{
		ArtifactURL:  "http://localhost:1/nonexistent-artifact.jsonl.gz",
		ArtifactHash: "sha256:fakehash",
	})

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
	// Should have fallen back to source and synced successfully.
	if results[0].Status != "synced" {
		t.Errorf("dep1 status = %q, want %q", results[0].Status, "synced")
	}
}

func TestSyncDependencyFromArtifact_Success(t *testing.T) {
	// Create a valid gzipped JSONL artifact and serve it via httptest.
	chunks := []store.StoredChunk{
		{
			Text:          "package main",
			ChunkType:     "code",
			ChunkIndex:    0,
			Path:          "main.go",
			Source:        "test/repo",
			SourceVersion: "v1.0.0",
			StoreKey:      "sha256:artifactstorekey",
			Language:      "go",
			Vector:        []float32{0.1, 0.2, 0.3, 0.4},
		},
	}
	meta := artifact.ArtifactMeta{
		Source:         "test/repo",
		SourceVersion:  "v1.0.0",
		Commit:         "abc123",
		EmbeddingModel: "mock-model",
		StoreKey:       "sha256:artifactstorekey",
	}

	var buf bytes.Buffer
	if err := artifact.Write(&buf, chunks, meta); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	artifactData := buf.Bytes()

	// Compute checksum of the artifact.
	h := sha256.Sum256(artifactData)
	checksum := fmt.Sprintf("sha256:%x", h)

	// Serve the artifact and its checksum via httptest.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".sha256") {
			fmt.Fprintf(w, "%s", strings.TrimPrefix(checksum, "sha256:"))
			return
		}
		w.Header().Set("Content-Type", "application/gzip")
		w.Write(artifactData)
	}))
	defer srv.Close()

	ctx := context.Background()
	st := newMockStore()

	dep := manifest.Dependency{
		ID:     "test/repo",
		Source: "github.com/test/repo",
		Ref:    "v1.0.0",
	}

	// Pre-populate lockfile with the test server artifact URL.
	existingLock := &lockfile.SourceLock{
		ArtifactURL:  srv.URL + "/artifact.jsonl.gz",
		ArtifactHash: checksum,
	}

	sl, skipped, found, err := syncDependencyFromArtifact(ctx, dep, "mock-model", st, existingLock)
	if err != nil {
		t.Fatalf("syncDependencyFromArtifact error: %v", err)
	}
	if !found {
		t.Error("expected found=true for valid artifact")
	}
	if skipped {
		t.Error("expected skipped=false on first load")
	}
	if sl == nil {
		t.Fatal("expected non-nil SourceLock")
	}
	if sl.StoreKey != "sha256:artifactstorekey" {
		t.Errorf("StoreKey = %q, want %q", sl.StoreKey, "sha256:artifactstorekey")
	}
	if sl.IngestionType != "artifact" {
		t.Errorf("IngestionType = %q, want %q", sl.IngestionType, "artifact")
	}
	if sl.ArtifactURL != srv.URL+"/artifact.jsonl.gz" {
		t.Errorf("ArtifactURL = %q, want %q", sl.ArtifactURL, srv.URL+"/artifact.jsonl.gz")
	}

	// Verify chunks were upserted.
	if len(st.upserted) == 0 {
		t.Error("expected Upsert to be called")
	}
}

func TestSyncDependencyFromArtifact_AlreadyExists(t *testing.T) {
	// Same as above but pre-populate the store key so it's skipped.
	chunks := []store.StoredChunk{
		{
			Text:      "package main",
			ChunkType: "code",
			Path:      "main.go",
			Source:    "test/repo",
			StoreKey:  "sha256:artifactstorekey",
			Vector:    []float32{0.1, 0.2, 0.3, 0.4},
		},
	}
	meta := artifact.ArtifactMeta{
		Source:   "test/repo",
		Commit:   "abc123",
		StoreKey: "sha256:artifactstorekey",
	}

	var buf bytes.Buffer
	if err := artifact.Write(&buf, chunks, meta); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	artifactData := buf.Bytes()

	h := sha256.Sum256(artifactData)
	checksum := fmt.Sprintf("sha256:%x", h)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(artifactData)
	}))
	defer srv.Close()

	ctx := context.Background()
	st := newMockStore()
	st.keys["sha256:artifactstorekey"] = true // Pre-populate so it skips.

	dep := manifest.Dependency{
		ID:     "test/repo",
		Source: "github.com/test/repo",
		Ref:    "v1.0.0",
	}

	existingLock := &lockfile.SourceLock{
		ArtifactURL:  srv.URL + "/artifact.jsonl.gz",
		ArtifactHash: checksum,
	}

	sl, skipped, found, err := syncDependencyFromArtifact(ctx, dep, "mock-model", st, existingLock)
	if err != nil {
		t.Fatalf("syncDependencyFromArtifact error: %v", err)
	}
	if !found {
		t.Error("expected found=true")
	}
	if !skipped {
		t.Error("expected skipped=true when key already exists")
	}
	if sl.IngestionType != "artifact" {
		t.Errorf("IngestionType = %q, want %q", sl.IngestionType, "artifact")
	}

	// Upsert should NOT have been called.
	if len(st.upserted) != 0 {
		t.Error("Upsert should not be called when key already exists")
	}
}

func TestSyncAll_LockfileWriteError(t *testing.T) {
	ctx := context.Background()
	st := newMockStore()
	emb := &mockEmbedder{dims: 4}
	mdChunker := chunker.NewMarkdownChunker(chunker.DefaultOptions())
	codeChunker := chunker.NewLineChunker(chunker.DefaultOptions())

	m := &manifest.Manifest{
		Config: manifest.Config{EmbeddingModel: "mock-model"},
	}
	lf := lockfile.New()
	fetcher := &multiFetcher{results: map[string]*mockFetchResult{}}

	// Point lockfile to a path in a nonexistent directory so WriteFile fails.
	opts := SyncOptions{
		LockfilePath: "/nonexistent/dir/mycelium.lock",
		Output:       io.Discard,
	}

	_, err := syncAll(ctx, m, lf, st, emb, fetcher, mdChunker, codeChunker, opts)
	if err == nil {
		t.Fatal("expected error from lockfile write failure")
	}
	if !strings.Contains(err.Error(), "write lockfile") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "write lockfile")
	}
}
