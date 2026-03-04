package chunker

import (
	"strings"
	"testing"
)

func TestLineChunker_EmptyInput(t *testing.T) {
	lc := NewLineChunker(Options{})
	tests := []struct {
		name    string
		content string
	}{
		{"empty string", ""},
		{"whitespace only", "   \n\n  \t  \n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chunks, err := lc.Chunk([]byte(tt.content), ChunkMetadata{})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if chunks != nil {
				t.Fatalf("expected nil, got %d chunks", len(chunks))
			}
		})
	}
}

func TestLineChunker_SingleSmallBlock(t *testing.T) {
	lc := NewLineChunker(Options{})
	content := "func main() {\n\tfmt.Println(\"hello\")\n}"
	chunks, err := lc.Chunk([]byte(content), ChunkMetadata{
		Source:        "test/repo",
		SourceVersion: "v1.0.0",
		Path:          "main.go",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0].Text != content {
		t.Errorf("text mismatch:\ngot:  %q\nwant: %q", chunks[0].Text, content)
	}
}

func TestLineChunker_BlankLineSeparatedBlocks(t *testing.T) {
	// Two small blocks separated by a blank line should be merged into one
	// chunk when combined they are under TargetSize.
	lc := NewLineChunker(Options{TargetSize: 2000, MinSize: 100, MaxSize: 3000})
	content := "block one line 1\nblock one line 2\n\nblock two line 1\nblock two line 2"
	chunks, err := lc.Chunk([]byte(content), ChunkMetadata{Path: "test.go"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(chunks) != 1 {
		t.Fatalf("expected 1 merged chunk, got %d", len(chunks))
	}
	if !strings.Contains(chunks[0].Text, "block one") || !strings.Contains(chunks[0].Text, "block two") {
		t.Error("merged chunk should contain both blocks")
	}
}

func TestLineChunker_OversizedBlockSplit(t *testing.T) {
	// Create a block that exceeds MaxSize and verify it gets split.
	lc := NewLineChunker(Options{TargetSize: 20, MinSize: 10, MaxSize: 30})

	// Each line is roughly 10 words -> ~13 tokens. 4 lines together -> ~53 tokens > MaxSize=30.
	lines := []string{
		"one two three four five six seven eight nine ten",
		"alpha bravo charlie delta echo foxtrot golf hotel india juliet",
		"kilo lima mike november oscar papa quebec romeo sierra tango",
		"uniform victor whiskey xray yankee zulu alpha bravo charlie delta",
	}
	content := strings.Join(lines, "\n")

	chunks, err := lc.Chunk([]byte(content), ChunkMetadata{Path: "big.go"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks from oversized block, got %d", len(chunks))
	}
}

func TestLineChunker_MetadataPropagation(t *testing.T) {
	lc := NewLineChunker(Options{})
	meta := ChunkMetadata{
		Source:        "github.com/foo/bar",
		SourceVersion: "v2.3.4",
		Path:          "pkg/util.go",
	}
	content := "package util\n\nfunc Helper() {}"
	chunks, err := lc.Chunk([]byte(content), meta)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for i, c := range chunks {
		if c.Source != meta.Source {
			t.Errorf("chunk[%d] Source = %q, want %q", i, c.Source, meta.Source)
		}
		if c.SourceVersion != meta.SourceVersion {
			t.Errorf("chunk[%d] SourceVersion = %q, want %q", i, c.SourceVersion, meta.SourceVersion)
		}
		if c.Path != meta.Path {
			t.Errorf("chunk[%d] Path = %q, want %q", i, c.Path, meta.Path)
		}
		if c.ChunkType != ChunkTypeCode {
			t.Errorf("chunk[%d] ChunkType = %q, want %q", i, c.ChunkType, ChunkTypeCode)
		}
		if c.Breadcrumb != meta.Path {
			t.Errorf("chunk[%d] Breadcrumb = %q, want %q", i, c.Breadcrumb, meta.Path)
		}
		if c.ChunkIndex != i {
			t.Errorf("chunk[%d] ChunkIndex = %d, want %d", i, c.ChunkIndex, i)
		}
	}
}

func TestLineChunker_ChunkTypeAlwaysCode(t *testing.T) {
	lc := NewLineChunker(Options{})
	content := "# This looks like markdown\n\nBut it's code"
	chunks, err := lc.Chunk([]byte(content), ChunkMetadata{Path: "file.py"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for i, c := range chunks {
		if c.ChunkType != ChunkTypeCode {
			t.Errorf("chunk[%d] ChunkType = %q, want %q", i, c.ChunkType, ChunkTypeCode)
		}
	}
}

func TestSplitBlankLines(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{"no blank lines", "a\nb\nc", 1},
		{"one blank line", "a\n\nb", 2},
		{"multiple blank lines", "a\n\n\n\nb", 2},
		{"trailing blank lines", "a\nb\n\n", 1},
		{"leading blank lines", "\n\na\nb", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blocks := splitBlankLines(tt.input)
			if len(blocks) != tt.want {
				t.Errorf("got %d blocks, want %d; blocks=%v", len(blocks), tt.want, blocks)
			}
		})
	}
}
