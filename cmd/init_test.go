package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/johnlanda/mycelium/internal/manifest"
)

func TestInitCreatesFile(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	cmd := rootCmd
	cmd.SetArgs([]string{"init"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	m, err := manifest.ParseFile(filepath.Join(dir, "mycelium.toml"))
	if err != nil {
		t.Fatalf("parse created file: %v", err)
	}
	if m.Config.EmbeddingModel != "voyage-code-2" {
		t.Errorf("expected default model voyage-code-2, got %q", m.Config.EmbeddingModel)
	}
}

func TestInitRespectsModelFlag(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	cmd := rootCmd
	cmd.SetArgs([]string{"init", "--model", "text-embedding-3-small"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	m, err := manifest.ParseFile(filepath.Join(dir, "mycelium.toml"))
	if err != nil {
		t.Fatalf("parse created file: %v", err)
	}
	if m.Config.EmbeddingModel != "text-embedding-3-small" {
		t.Errorf("expected model text-embedding-3-small, got %q", m.Config.EmbeddingModel)
	}
}

func TestInitErrorsWhenFileExists(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	// Create an existing file.
	os.WriteFile("mycelium.toml", []byte("[config]\nembedding_model = \"x\"\n"), 0644)

	cmd := rootCmd
	cmd.SetArgs([]string{"init"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when file exists")
	}
}

func TestInitRoundTrip(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	cmd := rootCmd
	cmd.SetArgs([]string{"init"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	// Read back the file and verify it's valid TOML.
	m, err := manifest.ParseFile(filepath.Join(dir, "mycelium.toml"))
	if err != nil {
		t.Fatalf("round-trip parse failed: %v", err)
	}

	// Write it again and parse again.
	if err := m.WriteFile(filepath.Join(dir, "mycelium2.toml")); err != nil {
		t.Fatalf("second write failed: %v", err)
	}
	m2, err := manifest.ParseFile(filepath.Join(dir, "mycelium2.toml"))
	if err != nil {
		t.Fatalf("second parse failed: %v", err)
	}
	if m.Config.EmbeddingModel != m2.Config.EmbeddingModel {
		t.Errorf("round-trip mismatch: %q != %q", m.Config.EmbeddingModel, m2.Config.EmbeddingModel)
	}
}
