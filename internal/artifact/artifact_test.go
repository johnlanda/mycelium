package artifact

import (
	"bytes"
	"strings"
	"testing"

	"github.com/johnlanda/mycelium/internal/store"
)

func TestWriteRead_RoundTrip(t *testing.T) {
	chunks := []store.StoredChunk{
		{
			Text:          "hello world",
			Breadcrumb:    "Guide > Intro",
			ChunkType:     "doc",
			ChunkIndex:    0,
			Path:          "docs/intro.md",
			Source:        "github.com/org/repo",
			SourceVersion: "v1.0.0",
			StoreKey:      "sha256:abc123",
			Language:      "",
			Vector:        []float32{0.1, 0.2, 0.3},
		},
		{
			Text:          "func main() {}",
			Breadcrumb:    "main",
			ChunkType:     "code",
			ChunkIndex:    1,
			Path:          "cmd/main.go",
			Source:        "github.com/org/repo",
			SourceVersion: "v1.0.0",
			StoreKey:      "sha256:abc123",
			Language:      "go",
			Vector:        []float32{0.4, 0.5, 0.6},
		},
	}

	meta := ArtifactMeta{
		Source:         "github.com/org/repo",
		SourceVersion:  "v1.0.0",
		Commit:         "deadbeef",
		EmbeddingModel: "voyage-code-2",
		StoreKey:       "sha256:abc123",
	}

	var buf bytes.Buffer
	if err := Write(&buf, chunks, meta); err != nil {
		t.Fatalf("Write: %v", err)
	}

	gotChunks, gotMeta, err := Read(&buf)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if len(gotChunks) != len(chunks) {
		t.Fatalf("got %d chunks, want %d", len(gotChunks), len(chunks))
	}

	for i, got := range gotChunks {
		want := chunks[i]
		if got.Text != want.Text {
			t.Errorf("chunk[%d].Text = %q, want %q", i, got.Text, want.Text)
		}
		if got.Breadcrumb != want.Breadcrumb {
			t.Errorf("chunk[%d].Breadcrumb = %q, want %q", i, got.Breadcrumb, want.Breadcrumb)
		}
		if got.ChunkType != want.ChunkType {
			t.Errorf("chunk[%d].ChunkType = %q, want %q", i, got.ChunkType, want.ChunkType)
		}
		if got.ChunkIndex != want.ChunkIndex {
			t.Errorf("chunk[%d].ChunkIndex = %d, want %d", i, got.ChunkIndex, want.ChunkIndex)
		}
		if got.Path != want.Path {
			t.Errorf("chunk[%d].Path = %q, want %q", i, got.Path, want.Path)
		}
		if got.Source != want.Source {
			t.Errorf("chunk[%d].Source = %q, want %q", i, got.Source, want.Source)
		}
		if got.Language != want.Language {
			t.Errorf("chunk[%d].Language = %q, want %q", i, got.Language, want.Language)
		}
		if len(got.Vector) != len(want.Vector) {
			t.Errorf("chunk[%d].Vector length = %d, want %d", i, len(got.Vector), len(want.Vector))
		} else {
			for j := range got.Vector {
				if got.Vector[j] != want.Vector[j] {
					t.Errorf("chunk[%d].Vector[%d] = %f, want %f", i, j, got.Vector[j], want.Vector[j])
				}
			}
		}
	}

	if gotMeta.Source != meta.Source {
		t.Errorf("meta.Source = %q, want %q", gotMeta.Source, meta.Source)
	}
	if gotMeta.Commit != meta.Commit {
		t.Errorf("meta.Commit = %q, want %q", gotMeta.Commit, meta.Commit)
	}
	if gotMeta.EmbeddingModel != meta.EmbeddingModel {
		t.Errorf("meta.EmbeddingModel = %q, want %q", gotMeta.EmbeddingModel, meta.EmbeddingModel)
	}
}

func TestWriteRead_EmptyChunks(t *testing.T) {
	meta := ArtifactMeta{EmbeddingModel: "test"}
	var buf bytes.Buffer
	if err := Write(&buf, nil, meta); err != nil {
		t.Fatalf("Write empty: %v", err)
	}

	chunks, _, err := Read(&buf)
	if err != nil {
		t.Fatalf("Read empty: %v", err)
	}
	if len(chunks) != 0 {
		t.Errorf("got %d chunks, want 0", len(chunks))
	}
}

func TestRead_InvalidGzip(t *testing.T) {
	_, _, err := Read(strings.NewReader("not gzip data"))
	if err == nil {
		t.Fatal("expected error for invalid gzip")
	}
}

func TestWrite_IDFormat(t *testing.T) {
	chunks := []store.StoredChunk{
		{
			Source:     "github.com/org/repo",
			Path:       "docs/guide.md",
			ChunkIndex: 3,
			Vector:     []float32{0.1},
		},
	}
	meta := ArtifactMeta{EmbeddingModel: "test"}

	var buf bytes.Buffer
	if err := Write(&buf, chunks, meta); err != nil {
		t.Fatalf("Write: %v", err)
	}

	gotChunks, _, err := Read(&buf)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	// The ID is not stored in StoredChunk, but we can verify the round-trip
	// by checking fields that compose the ID survive.
	if len(gotChunks) != 1 {
		t.Fatalf("got %d chunks, want 1", len(gotChunks))
	}
	got := gotChunks[0]
	if got.Source != "github.com/org/repo" {
		t.Errorf("Source = %q", got.Source)
	}
	if got.Path != "docs/guide.md" {
		t.Errorf("Path = %q", got.Path)
	}
	if got.ChunkIndex != 3 {
		t.Errorf("ChunkIndex = %d", got.ChunkIndex)
	}
}
