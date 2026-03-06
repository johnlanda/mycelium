package embedder

import (
	"context"
	"fmt"
	"testing"
)

// BenchmarkCachingEmbedder_Hit measures the latency of a fully cached embedding lookup.
func BenchmarkCachingEmbedder_Hit(b *testing.B) {
	spy := &spyEmbedder{modelID: "bench", dims: 1536}
	c, err := NewCachingEmbedder(spy, DefaultCacheSize)
	if err != nil {
		b.Fatal(err)
	}

	ctx := context.Background()
	texts := []string{"install envoy gateway"}

	// Prime the cache.
	_, err = c.Embed(ctx, texts)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for b.Loop() {
		c.Embed(ctx, texts)
	}
}

// BenchmarkCachingEmbedder_Miss measures the latency of a cache miss requiring
// a call to the inner embedder.
func BenchmarkCachingEmbedder_Miss(b *testing.B) {
	spy := &spyEmbedder{modelID: "bench", dims: 1536}
	c, err := NewCachingEmbedder(spy, DefaultCacheSize)
	if err != nil {
		b.Fatal(err)
	}

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Unique text each iteration to guarantee a miss.
		c.Embed(ctx, []string{fmt.Sprintf("query-%d", i)})
	}
}

// BenchmarkCachingEmbedder_MixedBatch measures a batch where some texts are cached
// and some are misses — the common real-world scenario.
func BenchmarkCachingEmbedder_MixedBatch(b *testing.B) {
	spy := &spyEmbedder{modelID: "bench", dims: 1536}
	c, err := NewCachingEmbedder(spy, DefaultCacheSize)
	if err != nil {
		b.Fatal(err)
	}

	ctx := context.Background()

	// Prime "cached-query" into the cache.
	_, err = c.Embed(ctx, []string{"cached-query"})
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Embed(ctx, []string{"cached-query", fmt.Sprintf("miss-%d", i)})
	}
}
