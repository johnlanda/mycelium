// Package artifact handles reading and writing of pre-built embedding artifacts
// in gzipped JSONL format.
package artifact

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"

	"github.com/johnlanda/mycelium/internal/store"
)

// Record is a single line in the artifact JSONL file.
type Record struct {
	ID      string  `json:"id"`
	Vector  []float32 `json:"vector"`
	Payload Payload `json:"payload"`
}

// Payload carries the metadata and text for a single chunk.
type Payload struct {
	Source         string `json:"source"`
	SourceVersion string `json:"source_version"`
	Commit         string `json:"commit,omitempty"`
	Path           string `json:"path"`
	Breadcrumb     string `json:"breadcrumb"`
	Text           string `json:"text"`
	ChunkType      string `json:"chunk_type"`
	Language       string `json:"language,omitempty"`
	EmbeddingModel string `json:"embedding_model"`
	ChunkIndex     int    `json:"chunk_index"`
	StoreKey       string `json:"store_key"`
}

// ArtifactMeta holds metadata extracted from an artifact.
type ArtifactMeta struct {
	Source         string
	SourceVersion  string
	Commit         string
	EmbeddingModel string
	StoreKey       string
}

// Write serialises chunks as gzipped JSONL to w.
func Write(w io.Writer, chunks []store.StoredChunk, meta ArtifactMeta) error {
	gw := gzip.NewWriter(w)
	enc := json.NewEncoder(gw)

	for _, c := range chunks {
		rec := Record{
			ID:     fmt.Sprintf("%s::%s::%d", c.Source, c.Path, c.ChunkIndex),
			Vector: c.Vector,
			Payload: Payload{
				Source:         c.Source,
				SourceVersion: c.SourceVersion,
				Commit:         meta.Commit,
				Path:           c.Path,
				Breadcrumb:     c.Breadcrumb,
				Text:           c.Text,
				ChunkType:      c.ChunkType,
				Language:       c.Language,
				EmbeddingModel: meta.EmbeddingModel,
				ChunkIndex:     c.ChunkIndex,
				StoreKey:       c.StoreKey,
			},
		}
		if err := enc.Encode(rec); err != nil {
			gw.Close()
			return fmt.Errorf("encode record: %w", err)
		}
	}

	if err := gw.Close(); err != nil {
		return fmt.Errorf("close gzip writer: %w", err)
	}
	return nil
}

// Read deserialises gzipped JSONL from r into StoredChunks and metadata.
// Metadata is extracted from the first record's payload.
func Read(r io.Reader) ([]store.StoredChunk, ArtifactMeta, error) {
	gr, err := gzip.NewReader(r)
	if err != nil {
		return nil, ArtifactMeta{}, fmt.Errorf("open gzip reader: %w", err)
	}
	defer gr.Close()

	scanner := bufio.NewScanner(gr)
	// Vector lines can be large; allow up to 10 MB per line.
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)

	var chunks []store.StoredChunk
	var meta ArtifactMeta
	first := true

	for scanner.Scan() {
		var rec Record
		if err := json.Unmarshal(scanner.Bytes(), &rec); err != nil {
			return nil, ArtifactMeta{}, fmt.Errorf("unmarshal record: %w", err)
		}

		if first {
			meta = ArtifactMeta{
				Source:         rec.Payload.Source,
				SourceVersion:  rec.Payload.SourceVersion,
				Commit:         rec.Payload.Commit,
				EmbeddingModel: rec.Payload.EmbeddingModel,
				StoreKey:       rec.Payload.StoreKey,
			}
			first = false
		}

		chunks = append(chunks, store.StoredChunk{
			Text:          rec.Payload.Text,
			Breadcrumb:    rec.Payload.Breadcrumb,
			ChunkType:     rec.Payload.ChunkType,
			ChunkIndex:    rec.Payload.ChunkIndex,
			Path:          rec.Payload.Path,
			Source:        rec.Payload.Source,
			SourceVersion: rec.Payload.SourceVersion,
			StoreKey:      rec.Payload.StoreKey,
			Language:      rec.Payload.Language,
			Vector:        rec.Vector,
		})
	}

	if err := scanner.Err(); err != nil {
		return nil, ArtifactMeta{}, fmt.Errorf("scan records: %w", err)
	}

	return chunks, meta, nil
}
