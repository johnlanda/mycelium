// Package lockfile handles reading and writing of mycelium.lock.
package lockfile

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/BurntSushi/toml"
)

// SchemaVersion is the current lockfile schema version.
const SchemaVersion = 1

// Lockfile represents the full mycelium.lock structure.
type Lockfile struct {
	Meta    Meta                  `toml:"meta"`
	Sources map[string]SourceLock `toml:"sources"`
}

// Meta holds lockfile metadata.
type Meta struct {
	MyceliumVersion       string `toml:"mycelium_version"`
	EmbeddingModel        string `toml:"embedding_model"`
	EmbeddingModelVersion string `toml:"embedding_model_version"`
	LockedAt              string `toml:"locked_at"`
	SchemaVersion         int    `toml:"schema_version"`
}

// SourceLock holds the resolved state of a single dependency.
type SourceLock struct {
	Version       string `toml:"version,omitempty"`
	Commit        string `toml:"commit,omitempty"`
	ContentHash   string `toml:"content_hash"`
	ArtifactURL   string `toml:"artifact_url,omitempty"`
	ArtifactHash  string `toml:"artifact_hash,omitempty"`
	StoreKey      string `toml:"store_key"`
	IngestionType string `toml:"ingestion_type"`
}

// New creates an empty lockfile with schema version set.
func New() *Lockfile {
	return &Lockfile{
		Meta: Meta{
			SchemaVersion: SchemaVersion,
		},
		Sources: make(map[string]SourceLock),
	}
}

// ReadFile reads and parses a lockfile from the given path.
func ReadFile(path string) (*Lockfile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open lockfile: %w", err)
	}
	defer f.Close()
	return Read(f)
}

// Read parses a lockfile from a reader.
func Read(r io.Reader) (*Lockfile, error) {
	var lf Lockfile
	if _, err := toml.NewDecoder(r).Decode(&lf); err != nil {
		return nil, fmt.Errorf("decode lockfile: %w", err)
	}
	if lf.Sources == nil {
		lf.Sources = make(map[string]SourceLock)
	}
	return &lf, nil
}

// WriteFile writes the lockfile atomically to the given path.
// It writes to a temporary file in the same directory, then renames.
func (lf *Lockfile) WriteFile(path string) error {
	data, err := lf.encode()
	if err != nil {
		return fmt.Errorf("encode lockfile: %w", err)
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "mycelium.lock.*.tmp")
	if err != nil {
		return fmt.Errorf("create temp lockfile: %w", err)
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write temp lockfile: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close temp lockfile: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename lockfile: %w", err)
	}
	return nil
}

// SetSource adds or updates a source entry.
func (lf *Lockfile) SetSource(id string, lock SourceLock) {
	if lf.Sources == nil {
		lf.Sources = make(map[string]SourceLock)
	}
	lf.Sources[id] = lock
}

// RemoveSource removes a source entry.
func (lf *Lockfile) RemoveSource(id string) {
	delete(lf.Sources, id)
}

// encode serializes the lockfile to TOML with deterministic key ordering.
func (lf *Lockfile) encode() ([]byte, error) {
	var buf bytes.Buffer

	// Encode meta section first.
	buf.WriteString("[meta]\n")
	if err := toml.NewEncoder(&buf).Encode(lf.Meta); err != nil {
		return nil, err
	}

	// Encode sources in sorted key order for diffability.
	keys := make([]string, 0, len(lf.Sources))
	for k := range lf.Sources {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		fmt.Fprintf(&buf, "\n[sources.%s]\n", k)
		if err := toml.NewEncoder(&buf).Encode(lf.Sources[k]); err != nil {
			return nil, err
		}
	}

	return buf.Bytes(), nil
}
