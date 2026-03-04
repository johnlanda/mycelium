package embedder

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"time"
)

const (
	openaiBatchSize  = 2048
	openaiDimensions = 1536
	openaiModel      = "text-embedding-3-small"
)

type openaiEmbedder struct {
	apiKey  string
	baseURL string
	cfg     Config
}

func newOpenAIEmbedder(apiKey, baseURL string, cfg Config) *openaiEmbedder {
	applyDefaults(&cfg)
	return &openaiEmbedder{
		apiKey:  apiKey,
		baseURL: baseURL,
		cfg:     cfg,
	}
}

func (o *openaiEmbedder) ModelID() string  { return openaiModel }
func (o *openaiEmbedder) Dimensions() int  { return openaiDimensions }

type openaiRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type openaiResponse struct {
	Data []struct {
		Embedding []float64 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
}

func (o *openaiEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	all := make([][]float32, 0, len(texts))

	for start := 0; start < len(texts); start += openaiBatchSize {
		end := start + openaiBatchSize
		if end > len(texts) {
			end = len(texts)
		}
		batch := texts[start:end]

		var result [][]float32
		err := doWithRetry(ctx, o.cfg.MaxRetries, func() error {
			reqBody := openaiRequest{
				Model: openaiModel,
				Input: batch,
			}
			body, err := json.Marshal(reqBody)
			if err != nil {
				return fmt.Errorf("openai: marshal request: %w", err)
			}

			req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/v1/embeddings", bytes.NewReader(body))
			if err != nil {
				return fmt.Errorf("openai: create request: %w", err)
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+o.apiKey)

			resp, err := o.cfg.HTTPClient.Do(req)
			if err != nil {
				return fmt.Errorf("openai: send request: %w", err)
			}
			defer resp.Body.Close()

			respBody, err := io.ReadAll(resp.Body)
			if err != nil {
				return fmt.Errorf("openai: read response: %w", err)
			}

			if resp.StatusCode == http.StatusTooManyRequests {
				re := &retryableError{
					err: fmt.Errorf("openai: rate limited (HTTP 429)"),
				}
				if ra := resp.Header.Get("Retry-After"); ra != "" {
					if secs, parseErr := strconv.Atoi(ra); parseErr == nil {
						re.retryAfter = time.Duration(secs) * time.Second
					}
				}
				return re
			}

			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				return fmt.Errorf("openai: unexpected status %d: %s", resp.StatusCode, string(respBody))
			}

			var openaiResp openaiResponse
			if err := json.Unmarshal(respBody, &openaiResp); err != nil {
				return fmt.Errorf("openai: decode response: %w", err)
			}

			sort.Slice(openaiResp.Data, func(i, j int) bool {
				return openaiResp.Data[i].Index < openaiResp.Data[j].Index
			})

			result = make([][]float32, len(openaiResp.Data))
			for i, d := range openaiResp.Data {
				vec := make([]float32, len(d.Embedding))
				for j, f := range d.Embedding {
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
