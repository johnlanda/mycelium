package embedder

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

type ollamaEmbedder struct {
	model      string
	baseURL    string
	cfg        Config
	dimensions int
}

func newOllamaEmbedder(model, baseURL string, cfg Config) *ollamaEmbedder {
	applyDefaults(&cfg)
	return &ollamaEmbedder{
		model:   model,
		baseURL: baseURL,
		cfg:     cfg,
	}
}

func (o *ollamaEmbedder) ModelID() string { return "ollama/" + o.model }
func (o *ollamaEmbedder) Dimensions() int { return o.dimensions }

type ollamaRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type ollamaResponse struct {
	Embeddings [][]float64 `json:"embeddings"`
}

func (o *ollamaEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	all := make([][]float32, 0, len(texts))

	for i, text := range texts {
		var result []float32
		err := doWithRetry(ctx, o.cfg.MaxRetries, func() error {
			reqBody := ollamaRequest{
				Model: o.model,
				Input: []string{text},
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

			if len(ollamaResp.Embeddings) == 0 {
				return fmt.Errorf("ollama: no embeddings in response")
			}

			vec := make([]float32, len(ollamaResp.Embeddings[0]))
			for j, f := range ollamaResp.Embeddings[0] {
				vec[j] = float32(f)
			}
			result = vec
			return nil
		})
		if err != nil {
			return nil, err
		}

		if i == 0 {
			o.dimensions = len(result)
		}

		all = append(all, result)
	}

	return all, nil
}
