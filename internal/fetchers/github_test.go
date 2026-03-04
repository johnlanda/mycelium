package fetchers

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/johnlanda/mycelium/internal/hasher"
	"github.com/johnlanda/mycelium/internal/manifest"
)

// ---------------------------------------------------------------------------
// Test helpers — create disposable local git repos
// ---------------------------------------------------------------------------

// createTestRepo initialises a bare git repo in a temp dir and returns its path.
func createTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run(t, dir, "git", "init")
	run(t, dir, "git", "config", "user.email", "test@test.com")
	run(t, dir, "git", "config", "user.name", "Test")
	run(t, dir, "git", "config", "commit.gpgSign", "false")
	run(t, dir, "git", "config", "tag.gpgSign", "false")
	return dir
}

// writeFile creates (or overwrites) a file inside the repo.
func writeFile(t *testing.T, repoPath, relPath, content string) {
	t.Helper()
	abs := filepath.Join(repoPath, relPath)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// commitAll stages everything and commits, returning the commit SHA.
func commitAll(t *testing.T, repoPath, msg string) string {
	t.Helper()
	run(t, repoPath, "git", "add", ".")
	run(t, repoPath, "git", "commit", "-m", msg)
	out := run(t, repoPath, "git", "rev-parse", "HEAD")
	return strings.TrimSpace(out)
}

// tagRepo creates a lightweight tag (bypassing any global GPG signing config).
func tagRepo(t *testing.T, repoPath, name string) {
	t.Helper()
	cmd := exec.Command("git", "tag", "-a", name, "-m", name, "--no-sign")
	cmd.Dir = repoPath
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git tag %s failed: %v\n%s", name, err, out)
	}
}

// run executes a command in dir and returns stdout. Fails the test on error.
func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// fetchLocal creates a GitHubFetcher and calls Fetch with a local file://
// clone URL. It overrides buildCloneURL behaviour by cloning locally.
func fetchLocal(t *testing.T, repoPath string, dep manifest.Dependency) (*FetchResult, error) {
	t.Helper()
	ctx := context.Background()

	// We cannot use the normal buildCloneURL (which prepends https://) for
	// local repos, so we drive the clone/extract logic directly.
	tmpDir := t.TempDir()

	if err := cloneRepo(ctx, repoPath, dep.Ref, tmpDir); err != nil {
		return nil, err
	}

	sha, err := resolveHEAD(ctx, tmpDir)
	if err != nil {
		return nil, err
	}

	docExts := map[string]bool{".md": true, ".mdx": true}
	codeExts := buildExtensionSet(dep.CodeExtensions)

	var files []hasher.FileContent

	docFiles, err := extractFiles(tmpDir, dep.Docs, docExts)
	if err != nil {
		return nil, err
	}
	files = append(files, docFiles...)

	codeFiles, err := extractFiles(tmpDir, dep.Code, codeExts)
	if err != nil {
		return nil, err
	}
	files = append(files, codeFiles...)

	return &FetchResult{CommitSHA: sha, Files: files}, nil
}

// ---------------------------------------------------------------------------
// Fetch tests
// ---------------------------------------------------------------------------

func TestFetch_DocsOnly(t *testing.T) {
	repo := createTestRepo(t)
	writeFile(t, repo, "docs/guide.md", "# Guide")
	writeFile(t, repo, "docs/ref.mdx", "# Ref")
	writeFile(t, repo, "src/main.go", "package main")
	sha := commitAll(t, repo, "init")
	tagRepo(t, repo, "v1.0.0")

	dep := manifest.Dependency{
		ID:   "test",
		Ref:  "v1.0.0",
		Docs: []string{"docs"},
	}

	res, err := fetchLocal(t, repo, dep)
	if err != nil {
		t.Fatal(err)
	}
	if res.CommitSHA != sha {
		t.Errorf("got SHA %s, want %s", res.CommitSHA, sha)
	}
	if len(res.Files) != 2 {
		t.Fatalf("got %d files, want 2", len(res.Files))
	}

	paths := filePaths(res.Files)
	assertContains(t, paths, "docs/guide.md")
	assertContains(t, paths, "docs/ref.mdx")
	assertNotContains(t, paths, "src/main.go")
}

func TestFetch_CodeOnly(t *testing.T) {
	repo := createTestRepo(t)
	writeFile(t, repo, "src/lib.go", "package lib")
	writeFile(t, repo, "src/data.json", `{"a":1}`)
	writeFile(t, repo, "docs/readme.md", "# Read me")
	commitAll(t, repo, "init")
	tagRepo(t, repo, "v1.0.0")

	dep := manifest.Dependency{
		ID:             "test",
		Ref:            "v1.0.0",
		Code:           []string{"src"},
		CodeExtensions: []string{".go"},
	}

	res, err := fetchLocal(t, repo, dep)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Files) != 1 {
		t.Fatalf("got %d files, want 1", len(res.Files))
	}
	if res.Files[0].Path != "src/lib.go" {
		t.Errorf("got path %s, want src/lib.go", res.Files[0].Path)
	}
}

func TestFetch_DocsAndCode(t *testing.T) {
	repo := createTestRepo(t)
	writeFile(t, repo, "docs/guide.md", "# Guide")
	writeFile(t, repo, "src/main.go", "package main")
	writeFile(t, repo, "src/util.py", "def util(): pass")
	commitAll(t, repo, "init")
	tagRepo(t, repo, "v1.0.0")

	dep := manifest.Dependency{
		ID:   "test",
		Ref:  "v1.0.0",
		Docs: []string{"docs"},
		Code: []string{"src"},
	}

	res, err := fetchLocal(t, repo, dep)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Files) != 3 {
		t.Fatalf("got %d files, want 3", len(res.Files))
	}

	paths := filePaths(res.Files)
	assertContains(t, paths, "docs/guide.md")
	assertContains(t, paths, "src/main.go")
	assertContains(t, paths, "src/util.py")
}

func TestFetch_DefaultCodeExtensions(t *testing.T) {
	repo := createTestRepo(t)
	// Files that should be included (default extensions).
	writeFile(t, repo, "src/main.go", "package main")
	writeFile(t, repo, "src/app.py", "pass")
	writeFile(t, repo, "src/index.ts", "export {}")
	writeFile(t, repo, "src/comp.tsx", "<div/>")
	writeFile(t, repo, "src/app.js", "module.exports = {}")
	writeFile(t, repo, "src/comp.jsx", "<div/>")
	writeFile(t, repo, "src/Main.java", "class Main {}")
	writeFile(t, repo, "src/lib.rs", "fn main() {}")
	// Files that should be excluded.
	writeFile(t, repo, "src/data.json", "{}")
	writeFile(t, repo, "src/style.css", "body {}")
	writeFile(t, repo, "src/config.yaml", "key: val")
	commitAll(t, repo, "init")
	tagRepo(t, repo, "v1.0.0")

	dep := manifest.Dependency{
		ID:   "test",
		Ref:  "v1.0.0",
		Code: []string{"src"},
		// No CodeExtensions — should use defaults.
	}

	res, err := fetchLocal(t, repo, dep)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Files) != 8 {
		t.Fatalf("got %d files, want 8: %v", len(res.Files), filePaths(res.Files))
	}

	paths := filePaths(res.Files)
	assertNotContains(t, paths, "src/data.json")
	assertNotContains(t, paths, "src/style.css")
	assertNotContains(t, paths, "src/config.yaml")
}

func TestFetch_TagRef(t *testing.T) {
	repo := createTestRepo(t)
	writeFile(t, repo, "docs/a.md", "v1")
	sha1 := commitAll(t, repo, "v1")
	tagRepo(t, repo, "v1.0.0")

	writeFile(t, repo, "docs/b.md", "v2")
	commitAll(t, repo, "v2")
	tagRepo(t, repo, "v2.0.0")

	dep := manifest.Dependency{
		ID:   "test",
		Ref:  "v1.0.0",
		Docs: []string{"docs"},
	}

	res, err := fetchLocal(t, repo, dep)
	if err != nil {
		t.Fatal(err)
	}
	if res.CommitSHA != sha1 {
		t.Errorf("got SHA %s, want %s (v1.0.0)", res.CommitSHA, sha1)
	}
	// At v1.0.0 only a.md exists.
	if len(res.Files) != 1 {
		t.Fatalf("got %d files, want 1", len(res.Files))
	}
	if res.Files[0].Path != "docs/a.md" {
		t.Errorf("got path %s, want docs/a.md", res.Files[0].Path)
	}
}

func TestFetch_CommitRef(t *testing.T) {
	repo := createTestRepo(t)
	writeFile(t, repo, "docs/a.md", "first")
	sha1 := commitAll(t, repo, "first")

	writeFile(t, repo, "docs/b.md", "second")
	commitAll(t, repo, "second")

	dep := manifest.Dependency{
		ID:   "test",
		Ref:  sha1,
		Docs: []string{"docs"},
	}

	res, err := fetchLocal(t, repo, dep)
	if err != nil {
		t.Fatal(err)
	}
	if res.CommitSHA != sha1 {
		t.Errorf("got SHA %s, want %s", res.CommitSHA, sha1)
	}
	if len(res.Files) != 1 {
		t.Fatalf("got %d files, want 1 (only a.md at first commit)", len(res.Files))
	}
}

func TestFetch_NonexistentPath(t *testing.T) {
	repo := createTestRepo(t)
	writeFile(t, repo, "docs/guide.md", "# Guide")
	commitAll(t, repo, "init")
	tagRepo(t, repo, "v1.0.0")

	dep := manifest.Dependency{
		ID:   "test",
		Ref:  "v1.0.0",
		Docs: []string{"docs", "nonexistent/path"},
		Code: []string{"also/missing"},
	}

	res, err := fetchLocal(t, repo, dep)
	if err != nil {
		t.Fatalf("expected no error for non-existent paths, got: %v", err)
	}
	if len(res.Files) != 1 {
		t.Fatalf("got %d files, want 1", len(res.Files))
	}
}

func TestFetch_InvalidRef(t *testing.T) {
	repo := createTestRepo(t)
	writeFile(t, repo, "README.md", "hello")
	commitAll(t, repo, "init")

	dep := manifest.Dependency{
		ID:   "test",
		Ref:  "nonexistent-tag",
		Docs: []string{"docs"},
	}

	_, err := fetchLocal(t, repo, dep)
	if err == nil {
		t.Fatal("expected error for invalid ref")
	}
}

// ---------------------------------------------------------------------------
// SelectToken tests
// ---------------------------------------------------------------------------

func TestSelectToken_GitHubCom(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "ghp_test123")
	tok := SelectToken("github.com/org/repo")
	if tok != "ghp_test123" {
		t.Errorf("got %q, want ghp_test123", tok)
	}
}

func TestSelectToken_GHE(t *testing.T) {
	t.Setenv("GHE_URL", "https://git.corp.example.com")
	t.Setenv("GHE_TOKEN", "ghe_secret")

	tok := SelectToken("git.corp.example.com/org/repo")
	if tok != "ghe_secret" {
		t.Errorf("got %q, want ghe_secret", tok)
	}
}

func TestSelectToken_GHE_NoScheme(t *testing.T) {
	t.Setenv("GHE_URL", "git.corp.example.com")
	t.Setenv("GHE_TOKEN", "ghe_secret")

	tok := SelectToken("git.corp.example.com/org/repo")
	if tok != "ghe_secret" {
		t.Errorf("got %q, want ghe_secret", tok)
	}
}

func TestSelectToken_NoAuth(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GHE_URL", "")
	t.Setenv("GHE_TOKEN", "")

	tok := SelectToken("github.com/org/repo")
	if tok != "" {
		t.Errorf("got %q, want empty", tok)
	}
}

// ---------------------------------------------------------------------------
// buildCloneURL tests
// ---------------------------------------------------------------------------

func TestBuildCloneURL(t *testing.T) {
	tests := []struct {
		name   string
		source string
		envs   map[string]string
		want   string
	}{
		{
			name:   "no token",
			source: "github.com/org/repo",
			envs:   map[string]string{"GITHUB_TOKEN": ""},
			want:   "https://github.com/org/repo.git",
		},
		{
			name:   "with github token",
			source: "github.com/org/repo",
			envs:   map[string]string{"GITHUB_TOKEN": "ghp_abc"},
			want:   "https://ghp_abc@github.com/org/repo.git",
		},
		{
			name:   "GHE with token",
			source: "git.corp.co/team/proj",
			envs: map[string]string{
				"GHE_URL":   "https://git.corp.co",
				"GHE_TOKEN": "ghe_xyz",
			},
			want: "https://ghe_xyz@git.corp.co/team/proj.git",
		},
		{
			name:   "GHE without token",
			source: "git.corp.co/team/proj",
			envs: map[string]string{
				"GHE_URL":   "https://git.corp.co",
				"GHE_TOKEN": "",
			},
			want: "https://git.corp.co/team/proj.git",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear relevant env vars first.
			t.Setenv("GITHUB_TOKEN", "")
			t.Setenv("GHE_URL", "")
			t.Setenv("GHE_TOKEN", "")
			for k, v := range tt.envs {
				t.Setenv(k, v)
			}
			got := buildCloneURL(tt.source)
			if got != tt.want {
				t.Errorf("buildCloneURL(%q) = %q, want %q", tt.source, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func filePaths(files []hasher.FileContent) []string {
	out := make([]string, len(files))
	for i, f := range files {
		out[i] = f.Path
	}
	return out
}

func assertContains(t *testing.T, paths []string, want string) {
	t.Helper()
	for _, p := range paths {
		if p == want {
			return
		}
	}
	t.Errorf("expected %q in %v", want, paths)
}

func assertNotContains(t *testing.T, paths []string, notWant string) {
	t.Helper()
	for _, p := range paths {
		if p == notWant {
			t.Errorf("did not expect %q in %v", notWant, paths)
			return
		}
	}
}
