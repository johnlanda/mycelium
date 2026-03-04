package fetchers

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/johnlanda/mycelium/internal/hasher"
	"github.com/johnlanda/mycelium/internal/manifest"
)

var shaPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

// defaultCodeExtensions returns the file extensions indexed when
// a dependency does not specify code_extensions.
func defaultCodeExtensions() map[string]bool {
	return map[string]bool{
		".go":   true,
		".py":   true,
		".ts":   true,
		".tsx":  true,
		".js":   true,
		".jsx":  true,
		".java": true,
		".rs":   true,
	}
}

// GitHubFetcher clones a git repository and extracts files for indexing.
type GitHubFetcher struct{}

// Fetch clones the repository described by dep at the pinned ref,
// walks the configured docs and code paths, and returns matching files.
func (f *GitHubFetcher) Fetch(ctx context.Context, dep manifest.Dependency) (*FetchResult, error) {
	tmpDir, err := os.MkdirTemp("", "mycelium-fetch-*")
	if err != nil {
		return nil, fmt.Errorf("creating temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	cloneURL := buildCloneURL(dep.Source)

	if err := cloneRepo(ctx, cloneURL, dep.Ref, tmpDir); err != nil {
		return nil, fmt.Errorf("git clone %s ref %s: %w", dep.Source, dep.Ref, err)
	}

	sha, err := resolveHEAD(ctx, tmpDir)
	if err != nil {
		return nil, fmt.Errorf("resolving HEAD: %w", err)
	}

	docExts := map[string]bool{".md": true, ".mdx": true}
	codeExts := buildExtensionSet(dep.CodeExtensions)

	var files []hasher.FileContent

	docFiles, err := extractFiles(tmpDir, dep.Docs, docExts)
	if err != nil {
		return nil, fmt.Errorf("extracting docs: %w", err)
	}
	files = append(files, docFiles...)

	codeFiles, err := extractFiles(tmpDir, dep.Code, codeExts)
	if err != nil {
		return nil, fmt.Errorf("extracting code: %w", err)
	}
	files = append(files, codeFiles...)

	return &FetchResult{
		CommitSHA: sha,
		Files:     files,
	}, nil
}

// cloneRepo performs a git clone into dst. For tag/branch refs it uses
// a shallow clone; for commit SHAs it does a full clone + checkout.
func cloneRepo(ctx context.Context, cloneURL, ref, dst string) error {
	if shaPattern.MatchString(ref) {
		// Full clone then checkout for commit SHAs.
		cmd := exec.CommandContext(ctx, "git", "clone", cloneURL, dst)
		cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("%w: %s", err, out)
		}
		cmd = exec.CommandContext(ctx, "git", "-C", dst, "checkout", ref)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("checkout %s: %w: %s", ref, err, out)
		}
		return nil
	}

	// Shallow clone for tags/branches.
	cmd := exec.CommandContext(ctx, "git", "clone", "--depth", "1", "--branch", ref, cloneURL, dst)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%w: %s", err, out)
	}
	return nil
}

// resolveHEAD returns the full commit SHA of HEAD in the repository at dir.
func resolveHEAD(ctx context.Context, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// selectToken returns an auth token for the given source based on
// environment variables.
func selectToken(source string) string {
	if strings.HasPrefix(source, "github.com/") {
		return os.Getenv("GITHUB_TOKEN")
	}

	gheURL := os.Getenv("GHE_URL")
	if gheURL != "" {
		// Normalise: strip scheme so we can compare against the source host.
		host := gheURL
		if u, err := url.Parse(gheURL); err == nil && u.Host != "" {
			host = u.Host
		}
		if strings.HasPrefix(source, host+"/") {
			return os.Getenv("GHE_TOKEN")
		}
	}

	return ""
}

// buildCloneURL constructs the HTTPS clone URL, embedding an auth token
// when available.
func buildCloneURL(source string) string {
	token := selectToken(source)
	if token != "" {
		return "https://" + token + "@" + source + ".git"
	}
	return "https://" + source + ".git"
}

// buildExtensionSet creates a set of extensions from the dependency config.
// If none are specified it returns the default set.
func buildExtensionSet(exts []string) map[string]bool {
	if len(exts) == 0 {
		return defaultCodeExtensions()
	}
	m := make(map[string]bool, len(exts))
	for _, e := range exts {
		if !strings.HasPrefix(e, ".") {
			e = "." + e
		}
		m[e] = true
	}
	return m
}

// extractFiles walks each path within repoDir, collecting files whose
// extension is in the allowed set. Paths that do not exist are silently
// skipped. Returned FileContent paths are relative to repoDir.
func extractFiles(repoDir string, paths []string, exts map[string]bool) ([]hasher.FileContent, error) {
	var result []hasher.FileContent

	for _, p := range paths {
		abs := filepath.Join(repoDir, p)

		info, err := os.Stat(abs)
		if os.IsNotExist(err) {
			continue // skip non-existent paths silently
		}
		if err != nil {
			return nil, fmt.Errorf("stat %s: %w", p, err)
		}

		if !info.IsDir() {
			if exts[filepath.Ext(abs)] {
				content, err := os.ReadFile(abs)
				if err != nil {
					return nil, fmt.Errorf("reading %s: %w", p, err)
				}
				result = append(result, hasher.FileContent{
					Path:    p,
					Content: content,
				})
			}
			continue
		}

		err = filepath.WalkDir(abs, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if !exts[filepath.Ext(path)] {
				return nil
			}
			rel, err := filepath.Rel(repoDir, path)
			if err != nil {
				return fmt.Errorf("computing relative path: %w", err)
			}
			content, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("reading %s: %w", rel, err)
			}
			result = append(result, hasher.FileContent{
				Path:    rel,
				Content: content,
			})
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("walking %s: %w", p, err)
		}
	}

	return result, nil
}
