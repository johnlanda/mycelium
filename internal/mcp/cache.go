package mcp

import (
	"fmt"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"
)

const (
	// DefaultCacheSize is the default maximum number of cached search results.
	DefaultCacheSize = 256

	// DefaultCacheTTL is the default time-to-live for cached entries.
	DefaultCacheTTL = 5 * time.Minute
)

// CacheConfig controls the result cache behavior.
type CacheConfig struct {
	MaxSize int           // 0 → DefaultCacheSize
	TTL     time.Duration // 0 → DefaultCacheTTL
}

// resultCache wraps an expirable LRU cache for formatted search results.
type resultCache struct {
	lru *expirable.LRU[string, string]
}

// newResultCache creates a result cache with the given config, applying defaults
// for zero values.
func newResultCache(cfg CacheConfig) *resultCache {
	size := cfg.MaxSize
	if size <= 0 {
		size = DefaultCacheSize
	}
	ttl := cfg.TTL
	if ttl <= 0 {
		ttl = DefaultCacheTTL
	}
	return &resultCache{
		lru: expirable.NewLRU[string, string](size, nil, ttl),
	}
}

// searchKey builds a cache key for the search handler.
func searchKey(query, source, chunkType string, topK int) string {
	return fmt.Sprintf("s\x00%s\x00%s\x00%s\x00%d", query, source, chunkType, topK)
}

// searchCodeKey builds a cache key for the search_code handler.
func searchCodeKey(query, source, language string, topK int) string {
	return fmt.Sprintf("c\x00%s\x00%s\x00%s\x00%d", query, source, language, topK)
}

// listSourcesKey returns the fixed cache key for the list_sources handler.
func listSourcesKey() string {
	return "l"
}
