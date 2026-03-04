package lockfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testLockfile = `
[meta]
mycelium_version = "1.0.0"
embedding_model = "voyage-code-2"
embedding_model_version = "2024-05"
locked_at = "2026-02-28T14:30:00Z"
schema_version = 1

[sources.envoy-gateway]
version = "v1.3.0"
commit = "8f3a2b1c9d4e"
content_hash = "sha256:3f8a"
artifact_url = "https://github.com/envoyproxy/gateway/releases/download/v1.3.0/mycelium-voyage-code-2.jsonl.gz"
artifact_hash = "sha256:9d2c"
store_key = "sha256:a1b2"
ingestion_type = "artifact"

[sources.platform-sdk]
version = "main"
commit = "4b7c1d2e3f5a"
content_hash = "sha256:7e1f"
store_key = "sha256:b3c4"
ingestion_type = "built"
`

func TestRead(t *testing.T) {
	lf, err := Read(strings.NewReader(testLockfile))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if lf.Meta.MyceliumVersion != "1.0.0" {
		t.Errorf("mycelium_version = %q", lf.Meta.MyceliumVersion)
	}
	if lf.Meta.EmbeddingModel != "voyage-code-2" {
		t.Errorf("embedding_model = %q", lf.Meta.EmbeddingModel)
	}
	if lf.Meta.EmbeddingModelVersion != "2024-05" {
		t.Errorf("embedding_model_version = %q", lf.Meta.EmbeddingModelVersion)
	}
	if lf.Meta.LockedAt != "2026-02-28T14:30:00Z" {
		t.Errorf("locked_at = %q", lf.Meta.LockedAt)
	}
	if lf.Meta.SchemaVersion != 1 {
		t.Errorf("schema_version = %d", lf.Meta.SchemaVersion)
	}

	if len(lf.Sources) != 2 {
		t.Fatalf("sources len = %d, want 2", len(lf.Sources))
	}

	eg := lf.Sources["envoy-gateway"]
	if eg.Version != "v1.3.0" {
		t.Errorf("envoy-gateway.version = %q", eg.Version)
	}
	if eg.IngestionType != "artifact" {
		t.Errorf("envoy-gateway.ingestion_type = %q", eg.IngestionType)
	}
	if eg.ArtifactURL == "" {
		t.Error("envoy-gateway.artifact_url is empty")
	}

	ps := lf.Sources["platform-sdk"]
	if ps.IngestionType != "built" {
		t.Errorf("platform-sdk.ingestion_type = %q", ps.IngestionType)
	}
	if ps.ArtifactURL != "" {
		t.Errorf("platform-sdk.artifact_url should be empty, got %q", ps.ArtifactURL)
	}
}

func TestReadEmptySources(t *testing.T) {
	input := `
[meta]
mycelium_version = "1.0.0"
embedding_model = "voyage-code-2"
embedding_model_version = "2024-05"
locked_at = "2026-02-28T14:30:00Z"
schema_version = 1
`
	lf, err := Read(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if lf.Sources == nil {
		t.Fatal("sources map should be initialized, not nil")
	}
	if len(lf.Sources) != 0 {
		t.Errorf("sources len = %d, want 0", len(lf.Sources))
	}
}

func TestSetAndRemoveSource(t *testing.T) {
	lf := New()
	lf.SetSource("foo", SourceLock{
		ContentHash:   "sha256:abc",
		StoreKey:      "sha256:def",
		IngestionType: "built",
	})

	if _, ok := lf.Sources["foo"]; !ok {
		t.Fatal("expected source 'foo' to exist")
	}

	lf.SetSource("foo", SourceLock{
		ContentHash:   "sha256:updated",
		StoreKey:      "sha256:ghi",
		IngestionType: "built",
	})
	if lf.Sources["foo"].ContentHash != "sha256:updated" {
		t.Errorf("content_hash = %q, want sha256:updated", lf.Sources["foo"].ContentHash)
	}

	lf.RemoveSource("foo")
	if _, ok := lf.Sources["foo"]; ok {
		t.Fatal("expected source 'foo' to be removed")
	}
}

func TestWriteAndReadFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mycelium.lock")

	lf := New()
	lf.Meta = Meta{
		MyceliumVersion:       "1.0.0",
		EmbeddingModel:        "voyage-code-2",
		EmbeddingModelVersion: "2024-05",
		LockedAt:              "2026-02-28T14:30:00Z",
		SchemaVersion:         SchemaVersion,
	}
	lf.SetSource("beta", SourceLock{
		Version:       "v2.0.0",
		Commit:        "abcdef123456",
		ContentHash:   "sha256:111",
		StoreKey:      "sha256:222",
		IngestionType: "built",
	})
	lf.SetSource("alpha", SourceLock{
		Version:       "v1.0.0",
		Commit:        "fedcba654321",
		ContentHash:   "sha256:333",
		StoreKey:      "sha256:444",
		IngestionType: "built",
	})

	if err := lf.WriteFile(path); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	lf2, err := ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	if lf2.Meta.EmbeddingModel != "voyage-code-2" {
		t.Errorf("meta.embedding_model = %q", lf2.Meta.EmbeddingModel)
	}
	if len(lf2.Sources) != 2 {
		t.Fatalf("sources len = %d, want 2", len(lf2.Sources))
	}
	if lf2.Sources["alpha"].Version != "v1.0.0" {
		t.Errorf("alpha.version = %q", lf2.Sources["alpha"].Version)
	}
	if lf2.Sources["beta"].Version != "v2.0.0" {
		t.Errorf("beta.version = %q", lf2.Sources["beta"].Version)
	}
}

func TestWriteFileDeterministicOrder(t *testing.T) {
	dir := t.TempDir()

	lf := New()
	lf.Meta = Meta{
		MyceliumVersion:       "1.0.0",
		EmbeddingModel:        "test",
		EmbeddingModelVersion: "1",
		LockedAt:              "2026-01-01T00:00:00Z",
		SchemaVersion:         SchemaVersion,
	}
	lf.SetSource("charlie", SourceLock{ContentHash: "sha256:c", StoreKey: "sha256:c", IngestionType: "built"})
	lf.SetSource("alpha", SourceLock{ContentHash: "sha256:a", StoreKey: "sha256:a", IngestionType: "built"})
	lf.SetSource("bravo", SourceLock{ContentHash: "sha256:b", StoreKey: "sha256:b", IngestionType: "built"})

	path1 := filepath.Join(dir, "lock1")
	path2 := filepath.Join(dir, "lock2")

	if err := lf.WriteFile(path1); err != nil {
		t.Fatalf("WriteFile 1: %v", err)
	}
	if err := lf.WriteFile(path2); err != nil {
		t.Fatalf("WriteFile 2: %v", err)
	}

	data1, _ := os.ReadFile(path1)
	data2, _ := os.ReadFile(path2)

	if string(data1) != string(data2) {
		t.Error("two writes produced different output; encoding is not deterministic")
	}

	// Verify alphabetical ordering: alpha before bravo before charlie.
	content := string(data1)
	alphaIdx := strings.Index(content, "[sources.alpha]")
	bravoIdx := strings.Index(content, "[sources.bravo]")
	charlieIdx := strings.Index(content, "[sources.charlie]")

	if alphaIdx == -1 || bravoIdx == -1 || charlieIdx == -1 {
		t.Fatal("missing expected source sections")
	}
	if !(alphaIdx < bravoIdx && bravoIdx < charlieIdx) {
		t.Error("sources not in alphabetical order")
	}
}

func TestAtomicWriteNoPartial(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mycelium.lock")

	// Write a valid lockfile first.
	lf := New()
	lf.Meta.MyceliumVersion = "1.0.0"
	lf.Meta.EmbeddingModel = "test"
	lf.Meta.EmbeddingModelVersion = "1"
	lf.Meta.LockedAt = "2026-01-01T00:00:00Z"
	lf.SetSource("original", SourceLock{ContentHash: "sha256:orig", StoreKey: "sha256:orig", IngestionType: "built"})
	if err := lf.WriteFile(path); err != nil {
		t.Fatalf("initial write: %v", err)
	}

	// Attempt to write to a non-existent directory (should fail).
	badPath := filepath.Join(dir, "nonexistent", "mycelium.lock")
	lf.SetSource("new", SourceLock{ContentHash: "sha256:new", StoreKey: "sha256:new", IngestionType: "built"})
	if err := lf.WriteFile(badPath); err == nil {
		t.Fatal("expected error writing to non-existent directory")
	}

	// Original file should be unchanged.
	lf2, err := ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile after failed write: %v", err)
	}
	if _, ok := lf2.Sources["new"]; ok {
		t.Error("original file should not contain 'new' source")
	}
	if _, ok := lf2.Sources["original"]; !ok {
		t.Error("original file should still contain 'original' source")
	}
}
