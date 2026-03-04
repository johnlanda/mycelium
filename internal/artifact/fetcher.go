package artifact

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/johnlanda/mycelium/internal/fetchers"
	"github.com/johnlanda/mycelium/internal/store"
)

// FetchArtifactResult holds the output of a successful artifact download.
type FetchArtifactResult struct {
	Chunks   []store.StoredChunk
	Meta     ArtifactMeta
	Checksum string // "sha256:<hex>"
}

// FetchArtifact downloads a gzipped JSONL artifact, verifies its checksum,
// and parses it into StoredChunks.
func FetchArtifact(ctx context.Context, artifactURL, expectedHash string) (*FetchArtifactResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, artifactURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	addAuthHeader(req, artifactURL)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download artifact: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download artifact: HTTP %d", resp.StatusCode)
	}

	// Stream through TeeReader so we hash while reading.
	h := sha256.New()
	tr := io.TeeReader(resp.Body, h)

	chunks, meta, err := Read(tr)
	if err != nil {
		return nil, fmt.Errorf("parse artifact: %w", err)
	}

	checksum := fmt.Sprintf("sha256:%x", h.Sum(nil))
	if expectedHash != "" && checksum != expectedHash {
		return nil, fmt.Errorf("checksum mismatch: got %s, want %s", checksum, expectedHash)
	}

	return &FetchArtifactResult{
		Chunks:   chunks,
		Meta:     meta,
		Checksum: checksum,
	}, nil
}

// ResolveArtifactURL constructs the default GitHub release artifact URL.
// Format: https://{host}/{owner}/{repo}/releases/download/{ref}/mycelium-{model-slug}.jsonl.gz
func ResolveArtifactURL(source, ref, modelSlug string) string {
	return fmt.Sprintf("https://%s/releases/download/%s/mycelium-%s.jsonl.gz", source, ref, modelSlug)
}

// CheckArtifactExists performs an HTTP HEAD on the URL and returns true on 200.
func CheckArtifactExists(ctx context.Context, url string) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return false, fmt.Errorf("create head request: %w", err)
	}
	addAuthHeader(req, url)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("head artifact: %w", err)
	}
	resp.Body.Close()

	return resp.StatusCode == http.StatusOK, nil
}

// FetchChecksumFromURL downloads the .sha256 companion file and returns the
// checksum as "sha256:<hex>".
func FetchChecksumFromURL(ctx context.Context, artifactURL string) (string, error) {
	checksumURL := artifactURL + ".sha256"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, checksumURL, nil)
	if err != nil {
		return "", fmt.Errorf("create checksum request: %w", err)
	}
	addAuthHeader(req, checksumURL)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("download checksum: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download checksum: HTTP %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read checksum body: %w", err)
	}

	fields := strings.Fields(strings.TrimSpace(string(data)))
	if len(fields) == 0 {
		return "", fmt.Errorf("empty checksum response")
	}
	return "sha256:" + fields[0], nil
}

// addAuthHeader sets the Authorization header if a token is available.
func addAuthHeader(req *http.Request, rawURL string) {
	// Extract the host/owner/repo portion to pass to SelectToken.
	// URLs look like https://github.com/owner/repo/releases/...
	host := req.URL.Host
	path := strings.TrimPrefix(req.URL.Path, "/")
	parts := strings.SplitN(path, "/", 3)
	var source string
	if len(parts) >= 2 {
		source = host + "/" + parts[0] + "/" + parts[1]
	} else {
		source = host + "/" + path
	}

	token := fetchers.SelectToken(source)
	if token != "" {
		req.Header.Set("Authorization", "token "+token)
	}
}
