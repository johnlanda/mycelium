package chunker

import (
	"bytes"
	"math"
	"strings"
)

// MarkdownChunker splits Markdown files along heading boundaries (h1–h3),
// preserving heading hierarchy as breadcrumb metadata.
type MarkdownChunker struct {
	Options Options
}

// NewMarkdownChunker returns a chunker with the given options.
// Zero-value fields in opts are replaced with defaults.
func NewMarkdownChunker(opts Options) *MarkdownChunker {
	d := DefaultOptions()
	if opts.TargetSize == 0 {
		opts.TargetSize = d.TargetSize
	}
	if opts.MinSize == 0 {
		opts.MinSize = d.MinSize
	}
	if opts.MaxSize == 0 {
		opts.MaxSize = d.MaxSize
	}
	return &MarkdownChunker{Options: opts}
}

// section is an N-ary tree node representing a heading and its body text.
type section struct {
	level    int        // 1, 2, or 3 (0 = synthetic root)
	heading  string     // heading text without '#' prefix
	body     strings.Builder
	children []*section
}

// rawChunk is an intermediate chunk before size normalization.
type rawChunk struct {
	breadcrumb string
	text       string
}

// Chunk implements the Chunker interface for Markdown content.
func (m *MarkdownChunker) Chunk(content []byte, metadata ChunkMetadata) ([]Chunk, error) {
	content = stripFrontMatter(content)

	if isBlank(content) {
		return nil, nil
	}

	root := buildSectionTree(content)
	raw := flattenSections(root)

	if len(raw) == 0 {
		return nil, nil
	}

	raw = splitOversized(raw, m.Options.MaxSize)
	raw = mergeUndersized(raw, m.Options.MinSize, m.Options.MaxSize)
	raw = applyOverlap(raw, m.Options.Overlap)

	chunks := make([]Chunk, len(raw))
	for i, r := range raw {
		chunks[i] = Chunk{
			Text:          r.text,
			Breadcrumb:    r.breadcrumb,
			ChunkType:     ChunkTypeDoc,
			ChunkIndex:    i,
			Path:          metadata.Path,
			Source:        metadata.Source,
			SourceVersion: metadata.SourceVersion,
		}
	}
	return chunks, nil
}

// stripFrontMatter removes YAML front matter delimited by "---".
func stripFrontMatter(data []byte) []byte {
	if !bytes.HasPrefix(data, []byte("---")) {
		return data
	}
	// Find the closing "---" after the opening one.
	rest := data[3:]
	// Skip past the first newline after opening "---".
	idx := bytes.IndexByte(rest, '\n')
	if idx < 0 {
		return data
	}
	rest = rest[idx+1:]

	_, after, found := bytes.Cut(rest, []byte("\n---"))
	if !found {
		return data // no closing delimiter; treat whole thing as content
	}
	if len(after) > 0 && after[0] == '\n' {
		after = after[1:]
	}
	return after
}

// parseHeading checks if a line is an ATX heading (h1–h3).
// Returns (level, heading text) or (0, "") if not a heading.
func parseHeading(line string) (int, string) {
	trimmed := strings.TrimRight(line, " \t")
	level := 0
	for level < len(trimmed) && trimmed[level] == '#' {
		level++
	}
	if level == 0 || level > 3 {
		return 0, ""
	}
	// Must be followed by a space or be just "#" characters.
	rest := trimmed[level:]
	if len(rest) == 0 {
		return level, ""
	}
	if rest[0] != ' ' && rest[0] != '\t' {
		return 0, ""
	}
	text := strings.TrimSpace(rest)
	return level, text
}

// buildSectionTree parses the content into an N-ary section tree.
// h1/h2/h3 create tree structure; h4+ are treated as body text.
// Lines inside fenced code blocks are never treated as headings.
func buildSectionTree(content []byte) *section {
	root := &section{level: 0}
	stack := []*section{root}

	lines := strings.Split(string(content), "\n")
	inCodeBlock := false
	codeFenceChar := byte(0)

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Track fenced code block state.
		if isCodeFenceLine(trimmed) {
			if !inCodeBlock {
				inCodeBlock = true
				codeFenceChar = trimmed[0]
			} else if trimmed[0] == codeFenceChar {
				inCodeBlock = false
				codeFenceChar = 0
			}
			// Append the fence line to current section body.
			current := stack[len(stack)-1]
			current.body.WriteString(line)
			current.body.WriteByte('\n')
			continue
		}

		if inCodeBlock {
			current := stack[len(stack)-1]
			current.body.WriteString(line)
			current.body.WriteByte('\n')
			continue
		}

		level, heading := parseHeading(line)
		if level > 0 {
			sec := &section{level: level, heading: heading}
			// Pop stack until we find a parent with level < this heading.
			for len(stack) > 1 && stack[len(stack)-1].level >= level {
				stack = stack[:len(stack)-1]
			}
			parent := stack[len(stack)-1]
			parent.children = append(parent.children, sec)
			stack = append(stack, sec)
		} else {
			// Body text (including h4+ lines).
			current := stack[len(stack)-1]
			current.body.WriteString(line)
			current.body.WriteByte('\n')
		}
	}
	return root
}

// isCodeFenceLine returns true if the line starts with 3+ backticks or tildes.
func isCodeFenceLine(trimmed string) bool {
	if len(trimmed) < 3 {
		return false
	}
	ch := trimmed[0]
	if ch != '`' && ch != '~' {
		return false
	}
	count := 0
	for _, c := range trimmed {
		if byte(c) == ch {
			count++
		} else {
			break
		}
	}
	return count >= 3
}

// flattenSections performs a depth-first walk, producing a rawChunk per section
// that has non-empty body text. The breadcrumb is built from the heading chain.
func flattenSections(root *section) []rawChunk {
	var chunks []rawChunk
	var walk func(s *section, crumbs []string)
	walk = func(s *section, crumbs []string) {
		if s.heading != "" {
			crumbs = append(crumbs, s.heading)
		}
		body := strings.TrimSpace(s.body.String())
		if body != "" {
			chunks = append(chunks, rawChunk{
				breadcrumb: strings.Join(crumbs, " > "),
				text:       body,
			})
		}
		for _, child := range s.children {
			walk(child, crumbs)
		}
	}
	walk(root, nil)
	return chunks
}

// splitOversized splits any chunk exceeding maxSize at paragraph boundaries,
// keeping fenced code blocks as indivisible units.
func splitOversized(chunks []rawChunk, maxSize int) []rawChunk {
	var result []rawChunk
	for _, c := range chunks {
		if estimateTokens(c.text) <= maxSize {
			result = append(result, c)
			continue
		}
		parts := splitPreservingCodeBlocks(c.text)
		var current strings.Builder
		for _, part := range parts {
			candidate := current.String()
			if candidate != "" {
				candidate += "\n\n" + part
			} else {
				candidate = part
			}
			if estimateTokens(candidate) > maxSize && current.Len() > 0 {
				result = append(result, rawChunk{
					breadcrumb: c.breadcrumb,
					text:       strings.TrimSpace(current.String()),
				})
				current.Reset()
				current.WriteString(part)
			} else {
				current.Reset()
				current.WriteString(candidate)
			}
		}
		if current.Len() > 0 {
			text := strings.TrimSpace(current.String())
			if text != "" {
				result = append(result, rawChunk{
					breadcrumb: c.breadcrumb,
					text:       text,
				})
			}
		}
	}
	return result
}

// splitPreservingCodeBlocks splits text on double newlines (\n\n) but keeps
// fenced code blocks (``` or ~~~) as single indivisible units.
func splitPreservingCodeBlocks(text string) []string {
	lines := strings.Split(text, "\n")
	var parts []string
	var current strings.Builder
	inCodeBlock := false
	codeFenceChar := byte(0)

	flush := func() {
		s := strings.TrimSpace(current.String())
		if s != "" {
			parts = append(parts, s)
		}
		current.Reset()
	}

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		if isCodeFenceLine(trimmed) {
			if !inCodeBlock {
				inCodeBlock = true
				codeFenceChar = trimmed[0]
			} else if trimmed[0] == codeFenceChar {
				inCodeBlock = false
				codeFenceChar = 0
			}
			current.WriteString(line)
			if i < len(lines)-1 {
				current.WriteByte('\n')
			}
			continue
		}

		if inCodeBlock {
			current.WriteString(line)
			if i < len(lines)-1 {
				current.WriteByte('\n')
			}
			continue
		}

		// Outside code blocks: split on blank lines (paragraph boundaries).
		if trimmed == "" {
			// Check if next non-empty line continues, or if this is a true paragraph break.
			flush()
		} else {
			current.WriteString(line)
			if i < len(lines)-1 {
				current.WriteByte('\n')
			}
		}
	}
	flush()
	return parts
}

// mergeUndersized merges adjacent chunks that share the same breadcrumb and are
// individually smaller than minSize, as long as the merged result stays under maxSize.
func mergeUndersized(chunks []rawChunk, minSize, maxSize int) []rawChunk {
	if len(chunks) == 0 {
		return chunks
	}
	var result []rawChunk
	result = append(result, chunks[0])

	for i := 1; i < len(chunks); i++ {
		last := &result[len(result)-1]
		if last.breadcrumb == chunks[i].breadcrumb &&
			estimateTokens(last.text) < minSize &&
			estimateTokens(chunks[i].text) < minSize {
			merged := last.text + "\n\n" + chunks[i].text
			if estimateTokens(merged) <= maxSize {
				last.text = merged
				continue
			}
		}
		result = append(result, chunks[i])
	}
	return result
}

// applyOverlap prepends the last N tokens worth of words from chunk i-1 to chunk i.
func applyOverlap(chunks []rawChunk, overlap int) []rawChunk {
	if overlap <= 0 || len(chunks) < 2 {
		return chunks
	}
	for i := 1; i < len(chunks); i++ {
		words := strings.Fields(chunks[i-1].text)
		// Determine how many words correspond to `overlap` tokens.
		// estimateTokens uses ceil(wordCount * 1.33), so we reverse: words ≈ tokens / 1.33
		targetWords := int(math.Ceil(float64(overlap) / 1.33))
		targetWords = min(targetWords, len(words))
		if targetWords == 0 {
			continue
		}
		tail := strings.Join(words[len(words)-targetWords:], " ")
		chunks[i].text = tail + "\n\n" + chunks[i].text
	}
	return chunks
}

// estimateTokens estimates the token count of text using ceil(wordCount * 1.33).
func estimateTokens(text string) int {
	words := len(strings.Fields(text))
	return int(math.Ceil(float64(words) * 1.33))
}

// isBlank returns true if the content is empty or whitespace only.
func isBlank(data []byte) bool {
	return len(bytes.TrimSpace(data)) == 0
}
