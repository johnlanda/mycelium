package embedder

import (
	"context"
	"fmt"

	lru "github.com/hashicorp/golang-lru/v2"
)

// DefaultCacheSize is the default number of query embeddings to cache.
// 512 entries × 1536 dims × 4 bytes ≈ 3 MB.
const DefaultCacheSize = 512

// CachingEmbedder wraps an Embedder with an LRU cache for query embeddings.
// It is safe for concurrent use.
type CachingEmbedder struct {
	inner Embedder
	cache *lru.Cache[string, []float32]
}

// NewCachingEmbedder creates a CachingEmbedder wrapping inner with the given
// maximum cache size. Returns an error if maxSize is < 1.
func NewCachingEmbedder(inner Embedder, maxSize int) (*CachingEmbedder, error) {
	cache, err := lru.New[string, []float32](maxSize)
	if err != nil {
		return nil, fmt.Errorf("caching embedder: %w", err)
	}
	return &CachingEmbedder{inner: inner, cache: cache}, nil
}

// Embed returns embeddings for the given texts, serving cached results where
// available and batching misses into a single call to the inner embedder.
func (c *CachingEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	results := make([][]float32, len(texts))

	// Collect cache misses and their indices.
	var missTexts []string
	var missIndices []int
	for i, t := range texts {
		if vec, ok := c.cache.Get(t); ok {
			results[i] = vec
		} else {
			missTexts = append(missTexts, t)
			missIndices = append(missIndices, i)
		}
	}

	// All hits — no API call needed.
	if len(missTexts) == 0 {
		return results, nil
	}

	// Embed misses in a single batched call.
	vecs, err := c.inner.Embed(ctx, missTexts)
	if err != nil {
		return nil, err
	}

	// Populate cache and merge into results.
	for j, idx := range missIndices {
		c.cache.Add(missTexts[j], vecs[j])
		results[idx] = vecs[j]
	}

	return results, nil
}

// ModelID delegates to the inner embedder.
func (c *CachingEmbedder) ModelID() string {
	return c.inner.ModelID()
}

// Dimensions delegates to the inner embedder.
func (c *CachingEmbedder) Dimensions() int {
	return c.inner.Dimensions()
}
