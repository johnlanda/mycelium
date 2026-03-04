package chunker

import (
	"strings"
	"testing"
)

func TestMarkdownChunker(t *testing.T) {
	meta := ChunkMetadata{
		Source:        "github.com/foo/bar",
		SourceVersion: "v1.2.3",
		Path:          "docs/guide.md",
	}

	// Use small token limits so we can test splitting/merging with short content.
	smallOpts := Options{
		TargetSize: 20,
		MinSize:    10,
		MaxSize:    40,
		Overlap:    0,
	}

	tests := []struct {
		name    string
		opts    Options
		input   string
		meta    ChunkMetadata
		check   func(t *testing.T, chunks []Chunk, err error)
	}{
		{
			name: "simple document",
			opts: DefaultOptions(),
			input: "# Hello\n\nThis is a simple document.",
			meta: meta,
			check: func(t *testing.T, chunks []Chunk, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if len(chunks) != 1 {
					t.Fatalf("expected 1 chunk, got %d", len(chunks))
				}
				if chunks[0].Breadcrumb != "Hello" {
					t.Errorf("breadcrumb = %q, want %q", chunks[0].Breadcrumb, "Hello")
				}
				if chunks[0].ChunkType != ChunkTypeDoc {
					t.Errorf("chunk type = %q, want %q", chunks[0].ChunkType, ChunkTypeDoc)
				}
				if chunks[0].ChunkIndex != 0 {
					t.Errorf("chunk index = %d, want 0", chunks[0].ChunkIndex)
				}
			},
		},
		{
			name: "nested headings",
			opts: DefaultOptions(),
			input: "# Guide\n\nIntro text.\n\n## Installation\n\nInstall steps.\n\n### macOS\n\nmacOS specific.\n\n## Usage\n\nUsage info.",
			meta:  meta,
			check: func(t *testing.T, chunks []Chunk, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if len(chunks) < 3 {
					t.Fatalf("expected at least 3 chunks, got %d", len(chunks))
				}
				// Check breadcrumbs for hierarchy.
				wantCrumbs := []string{
					"Guide",
					"Guide > Installation",
					"Guide > Installation > macOS",
					"Guide > Usage",
				}
				for i, want := range wantCrumbs {
					if i >= len(chunks) {
						break
					}
					if chunks[i].Breadcrumb != want {
						t.Errorf("chunk[%d] breadcrumb = %q, want %q", i, chunks[i].Breadcrumb, want)
					}
				}
			},
		},
		{
			name: "oversized section splits at paragraphs",
			opts: smallOpts,
			input: "# Big\n\n" + strings.Repeat("word ", 30) + "\n\n" + strings.Repeat("more ", 30),
			meta:  meta,
			check: func(t *testing.T, chunks []Chunk, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if len(chunks) < 2 {
					t.Fatalf("expected at least 2 chunks after split, got %d", len(chunks))
				}
				for _, c := range chunks {
					if c.Breadcrumb != "Big" {
						t.Errorf("all chunks should have breadcrumb 'Big', got %q", c.Breadcrumb)
					}
				}
			},
		},
		{
			name: "undersized sections merged",
			opts: Options{
				TargetSize: 100,
				MinSize:    50,
				MaxSize:    200,
				Overlap:    0,
			},
			input: "# Root\n\n## A\n\nShort.\n\n## A\n\nAlso short.",
			meta:  meta,
			check: func(t *testing.T, chunks []Chunk, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				// Two undersized chunks with same breadcrumb should merge.
				if len(chunks) != 1 {
					t.Fatalf("expected 1 merged chunk, got %d", len(chunks))
				}
				if !strings.Contains(chunks[0].Text, "Short.") || !strings.Contains(chunks[0].Text, "Also short.") {
					t.Error("merged chunk should contain text from both sections")
				}
			},
		},
		{
			name: "code blocks preserved",
			opts: smallOpts,
			input: "# Code\n\n```go\nfunc main() {\n\tprintln(\"hello\")\n}\n```\n\nSome text after.",
			meta:  meta,
			check: func(t *testing.T, chunks []Chunk, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				// The code block should not be split in the middle.
				found := false
				for _, c := range chunks {
					if strings.Contains(c.Text, "```go") && strings.Contains(c.Text, "```") {
						found = true
						// The code block lines should all be together.
						if !strings.Contains(c.Text, "func main()") {
							t.Error("code block was split")
						}
					}
				}
				if !found {
					t.Error("no chunk contains the complete code block")
				}
			},
		},
		{
			name: "code block with heading-like lines",
			opts: DefaultOptions(),
			input: "# Real\n\nBefore code.\n\n```\n# Not a heading\n## Also not\n```\n\nAfter code.",
			meta:  meta,
			check: func(t *testing.T, chunks []Chunk, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				// Should be one chunk under "Real" — the lines inside the code block
				// should not create new headings.
				if len(chunks) != 1 {
					t.Fatalf("expected 1 chunk (code block headings ignored), got %d", len(chunks))
				}
				if chunks[0].Breadcrumb != "Real" {
					t.Errorf("breadcrumb = %q, want %q", chunks[0].Breadcrumb, "Real")
				}
				if !strings.Contains(chunks[0].Text, "# Not a heading") {
					t.Error("heading-like line inside code block should be in body text")
				}
			},
		},
		{
			name:  "empty section produces no chunk",
			opts:  DefaultOptions(),
			input: "# Heading\n\n## Empty\n\n## Has content\n\nSome text.",
			meta:  meta,
			check: func(t *testing.T, chunks []Chunk, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				for _, c := range chunks {
					if c.Breadcrumb == "Heading > Empty" {
						t.Error("empty section should not produce a chunk")
					}
				}
				found := false
				for _, c := range chunks {
					if c.Breadcrumb == "Heading > Has content" {
						found = true
					}
				}
				if !found {
					t.Error("expected chunk for 'Has content' section")
				}
			},
		},
		{
			name: "front matter stripped",
			opts: DefaultOptions(),
			input: "---\ntitle: Test\ndate: 2024-01-01\n---\n\n# Hello\n\nContent here.",
			meta:  meta,
			check: func(t *testing.T, chunks []Chunk, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if len(chunks) != 1 {
					t.Fatalf("expected 1 chunk, got %d", len(chunks))
				}
				if strings.Contains(chunks[0].Text, "title: Test") {
					t.Error("front matter should have been stripped")
				}
				if !strings.Contains(chunks[0].Text, "Content here.") {
					t.Error("content after front matter should be present")
				}
			},
		},
		{
			name:  "no headings",
			opts:  DefaultOptions(),
			input: "Just some text without any headings.\n\nAnother paragraph.",
			meta:  meta,
			check: func(t *testing.T, chunks []Chunk, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if len(chunks) != 1 {
					t.Fatalf("expected 1 chunk, got %d", len(chunks))
				}
				if chunks[0].Breadcrumb != "" {
					t.Errorf("breadcrumb should be empty for headingless doc, got %q", chunks[0].Breadcrumb)
				}
			},
		},
		{
			name:  "empty input",
			opts:  DefaultOptions(),
			input: "",
			meta:  meta,
			check: func(t *testing.T, chunks []Chunk, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if len(chunks) != 0 {
					t.Fatalf("expected 0 chunks, got %d", len(chunks))
				}
			},
		},
		{
			name:  "whitespace only",
			opts:  DefaultOptions(),
			input: "   \n\n  \t\n  ",
			meta:  meta,
			check: func(t *testing.T, chunks []Chunk, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if len(chunks) != 0 {
					t.Fatalf("expected 0 chunks, got %d", len(chunks))
				}
			},
		},
		{
			name:  "h4 treated as body",
			opts:  DefaultOptions(),
			input: "# Top\n\n## Section\n\nBody text.\n\n#### Detail\n\nMore detail text.",
			meta:  meta,
			check: func(t *testing.T, chunks []Chunk, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				// h4 should be body text under the parent section.
				found := false
				for _, c := range chunks {
					if strings.Contains(c.Text, "#### Detail") {
						found = true
						if c.Breadcrumb != "Top > Section" {
							t.Errorf("h4 should be under parent breadcrumb, got %q", c.Breadcrumb)
						}
					}
				}
				if !found {
					t.Error("h4 line should appear in a chunk's text")
				}
			},
		},
		{
			name: "metadata propagation",
			opts: DefaultOptions(),
			input: "# A\n\nText.\n\n## B\n\nMore text.",
			meta: ChunkMetadata{
				Source:        "mylib",
				SourceVersion: "v2.0.0",
				Path:          "README.md",
			},
			check: func(t *testing.T, chunks []Chunk, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				for i, c := range chunks {
					if c.Source != "mylib" {
						t.Errorf("chunk[%d] source = %q, want %q", i, c.Source, "mylib")
					}
					if c.SourceVersion != "v2.0.0" {
						t.Errorf("chunk[%d] version = %q, want %q", i, c.SourceVersion, "v2.0.0")
					}
					if c.Path != "README.md" {
						t.Errorf("chunk[%d] path = %q, want %q", i, c.Path, "README.md")
					}
				}
			},
		},
		{
			name: "overlap applied",
			opts: Options{
				TargetSize: 768,
				MinSize:    512,
				MaxSize:    1024,
				Overlap:    5,
			},
			input: "# A\n\nFirst chunk with some words at the end.\n\n## B\n\nSecond chunk content.",
			meta:  meta,
			check: func(t *testing.T, chunks []Chunk, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if len(chunks) < 2 {
					t.Fatalf("expected at least 2 chunks, got %d", len(chunks))
				}
				// The second chunk should start with tail words from the first.
				firstWords := strings.Fields(chunks[0].Text)
				if len(firstWords) == 0 {
					t.Fatal("first chunk has no words")
				}
				// Some tail words from the first chunk should appear at start of second.
				lastWord := firstWords[len(firstWords)-1]
				if !strings.Contains(chunks[1].Text, lastWord) {
					t.Errorf("second chunk should contain overlap word %q from first chunk", lastWord)
				}
			},
		},
		{
			name: "sequential chunk indices",
			opts: DefaultOptions(),
			input: "# A\n\nText A.\n\n## B\n\nText B.\n\n## C\n\nText C.",
			meta:  meta,
			check: func(t *testing.T, chunks []Chunk, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				for i, c := range chunks {
					if c.ChunkIndex != i {
						t.Errorf("chunk[%d] index = %d, want %d", i, c.ChunkIndex, i)
					}
				}
			},
		},
		{
			name: "tilde code fence",
			opts: DefaultOptions(),
			input: "# Code\n\nBefore.\n\n~~~python\ndef hello():\n    # This is heading-like\n    pass\n~~~\n\nAfter.",
			meta:  meta,
			check: func(t *testing.T, chunks []Chunk, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if len(chunks) != 1 {
					t.Fatalf("expected 1 chunk, got %d", len(chunks))
				}
				if !strings.Contains(chunks[0].Text, "~~~python") {
					t.Error("tilde fence should be preserved")
				}
				if !strings.Contains(chunks[0].Text, "# This is heading-like") {
					t.Error("heading-like line inside tilde fence should be body text")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chunker := NewMarkdownChunker(tt.opts)
			chunks, err := chunker.Chunk([]byte(tt.input), tt.meta)
			tt.check(t, chunks, err)
		})
	}
}

func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		text string
		want int
	}{
		{"", 0},
		{"hello", 2},           // ceil(1 * 1.33) = 2
		{"hello world", 3},     // ceil(2 * 1.33) = 3
		{"one two three", 4},   // ceil(3 * 1.33) = 4
	}
	for _, tt := range tests {
		got := estimateTokens(tt.text)
		if got != tt.want {
			t.Errorf("estimateTokens(%q) = %d, want %d", tt.text, got, tt.want)
		}
	}
}

func TestStripFrontMatter(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "with front matter",
			input: "---\ntitle: Test\n---\nContent",
			want:  "Content",
		},
		{
			name:  "no front matter",
			input: "Just content",
			want:  "Just content",
		},
		{
			name:  "unclosed front matter",
			input: "---\ntitle: Test\nContent",
			want:  "---\ntitle: Test\nContent",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := string(stripFrontMatter([]byte(tt.input)))
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseHeading(t *testing.T) {
	tests := []struct {
		line      string
		wantLevel int
		wantText  string
	}{
		{"# Hello", 1, "Hello"},
		{"## World", 2, "World"},
		{"### Deep", 3, "Deep"},
		{"#### Too deep", 0, ""},
		{"Not a heading", 0, ""},
		{"#NoSpace", 0, ""},
		{"# ", 1, ""},
		{"", 0, ""},
	}
	for _, tt := range tests {
		level, text := parseHeading(tt.line)
		if level != tt.wantLevel || text != tt.wantText {
			t.Errorf("parseHeading(%q) = (%d, %q), want (%d, %q)",
				tt.line, level, text, tt.wantLevel, tt.wantText)
		}
	}
}
