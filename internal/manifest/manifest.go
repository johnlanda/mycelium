// Package manifest handles parsing and validation of mycelium.toml.
package manifest

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// Manifest represents the full mycelium.toml configuration.
type Manifest struct {
	Config       Config       `toml:"config"`
	Local        Local        `toml:"local"`
	Dependencies []Dependency `toml:"dependencies"`
}

// Config holds top-level configuration.
type Config struct {
	EmbeddingModel string `toml:"embedding_model"`
	Publish        string `toml:"publish,omitempty"`
}

// Local defines paths to index from the local project.
type Local struct {
	Index   []string `toml:"index,omitempty"`
	Private []string `toml:"private,omitempty"`
}

// Dependency declares a single dependency source.
type Dependency struct {
	ID             string   `toml:"id"`
	Source         string   `toml:"source"`
	Ref            string   `toml:"ref"`
	Docs           []string `toml:"docs,omitempty"`
	Code           []string `toml:"code,omitempty"`
	CodeExtensions []string `toml:"code_extensions,omitempty"`
}

// ParseFile reads and parses a mycelium.toml from the given path.
func ParseFile(path string) (*Manifest, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open manifest: %w", err)
	}
	defer f.Close()
	return Parse(f)
}

// Parse reads and parses a mycelium.toml from a reader.
func Parse(r io.Reader) (*Manifest, error) {
	var m Manifest
	if _, err := toml.NewDecoder(r).Decode(&m); err != nil {
		return nil, fmt.Errorf("decode manifest: %w", err)
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

// Validate checks that the manifest has all required fields and no duplicates.
func (m *Manifest) Validate() error {
	var errs []string

	if m.Config.EmbeddingModel == "" {
		errs = append(errs, "config.embedding_model is required")
	}

	seen := make(map[string]bool)
	for i, dep := range m.Dependencies {
		prefix := fmt.Sprintf("dependencies[%d]", i)

		if dep.ID == "" {
			errs = append(errs, prefix+".id is required")
		}
		if dep.Source == "" {
			errs = append(errs, prefix+".source is required")
		}
		if dep.Ref == "" {
			errs = append(errs, prefix+".ref is required")
		}

		if dep.ID != "" {
			if seen[dep.ID] {
				errs = append(errs, fmt.Sprintf("duplicate dependency id %q", dep.ID))
			}
			seen[dep.ID] = true
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("invalid manifest:\n  %s", strings.Join(errs, "\n  "))
	}
	return nil
}

// WriteFile writes the manifest atomically to the given path.
// It writes to a temporary file in the same directory, then renames.
func (m *Manifest) WriteFile(path string) error {
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(m); err != nil {
		return fmt.Errorf("encode manifest: %w", err)
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "mycelium.toml.*.tmp")
	if err != nil {
		return fmt.Errorf("create temp manifest: %w", err)
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(buf.Bytes()); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write temp manifest: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close temp manifest: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename manifest: %w", err)
	}
	return nil
}
