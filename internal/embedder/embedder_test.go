package embedder

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// --- Factory tests ---

func TestFactoryKnownModels(t *testing.T) {
	t.Setenv("VOYAGE_API_KEY", "test-key")
	t.Setenv("OPENAI_API_KEY", "test-key")

	// For ollama, we need a mock server that handles /api/tags + /api/embed.
	srv := newMockOllamaServer(t, mockOllamaOpts{
		models: []string{"nomic-embed-text"},
		dims:   4,
	})
	defer srv.Close()
	t.Setenv("OLLAMA_URL", srv.URL)

	tests := []struct {
		model    string
		wantType string
	}{
		{"voyage-code-2", "*embedder.voyageEmbedder"},
		{"text-embedding-3-small", "*embedder.openaiEmbedder"},
		{"ollama/nomic-embed-text", "*embedder.ollamaEmbedder"},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			e, err := NewEmbedder(tt.model, Config{})
			if err != nil {
				t.Fatalf("NewEmbedder(%q) error: %v", tt.model, err)
			}
			got := fmt.Sprintf("%T", e)
			if got != tt.wantType {
				t.Errorf("NewEmbedder(%q) type = %s, want %s", tt.model, got, tt.wantType)
			}
		})
	}
}

func TestFactoryUnknownModel(t *testing.T) {
	_, err := NewEmbedder("unknown-model", Config{})
	if err == nil {
		t.Fatal("expected error for unknown model")
	}
	errMsg := err.Error()
	if !strings.Contains(errMsg, "unsupported model") {
		t.Errorf("error should mention unsupported model, got: %s", errMsg)
	}
	for _, supported := range []string{"voyage-code-2", "text-embedding-3-small", "ollama/<model>"} {
		if !strings.Contains(errMsg, supported) {
			t.Errorf("error should list supported model %q, got: %s", supported, errMsg)
		}
	}
}

func TestFactoryMissingAPIKey(t *testing.T) {
	tests := []struct {
		model  string
		envVar string
	}{
		{"voyage-code-2", "VOYAGE_API_KEY"},
		{"text-embedding-3-small", "OPENAI_API_KEY"},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			t.Setenv(tt.envVar, "")
			_, err := NewEmbedder(tt.model, Config{})
			if err == nil {
				t.Fatalf("expected error when %s is not set", tt.envVar)
			}
			if !strings.Contains(err.Error(), tt.envVar) {
				t.Errorf("error should mention %s, got: %s", tt.envVar, err.Error())
			}
		})
	}
}

// --- Mock Ollama server helper ---

type mockOllamaOpts struct {
	models []string // model names to list in /api/tags
	dims   int      // embedding dimensions to return
}

// newMockOllamaServer creates a httptest.Server that handles /api/tags and /api/embed.
func newMockOllamaServer(t *testing.T, opts mockOllamaOpts) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/tags":
			tagsResp := ollamaTagsResponse{}
			for _, m := range opts.models {
				tagsResp.Models = append(tagsResp.Models, ollamaModelInfo{Name: m + ":latest"})
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(tagsResp)

		case r.Method == http.MethodPost && r.URL.Path == "/api/embed":
			var req ollamaRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Errorf("decode ollama request: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}

			dims := opts.dims
			if req.Dimensions > 0 {
				dims = req.Dimensions
			}

			resp := ollamaResponse{}
			for range req.Input {
				vec := make([]float64, dims)
				for j := range vec {
					vec[j] = 0.1 * float64(j+1)
				}
				resp.Embeddings = append(resp.Embeddings, vec)
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

// --- Helper to create a mock embedding server ---

func mockEmbeddingServer(t *testing.T, dims int, handler func(w http.ResponseWriter, r *http.Request)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(handler))
}

// --- Voyage tests ---

func TestVoyageEmbed(t *testing.T) {
	srv := mockEmbeddingServer(t, 3, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/embeddings" {
			t.Errorf("expected /v1/embeddings, got %s", r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-voyage-key" {
			t.Errorf("expected Bearer test-voyage-key, got %s", auth)
		}

		var req voyageRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Model != "voyage-code-2" {
			t.Errorf("expected model voyage-code-2, got %s", req.Model)
		}

		resp := voyageResponse{}
		for i := range req.Input {
			resp.Data = append(resp.Data, struct {
				Embedding []float64 `json:"embedding"`
				Index     int       `json:"index"`
			}{
				Embedding: []float64{float64(i) + 0.1, float64(i) + 0.2, float64(i) + 0.3},
				Index:     i,
			})
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})
	defer srv.Close()

	e := newVoyageEmbedder("test-voyage-key", srv.URL, Config{MaxRetries: 1})
	vecs, err := e.Embed(context.Background(), []string{"hello", "world"})
	if err != nil {
		t.Fatalf("Embed error: %v", err)
	}
	if len(vecs) != 2 {
		t.Fatalf("expected 2 vectors, got %d", len(vecs))
	}
	if len(vecs[0]) != 3 {
		t.Fatalf("expected 3 dimensions, got %d", len(vecs[0]))
	}
	if vecs[0][0] != float32(0.1) {
		t.Errorf("expected vecs[0][0] = 0.1, got %f", vecs[0][0])
	}
	if vecs[1][0] != float32(1.1) {
		t.Errorf("expected vecs[1][0] = 1.1, got %f", vecs[1][0])
	}
}

// --- OpenAI tests ---

func TestOpenAIEmbed(t *testing.T) {
	srv := mockEmbeddingServer(t, 3, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/embeddings" {
			t.Errorf("expected /v1/embeddings, got %s", r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-openai-key" {
			t.Errorf("expected Bearer test-openai-key, got %s", auth)
		}

		var req openaiRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Model != "text-embedding-3-small" {
			t.Errorf("expected model text-embedding-3-small, got %s", req.Model)
		}

		resp := openaiResponse{}
		for i := range req.Input {
			resp.Data = append(resp.Data, struct {
				Embedding []float64 `json:"embedding"`
				Index     int       `json:"index"`
			}{
				Embedding: []float64{float64(i) + 0.5, float64(i) + 0.6, float64(i) + 0.7},
				Index:     i,
			})
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})
	defer srv.Close()

	e := newOpenAIEmbedder("test-openai-key", srv.URL, Config{MaxRetries: 1})
	vecs, err := e.Embed(context.Background(), []string{"hello", "world"})
	if err != nil {
		t.Fatalf("Embed error: %v", err)
	}
	if len(vecs) != 2 {
		t.Fatalf("expected 2 vectors, got %d", len(vecs))
	}
	if len(vecs[0]) != 3 {
		t.Fatalf("expected 3 dimensions, got %d", len(vecs[0]))
	}
	if vecs[0][0] != float32(0.5) {
		t.Errorf("expected vecs[0][0] = 0.5, got %f", vecs[0][0])
	}
}

// --- Ollama tests ---

func TestOllamaEmbed(t *testing.T) {
	srv := newMockOllamaServer(t, mockOllamaOpts{
		models: []string{"nomic-embed-text"},
		dims:   4,
	})
	defer srv.Close()

	e, err := newOllamaEmbedder("nomic-embed-text", srv.URL, Config{MaxRetries: 1})
	if err != nil {
		t.Fatalf("newOllamaEmbedder error: %v", err)
	}

	vecs, err := e.Embed(context.Background(), []string{"hello", "world"})
	if err != nil {
		t.Fatalf("Embed error: %v", err)
	}
	if len(vecs) != 2 {
		t.Fatalf("expected 2 vectors, got %d", len(vecs))
	}
	if len(vecs[0]) != 4 {
		t.Fatalf("expected 4 dimensions, got %d", len(vecs[0]))
	}
	if e.Dimensions() != 4 {
		t.Errorf("expected dimensions=4, got %d", e.Dimensions())
	}
}

func TestOllamaEmbed_Batch(t *testing.T) {
	var callCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/tags":
			json.NewEncoder(w).Encode(ollamaTagsResponse{
				Models: []ollamaModelInfo{{Name: "test-model:latest"}},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/embed":
			callCount.Add(1)
			var req ollamaRequest
			json.NewDecoder(r.Body).Decode(&req)
			resp := ollamaResponse{}
			for range req.Input {
				resp.Embeddings = append(resp.Embeddings, []float64{0.1, 0.2})
			}
			json.NewEncoder(w).Encode(resp)
		}
	}))
	defer srv.Close()

	e, err := newOllamaEmbedder("test-model", srv.URL, Config{
		MaxRetries:          1,
		EmbeddingDimensions: 2, // skip probe
	})
	if err != nil {
		t.Fatalf("newOllamaEmbedder error: %v", err)
	}

	// Reset call count after construction (probe/tags calls).
	callCount.Store(0)

	// 200 texts with batch size 64 → ceil(200/64) = 4 requests.
	texts := make([]string, 200)
	for i := range texts {
		texts[i] = fmt.Sprintf("text-%d", i)
	}

	vecs, err := e.Embed(context.Background(), texts)
	if err != nil {
		t.Fatalf("Embed error: %v", err)
	}
	if len(vecs) != 200 {
		t.Errorf("expected 200 vectors, got %d", len(vecs))
	}
	if calls := callCount.Load(); calls != 4 {
		t.Errorf("expected 4 API calls (batching), got %d", calls)
	}
}

func TestOllamaEmbed_ConfiguredDimensions(t *testing.T) {
	var capturedDims int

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/tags":
			json.NewEncoder(w).Encode(ollamaTagsResponse{
				Models: []ollamaModelInfo{{Name: "qwen3-embedding:latest"}},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/embed":
			var req ollamaRequest
			json.NewDecoder(r.Body).Decode(&req)
			capturedDims = req.Dimensions
			dims := req.Dimensions
			if dims == 0 {
				dims = 4096
			}
			resp := ollamaResponse{}
			for range req.Input {
				vec := make([]float64, dims)
				resp.Embeddings = append(resp.Embeddings, vec)
			}
			json.NewEncoder(w).Encode(resp)
		}
	}))
	defer srv.Close()

	e, err := newOllamaEmbedder("qwen3-embedding", srv.URL, Config{
		MaxRetries:          1,
		EmbeddingDimensions: 1024,
	})
	if err != nil {
		t.Fatalf("newOllamaEmbedder error: %v", err)
	}

	if e.Dimensions() != 1024 {
		t.Errorf("Dimensions() = %d, want 1024", e.Dimensions())
	}

	vecs, err := e.Embed(context.Background(), []string{"hello"})
	if err != nil {
		t.Fatalf("Embed error: %v", err)
	}
	if capturedDims != 1024 {
		t.Errorf("request dimensions = %d, want 1024", capturedDims)
	}
	if len(vecs[0]) != 1024 {
		t.Errorf("vector length = %d, want 1024", len(vecs[0]))
	}
}

func TestOllamaEmbed_DimensionProbe(t *testing.T) {
	srv := newMockOllamaServer(t, mockOllamaOpts{
		models: []string{"test-model"},
		dims:   768,
	})
	defer srv.Close()

	e, err := newOllamaEmbedder("test-model", srv.URL, Config{MaxRetries: 1})
	if err != nil {
		t.Fatalf("newOllamaEmbedder error: %v", err)
	}

	if e.Dimensions() != 768 {
		t.Errorf("Dimensions() = %d, want 768 (probed)", e.Dimensions())
	}
}

func TestOllamaEmbed_ModelNotFound(t *testing.T) {
	// Server that lists no matching models.
	srv := newMockOllamaServer(t, mockOllamaOpts{
		models: []string{"other-model"},
		dims:   4,
	})
	defer srv.Close()

	_, err := newOllamaEmbedder("nonexistent-model", srv.URL, Config{MaxRetries: 1})
	if err == nil {
		t.Fatal("expected error for model not found")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention 'not found', got: %s", err.Error())
	}
	if !strings.Contains(err.Error(), "ollama pull") {
		t.Errorf("error should suggest 'ollama pull', got: %s", err.Error())
	}
}

func TestOllamaEmbed_OllamaUnreachable(t *testing.T) {
	// Use an address that will not connect.
	_, err := newOllamaEmbedder("test-model", "http://127.0.0.1:1", Config{MaxRetries: 1})
	if err == nil {
		t.Fatal("expected error for unreachable server")
	}
	if !strings.Contains(err.Error(), "unreachable") {
		t.Errorf("error should mention 'unreachable', got: %s", err.Error())
	}
}

func TestOllamaEmbed_Truncate(t *testing.T) {
	var gotTruncate bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/tags":
			json.NewEncoder(w).Encode(ollamaTagsResponse{
				Models: []ollamaModelInfo{{Name: "test-model:latest"}},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/embed":
			var req ollamaRequest
			json.NewDecoder(r.Body).Decode(&req)
			gotTruncate = req.Truncate
			resp := ollamaResponse{}
			for range req.Input {
				resp.Embeddings = append(resp.Embeddings, []float64{0.1, 0.2, 0.3})
			}
			json.NewEncoder(w).Encode(resp)
		}
	}))
	defer srv.Close()

	e, err := newOllamaEmbedder("test-model", srv.URL, Config{
		MaxRetries:          1,
		EmbeddingDimensions: 3,
	})
	if err != nil {
		t.Fatalf("newOllamaEmbedder error: %v", err)
	}

	_, err = e.Embed(context.Background(), []string{"hello"})
	if err != nil {
		t.Fatalf("Embed error: %v", err)
	}
	if !gotTruncate {
		t.Error("expected truncate=true in request, got false")
	}
}

func TestOllamaEmbed_BatchResponseMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/tags":
			json.NewEncoder(w).Encode(ollamaTagsResponse{
				Models: []ollamaModelInfo{{Name: "test-model:latest"}},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/embed":
			// Always return exactly 1 embedding regardless of input count.
			resp := ollamaResponse{
				Embeddings: [][]float64{{0.1, 0.2}},
			}
			json.NewEncoder(w).Encode(resp)
		}
	}))
	defer srv.Close()

	e, err := newOllamaEmbedder("test-model", srv.URL, Config{
		MaxRetries:          1,
		EmbeddingDimensions: 2,
	})
	if err != nil {
		t.Fatalf("newOllamaEmbedder error: %v", err)
	}

	_, err = e.Embed(context.Background(), []string{"one", "two", "three"})
	if err == nil {
		t.Fatal("expected error for response count mismatch")
	}
	if !strings.Contains(err.Error(), "expected 3 embeddings") {
		t.Errorf("error should mention expected count, got: %s", err.Error())
	}
}

// --- Batching test ---

func TestVoyageBatching(t *testing.T) {
	var callCount atomic.Int32

	srv := mockEmbeddingServer(t, 2, func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)

		var req voyageRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		resp := voyageResponse{}
		for i := range req.Input {
			resp.Data = append(resp.Data, struct {
				Embedding []float64 `json:"embedding"`
				Index     int       `json:"index"`
			}{
				Embedding: []float64{0.1, 0.2},
				Index:     i,
			})
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})
	defer srv.Close()

	e := newVoyageEmbedder("test-key", srv.URL, Config{MaxRetries: 1})

	// 200 texts should result in 2 batches (128 + 72)
	texts := make([]string, 200)
	for i := range texts {
		texts[i] = fmt.Sprintf("text-%d", i)
	}

	vecs, err := e.Embed(context.Background(), texts)
	if err != nil {
		t.Fatalf("Embed error: %v", err)
	}
	if len(vecs) != 200 {
		t.Errorf("expected 200 vectors, got %d", len(vecs))
	}
	if calls := callCount.Load(); calls != 2 {
		t.Errorf("expected 2 API calls (batching), got %d", calls)
	}
}

// --- Retry tests ---

func TestRetryOn429(t *testing.T) {
	var callCount atomic.Int32

	srv := mockEmbeddingServer(t, 2, func(w http.ResponseWriter, r *http.Request) {
		n := callCount.Add(1)
		if n <= 2 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			fmt.Fprint(w, "rate limited")
			return
		}

		var req voyageRequest
		json.NewDecoder(r.Body).Decode(&req)

		resp := voyageResponse{}
		for i := range req.Input {
			resp.Data = append(resp.Data, struct {
				Embedding []float64 `json:"embedding"`
				Index     int       `json:"index"`
			}{
				Embedding: []float64{0.1, 0.2},
				Index:     i,
			})
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})
	defer srv.Close()

	e := newVoyageEmbedder("test-key", srv.URL, Config{MaxRetries: 5})
	vecs, err := e.Embed(context.Background(), []string{"hello"})
	if err != nil {
		t.Fatalf("Embed error: %v", err)
	}
	if len(vecs) != 1 {
		t.Fatalf("expected 1 vector, got %d", len(vecs))
	}
	if calls := callCount.Load(); calls != 3 {
		t.Errorf("expected 3 calls (2 retries + success), got %d", calls)
	}
}

func TestMaxRetriesExceeded(t *testing.T) {
	srv := mockEmbeddingServer(t, 0, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, "rate limited")
	})
	defer srv.Close()

	e := newVoyageEmbedder("test-key", srv.URL, Config{MaxRetries: 2})
	_, err := e.Embed(context.Background(), []string{"hello"})
	if err == nil {
		t.Fatal("expected error after max retries exceeded")
	}
	if !strings.Contains(err.Error(), "429") {
		t.Errorf("error should mention 429, got: %s", err.Error())
	}
}

func TestNon429NotRetried(t *testing.T) {
	var callCount atomic.Int32

	tests := []struct {
		name   string
		status int
	}{
		{"401 Unauthorized", http.StatusUnauthorized},
		{"500 Internal Server Error", http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			callCount.Store(0)

			srv := mockEmbeddingServer(t, 0, func(w http.ResponseWriter, r *http.Request) {
				callCount.Add(1)
				w.WriteHeader(tt.status)
				fmt.Fprintf(w, "error %d", tt.status)
			})
			defer srv.Close()

			e := newVoyageEmbedder("test-key", srv.URL, Config{MaxRetries: 3})
			_, err := e.Embed(context.Background(), []string{"hello"})
			if err == nil {
				t.Fatal("expected error")
			}
			if calls := callCount.Load(); calls != 1 {
				t.Errorf("expected exactly 1 call (no retry for %d), got %d", tt.status, calls)
			}
		})
	}
}

// --- Empty input tests ---

func TestEmptyInput(t *testing.T) {
	// Ollama constructor needs a server, so create one for the test.
	srv := newMockOllamaServer(t, mockOllamaOpts{
		models: []string{"model"},
		dims:   4,
	})
	defer srv.Close()

	tests := []struct {
		name    string
		embedFn func() ([][]float32, error)
	}{
		{
			"voyage",
			func() ([][]float32, error) {
				e := newVoyageEmbedder("key", "http://unused", Config{MaxRetries: 1})
				return e.Embed(context.Background(), []string{})
			},
		},
		{
			"openai",
			func() ([][]float32, error) {
				e := newOpenAIEmbedder("key", "http://unused", Config{MaxRetries: 1})
				return e.Embed(context.Background(), []string{})
			},
		},
		{
			"ollama",
			func() ([][]float32, error) {
				e, err := newOllamaEmbedder("model", srv.URL, Config{MaxRetries: 1})
				if err != nil {
					return nil, err
				}
				return e.Embed(context.Background(), []string{})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vecs, err := tt.embedFn()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if vecs != nil {
				t.Errorf("expected nil, got %v", vecs)
			}
		})
	}
}

// --- Context cancellation ---

func TestContextCancellation(t *testing.T) {
	srv := mockEmbeddingServer(t, 0, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, "rate limited")
	})
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel after a short delay to allow the first attempt to fail with 429
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	e := newVoyageEmbedder("test-key", srv.URL, Config{MaxRetries: 10})
	_, err := e.Embed(ctx, []string{"hello"})
	if err == nil {
		t.Fatal("expected error from context cancellation")
	}
	if err != context.Canceled {
		// The error might be wrapped, check if it contains context.Canceled
		if !strings.Contains(err.Error(), "context canceled") {
			t.Errorf("expected context.Canceled, got: %v", err)
		}
	}
}

// --- ModelID / Dimensions ---

func TestModelIDAndDimensions(t *testing.T) {
	srv := newMockOllamaServer(t, mockOllamaOpts{
		models: []string{"nomic-embed-text"},
		dims:   768,
	})
	defer srv.Close()

	ollamaEmb, err := newOllamaEmbedder("nomic-embed-text", srv.URL, Config{MaxRetries: 1})
	if err != nil {
		t.Fatalf("newOllamaEmbedder error: %v", err)
	}

	tests := []struct {
		name      string
		embedder  Embedder
		wantModel string
		wantDims  int
	}{
		{
			"voyage",
			newVoyageEmbedder("key", "http://unused", Config{MaxRetries: 1}),
			"voyage-code-2",
			1536,
		},
		{
			"openai",
			newOpenAIEmbedder("key", "http://unused", Config{MaxRetries: 1}),
			"text-embedding-3-small",
			1536,
		},
		{
			"ollama",
			ollamaEmb,
			"ollama/nomic-embed-text",
			768, // now probed at construction time
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.embedder.ModelID(); got != tt.wantModel {
				t.Errorf("ModelID() = %q, want %q", got, tt.wantModel)
			}
			if got := tt.embedder.Dimensions(); got != tt.wantDims {
				t.Errorf("Dimensions() = %d, want %d", got, tt.wantDims)
			}
		})
	}
}
