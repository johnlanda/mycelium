// Package store provides the vector store abstraction for indexed content.
package store

import "context"

// StoredChunk represents a chunk of content stored in the vector store.
type StoredChunk struct {
	Text          string
	Breadcrumb    string
	ChunkType     string // "doc" or "code"
	ChunkIndex    int
	Path          string
	Source        string
	SourceVersion string
	StoreKey      string
	Language      string // for code chunks
	Vector        []float32
}

// SearchOpts configures a vector search query.
type SearchOpts struct {
	TopK      int
	Source    string // filter by dependency
	ChunkType string // "doc", "code", or "" for both
	Language  string // filter code by language
}

// SearchResult pairs a stored chunk with its similarity score.
type SearchResult struct {
	Chunk StoredChunk
	Score float32
}

// SourceInfo describes the indexed content for a single store key.
type SourceInfo struct {
	Source        string
	SourceVersion string
	StoreKey      string
	ChunkCount   int
}

// Store is the interface for persisting and querying vectorized content chunks.
type Store interface {
	Upsert(ctx context.Context, storeKey string, chunks []StoredChunk) error
	Search(ctx context.Context, query []float32, opts SearchOpts) ([]SearchResult, error)
	Delete(ctx context.Context, storeKey string) error
	HasKey(ctx context.Context, storeKey string) (bool, error)
	ListSources(ctx context.Context) ([]SourceInfo, error)
	Close() error
}
