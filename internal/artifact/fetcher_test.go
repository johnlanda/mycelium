package artifact

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/johnlanda/mycelium/internal/store"
)

func TestResolveArtifactURL(t *testing.T) {
	tests := []struct {
		source, ref, modelSlug, want string
	}{
		{
			"github.com/org/repo", "v1.0.0", "voyage-code-2",
			"https://github.com/org/repo/releases/download/v1.0.0/mycelium-voyage-code-2.jsonl.gz",
		},
		{
			"github.com/owner/lib", "v2.3.4", "text-embedding-3-small",
			"https://github.com/owner/lib/releases/download/v2.3.4/mycelium-text-embedding-3-small.jsonl.gz",
		},
	}

	for _, tt := range tests {
		got := ResolveArtifactURL(tt.source, tt.ref, tt.modelSlug)
		if got != tt.want {
			t.Errorf("ResolveArtifactURL(%q, %q, %q) = %q, want %q",
				tt.source, tt.ref, tt.modelSlug, got, tt.want)
		}
	}
}

func TestFetchArtifact_Success(t *testing.T) {
	// Build a valid gzipped JSONL artifact.
	chunks := []store.StoredChunk{
		{
			Text:          "hello",
			ChunkType:     "doc",
			ChunkIndex:    0,
			Path:          "README.md",
			Source:        "test",
			SourceVersion: "v1",
			StoreKey:      "sha256:testkey",
			Vector:        []float32{0.1, 0.2},
		},
	}
	meta := ArtifactMeta{EmbeddingModel: "test-model", StoreKey: "sha256:testkey"}

	var buf bytes.Buffer
	if err := Write(&buf, chunks, meta); err != nil {
		t.Fatalf("Write: %v", err)
	}
	artifactData := buf.Bytes()

	// Compute the checksum.
	checksum, err := ComputeChecksum(bytes.NewReader(artifactData))
	if err != nil {
		t.Fatalf("ComputeChecksum: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(artifactData)
	}))
	defer srv.Close()

	result, err := FetchArtifact(context.Background(), srv.URL+"/artifact.jsonl.gz", checksum)
	if err != nil {
		t.Fatalf("FetchArtifact: %v", err)
	}

	if len(result.Chunks) != 1 {
		t.Fatalf("got %d chunks, want 1", len(result.Chunks))
	}
	if result.Chunks[0].Text != "hello" {
		t.Errorf("chunk text = %q, want %q", result.Chunks[0].Text, "hello")
	}
	if result.Checksum != checksum {
		t.Errorf("checksum = %q, want %q", result.Checksum, checksum)
	}
}

func TestFetchArtifact_ChecksumMismatch(t *testing.T) {
	// Build a valid artifact.
	chunks := []store.StoredChunk{
		{Text: "data", Vector: []float32{0.1}},
	}
	var buf bytes.Buffer
	if err := Write(&buf, chunks, ArtifactMeta{}); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(buf.Bytes())
	}))
	defer srv.Close()

	_, err := FetchArtifact(context.Background(), srv.URL+"/artifact.jsonl.gz", "sha256:0000000000000000000000000000000000000000000000000000000000000000")
	if err == nil {
		t.Fatal("expected checksum mismatch error")
	}
	if !contains(err.Error(), "checksum mismatch") {
		t.Errorf("error should mention checksum mismatch: %v", err)
	}
}

func TestCheckArtifactExists_200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			t.Errorf("expected HEAD, got %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	exists, err := CheckArtifactExists(context.Background(), srv.URL+"/artifact.jsonl.gz")
	if err != nil {
		t.Fatalf("CheckArtifactExists: %v", err)
	}
	if !exists {
		t.Error("expected exists=true")
	}
}

func TestCheckArtifactExists_404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	exists, err := CheckArtifactExists(context.Background(), srv.URL+"/artifact.jsonl.gz")
	if err != nil {
		t.Fatalf("CheckArtifactExists: %v", err)
	}
	if exists {
		t.Error("expected exists=false")
	}
}

func TestFetchChecksumFromURL(t *testing.T) {
	hex := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	body := fmt.Sprintf("%s  mycelium-test.jsonl.gz\n", hex)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(body))
	}))
	defer srv.Close()

	// FetchChecksumFromURL appends ".sha256" to the URL, so serve at any path.
	got, err := FetchChecksumFromURL(context.Background(), srv.URL+"/artifact.jsonl.gz")
	if err != nil {
		t.Fatalf("FetchChecksumFromURL: %v", err)
	}

	want := "sha256:" + hex
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && bytes.Contains([]byte(s), []byte(substr))
}
