package embedder

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const ollamaBatchSize = 64

type ollamaEmbedder struct {
	model      string
	baseURL    string
	cfg        Config
	dimensions int
}

func newOllamaEmbedder(model, baseURL string, cfg Config) (*ollamaEmbedder, error) {
	applyDefaults(&cfg)
	o := &ollamaEmbedder{
		model:   model,
		baseURL: baseURL,
		cfg:     cfg,
	}

	// Check that the model is available on the Ollama server.
	if err := o.checkModelAvailable(); err != nil {
		return nil, err
	}

	// Resolve dimensions: either from config or by probing.
	if cfg.EmbeddingDimensions > 0 {
		o.dimensions = cfg.EmbeddingDimensions
	} else {
		dims, err := o.probeDimensions()
		if err != nil {
			return nil, fmt.Errorf("ollama: probe dimensions: %w", err)
		}
		o.dimensions = dims
	}

	return o, nil
}

func (o *ollamaEmbedder) ModelID() string { return "ollama/" + o.model }
func (o *ollamaEmbedder) Dimensions() int { return o.dimensions }

type ollamaRequest struct {
	Model      string   `json:"model"`
	Input      []string `json:"input"`
	Truncate   bool     `json:"truncate,omitempty"`
	Dimensions int      `json:"dimensions,omitempty"`
}

type ollamaResponse struct {
	Embeddings [][]float64 `json:"embeddings"`
}

type ollamaTagsResponse struct {
	Models []ollamaModelInfo `json:"models"`
}

type ollamaModelInfo struct {
	Name string `json:"name"`
}

// checkModelAvailable verifies the model exists on the Ollama server via GET /api/tags.
func (o *ollamaEmbedder) checkModelAvailable() error {
	req, err := http.NewRequest(http.MethodGet, o.baseURL+"/api/tags", nil)
	if err != nil {
		return fmt.Errorf("ollama: create tags request: %w", err)
	}

	resp, err := o.cfg.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("ollama: server unreachable at %s: %w", o.baseURL, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("ollama: read tags response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ollama: unexpected status %d from /api/tags: %s", resp.StatusCode, string(body))
	}

	var tagsResp ollamaTagsResponse
	if err := json.Unmarshal(body, &tagsResp); err != nil {
		return fmt.Errorf("ollama: decode tags response: %w", err)
	}

	// Normalize model name: strip ":latest" suffix for comparison.
	normalizedTarget := strings.TrimSuffix(o.model, ":latest")
	for _, m := range tagsResp.Models {
		normalizedName := strings.TrimSuffix(m.Name, ":latest")
		if normalizedName == normalizedTarget {
			return nil
		}
	}

	return fmt.Errorf("ollama: model %q not found; run: ollama pull %s", o.model, o.model)
}

// probeDimensions sends a single-text embed request to discover the output dimensions.
func (o *ollamaEmbedder) probeDimensions() (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	reqBody := ollamaRequest{
		Model:    o.model,
		Input:    []string{"dimension probe"},
		Truncate: true,
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return 0, fmt.Errorf("marshal probe request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/api/embed", bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("create probe request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.cfg.HTTPClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("send probe request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("read probe response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, fmt.Errorf("probe embed status %d: %s", resp.StatusCode, string(respBody))
	}

	var ollamaResp ollamaResponse
	if err := json.Unmarshal(respBody, &ollamaResp); err != nil {
		return 0, fmt.Errorf("decode probe response: %w", err)
	}

	if len(ollamaResp.Embeddings) == 0 || len(ollamaResp.Embeddings[0]) == 0 {
		return 0, fmt.Errorf("probe returned no embeddings")
	}

	return len(ollamaResp.Embeddings[0]), nil
}

func (o *ollamaEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	all := make([][]float32, 0, len(texts))

	for start := 0; start < len(texts); start += ollamaBatchSize {
		end := start + ollamaBatchSize
		if end > len(texts) {
			end = len(texts)
		}
		batch := texts[start:end]

		var result [][]float32
		err := doWithRetry(ctx, o.cfg.MaxRetries, func() error {
			reqBody := ollamaRequest{
				Model:    o.model,
				Input:    batch,
				Truncate: true,
			}
			if o.cfg.EmbeddingDimensions > 0 {
				reqBody.Dimensions = o.cfg.EmbeddingDimensions
			}

			body, err := json.Marshal(reqBody)
			if err != nil {
				return fmt.Errorf("ollama: marshal request: %w", err)
			}

			req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/api/embed", bytes.NewReader(body))
			if err != nil {
				return fmt.Errorf("ollama: create request: %w", err)
			}
			req.Header.Set("Content-Type", "application/json")

			resp, err := o.cfg.HTTPClient.Do(req)
			if err != nil {
				return fmt.Errorf("ollama: send request: %w", err)
			}
			defer resp.Body.Close()

			respBody, err := io.ReadAll(resp.Body)
			if err != nil {
				return fmt.Errorf("ollama: read response: %w", err)
			}

			if resp.StatusCode == http.StatusTooManyRequests {
				re := &retryableError{
					err: fmt.Errorf("ollama: rate limited (HTTP 429)"),
				}
				if ra := resp.Header.Get("Retry-After"); ra != "" {
					if secs, parseErr := strconv.Atoi(ra); parseErr == nil {
						re.retryAfter = time.Duration(secs) * time.Second
					}
				}
				return re
			}

			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				return fmt.Errorf("ollama: unexpected status %d: %s", resp.StatusCode, string(respBody))
			}

			var ollamaResp ollamaResponse
			if err := json.Unmarshal(respBody, &ollamaResp); err != nil {
				return fmt.Errorf("ollama: decode response: %w", err)
			}

			if len(ollamaResp.Embeddings) != len(batch) {
				return fmt.Errorf("ollama: expected %d embeddings, got %d", len(batch), len(ollamaResp.Embeddings))
			}

			result = make([][]float32, len(ollamaResp.Embeddings))
			for i, emb := range ollamaResp.Embeddings {
				vec := make([]float32, len(emb))
				for j, f := range emb {
					vec[j] = float32(f)
				}
				result[i] = vec
			}
			return nil
		})
		if err != nil {
			return nil, err
		}

		all = append(all, result...)
	}

	return all, nil
}
