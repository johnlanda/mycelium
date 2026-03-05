package embedder

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

// spyEmbedder records calls to Embed and returns deterministic vectors.
type spyEmbedder struct {
	mu       sync.Mutex
	calls    [][]string // texts passed to each Embed call
	modelID  string
	dims     int
	embedErr error // if set, Embed returns this error
}

func (s *spyEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]string, len(texts))
	copy(cp, texts)
	s.calls = append(s.calls, cp)
	if s.embedErr != nil {
		return nil, s.embedErr
	}
	vecs := make([][]float32, len(texts))
	for i, t := range texts {
		vec := make([]float32, s.dims)
		// Deterministic: first float is hash of text so we can verify identity.
		vec[0] = float32(len(t))
		vecs[i] = vec
	}
	return vecs, nil
}

func (s *spyEmbedder) ModelID() string { return s.modelID }
func (s *spyEmbedder) Dimensions() int { return s.dims }

func (s *spyEmbedder) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

func (s *spyEmbedder) lastCall() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.calls) == 0 {
		return nil
	}
	return s.calls[len(s.calls)-1]
}

func TestCachingEmbedder_AllMiss(t *testing.T) {
	spy := &spyEmbedder{modelID: "test", dims: 4}
	c, err := NewCachingEmbedder(spy, 10)
	if err != nil {
		t.Fatal(err)
	}

	vecs, err := c.Embed(context.Background(), []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vecs) != 2 {
		t.Fatalf("expected 2 vectors, got %d", len(vecs))
	}
	if spy.callCount() != 1 {
		t.Errorf("expected 1 inner call, got %d", spy.callCount())
	}
}

func TestCachingEmbedder_AllHit(t *testing.T) {
	spy := &spyEmbedder{modelID: "test", dims: 4}
	c, err := NewCachingEmbedder(spy, 10)
	if err != nil {
		t.Fatal(err)
	}

	// First call populates cache.
	_, err = c.Embed(context.Background(), []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}

	// Second identical call should hit cache entirely.
	vecs, err := c.Embed(context.Background(), []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vecs) != 2 {
		t.Fatalf("expected 2 vectors, got %d", len(vecs))
	}
	if spy.callCount() != 1 {
		t.Errorf("expected 1 inner call (all cached), got %d", spy.callCount())
	}
}

func TestCachingEmbedder_MixedBatch(t *testing.T) {
	spy := &spyEmbedder{modelID: "test", dims: 4}
	c, err := NewCachingEmbedder(spy, 10)
	if err != nil {
		t.Fatal(err)
	}

	// Cache "a" and "b".
	_, err = c.Embed(context.Background(), []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}

	// Now request "b" (cached) and "c" (miss).
	vecs, err := c.Embed(context.Background(), []string{"b", "c"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vecs) != 2 {
		t.Fatalf("expected 2 vectors, got %d", len(vecs))
	}
	if spy.callCount() != 2 {
		t.Fatalf("expected 2 inner calls, got %d", spy.callCount())
	}
	// Second inner call should only contain the miss.
	last := spy.lastCall()
	if len(last) != 1 || last[0] != "c" {
		t.Errorf("expected inner call with [\"c\"], got %v", last)
	}
}

func TestCachingEmbedder_LRUEviction(t *testing.T) {
	spy := &spyEmbedder{modelID: "test", dims: 2}
	c, err := NewCachingEmbedder(spy, 2) // capacity 2
	if err != nil {
		t.Fatal(err)
	}

	// Fill cache: "a", "b".
	_, err = c.Embed(context.Background(), []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}

	// Add "c" — should evict "a" (least recently used).
	_, err = c.Embed(context.Background(), []string{"c"})
	if err != nil {
		t.Fatal(err)
	}

	// Now request "a" again — should be a miss (was evicted).
	_, err = c.Embed(context.Background(), []string{"a"})
	if err != nil {
		t.Fatal(err)
	}
	if spy.callCount() != 3 {
		t.Errorf("expected 3 inner calls (a evicted), got %d", spy.callCount())
	}

	// "c" should still be cached (was recently added in step 2, not evicted).
	_, err = c.Embed(context.Background(), []string{"c"})
	if err != nil {
		t.Fatal(err)
	}
	if spy.callCount() != 3 {
		t.Errorf("expected 3 inner calls (c still cached), got %d", spy.callCount())
	}
}

func TestCachingEmbedder_EmptyInput(t *testing.T) {
	spy := &spyEmbedder{modelID: "test", dims: 4}
	c, err := NewCachingEmbedder(spy, 10)
	if err != nil {
		t.Fatal(err)
	}

	vecs, err := c.Embed(context.Background(), []string{})
	if err != nil {
		t.Fatal(err)
	}
	if vecs != nil {
		t.Errorf("expected nil, got %v", vecs)
	}
	if spy.callCount() != 0 {
		t.Errorf("expected 0 inner calls for empty input, got %d", spy.callCount())
	}
}

func TestCachingEmbedder_NilInput(t *testing.T) {
	spy := &spyEmbedder{modelID: "test", dims: 4}
	c, err := NewCachingEmbedder(spy, 10)
	if err != nil {
		t.Fatal(err)
	}

	vecs, err := c.Embed(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if vecs != nil {
		t.Errorf("expected nil, got %v", vecs)
	}
}

func TestCachingEmbedder_InnerErrorNotCached(t *testing.T) {
	spy := &spyEmbedder{modelID: "test", dims: 4, embedErr: fmt.Errorf("api down")}
	c, err := NewCachingEmbedder(spy, 10)
	if err != nil {
		t.Fatal(err)
	}

	_, err = c.Embed(context.Background(), []string{"a"})
	if err == nil {
		t.Fatal("expected error")
	}

	// Fix the inner embedder — "a" should not be cached from the failed call.
	spy.embedErr = nil
	vecs, err := c.Embed(context.Background(), []string{"a"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vecs) != 1 {
		t.Fatalf("expected 1 vector, got %d", len(vecs))
	}
	if spy.callCount() != 2 {
		t.Errorf("expected 2 inner calls (error not cached), got %d", spy.callCount())
	}
}

func TestCachingEmbedder_DelegatesModelIDAndDimensions(t *testing.T) {
	spy := &spyEmbedder{modelID: "voyage-code-2", dims: 1536}
	c, err := NewCachingEmbedder(spy, 10)
	if err != nil {
		t.Fatal(err)
	}

	if got := c.ModelID(); got != "voyage-code-2" {
		t.Errorf("ModelID() = %q, want %q", got, "voyage-code-2")
	}
	if got := c.Dimensions(); got != 1536 {
		t.Errorf("Dimensions() = %d, want %d", got, 1536)
	}
}

func TestCachingEmbedder_ConcurrentAccess(t *testing.T) {
	spy := &spyEmbedder{modelID: "test", dims: 4}
	c, err := NewCachingEmbedder(spy, 100)
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			text := fmt.Sprintf("query-%d", i%10) // 10 unique queries
			vecs, err := c.Embed(context.Background(), []string{text})
			if err != nil {
				t.Errorf("goroutine %d: unexpected error: %v", i, err)
				return
			}
			if len(vecs) != 1 {
				t.Errorf("goroutine %d: expected 1 vector, got %d", i, len(vecs))
			}
		}(i)
	}
	wg.Wait()

	// With 10 unique queries and 50 goroutines, we should have fewer than 50
	// inner calls due to caching (exact count depends on scheduling).
	if spy.callCount() >= 50 {
		t.Errorf("expected fewer than 50 inner calls due to caching, got %d", spy.callCount())
	}
}
