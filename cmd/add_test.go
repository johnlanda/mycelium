package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/johnlanda/mycelium/internal/manifest"
)

func setupManifest(t *testing.T) (string, func()) {
	t.Helper()
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)

	m := &manifest.Manifest{
		Config: manifest.Config{EmbeddingModel: "voyage-code-2"},
	}
	if err := m.WriteFile("mycelium.toml"); err != nil {
		t.Fatalf("setup manifest: %v", err)
	}

	return dir, func() { os.Chdir(origDir) }
}

func TestAddParsesSourceRef(t *testing.T) {
	dir, cleanup := setupManifest(t)
	defer cleanup()

	cmd := rootCmd
	cmd.SetArgs([]string{"add", "github.com/envoyproxy/gateway@v1.3.0", "--docs", "site/content"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("add failed: %v", err)
	}

	m, err := manifest.ParseFile(filepath.Join(dir, "mycelium.toml"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if len(m.Dependencies) != 1 {
		t.Fatalf("expected 1 dep, got %d", len(m.Dependencies))
	}
	dep := m.Dependencies[0]
	if dep.Source != "github.com/envoyproxy/gateway" {
		t.Errorf("source = %q", dep.Source)
	}
	if dep.Ref != "v1.3.0" {
		t.Errorf("ref = %q", dep.Ref)
	}
}

func TestAddAutoGeneratesID(t *testing.T) {
	dir, cleanup := setupManifest(t)
	defer cleanup()

	cmd := rootCmd
	cmd.SetArgs([]string{"add", "github.com/envoyproxy/gateway@v1.3.0"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("add failed: %v", err)
	}

	m, err := manifest.ParseFile(filepath.Join(dir, "mycelium.toml"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if m.Dependencies[0].ID != "gateway" {
		t.Errorf("expected auto-generated ID 'gateway', got %q", m.Dependencies[0].ID)
	}
}

func TestAddRespectsIDFlag(t *testing.T) {
	dir, cleanup := setupManifest(t)
	defer cleanup()

	cmd := rootCmd
	cmd.SetArgs([]string{"add", "github.com/envoyproxy/gateway@v1.3.0", "--id", "envoy-gw"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("add failed: %v", err)
	}

	m, err := manifest.ParseFile(filepath.Join(dir, "mycelium.toml"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if m.Dependencies[0].ID != "envoy-gw" {
		t.Errorf("expected ID 'envoy-gw', got %q", m.Dependencies[0].ID)
	}
}

func TestAddRespectsCodeFlag(t *testing.T) {
	dir, cleanup := setupManifest(t)
	defer cleanup()

	cmd := rootCmd
	cmd.SetArgs([]string{"add", "github.com/envoyproxy/gateway@v1.3.0", "--code", "api/v1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("add failed: %v", err)
	}

	m, err := manifest.ParseFile(filepath.Join(dir, "mycelium.toml"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	dep := m.Dependencies[0]
	if len(dep.Code) != 1 || dep.Code[0] != "api/v1" {
		t.Errorf("expected code=[api/v1], got %v", dep.Code)
	}
}

func TestAddRejectsDuplicateID(t *testing.T) {
	_, cleanup := setupManifest(t)
	defer cleanup()

	cmd := rootCmd
	cmd.SetArgs([]string{"add", "github.com/envoyproxy/gateway@v1.3.0"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("first add failed: %v", err)
	}

	cmd.SetArgs([]string{"add", "github.com/envoyproxy/gateway@v1.4.0"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for duplicate ID")
	}
}

func TestAddRejectsMissingRef(t *testing.T) {
	_, cleanup := setupManifest(t)
	defer cleanup()

	cmd := rootCmd
	cmd.SetArgs([]string{"add", "github.com/envoyproxy/gateway"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing @ref")
	}
}
