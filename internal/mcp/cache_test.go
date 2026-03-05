package mcp

import (
	"testing"
	"time"
)

func TestSearchKeyDeterminism(t *testing.T) {
	k1 := searchKey("install", "envoy", "doc", 5)
	k2 := searchKey("install", "envoy", "doc", 5)
	if k1 != k2 {
		t.Errorf("same inputs produced different keys: %q vs %q", k1, k2)
	}
}

func TestSearchKeyDifferentiation(t *testing.T) {
	base := searchKey("install", "envoy", "doc", 5)

	tests := []struct {
		name string
		key  string
	}{
		{"different query", searchKey("upgrade", "envoy", "doc", 5)},
		{"different source", searchKey("install", "istio", "doc", 5)},
		{"different type", searchKey("install", "envoy", "code", 5)},
		{"different topK", searchKey("install", "envoy", "doc", 10)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.key == base {
				t.Errorf("expected different key for %s, got same: %q", tt.name, tt.key)
			}
		})
	}
}

func TestSearchCodeKeyDeterminism(t *testing.T) {
	k1 := searchCodeKey("main", "repo", "go", 5)
	k2 := searchCodeKey("main", "repo", "go", 5)
	if k1 != k2 {
		t.Errorf("same inputs produced different keys: %q vs %q", k1, k2)
	}
}

func TestSearchCodeKeyDifferentiation(t *testing.T) {
	base := searchCodeKey("main", "repo", "go", 5)

	tests := []struct {
		name string
		key  string
	}{
		{"different query", searchCodeKey("init", "repo", "go", 5)},
		{"different source", searchCodeKey("main", "other", "go", 5)},
		{"different language", searchCodeKey("main", "repo", "rust", 5)},
		{"different topK", searchCodeKey("main", "repo", "go", 10)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.key == base {
				t.Errorf("expected different key for %s, got same: %q", tt.name, tt.key)
			}
		})
	}
}

func TestPrefixSeparation(t *testing.T) {
	sk := searchKey("main", "", "", 5)
	ck := searchCodeKey("main", "", "", 5)
	if sk == ck {
		t.Error("search and search_code keys should differ for identical query")
	}
}

func TestListSourcesKeyIsConstant(t *testing.T) {
	k1 := listSourcesKey()
	k2 := listSourcesKey()
	if k1 != k2 {
		t.Errorf("list_sources key should be constant, got %q and %q", k1, k2)
	}
}

func TestDefaultConfigApplied(t *testing.T) {
	rc := newResultCache(CacheConfig{})
	if rc == nil || rc.lru == nil {
		t.Fatal("expected non-nil cache with zero-value config")
	}
}

func TestCustomConfig(t *testing.T) {
	rc := newResultCache(CacheConfig{
		MaxSize: 10,
		TTL:     1 * time.Second,
	})
	if rc == nil || rc.lru == nil {
		t.Fatal("expected non-nil cache with custom config")
	}

	// Verify the cache works with custom settings.
	rc.lru.Add("key", "value")
	if v, ok := rc.lru.Get("key"); !ok || v != "value" {
		t.Errorf("expected cached value 'value', got %q (ok=%v)", v, ok)
	}
}
