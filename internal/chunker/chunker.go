// Package chunker implements chunking strategies for markdown and code.
package chunker

// ChunkType distinguishes documentation chunks from code chunks.
type ChunkType string

const (
	ChunkTypeDoc  ChunkType = "doc"
	ChunkTypeCode ChunkType = "code"
)

// ChunkMetadata carries source-level context passed through to every chunk.
type ChunkMetadata struct {
	Source        string // dependency ID (e.g. "github.com/foo/bar")
	SourceVersion string // resolved version or git ref
	Path          string // file path relative to repo root
}

// Chunk is a single semantically meaningful unit of text produced by a Chunker.
type Chunk struct {
	Text          string
	Breadcrumb    string    // heading hierarchy, e.g. "Guide > Installation > macOS"
	ChunkType     ChunkType
	ChunkIndex    int       // zero-based sequential index within the file
	Path          string
	Source        string
	SourceVersion string
}

// Options controls chunk sizing behaviour. All sizes are in estimated tokens.
type Options struct {
	TargetSize int // ideal chunk size (default 768)
	MinSize    int // merge chunks smaller than this (default 512)
	MaxSize    int // split chunks larger than this (default 1024)
	Overlap    int // tokens to repeat from previous chunk (default 0)
}

// DefaultOptions returns sensible defaults per the PRD (512-1024 target range).
func DefaultOptions() Options {
	return Options{
		TargetSize: 768,
		MinSize:    512,
		MaxSize:    1024,
		Overlap:    0,
	}
}

// Chunker splits raw file content into semantically meaningful chunks.
type Chunker interface {
	Chunk(content []byte, metadata ChunkMetadata) ([]Chunk, error)
}
