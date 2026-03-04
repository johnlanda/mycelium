// Package hasher computes content hashes and store keys.
package hasher

import (
	"crypto/sha256"
	"fmt"
	"sort"
)

// FileContent represents a file's path and raw content for hashing.
type FileContent struct {
	Path    string
	Content []byte
}

// ChunkingConfig captures the chunking parameters that affect store_key identity.
type ChunkingConfig struct {
	ChunkerType string   // "markdown" or "code"
	Languages   []string // tree-sitter grammars used (e.g., ["go", "python"])
	TargetSize  int      // target chunk size in tokens
	Overlap     int      // overlap between chunks in tokens
}

// ContentHash computes a SHA-256 hash over sorted file paths and their contents.
// The result is formatted as "sha256:<hex>".
func ContentHash(files []FileContent) string {
	// Sort by path for determinism.
	sorted := make([]FileContent, len(files))
	copy(sorted, files)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Path < sorted[j].Path
	})

	h := sha256.New()
	for _, f := range sorted {
		// Write path length + path + content length + content
		// to avoid ambiguity between files.
		fmt.Fprintf(h, "%d:%s\n", len(f.Content), f.Path)
		h.Write(f.Content)
	}

	return fmt.Sprintf("sha256:%x", h.Sum(nil))
}

// StoreKey computes the store partition key from the content hash,
// embedding model info, and chunking configuration.
// Formula: SHA-256(content_hash + embedding_model + model_version + chunking_config)
func StoreKey(contentHash, embeddingModel, modelVersion string, cfg ChunkingConfig) string {
	// Sort languages for determinism.
	langs := make([]string, len(cfg.Languages))
	copy(langs, cfg.Languages)
	sort.Strings(langs)

	h := sha256.New()
	fmt.Fprintf(h, "content_hash:%s\n", contentHash)
	fmt.Fprintf(h, "embedding_model:%s\n", embeddingModel)
	fmt.Fprintf(h, "model_version:%s\n", modelVersion)
	fmt.Fprintf(h, "chunker_type:%s\n", cfg.ChunkerType)
	for _, lang := range langs {
		fmt.Fprintf(h, "language:%s\n", lang)
	}
	fmt.Fprintf(h, "target_size:%d\n", cfg.TargetSize)
	fmt.Fprintf(h, "overlap:%d\n", cfg.Overlap)

	return fmt.Sprintf("sha256:%x", h.Sum(nil))
}
