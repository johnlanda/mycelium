package hasher

import (
	"strings"
	"testing"
)

func TestContentHash_Determinism(t *testing.T) {
	files := []FileContent{
		{Path: "a.go", Content: []byte("package a")},
		{Path: "b.go", Content: []byte("package b")},
	}

	h1 := ContentHash(files)
	h2 := ContentHash(files)

	if h1 != h2 {
		t.Errorf("same input produced different hashes: %q vs %q", h1, h2)
	}
	if !strings.HasPrefix(h1, "sha256:") {
		t.Errorf("hash should start with 'sha256:', got %q", h1)
	}
}

func TestContentHash_FileOrderIndependence(t *testing.T) {
	filesAB := []FileContent{
		{Path: "a.go", Content: []byte("package a")},
		{Path: "b.go", Content: []byte("package b")},
	}
	filesBA := []FileContent{
		{Path: "b.go", Content: []byte("package b")},
		{Path: "a.go", Content: []byte("package a")},
	}

	h1 := ContentHash(filesAB)
	h2 := ContentHash(filesBA)

	if h1 != h2 {
		t.Errorf("file order should not matter: %q vs %q", h1, h2)
	}
}

func TestContentHash_DifferentInputsDifferentHashes(t *testing.T) {
	files1 := []FileContent{
		{Path: "a.go", Content: []byte("package a")},
	}
	files2 := []FileContent{
		{Path: "a.go", Content: []byte("package b")},
	}
	files3 := []FileContent{
		{Path: "b.go", Content: []byte("package a")},
	}

	h1 := ContentHash(files1)
	h2 := ContentHash(files2)
	h3 := ContentHash(files3)

	if h1 == h2 {
		t.Error("different content should produce different hashes")
	}
	if h1 == h3 {
		t.Error("different paths should produce different hashes")
	}
}

func TestContentHash_EmptyInput(t *testing.T) {
	h := ContentHash(nil)
	if !strings.HasPrefix(h, "sha256:") {
		t.Errorf("empty input should still produce valid hash, got %q", h)
	}
}

func TestStoreKey_Determinism(t *testing.T) {
	cfg := ChunkingConfig{
		ChunkerType: "markdown",
		Languages:   nil,
		TargetSize:  512,
		Overlap:     64,
	}

	k1 := StoreKey("sha256:abc", "voyage-code-2", "2024-05", cfg)
	k2 := StoreKey("sha256:abc", "voyage-code-2", "2024-05", cfg)

	if k1 != k2 {
		t.Errorf("same input produced different store keys: %q vs %q", k1, k2)
	}
	if !strings.HasPrefix(k1, "sha256:") {
		t.Errorf("store key should start with 'sha256:', got %q", k1)
	}
}

func TestStoreKey_LanguageOrderIndependence(t *testing.T) {
	cfg1 := ChunkingConfig{
		ChunkerType: "code",
		Languages:   []string{"go", "python", "rust"},
		TargetSize:  512,
		Overlap:     64,
	}
	cfg2 := ChunkingConfig{
		ChunkerType: "code",
		Languages:   []string{"rust", "go", "python"},
		TargetSize:  512,
		Overlap:     64,
	}

	k1 := StoreKey("sha256:abc", "voyage-code-2", "2024-05", cfg1)
	k2 := StoreKey("sha256:abc", "voyage-code-2", "2024-05", cfg2)

	if k1 != k2 {
		t.Errorf("language order should not matter: %q vs %q", k1, k2)
	}
}

func TestStoreKey_DifferentInputsDifferentKeys(t *testing.T) {
	cfg := ChunkingConfig{
		ChunkerType: "markdown",
		TargetSize:  512,
		Overlap:     64,
	}

	base := StoreKey("sha256:abc", "voyage-code-2", "2024-05", cfg)

	// Different content hash.
	k1 := StoreKey("sha256:def", "voyage-code-2", "2024-05", cfg)
	if base == k1 {
		t.Error("different content_hash should produce different store key")
	}

	// Different model.
	k2 := StoreKey("sha256:abc", "text-embedding-3-small", "2024-05", cfg)
	if base == k2 {
		t.Error("different embedding_model should produce different store key")
	}

	// Different model version.
	k3 := StoreKey("sha256:abc", "voyage-code-2", "2025-01", cfg)
	if base == k3 {
		t.Error("different model_version should produce different store key")
	}

	// Different chunking config.
	cfg2 := ChunkingConfig{
		ChunkerType: "code",
		TargetSize:  512,
		Overlap:     64,
	}
	k4 := StoreKey("sha256:abc", "voyage-code-2", "2024-05", cfg2)
	if base == k4 {
		t.Error("different chunker_type should produce different store key")
	}

	// Different target size.
	cfg3 := ChunkingConfig{
		ChunkerType: "markdown",
		TargetSize:  1024,
		Overlap:     64,
	}
	k5 := StoreKey("sha256:abc", "voyage-code-2", "2024-05", cfg3)
	if base == k5 {
		t.Error("different target_size should produce different store key")
	}
}

func TestContentHash_DoesNotMutateInput(t *testing.T) {
	files := []FileContent{
		{Path: "b.go", Content: []byte("package b")},
		{Path: "a.go", Content: []byte("package a")},
	}

	ContentHash(files)

	if files[0].Path != "b.go" || files[1].Path != "a.go" {
		t.Error("ContentHash should not mutate the input slice order")
	}
}
