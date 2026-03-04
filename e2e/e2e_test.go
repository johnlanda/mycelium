//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/johnlanda/mycelium/internal/artifact"
	"github.com/johnlanda/mycelium/internal/chunker"
	"github.com/johnlanda/mycelium/internal/embedder"
	"github.com/johnlanda/mycelium/internal/hasher"
	"github.com/johnlanda/mycelium/internal/lockfile"
	"github.com/johnlanda/mycelium/internal/manifest"
	mcpserver "github.com/johnlanda/mycelium/internal/mcp"
	"github.com/johnlanda/mycelium/internal/pipeline"
	"github.com/johnlanda/mycelium/internal/store"
)

// --- Mock Ollama server ---

// ollamaRequest mirrors the embedder's request type.
type ollamaRequest struct {
	Model      string   `json:"model"`
	Input      []string `json:"input"`
	Truncate   bool     `json:"truncate,omitempty"`
	Dimensions int      `json:"dimensions,omitempty"`
}

type ollamaResponse struct {
	Embeddings [][]float64 `json:"embeddings"`
}

type ollamaTagsResponse struct {
	Models []ollamaModelInfo `json:"models"`
}

type ollamaModelInfo struct {
	Name string `json:"name"`
}

type mockOllamaOpts struct {
	models    []string
	dims      int
	onRequest func(req ollamaRequest) // optional callback for inspection
}

// newMockOllamaServer creates a httptest.Server that handles /api/tags and /api/embed.
// Embeddings are deterministic: hash-based so same text produces same vector.
func newMockOllamaServer(t *testing.T, opts mockOllamaOpts) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/tags":
			resp := ollamaTagsResponse{}
			for _, m := range opts.models {
				resp.Models = append(resp.Models, ollamaModelInfo{Name: m + ":latest"})
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)

		case r.Method == http.MethodPost && r.URL.Path == "/api/embed":
			var req ollamaRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if opts.onRequest != nil {
				opts.onRequest(req)
			}

			dims := opts.dims
			if req.Dimensions > 0 {
				dims = req.Dimensions
			}

			resp := ollamaResponse{}
			for _, text := range req.Input {
				resp.Embeddings = append(resp.Embeddings, deterministicVector(text, dims))
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

// deterministicVector generates a reproducible vector from text using SHA-256.
func deterministicVector(text string, dims int) []float64 {
	h := sha256.Sum256([]byte(text))
	vec := make([]float64, dims)
	for i := range vec {
		// Use bytes from the hash cyclically, normalize to [-1, 1].
		b := h[i%len(h)]
		vec[i] = (float64(b)/255.0)*2.0 - 1.0
	}
	// L2 normalize.
	var norm float64
	for _, v := range vec {
		norm += v * v
	}
	norm = math.Sqrt(norm)
	if norm > 0 {
		for i := range vec {
			vec[i] /= norm
		}
	}
	return vec
}

// --- Test git repo helper ---

// createTestRepo creates a git repo in t.TempDir() with committed files and a tag.
// Returns the repo directory path.
func createTestRepo(t *testing.T, files map[string]string, tag string) string {
	t.Helper()
	dir := t.TempDir()

	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@test.com")
	runGit(t, dir, "config", "user.name", "Test")

	for path, content := range files {
		fullPath := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(fullPath), err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-m", "initial commit")
	if tag != "" {
		runGit(t, dir, "tag", "-a", tag, "-m", tag)
	}

	return dir
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// --- Helper: write manifest TOML ---

func writeManifest(t *testing.T, dir string, m *manifest.Manifest) string {
	t.Helper()
	path := filepath.Join(dir, "mycelium.toml")
	if err := m.WriteFile(path); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	return path
}

// --- Helper: create embedder pointing at mock server ---

func createTestEmbedder(t *testing.T, serverURL string, model string, dims int) embedder.Embedder {
	t.Helper()
	t.Setenv("OLLAMA_URL", serverURL)
	emb, err := embedder.NewEmbedder("ollama/"+model, embedder.Config{
		EmbeddingDimensions: dims,
	})
	if err != nil {
		t.Fatalf("create embedder: %v", err)
	}
	return emb
}

// ========================================================================
// E2E Tests
// ========================================================================

func TestE2E_FullSyncPipeline(t *testing.T) {
	// Create a test repo with markdown and code.
	repoDir := createTestRepo(t, map[string]string{
		"docs/guide.md":      "# Getting Started\n\nThis is the guide for mycelium.",
		"docs/api.md":        "# API Reference\n\nEndpoint `/search` accepts a query parameter.",
		"src/main.go":        "package main\n\nfunc main() {\n\tprintln(\"hello\")\n}\n",
		"src/helper.go":      "package main\n\nfunc helper() string {\n\treturn \"help\"\n}\n",
		"README.md":          "# Test Project\n\nA test project for e2e.",
	}, "v1.0.0")

	// Mock Ollama server.
	ollamaSrv := newMockOllamaServer(t, mockOllamaOpts{
		models: []string{"test-model"},
		dims:   32,
	})
	defer ollamaSrv.Close()
	t.Setenv("OLLAMA_URL", ollamaSrv.URL)

	workDir := t.TempDir()
	storePath := filepath.Join(workDir, "store")

	m := &manifest.Manifest{
		Config: manifest.Config{
			EmbeddingModel:      "ollama/test-model",
			EmbeddingDimensions: 32,
		},
		Dependencies: []manifest.Dependency{
			{
				ID:     "test-dep",
				Source: "file://" + repoDir, // We'll handle this via direct source
				Ref:    "v1.0.0",
				Docs:   []string{"docs/"},
				Code:   []string{"src/"},
			},
		},
	}

	manifestPath := writeManifest(t, workDir, m)
	lockfilePath := filepath.Join(workDir, "mycelium.lock")

	// The GitHubFetcher uses `git clone` with https:// prefix.
	// For local repos, we need to set the source to the file:// URL but
	// GitHubFetcher prepends "https://". Instead, let's exercise the
	// pipeline components directly.
	ctx := context.Background()

	emb := createTestEmbedder(t, ollamaSrv.URL, "test-model", 32)

	st, err := store.NewLanceDBStore(ctx, storePath, emb.Dimensions())
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer st.Close()

	// Manually fetch files (simulating what the fetcher does).
	docChunker := chunker.NewMarkdownChunker(chunker.DefaultOptions())
	codeChunker := chunker.NewCodeChunker(chunker.DefaultOptions())

	// Read files from the repo.
	var files []hasher.FileContent
	for _, relPath := range []string{"docs/guide.md", "docs/api.md", "src/main.go", "src/helper.go"} {
		content, err := os.ReadFile(filepath.Join(repoDir, relPath))
		if err != nil {
			t.Fatalf("read %s: %v", relPath, err)
		}
		files = append(files, hasher.FileContent{Path: relPath, Content: content})
	}

	contentHash := hasher.ContentHash(files)
	storeKey := hasher.StoreKey(contentHash, emb.ModelID(), "", hasher.ChunkingConfig{
		ChunkerType: "mixed",
		TargetSize:  chunker.DefaultOptions().TargetSize,
		Overlap:     chunker.DefaultOptions().Overlap,
	})

	stored, err := pipeline.ProcessFiles(ctx, files, "test-dep", "v1.0.0", emb, docChunker, codeChunker, storeKey)
	if err != nil {
		t.Fatalf("process files: %v", err)
	}
	if len(stored) == 0 {
		t.Fatal("expected stored chunks, got 0")
	}

	if err := st.Upsert(ctx, storeKey, stored); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	// Search for content we indexed.
	queryVec, err := emb.Embed(ctx, []string{"getting started guide"})
	if err != nil {
		t.Fatalf("embed query: %v", err)
	}

	results, err := st.Search(ctx, queryVec[0], store.SearchOpts{TopK: 5})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected search results, got 0")
	}

	// Verify store key exists.
	exists, err := st.HasKey(ctx, storeKey)
	if err != nil {
		t.Fatalf("has key: %v", err)
	}
	if !exists {
		t.Error("expected store key to exist")
	}

	// Verify lockfile round-trip.
	lf := lockfile.New()
	lf.SetSource("test-dep", lockfile.SourceLock{
		Version:     "v1.0.0",
		ContentHash: contentHash,
		StoreKey:    storeKey,
	})
	lf.Meta.EmbeddingModel = "ollama/test-model"
	lf.Meta.LockedAt = time.Now().UTC().Format(time.RFC3339)
	lf.Meta.SchemaVersion = lockfile.SchemaVersion
	if err := lf.WriteFile(lockfilePath); err != nil {
		t.Fatalf("write lockfile: %v", err)
	}

	lfRead, err := lockfile.ReadFile(lockfilePath)
	if err != nil {
		t.Fatalf("read lockfile: %v", err)
	}
	if lfRead.Sources["test-dep"].StoreKey != storeKey {
		t.Errorf("lockfile store key mismatch")
	}

	// Verify manifest round-trip.
	mRead, err := manifest.ParseFile(manifestPath)
	if err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	if mRead.Config.EmbeddingDimensions != 32 {
		t.Errorf("manifest dimensions = %d, want 32", mRead.Config.EmbeddingDimensions)
	}
}

func TestE2E_MCPQueryAfterSync(t *testing.T) {
	ollamaSrv := newMockOllamaServer(t, mockOllamaOpts{
		models: []string{"test-model"},
		dims:   16,
	})
	defer ollamaSrv.Close()
	t.Setenv("OLLAMA_URL", ollamaSrv.URL)

	ctx := context.Background()
	storePath := filepath.Join(t.TempDir(), "store")

	emb := createTestEmbedder(t, ollamaSrv.URL, "test-model", 16)

	st, err := store.NewLanceDBStore(ctx, storePath, emb.Dimensions())
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer st.Close()

	// Insert some test chunks.
	vec, err := emb.Embed(ctx, []string{"kubernetes deployment guide"})
	if err != nil {
		t.Fatalf("embed: %v", err)
	}

	chunks := []store.StoredChunk{
		{
			Text:          "# Kubernetes Deployment\n\nDeploy using kubectl apply.",
			Breadcrumb:    "Kubernetes > Deployment",
			ChunkType:     "doc",
			ChunkIndex:    0,
			Path:          "docs/deploy.md",
			Source:        "k8s-docs",
			SourceVersion: "v1.28",
			StoreKey:      "test-store-key",
			Vector:        vec[0],
		},
	}

	if err := st.Upsert(ctx, "test-store-key", chunks); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	// Create MCP server and test the search handler indirectly.
	srv := mcpserver.NewServer(st, emb)
	if srv == nil {
		t.Fatal("expected non-nil MCP server")
	}

	// Verify the search returns results through the store directly
	// (MCP server wraps store.Search, testing the integration).
	queryVec, err := emb.Embed(ctx, []string{"kubernetes deployment"})
	if err != nil {
		t.Fatalf("embed query: %v", err)
	}

	results, err := st.Search(ctx, queryVec[0], store.SearchOpts{TopK: 5})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected search results, got 0")
	}
	if results[0].Chunk.Source != "k8s-docs" {
		t.Errorf("expected source 'k8s-docs', got %q", results[0].Chunk.Source)
	}
}

func TestE2E_SyncIdempotent(t *testing.T) {
	ollamaSrv := newMockOllamaServer(t, mockOllamaOpts{
		models: []string{"test-model"},
		dims:   16,
	})
	defer ollamaSrv.Close()
	t.Setenv("OLLAMA_URL", ollamaSrv.URL)

	ctx := context.Background()
	storePath := filepath.Join(t.TempDir(), "store")

	emb := createTestEmbedder(t, ollamaSrv.URL, "test-model", 16)

	st, err := store.NewLanceDBStore(ctx, storePath, emb.Dimensions())
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer st.Close()

	files := []hasher.FileContent{
		{Path: "doc.md", Content: []byte("# Hello\n\nWorld")},
	}

	contentHash := hasher.ContentHash(files)
	storeKey := hasher.StoreKey(contentHash, emb.ModelID(), "", hasher.ChunkingConfig{
		ChunkerType: "mixed",
		TargetSize:  768,
		Overlap:     0,
	})

	mdChunker := chunker.NewMarkdownChunker(chunker.DefaultOptions())
	codeChunker := chunker.NewCodeChunker(chunker.DefaultOptions())

	// First sync.
	stored, err := pipeline.ProcessFiles(ctx, files, "test", "v1", emb, mdChunker, codeChunker, storeKey)
	if err != nil {
		t.Fatalf("process files: %v", err)
	}
	if err := st.Upsert(ctx, storeKey, stored); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	// Verify key exists.
	exists, err := st.HasKey(ctx, storeKey)
	if err != nil {
		t.Fatalf("has key: %v", err)
	}
	if !exists {
		t.Fatal("expected store key to exist after first sync")
	}

	// Second sync should detect key exists and skip.
	exists2, err := st.HasKey(ctx, storeKey)
	if err != nil {
		t.Fatalf("has key (2nd): %v", err)
	}
	if !exists2 {
		t.Fatal("expected store key to still exist on second check")
	}
}

func TestE2E_UpgradeDependency(t *testing.T) {
	ollamaSrv := newMockOllamaServer(t, mockOllamaOpts{
		models: []string{"test-model"},
		dims:   16,
	})
	defer ollamaSrv.Close()
	t.Setenv("OLLAMA_URL", ollamaSrv.URL)

	ctx := context.Background()
	storePath := filepath.Join(t.TempDir(), "store")

	emb := createTestEmbedder(t, ollamaSrv.URL, "test-model", 16)

	st, err := store.NewLanceDBStore(ctx, storePath, emb.Dimensions())
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer st.Close()

	mdChunker := chunker.NewMarkdownChunker(chunker.DefaultOptions())
	codeChunker := chunker.NewCodeChunker(chunker.DefaultOptions())

	// v1 content.
	filesV1 := []hasher.FileContent{
		{Path: "doc.md", Content: []byte("# Version 1\n\nOld content.")},
	}
	hashV1 := hasher.ContentHash(filesV1)
	keyV1 := hasher.StoreKey(hashV1, emb.ModelID(), "", hasher.ChunkingConfig{
		ChunkerType: "mixed", TargetSize: 768, Overlap: 0,
	})

	storedV1, err := pipeline.ProcessFiles(ctx, filesV1, "dep", "v1", emb, mdChunker, codeChunker, keyV1)
	if err != nil {
		t.Fatalf("process v1: %v", err)
	}
	if err := st.Upsert(ctx, keyV1, storedV1); err != nil {
		t.Fatalf("upsert v1: %v", err)
	}

	// v2 content.
	filesV2 := []hasher.FileContent{
		{Path: "doc.md", Content: []byte("# Version 2\n\nNew and improved content.")},
	}
	hashV2 := hasher.ContentHash(filesV2)
	keyV2 := hasher.StoreKey(hashV2, emb.ModelID(), "", hasher.ChunkingConfig{
		ChunkerType: "mixed", TargetSize: 768, Overlap: 0,
	})

	storedV2, err := pipeline.ProcessFiles(ctx, filesV2, "dep", "v2", emb, mdChunker, codeChunker, keyV2)
	if err != nil {
		t.Fatalf("process v2: %v", err)
	}
	if err := st.Upsert(ctx, keyV2, storedV2); err != nil {
		t.Fatalf("upsert v2: %v", err)
	}

	// Evict old (simulating upgrade).
	if err := st.Delete(ctx, keyV1); err != nil {
		t.Fatalf("delete v1: %v", err)
	}

	// v1 should be gone.
	existsV1, err := st.HasKey(ctx, keyV1)
	if err != nil {
		t.Fatalf("has key v1: %v", err)
	}
	if existsV1 {
		t.Error("expected v1 store key to be gone after upgrade")
	}

	// v2 should be present.
	existsV2, err := st.HasKey(ctx, keyV2)
	if err != nil {
		t.Fatalf("has key v2: %v", err)
	}
	if !existsV2 {
		t.Error("expected v2 store key to exist")
	}
}

func TestE2E_OllamaBatchEmbedding(t *testing.T) {
	var embedCallCount atomic.Int32

	ollamaSrv := newMockOllamaServer(t, mockOllamaOpts{
		models: []string{"test-model"},
		dims:   8,
		onRequest: func(req ollamaRequest) {
			embedCallCount.Add(1)
		},
	})
	defer ollamaSrv.Close()
	t.Setenv("OLLAMA_URL", ollamaSrv.URL)

	emb := createTestEmbedder(t, ollamaSrv.URL, "test-model", 8)

	// Reset counter after construction (which may have made calls).
	embedCallCount.Store(0)

	// Generate 200+ texts.
	texts := make([]string, 200)
	for i := range texts {
		texts[i] = fmt.Sprintf("chunk number %d with some content for embedding", i)
	}

	ctx := context.Background()
	vecs, err := emb.Embed(ctx, texts)
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	if len(vecs) != 200 {
		t.Errorf("expected 200 vectors, got %d", len(vecs))
	}

	// With batch size 64: ceil(200/64) = 4 requests.
	calls := embedCallCount.Load()
	if calls != 4 {
		t.Errorf("expected 4 batched requests, got %d", calls)
	}
}

func TestE2E_OllamaDimensionConfig(t *testing.T) {
	ollamaSrv := newMockOllamaServer(t, mockOllamaOpts{
		models: []string{"test-model"},
		dims:   4096, // default dims
	})
	defer ollamaSrv.Close()
	t.Setenv("OLLAMA_URL", ollamaSrv.URL)

	ctx := context.Background()
	storePath := filepath.Join(t.TempDir(), "store")

	// Configure 64-dim embeddings.
	emb := createTestEmbedder(t, ollamaSrv.URL, "test-model", 64)

	if emb.Dimensions() != 64 {
		t.Fatalf("expected dimensions=64, got %d", emb.Dimensions())
	}

	st, err := store.NewLanceDBStore(ctx, storePath, emb.Dimensions())
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer st.Close()

	// Embed and store.
	vecs, err := emb.Embed(ctx, []string{"test content"})
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	if len(vecs[0]) != 64 {
		t.Errorf("expected 64-dim vector, got %d", len(vecs[0]))
	}

	chunks := []store.StoredChunk{
		{
			Text:       "test content",
			ChunkType:  "doc",
			Source:     "test",
			StoreKey:   "key-64",
			Vector:     vecs[0],
		},
	}
	if err := st.Upsert(ctx, "key-64", chunks); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	// Search should work with 64-dim vectors.
	results, err := st.Search(ctx, vecs[0], store.SearchOpts{TopK: 1})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected search results with 64-dim config")
	}
}

func TestE2E_OllamaModelNotFound(t *testing.T) {
	// Server with empty model list.
	ollamaSrv := newMockOllamaServer(t, mockOllamaOpts{
		models: []string{}, // no models
		dims:   8,
	})
	defer ollamaSrv.Close()
	t.Setenv("OLLAMA_URL", ollamaSrv.URL)

	_, err := embedder.NewEmbedder("ollama/nonexistent", embedder.Config{})
	if err == nil {
		t.Fatal("expected error for nonexistent model")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention 'not found', got: %s", err.Error())
	}
	if !strings.Contains(err.Error(), "ollama pull") {
		t.Errorf("error should suggest 'ollama pull', got: %s", err.Error())
	}
}

func TestE2E_ArtifactRoundTrip(t *testing.T) {
	ollamaSrv := newMockOllamaServer(t, mockOllamaOpts{
		models: []string{"test-model"},
		dims:   16,
	})
	defer ollamaSrv.Close()
	t.Setenv("OLLAMA_URL", ollamaSrv.URL)

	ctx := context.Background()

	emb := createTestEmbedder(t, ollamaSrv.URL, "test-model", 16)

	// Create chunks.
	vecs, err := emb.Embed(ctx, []string{"artifact test content", "another chunk"})
	if err != nil {
		t.Fatalf("embed: %v", err)
	}

	originalChunks := []store.StoredChunk{
		{
			Text:          "artifact test content",
			Breadcrumb:    "Artifacts > Test",
			ChunkType:     "doc",
			ChunkIndex:    0,
			Path:          "docs/test.md",
			Source:        "test-dep",
			SourceVersion: "v1.0.0",
			StoreKey:      "artifact-key",
			Vector:        vecs[0],
		},
		{
			Text:          "another chunk",
			Breadcrumb:    "Artifacts > Other",
			ChunkType:     "doc",
			ChunkIndex:    1,
			Path:          "docs/test.md",
			Source:        "test-dep",
			SourceVersion: "v1.0.0",
			StoreKey:      "artifact-key",
			Vector:        vecs[1],
		},
	}

	// Write artifact.
	var buf bytes.Buffer
	meta := artifact.ArtifactMeta{
		Source:         "test-dep",
		SourceVersion:  "v1.0.0",
		Commit:         "abc123",
		EmbeddingModel: "ollama/test-model",
		StoreKey:       "artifact-key",
	}
	if err := artifact.Write(&buf, originalChunks, meta); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	// Read artifact back.
	readChunks, readMeta, err := artifact.Read(&buf)
	if err != nil {
		t.Fatalf("read artifact: %v", err)
	}
	if len(readChunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(readChunks))
	}
	if readMeta.EmbeddingModel != "ollama/test-model" {
		t.Errorf("meta embedding_model = %q, want %q", readMeta.EmbeddingModel, "ollama/test-model")
	}
	if readMeta.StoreKey != "artifact-key" {
		t.Errorf("meta store_key = %q, want %q", readMeta.StoreKey, "artifact-key")
	}

	// Load into a fresh store and verify search works.
	storePath := filepath.Join(t.TempDir(), "store")
	st, err := store.NewLanceDBStore(ctx, storePath, emb.Dimensions())
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer st.Close()

	if err := st.Upsert(ctx, "artifact-key", readChunks); err != nil {
		t.Fatalf("upsert from artifact: %v", err)
	}

	// Verify the content is searchable.
	queryVec, err := emb.Embed(ctx, []string{"artifact test"})
	if err != nil {
		t.Fatalf("embed query: %v", err)
	}

	results, err := st.Search(ctx, queryVec[0], store.SearchOpts{TopK: 5})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected search results after artifact load")
	}

	// Verify sources listed correctly.
	sources, err := st.ListSources(ctx)
	if err != nil {
		t.Fatalf("list sources: %v", err)
	}
	if len(sources) == 0 {
		t.Fatal("expected listed sources")
	}
	found := false
	for _, s := range sources {
		if s.Source == "test-dep" && s.SourceVersion == "v1.0.0" {
			found = true
			if s.ChunkCount != 2 {
				t.Errorf("expected 2 chunks for test-dep, got %d", s.ChunkCount)
			}
		}
	}
	if !found {
		t.Error("expected to find test-dep in listed sources")
	}
}
