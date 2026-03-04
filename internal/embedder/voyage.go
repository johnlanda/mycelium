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
	voyageBatchSize  = 128
	voyageDimensions = 1536
	voyageModel      = "voyage-code-2"
)

type voyageEmbedder struct {
	apiKey  string
	baseURL string
	cfg     Config
}

func newVoyageEmbedder(apiKey, baseURL string, cfg Config) *voyageEmbedder {
	applyDefaults(&cfg)
	return &voyageEmbedder{
		apiKey:  apiKey,
		baseURL: baseURL,
		cfg:     cfg,
	}
}

func (v *voyageEmbedder) ModelID() string  { return voyageModel }
func (v *voyageEmbedder) Dimensions() int  { return voyageDimensions }

type voyageRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type voyageResponse struct {
	Data []struct {
		Embedding []float64 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
}

func (v *voyageEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	all := make([][]float32, 0, len(texts))

	for start := 0; start < len(texts); start += voyageBatchSize {
		end := start + voyageBatchSize
		if end > len(texts) {
			end = len(texts)
		}
		batch := texts[start:end]

		var result [][]float32
		err := doWithRetry(ctx, v.cfg.MaxRetries, func() error {
			reqBody := voyageRequest{
				Model: voyageModel,
				Input: batch,
			}
			body, err := json.Marshal(reqBody)
			if err != nil {
				return fmt.Errorf("voyage: marshal request: %w", err)
			}

			req, err := http.NewRequestWithContext(ctx, http.MethodPost, v.baseURL+"/v1/embeddings", bytes.NewReader(body))
			if err != nil {
				return fmt.Errorf("voyage: create request: %w", err)
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+v.apiKey)

			resp, err := v.cfg.HTTPClient.Do(req)
			if err != nil {
				return fmt.Errorf("voyage: send request: %w", err)
			}
			defer resp.Body.Close()

			respBody, err := io.ReadAll(resp.Body)
			if err != nil {
				return fmt.Errorf("voyage: read response: %w", err)
			}

			if resp.StatusCode == http.StatusTooManyRequests {
				re := &retryableError{
					err: fmt.Errorf("voyage: rate limited (HTTP 429)"),
				}
				if ra := resp.Header.Get("Retry-After"); ra != "" {
					if secs, parseErr := strconv.Atoi(ra); parseErr == nil {
						re.retryAfter = time.Duration(secs) * time.Second
					}
				}
				return re
			}

			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				return fmt.Errorf("voyage: unexpected status %d: %s", resp.StatusCode, string(respBody))
			}

			var voyageResp voyageResponse
			if err := json.Unmarshal(respBody, &voyageResp); err != nil {
				return fmt.Errorf("voyage: decode response: %w", err)
			}

			sort.Slice(voyageResp.Data, func(i, j int) bool {
				return voyageResp.Data[i].Index < voyageResp.Data[j].Index
			})

			result = make([][]float32, len(voyageResp.Data))
			for i, d := range voyageResp.Data {
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
