// Package embedder provides embedding provider abstractions.
package embedder

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
)

// Embedder produces vector embeddings from text.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	ModelID() string
	Dimensions() int
}

// Config holds shared configuration for embedding providers.
type Config struct {
	MaxRetries int          // default 5
	HTTPClient *http.Client // injectable for tests
}

func applyDefaults(cfg *Config) {
	if cfg.MaxRetries == 0 {
		cfg.MaxRetries = 5
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = http.DefaultClient
	}
}

// NewEmbedder creates an Embedder for the given model identifier.
func NewEmbedder(model string, cfg Config) (Embedder, error) {
	applyDefaults(&cfg)

	switch {
	case model == "voyage-code-2":
		key := os.Getenv("VOYAGE_API_KEY")
		if key == "" {
			return nil, fmt.Errorf("embedder: VOYAGE_API_KEY environment variable is required for model %q", model)
		}
		return newVoyageEmbedder(key, "https://api.voyageai.com", cfg), nil

	case model == "text-embedding-3-small":
		key := os.Getenv("OPENAI_API_KEY")
		if key == "" {
			return nil, fmt.Errorf("embedder: OPENAI_API_KEY environment variable is required for model %q", model)
		}
		return newOpenAIEmbedder(key, "https://api.openai.com", cfg), nil

	case strings.HasPrefix(model, "ollama/"):
		ollamaModel := strings.TrimPrefix(model, "ollama/")
		if ollamaModel == "" {
			return nil, fmt.Errorf("embedder: ollama model name must not be empty")
		}
		baseURL := os.Getenv("OLLAMA_URL")
		if baseURL == "" {
			baseURL = "http://localhost:11434"
		}
		return newOllamaEmbedder(ollamaModel, baseURL, cfg), nil

	default:
		return nil, fmt.Errorf(
			"embedder: unsupported model %q; supported models: %q, %q, %q",
			model, "voyage-code-2", "text-embedding-3-small", "ollama/<model>",
		)
	}
}
