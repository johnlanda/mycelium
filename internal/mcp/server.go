package mcp

import (
	"context"
	"fmt"

	"github.com/johnlanda/mycelium/internal/embedder"
	"github.com/johnlanda/mycelium/internal/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const defaultTopK = 5

// SearchInput defines the input schema for the search tool.
type SearchInput struct {
	Query  string  `json:"query" jsonschema:"The search query text"`
	Source *string `json:"source,omitempty" jsonschema:"Filter by dependency source name"`
	Type   *string `json:"type,omitempty" jsonschema:"Filter by chunk type (doc or code)"`
	TopK   *int    `json:"top_k,omitempty" jsonschema:"Number of results to return (default 5)"`
}

// SearchCodeInput defines the input schema for the search_code tool.
type SearchCodeInput struct {
	Query    string  `json:"query" jsonschema:"The code search query text"`
	Source   *string `json:"source,omitempty" jsonschema:"Filter by dependency source name"`
	Language *string `json:"language,omitempty" jsonschema:"Filter by programming language"`
	TopK     *int    `json:"top_k,omitempty" jsonschema:"Number of results to return (default 5)"`
}

// ListSourcesInput defines the input schema for the list_sources tool.
type ListSourcesInput struct{}

// ServerOption configures optional Server behavior.
type ServerOption func(*Server)

// WithCache enables a TTL-based LRU result cache. Zero-value fields in cfg
// are replaced with defaults (256 entries, 5 min TTL).
func WithCache(cfg CacheConfig) ServerOption {
	return func(s *Server) {
		s.cache = newResultCache(cfg)
	}
}

// Server wraps the MCP server with mycelium-specific tool handlers.
type Server struct {
	store    store.Store
	embedder embedder.Embedder
	server   *mcp.Server
	cache    *resultCache // nil when caching is disabled
}

// NewServer creates a new MCP server with search and list_sources tools.
func NewServer(st store.Store, emb embedder.Embedder, opts ...ServerOption) *Server {
	s := &Server{
		store:    st,
		embedder: emb,
		server: mcp.NewServer(&mcp.Implementation{
			Name:    "mycelium",
			Version: "v0.1.0",
		}, nil),
	}

	for _, opt := range opts {
		opt(s)
	}

	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "search",
		Description: "Search indexed documentation and code by semantic similarity",
	}, s.handleSearch)

	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "search_code",
		Description: "Search indexed source code by semantic similarity",
	}, s.handleSearchCode)

	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "list_sources",
		Description: "List all indexed dependency sources",
	}, s.handleListSources)

	return s
}

// Serve runs the MCP server over stdio until the context is cancelled or the client disconnects.
func (s *Server) Serve(ctx context.Context) error {
	return s.server.Run(ctx, &mcp.StdioTransport{})
}

func (s *Server) handleSearch(ctx context.Context, _ *mcp.CallToolRequest, input SearchInput) (*mcp.CallToolResult, any, error) {
	topK := defaultTopK
	if input.TopK != nil {
		topK = *input.TopK
	}
	var source, chunkType string
	if input.Source != nil {
		source = *input.Source
	}
	if input.Type != nil {
		chunkType = *input.Type
	}

	if s.cache != nil {
		key := searchKey(input.Query, source, chunkType, topK)
		if cached, ok := s.cache.lru.Get(key); ok {
			return textResult(cached), nil, nil
		}
	}

	vectors, err := s.embedder.Embed(ctx, []string{input.Query})
	if err != nil {
		return errResult(fmt.Sprintf("embed query: %v", err)), nil, nil
	}

	opts := store.SearchOpts{
		TopK:      topK,
		Source:    source,
		ChunkType: chunkType,
	}

	results, err := s.store.Search(ctx, vectors[0], opts)
	if err != nil {
		return errResult(fmt.Sprintf("search: %v", err)), nil, nil
	}

	formatted := formatSearchResults(results)
	if s.cache != nil {
		key := searchKey(input.Query, source, chunkType, topK)
		s.cache.lru.Add(key, formatted)
	}

	return textResult(formatted), nil, nil
}

func (s *Server) handleSearchCode(ctx context.Context, _ *mcp.CallToolRequest, input SearchCodeInput) (*mcp.CallToolResult, any, error) {
	topK := defaultTopK
	if input.TopK != nil {
		topK = *input.TopK
	}
	var source, language string
	if input.Source != nil {
		source = *input.Source
	}
	if input.Language != nil {
		language = *input.Language
	}

	if s.cache != nil {
		key := searchCodeKey(input.Query, source, language, topK)
		if cached, ok := s.cache.lru.Get(key); ok {
			return textResult(cached), nil, nil
		}
	}

	vectors, err := s.embedder.Embed(ctx, []string{input.Query})
	if err != nil {
		return errResult(fmt.Sprintf("embed query: %v", err)), nil, nil
	}

	opts := store.SearchOpts{
		TopK:      topK,
		ChunkType: "code",
		Source:    source,
		Language:  language,
	}

	results, err := s.store.Search(ctx, vectors[0], opts)
	if err != nil {
		return errResult(fmt.Sprintf("search: %v", err)), nil, nil
	}

	formatted := formatSearchResults(results)
	if s.cache != nil {
		key := searchCodeKey(input.Query, source, language, topK)
		s.cache.lru.Add(key, formatted)
	}

	return textResult(formatted), nil, nil
}

func (s *Server) handleListSources(ctx context.Context, _ *mcp.CallToolRequest, _ ListSourcesInput) (*mcp.CallToolResult, any, error) {
	if s.cache != nil {
		key := listSourcesKey()
		if cached, ok := s.cache.lru.Get(key); ok {
			return textResult(cached), nil, nil
		}
	}

	sources, err := s.store.ListSources(ctx)
	if err != nil {
		return errResult(fmt.Sprintf("list sources: %v", err)), nil, nil
	}

	formatted := formatSourceList(sources)
	if s.cache != nil {
		s.cache.lru.Add(listSourcesKey(), formatted)
	}

	return textResult(formatted), nil, nil
}

func textResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
	}
}

func errResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
		IsError: true,
	}
}
