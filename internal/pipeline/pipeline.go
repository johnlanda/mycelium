// Package pipeline orchestrates the mctl up sync workflow:
// fetch -> hash -> check store -> chunk -> embed -> upsert.
package pipeline

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/johnlanda/mycelium/internal/chunker"
	"github.com/johnlanda/mycelium/internal/embedder"
	"github.com/johnlanda/mycelium/internal/fetchers"
	"github.com/johnlanda/mycelium/internal/hasher"
	"github.com/johnlanda/mycelium/internal/lockfile"
	"github.com/johnlanda/mycelium/internal/manifest"
	"github.com/johnlanda/mycelium/internal/store"
)

// SyncResult reports the outcome of syncing a single dependency.
type SyncResult struct {
	ID      string
	Status  string // "skipped", "synced", "error"
	Err     error
	Elapsed time.Duration
}

// SyncOptions configures the sync pipeline.
type SyncOptions struct {
	ManifestPath string
	LockfilePath string
	StoreHost    string
	Output       io.Writer
}

// Sync runs the full sync pipeline for all dependencies in the manifest.
func Sync(ctx context.Context, opts SyncOptions) ([]SyncResult, error) {
	if opts.ManifestPath == "" {
		opts.ManifestPath = "mycelium.toml"
	}
	if opts.LockfilePath == "" {
		opts.LockfilePath = "mycelium.lock"
	}
	if opts.StoreHost == "" {
		opts.StoreHost = "localhost:8080"
	}
	if opts.Output == nil {
		opts.Output = os.Stdout
	}

	m, err := manifest.ParseFile(opts.ManifestPath)
	if err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}

	var lf *lockfile.Lockfile
	if _, statErr := os.Stat(opts.LockfilePath); os.IsNotExist(statErr) {
		lf = lockfile.New()
	} else {
		lf, err = lockfile.ReadFile(opts.LockfilePath)
		if err != nil {
			return nil, fmt.Errorf("read lockfile: %w", err)
		}
	}

	emb, err := embedder.NewEmbedder(m.Config.EmbeddingModel, embedder.Config{})
	if err != nil {
		return nil, fmt.Errorf("create embedder: %w", err)
	}

	st, err := store.NewWeaviateStore(ctx, opts.StoreHost)
	if err != nil {
		return nil, fmt.Errorf("connect store: %w", err)
	}
	defer st.Close()

	mdChunker := chunker.NewMarkdownChunker(chunker.DefaultOptions())
	codeChunker := chunker.NewCodeChunker(chunker.DefaultOptions())
	fetcher := &fetchers.GitHubFetcher{}

	return syncAll(ctx, m, lf, st, emb, fetcher, mdChunker, codeChunker, opts)
}

// UpgradeDependency syncs a single dependency atomically. If the content has changed
// (new store key differs from oldStoreKey), the old vectors are evicted after the new
// ones are loaded (NFR-3). Returns the new SourceLock.
func UpgradeDependency(ctx context.Context, dep manifest.Dependency, embeddingModel string, oldStoreKey string, opts SyncOptions) (*lockfile.SourceLock, error) {
	if opts.StoreHost == "" {
		opts.StoreHost = "localhost:8080"
	}
	if opts.Output == nil {
		opts.Output = os.Stdout
	}

	emb, err := embedder.NewEmbedder(embeddingModel, embedder.Config{})
	if err != nil {
		return nil, fmt.Errorf("create embedder: %w", err)
	}

	st, err := store.NewWeaviateStore(ctx, opts.StoreHost)
	if err != nil {
		return nil, fmt.Errorf("connect store: %w", err)
	}
	defer st.Close()

	mdChunker := chunker.NewMarkdownChunker(chunker.DefaultOptions())
	codeChunker := chunker.NewCodeChunker(chunker.DefaultOptions())
	fetcher := &fetchers.GitHubFetcher{}

	fmt.Fprintf(opts.Output, "Upgrading %s...\n", dep.ID)

	sl, skipped, err := syncDependency(ctx, dep, fetcher, st, emb, mdChunker, codeChunker)
	if err != nil {
		return nil, err
	}

	if skipped {
		fmt.Fprintf(opts.Output, "  %s: unchanged\n", dep.ID)
	} else {
		fmt.Fprintf(opts.Output, "  %s: synced\n", dep.ID)
	}

	// Evict old vectors if the store key changed (atomic: new loaded before old evicted).
	if oldStoreKey != "" && oldStoreKey != sl.StoreKey {
		if err := st.Delete(ctx, oldStoreKey); err != nil {
			return nil, fmt.Errorf("evict old store key: %w", err)
		}
	}

	return sl, nil
}

// syncAll is the core loop extracted for testability. It accepts interfaces
// rather than concrete types so tests can inject mocks.
func syncAll(
	ctx context.Context,
	m *manifest.Manifest,
	lf *lockfile.Lockfile,
	st store.Store,
	emb embedder.Embedder,
	fetcher fetchers.Fetcher,
	mdChunker chunker.Chunker,
	codeChunker chunker.Chunker,
	opts SyncOptions,
) ([]SyncResult, error) {
	var results []SyncResult

	fmt.Fprintf(opts.Output, "Syncing %d dependencies...\n", len(m.Dependencies))

	for _, dep := range m.Dependencies {
		start := time.Now()
		sl, skipped, err := syncDependency(ctx, dep, fetcher, st, emb, mdChunker, codeChunker)
		elapsed := time.Since(start)

		if err != nil {
			results = append(results, SyncResult{ID: dep.ID, Status: "error", Err: err, Elapsed: elapsed})
			fmt.Fprintf(opts.Output, "  %s: error: %v\n", dep.ID, err)
			continue
		}

		status := "synced"
		if skipped {
			status = "skipped"
		}

		lf.SetSource(dep.ID, *sl)
		results = append(results, SyncResult{ID: dep.ID, Status: status, Elapsed: elapsed})
		fmt.Fprintf(opts.Output, "  %s: %s (%s)\n", dep.ID, status, elapsed.Round(time.Millisecond))
	}

	if len(m.Local.Index) > 0 {
		start := time.Now()
		sl, skipped, err := syncLocal(ctx, m.Local.Index, st, emb, mdChunker, codeChunker)
		elapsed := time.Since(start)
		if err != nil {
			results = append(results, SyncResult{ID: "local", Status: "error", Err: err, Elapsed: elapsed})
			fmt.Fprintf(opts.Output, "  local: error: %v\n", err)
		} else {
			status := "synced"
			if skipped {
				status = "skipped"
			}
			lf.SetSource("local", *sl)
			results = append(results, SyncResult{ID: "local", Status: status, Elapsed: elapsed})
			fmt.Fprintf(opts.Output, "  local: %s (%s)\n", status, elapsed.Round(time.Millisecond))
		}
	}

	lf.Meta.EmbeddingModel = m.Config.EmbeddingModel
	lf.Meta.LockedAt = time.Now().UTC().Format(time.RFC3339)
	lf.Meta.SchemaVersion = lockfile.SchemaVersion
	lf.Meta.MyceliumVersion = "0.1.0"

	if err := lf.WriteFile(opts.LockfilePath); err != nil {
		return results, fmt.Errorf("write lockfile: %w", err)
	}

	return results, nil
}

// syncDependency fetches, hashes, checks the store, chunks, embeds, and upserts
// a single dependency. Returns the SourceLock, whether the dep was skipped, and any error.
func syncDependency(
	ctx context.Context,
	dep manifest.Dependency,
	fetcher fetchers.Fetcher,
	st store.Store,
	emb embedder.Embedder,
	mdChunker chunker.Chunker,
	codeChunker chunker.Chunker,
) (*lockfile.SourceLock, bool, error) {
	result, err := fetcher.Fetch(ctx, dep)
	if err != nil {
		return nil, false, fmt.Errorf("fetch %s: %w", dep.ID, err)
	}

	contentHash := hasher.ContentHash(result.Files)
	sk := hasher.StoreKey(contentHash, emb.ModelID(), "", hasher.ChunkingConfig{
		ChunkerType: "mixed",
		TargetSize:  chunker.DefaultOptions().TargetSize,
		Overlap:     chunker.DefaultOptions().Overlap,
	})

	exists, err := st.HasKey(ctx, sk)
	if err != nil {
		return nil, false, fmt.Errorf("check store key: %w", err)
	}

	sl := &lockfile.SourceLock{
		Version:       dep.Ref,
		Commit:        result.CommitSHA,
		ContentHash:   contentHash,
		StoreKey:      sk,
		IngestionType: "built",
	}

	if exists {
		return sl, true, nil
	}

	stored, err := processFiles(ctx, result.Files, dep.ID, dep.Ref, emb, mdChunker, codeChunker, sk)
	if err != nil {
		return nil, false, err
	}

	if err := st.Upsert(ctx, sk, stored); err != nil {
		return nil, false, fmt.Errorf("upsert: %w", err)
	}

	return sl, false, nil
}

// syncLocal collects files from local index paths, then runs the same
// hash -> check -> chunk -> embed -> upsert pipeline.
func syncLocal(
	ctx context.Context,
	paths []string,
	st store.Store,
	emb embedder.Embedder,
	mdChunker chunker.Chunker,
	codeChunker chunker.Chunker,
) (*lockfile.SourceLock, bool, error) {
	var files []hasher.FileContent
	for _, p := range paths {
		err := filepath.WalkDir(p, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			content, readErr := os.ReadFile(path)
			if readErr != nil {
				return fmt.Errorf("read %s: %w", path, readErr)
			}
			files = append(files, hasher.FileContent{Path: path, Content: content})
			return nil
		})
		if err != nil {
			return nil, false, fmt.Errorf("walk local path %s: %w", p, err)
		}
	}

	contentHash := hasher.ContentHash(files)
	sk := hasher.StoreKey(contentHash, emb.ModelID(), "", hasher.ChunkingConfig{
		ChunkerType: "mixed",
		TargetSize:  chunker.DefaultOptions().TargetSize,
		Overlap:     chunker.DefaultOptions().Overlap,
	})

	exists, err := st.HasKey(ctx, sk)
	if err != nil {
		return nil, false, fmt.Errorf("check store key: %w", err)
	}

	sl := &lockfile.SourceLock{
		ContentHash:   contentHash,
		StoreKey:      sk,
		IngestionType: "built",
	}

	if exists {
		return sl, true, nil
	}

	stored, err := processFiles(ctx, files, "local", "", emb, mdChunker, codeChunker, sk)
	if err != nil {
		return nil, false, err
	}

	if err := st.Upsert(ctx, sk, stored); err != nil {
		return nil, false, fmt.Errorf("upsert local: %w", err)
	}

	return sl, false, nil
}

// processFiles classifies files, chunks them, embeds the chunks, and returns StoredChunks.
func processFiles(
	ctx context.Context,
	files []hasher.FileContent,
	source, sourceVersion string,
	emb embedder.Embedder,
	mdChunker chunker.Chunker,
	codeChunker chunker.Chunker,
	storeKey string,
) ([]store.StoredChunk, error) {
	meta := chunker.ChunkMetadata{
		Source:        source,
		SourceVersion: sourceVersion,
	}

	var allChunks []chunker.Chunk
	for _, f := range files {
		m := meta
		m.Path = f.Path

		var c chunker.Chunker
		if isMarkdown(f.Path) {
			c = mdChunker
		} else {
			c = codeChunker
		}

		chunks, err := c.Chunk(f.Content, m)
		if err != nil {
			return nil, fmt.Errorf("chunk %s: %w", f.Path, err)
		}
		allChunks = append(allChunks, chunks...)
	}

	if len(allChunks) == 0 {
		return nil, nil
	}

	// Collect texts for batch embedding.
	texts := make([]string, len(allChunks))
	for i, c := range allChunks {
		texts[i] = c.Text
	}

	vectors, err := emb.Embed(ctx, texts)
	if err != nil {
		return nil, fmt.Errorf("embed: %w", err)
	}

	stored := make([]store.StoredChunk, len(allChunks))
	for i, c := range allChunks {
		stored[i] = store.StoredChunk{
			Text:          c.Text,
			Breadcrumb:    c.Breadcrumb,
			ChunkType:     string(c.ChunkType),
			ChunkIndex:    c.ChunkIndex,
			Path:          c.Path,
			Source:        c.Source,
			SourceVersion: c.SourceVersion,
			StoreKey:      storeKey,
			Language:      languageFromExt(c.Path),
			Vector:        vectors[i],
		}
	}
	return stored, nil
}

// isMarkdown returns true if the file path has a Markdown extension.
func isMarkdown(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".md" || ext == ".mdx"
}

// languageFromExt maps file extensions to language names.
func languageFromExt(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".ts", ".tsx":
		return "typescript"
	case ".js", ".jsx":
		return "javascript"
	case ".java":
		return "java"
	case ".rs":
		return "rust"
	default:
		return ""
	}
}
