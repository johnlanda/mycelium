package chunker

import "strings"

// LineChunker splits code files along blank-line boundaries, grouping
// adjacent blocks toward TargetSize tokens.
type LineChunker struct {
	Options Options
}

// NewLineChunker returns a code chunker with the given options.
// Zero-value fields in opts are replaced with defaults.
func NewLineChunker(opts Options) *LineChunker {
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
	return &LineChunker{Options: opts}
}

// Chunk implements the Chunker interface for code content.
func (lc *LineChunker) Chunk(content []byte, metadata ChunkMetadata) ([]Chunk, error) {
	text := string(content)
	if strings.TrimSpace(text) == "" {
		return nil, nil
	}

	blocks := splitBlankLines(text)
	grouped := lc.groupBlocks(blocks)

	chunks := make([]Chunk, len(grouped))
	for i, g := range grouped {
		chunks[i] = Chunk{
			Text:          g,
			Breadcrumb:    metadata.Path,
			ChunkType:     ChunkTypeCode,
			ChunkIndex:    i,
			Path:          metadata.Path,
			Source:        metadata.Source,
			SourceVersion: metadata.SourceVersion,
		}
	}
	return chunks, nil
}

// splitBlankLines splits text on sequences of one or more blank lines,
// returning non-empty trimmed blocks.
func splitBlankLines(text string) []string {
	lines := strings.Split(text, "\n")
	var blocks []string
	var current strings.Builder

	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			if current.Len() > 0 {
				blocks = append(blocks, strings.TrimRight(current.String(), "\n"))
				current.Reset()
			}
			continue
		}
		if current.Len() > 0 {
			current.WriteByte('\n')
		}
		current.WriteString(line)
	}
	if current.Len() > 0 {
		blocks = append(blocks, strings.TrimRight(current.String(), "\n"))
	}
	return blocks
}

// groupBlocks merges adjacent blocks toward TargetSize tokens. If a single
// block exceeds MaxSize it is split at line boundaries.
func (lc *LineChunker) groupBlocks(blocks []string) []string {
	var result []string
	var current strings.Builder

	for _, block := range blocks {
		blockTokens := estimateTokens(block)

		// If this single block exceeds MaxSize, flush current and split the block.
		if blockTokens > lc.Options.MaxSize {
			if current.Len() > 0 {
				result = append(result, current.String())
				current.Reset()
			}
			result = append(result, lc.splitLargeBlock(block)...)
			continue
		}

		if current.Len() == 0 {
			current.WriteString(block)
			continue
		}

		merged := current.String() + "\n\n" + block
		if estimateTokens(merged) <= lc.Options.TargetSize {
			current.Reset()
			current.WriteString(merged)
		} else {
			result = append(result, current.String())
			current.Reset()
			current.WriteString(block)
		}
	}
	if current.Len() > 0 {
		result = append(result, current.String())
	}
	return result
}

// splitLargeBlock splits a block that exceeds MaxSize at line boundaries.
func (lc *LineChunker) splitLargeBlock(block string) []string {
	lines := strings.Split(block, "\n")
	var result []string
	var current strings.Builder

	for _, line := range lines {
		if current.Len() == 0 {
			current.WriteString(line)
			continue
		}
		candidate := current.String() + "\n" + line
		if estimateTokens(candidate) > lc.Options.MaxSize {
			result = append(result, current.String())
			current.Reset()
			current.WriteString(line)
		} else {
			current.Reset()
			current.WriteString(candidate)
		}
	}
	if current.Len() > 0 {
		result = append(result, current.String())
	}
	return result
}
