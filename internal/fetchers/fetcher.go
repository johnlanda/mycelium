// Package fetchers implements source fetchers (GitHub, artifact registries).
package fetchers

import (
	"context"

	"github.com/johnlanda/mycelium/internal/hasher"
	"github.com/johnlanda/mycelium/internal/manifest"
)

// FetchResult holds the output of a successful fetch operation.
type FetchResult struct {
	CommitSHA string
	Files     []hasher.FileContent
}

// Fetcher retrieves source files for a dependency at a pinned ref.
type Fetcher interface {
	Fetch(ctx context.Context, dep manifest.Dependency) (*FetchResult, error)
}
