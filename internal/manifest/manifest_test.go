package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr string
		check   func(t *testing.T, m *Manifest)
	}{
		{
			name: "full manifest",
			input: `
[config]
embedding_model = "voyage-code-2"
publish = "github-releases"

[local]
index = ["./docs", "./README.md"]
private = ["./notes"]

[[dependencies]]
id = "envoy-gateway"
source = "github.com/envoyproxy/gateway"
ref = "v1.3.0"
docs = ["site/content"]
code = ["api/v1alpha1"]
code_extensions = [".go"]

[[dependencies]]
id = "platform-sdk"
source = "github.example.com/platform/sdk"
ref = "v4.2.0"
docs = ["docs/", "api-reference/"]
code = ["pkg/client", "pkg/types"]
`,
			check: func(t *testing.T, m *Manifest) {
				if m.Config.EmbeddingModel != "voyage-code-2" {
					t.Errorf("embedding_model = %q, want %q", m.Config.EmbeddingModel, "voyage-code-2")
				}
				if m.Config.Publish != "github-releases" {
					t.Errorf("publish = %q, want %q", m.Config.Publish, "github-releases")
				}
				if len(m.Local.Index) != 2 {
					t.Errorf("local.index len = %d, want 2", len(m.Local.Index))
				}
				if len(m.Local.Private) != 1 {
					t.Errorf("local.private len = %d, want 1", len(m.Local.Private))
				}
				if len(m.Dependencies) != 2 {
					t.Fatalf("dependencies len = %d, want 2", len(m.Dependencies))
				}
				dep := m.Dependencies[0]
				if dep.ID != "envoy-gateway" {
					t.Errorf("dep[0].id = %q, want %q", dep.ID, "envoy-gateway")
				}
				if dep.Source != "github.com/envoyproxy/gateway" {
					t.Errorf("dep[0].source = %q", dep.Source)
				}
				if dep.Ref != "v1.3.0" {
					t.Errorf("dep[0].ref = %q", dep.Ref)
				}
				if len(dep.Docs) != 1 || dep.Docs[0] != "site/content" {
					t.Errorf("dep[0].docs = %v", dep.Docs)
				}
				if len(dep.Code) != 1 || dep.Code[0] != "api/v1alpha1" {
					t.Errorf("dep[0].code = %v", dep.Code)
				}
				if len(dep.CodeExtensions) != 1 || dep.CodeExtensions[0] != ".go" {
					t.Errorf("dep[0].code_extensions = %v", dep.CodeExtensions)
				}
			},
		},
		{
			name: "minimal manifest",
			input: `
[config]
embedding_model = "text-embedding-3-small"
`,
			check: func(t *testing.T, m *Manifest) {
				if m.Config.EmbeddingModel != "text-embedding-3-small" {
					t.Errorf("embedding_model = %q", m.Config.EmbeddingModel)
				}
				if len(m.Dependencies) != 0 {
					t.Errorf("dependencies len = %d, want 0", len(m.Dependencies))
				}
			},
		},
		{
			name: "code-only dependency",
			input: `
[config]
embedding_model = "voyage-code-2"

[[dependencies]]
id = "compliance-lib"
source = "github.example.com/infra/compliance"
ref = "v2.1.0"
code = ["pkg/"]
`,
			check: func(t *testing.T, m *Manifest) {
				if len(m.Dependencies) != 1 {
					t.Fatalf("dependencies len = %d, want 1", len(m.Dependencies))
				}
				dep := m.Dependencies[0]
				if len(dep.Docs) != 0 {
					t.Errorf("dep.docs = %v, want empty", dep.Docs)
				}
				if len(dep.Code) != 1 {
					t.Errorf("dep.code len = %d, want 1", len(dep.Code))
				}
			},
		},
		{
			name: "missing embedding_model",
			input: `
[config]
publish = "github-releases"
`,
			wantErr: "config.embedding_model is required",
		},
		{
			name: "missing dependency id",
			input: `
[config]
embedding_model = "voyage-code-2"

[[dependencies]]
source = "github.com/foo/bar"
ref = "v1.0.0"
`,
			wantErr: "dependencies[0].id is required",
		},
		{
			name: "missing dependency source",
			input: `
[config]
embedding_model = "voyage-code-2"

[[dependencies]]
id = "foo"
ref = "v1.0.0"
`,
			wantErr: "dependencies[0].source is required",
		},
		{
			name: "missing dependency ref",
			input: `
[config]
embedding_model = "voyage-code-2"

[[dependencies]]
id = "foo"
source = "github.com/foo/bar"
`,
			wantErr: "dependencies[0].ref is required",
		},
		{
			name: "duplicate dependency ids",
			input: `
[config]
embedding_model = "voyage-code-2"

[[dependencies]]
id = "foo"
source = "github.com/foo/bar"
ref = "v1.0.0"

[[dependencies]]
id = "foo"
source = "github.com/baz/qux"
ref = "v2.0.0"
`,
			wantErr: `duplicate dependency id "foo"`,
		},
		{
			name: "multiple validation errors",
			input: `
[config]

[[dependencies]]
source = "github.com/foo/bar"
ref = "v1.0.0"
`,
			wantErr: "config.embedding_model is required",
			check: func(t *testing.T, m *Manifest) {
				// should not reach here
				t.Fatal("expected error, got nil")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := Parse(strings.NewReader(tt.input))
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %q, want containing %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.check != nil {
				tt.check(t, m)
			}
		})
	}
}

func TestParseFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mycelium.toml")

	content := `[config]
embedding_model = "voyage-code-2"

[[dependencies]]
id = "example"
source = "github.com/example/repo"
ref = "v1.0.0"
docs = ["docs/"]
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	m, err := ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if m.Config.EmbeddingModel != "voyage-code-2" {
		t.Errorf("embedding_model = %q, want %q", m.Config.EmbeddingModel, "voyage-code-2")
	}
	if len(m.Dependencies) != 1 {
		t.Fatalf("dependencies len = %d, want 1", len(m.Dependencies))
	}
	if m.Dependencies[0].ID != "example" {
		t.Errorf("dep.id = %q, want %q", m.Dependencies[0].ID, "example")
	}
}

func TestParseFile_NotFound(t *testing.T) {
	_, err := ParseFile(filepath.Join(t.TempDir(), "nonexistent.toml"))
	if err == nil {
		t.Fatal("expected error for non-existent file, got nil")
	}
	if !strings.Contains(err.Error(), "open manifest") {
		t.Errorf("error = %q, want containing %q", err.Error(), "open manifest")
	}
}

func TestWriteFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mycelium.toml")

	m := &Manifest{
		Config: Config{EmbeddingModel: "text-embedding-3-small"},
		Dependencies: []Dependency{
			{
				ID:     "mylib",
				Source: "github.com/org/mylib",
				Ref:    "v2.0.0",
				Docs:   []string{"docs/"},
			},
		},
	}

	if err := m.WriteFile(path); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile after WriteFile: %v", err)
	}
	if got.Config.EmbeddingModel != "text-embedding-3-small" {
		t.Errorf("embedding_model = %q, want %q", got.Config.EmbeddingModel, "text-embedding-3-small")
	}
	if len(got.Dependencies) != 1 {
		t.Fatalf("dependencies len = %d, want 1", len(got.Dependencies))
	}
	dep := got.Dependencies[0]
	if dep.ID != "mylib" || dep.Source != "github.com/org/mylib" || dep.Ref != "v2.0.0" {
		t.Errorf("dep = %+v, want id=mylib source=github.com/org/mylib ref=v2.0.0", dep)
	}
}

func TestWriteFileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mycelium.toml")

	original := &Manifest{
		Config: Config{
			EmbeddingModel: "voyage-code-2",
			Publish:        "github-releases",
		},
		Local: Local{
			Index:   []string{"./docs", "./README.md"},
			Private: []string{"./notes"},
		},
		Dependencies: []Dependency{
			{
				ID:             "envoy-gateway",
				Source:         "github.com/envoyproxy/gateway",
				Ref:            "v1.3.0",
				Docs:           []string{"site/content"},
				Code:           []string{"api/v1alpha1"},
				CodeExtensions: []string{".go"},
			},
			{
				ID:     "platform-sdk",
				Source: "github.example.com/platform/sdk",
				Ref:    "v4.2.0",
				Docs:   []string{"docs/", "api-reference/"},
				Code:   []string{"pkg/client", "pkg/types"},
			},
		},
	}

	if err := original.WriteFile(path); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile after WriteFile: %v", err)
	}

	// Config
	if got.Config.EmbeddingModel != original.Config.EmbeddingModel {
		t.Errorf("embedding_model = %q, want %q", got.Config.EmbeddingModel, original.Config.EmbeddingModel)
	}
	if got.Config.Publish != original.Config.Publish {
		t.Errorf("publish = %q, want %q", got.Config.Publish, original.Config.Publish)
	}

	// Local
	if len(got.Local.Index) != len(original.Local.Index) {
		t.Errorf("local.index len = %d, want %d", len(got.Local.Index), len(original.Local.Index))
	}
	for i, v := range original.Local.Index {
		if i < len(got.Local.Index) && got.Local.Index[i] != v {
			t.Errorf("local.index[%d] = %q, want %q", i, got.Local.Index[i], v)
		}
	}
	if len(got.Local.Private) != len(original.Local.Private) {
		t.Errorf("local.private len = %d, want %d", len(got.Local.Private), len(original.Local.Private))
	}

	// Dependencies
	if len(got.Dependencies) != len(original.Dependencies) {
		t.Fatalf("dependencies len = %d, want %d", len(got.Dependencies), len(original.Dependencies))
	}
	for i, want := range original.Dependencies {
		dep := got.Dependencies[i]
		if dep.ID != want.ID {
			t.Errorf("dep[%d].id = %q, want %q", i, dep.ID, want.ID)
		}
		if dep.Source != want.Source {
			t.Errorf("dep[%d].source = %q, want %q", i, dep.Source, want.Source)
		}
		if dep.Ref != want.Ref {
			t.Errorf("dep[%d].ref = %q, want %q", i, dep.Ref, want.Ref)
		}
		if len(dep.Docs) != len(want.Docs) {
			t.Errorf("dep[%d].docs len = %d, want %d", i, len(dep.Docs), len(want.Docs))
		}
		if len(dep.Code) != len(want.Code) {
			t.Errorf("dep[%d].code len = %d, want %d", i, len(dep.Code), len(want.Code))
		}
		if len(dep.CodeExtensions) != len(want.CodeExtensions) {
			t.Errorf("dep[%d].code_extensions len = %d, want %d", i, len(dep.CodeExtensions), len(want.CodeExtensions))
		}
	}
}
