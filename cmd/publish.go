package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/johnlanda/mycelium/internal/artifact"
	"github.com/johnlanda/mycelium/internal/chunker"
	"github.com/johnlanda/mycelium/internal/embedder"
	"github.com/johnlanda/mycelium/internal/fetchers"
	"github.com/johnlanda/mycelium/internal/hasher"
	"github.com/johnlanda/mycelium/internal/lockfile"
	"github.com/johnlanda/mycelium/internal/manifest"
	"github.com/johnlanda/mycelium/internal/pipeline"
	"github.com/johnlanda/mycelium/internal/store"
	"github.com/spf13/cobra"
)

var publishCmd = &cobra.Command{
	Use:   "publish",
	Short: "Publish embedding artifacts to a registry",
	RunE:  runPublish,
}

func init() {
	publishCmd.Flags().String("tag", "", "Release tag (required)")
	publishCmd.Flags().String("output", "", "Write artifact files to this local directory instead of uploading")
	rootCmd.AddCommand(publishCmd)
}

func runPublish(cmd *cobra.Command, args []string) error {
	tag, _ := cmd.Flags().GetString("tag")
	outputDir, _ := cmd.Flags().GetString("output")

	if tag == "" {
		return fmt.Errorf("--tag is required")
	}

	m, err := manifest.ParseFile("mycelium.toml")
	if err != nil {
		return fmt.Errorf("read manifest: %w", err)
	}

	// Validate publish config unless using --output for local-only.
	if outputDir == "" && m.Config.Publish == "" {
		return fmt.Errorf("config.publish is not set in mycelium.toml (use --output for local output)")
	}

	// If publishing to GitHub releases, verify gh CLI is available.
	if outputDir == "" && m.Config.Publish == "github-releases" {
		if _, err := exec.LookPath("gh"); err != nil {
			return fmt.Errorf("gh CLI not found: install it from https://cli.github.com to publish to GitHub releases")
		}
	}

	emb, err := embedder.NewEmbedder(m.Config.EmbeddingModel, embedder.Config{})
	if err != nil {
		return fmt.Errorf("create embedder: %w", err)
	}

	mdChunker := chunker.NewMarkdownChunker(chunker.DefaultOptions())
	codeChunker := chunker.NewCodeChunker(chunker.DefaultOptions())
	fetcher := &fetchers.GitHubFetcher{}

	ctx := cmd.Context()
	var allChunks []store.StoredChunk

	// Process each dependency: fetch -> chunk -> embed.
	for _, dep := range m.Dependencies {
		cmd.Printf("Processing %s...\n", dep.ID)

		result, err := fetcher.Fetch(ctx, dep)
		if err != nil {
			return fmt.Errorf("fetch %s: %w", dep.ID, err)
		}

		contentHash := hasher.ContentHash(result.Files)
		sk := hasher.StoreKey(contentHash, emb.ModelID(), "", hasher.ChunkingConfig{
			ChunkerType: "mixed",
			TargetSize:  chunker.DefaultOptions().TargetSize,
			Overlap:     chunker.DefaultOptions().Overlap,
		})

		stored, err := pipeline.ProcessFiles(ctx, result.Files, dep.ID, dep.Ref, emb, mdChunker, codeChunker, sk)
		if err != nil {
			return fmt.Errorf("process %s: %w", dep.ID, err)
		}
		allChunks = append(allChunks, stored...)
	}

	// Process local.index paths (not local.private).
	if len(m.Local.Index) > 0 {
		cmd.Println("Processing local index paths...")
		var localFiles []hasher.FileContent
		for _, p := range m.Local.Index {
			err := filepath.WalkDir(p, func(path string, d os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if d.IsDir() {
					return nil
				}
				content, readErr := os.ReadFile(path)
				if readErr != nil {
					return fmt.Errorf("read %s: %w", path, readErr)
				}
				localFiles = append(localFiles, hasher.FileContent{Path: path, Content: content})
				return nil
			})
			if err != nil {
				return fmt.Errorf("walk local path %s: %w", p, err)
			}
		}

		if len(localFiles) > 0 {
			contentHash := hasher.ContentHash(localFiles)
			sk := hasher.StoreKey(contentHash, emb.ModelID(), "", hasher.ChunkingConfig{
				ChunkerType: "mixed",
				TargetSize:  chunker.DefaultOptions().TargetSize,
				Overlap:     chunker.DefaultOptions().Overlap,
			})
			stored, err := pipeline.ProcessFiles(ctx, localFiles, "local", "", emb, mdChunker, codeChunker, sk)
			if err != nil {
				return fmt.Errorf("process local: %w", err)
			}
			allChunks = append(allChunks, stored...)
		}
	}

	if len(allChunks) == 0 {
		return fmt.Errorf("no chunks produced; nothing to publish")
	}

	// Build the model slug for the filename (replace / with - for ollama models).
	modelSlug := strings.ReplaceAll(m.Config.EmbeddingModel, "/", "-")
	artifactName := fmt.Sprintf("mycelium-%s.jsonl.gz", modelSlug)
	checksumName := artifactName + ".sha256"

	// Determine output directory.
	dir := outputDir
	if dir == "" {
		var mkErr error
		dir, mkErr = os.MkdirTemp("", "mycelium-publish-*")
		if mkErr != nil {
			return fmt.Errorf("create temp dir: %w", mkErr)
		}
		defer os.RemoveAll(dir)
	} else {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create output dir: %w", err)
		}
	}

	artifactPath := filepath.Join(dir, artifactName)
	checksumPath := filepath.Join(dir, checksumName)

	meta := artifact.ArtifactMeta{
		EmbeddingModel: m.Config.EmbeddingModel,
	}

	// Write the gzipped JSONL artifact.
	f, err := os.Create(artifactPath)
	if err != nil {
		return fmt.Errorf("create artifact file: %w", err)
	}
	if err := artifact.Write(f, allChunks, meta); err != nil {
		f.Close()
		return fmt.Errorf("write artifact: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close artifact file: %w", err)
	}

	// Compute and write the checksum.
	af, err := os.Open(artifactPath)
	if err != nil {
		return fmt.Errorf("open artifact for checksum: %w", err)
	}
	checksum, err := artifact.ComputeChecksum(af)
	af.Close()
	if err != nil {
		return fmt.Errorf("compute checksum: %w", err)
	}

	if err := artifact.WriteChecksumFile(checksumPath, checksum, artifactName); err != nil {
		return fmt.Errorf("write checksum file: %w", err)
	}

	if outputDir != "" {
		cmd.Printf("Artifact written to %s\n", artifactPath)
		cmd.Printf("Checksum written to %s\n", checksumPath)
		cmd.Printf("Total chunks: %d\n", len(allChunks))
		return nil
	}

	// Upload to GitHub releases.
	if m.Config.Publish == "github-releases" {
		if err := uploadToGitHubRelease(cmd, tag, artifactPath, checksumPath); err != nil {
			return err
		}
	}

	// Update lockfile with artifact info.
	var lf *lockfile.Lockfile
	if _, statErr := os.Stat("mycelium.lock"); os.IsNotExist(statErr) {
		lf = lockfile.New()
	} else {
		lf, err = lockfile.ReadFile("mycelium.lock")
		if err != nil {
			return fmt.Errorf("read lockfile: %w", err)
		}
	}

	for _, dep := range m.Dependencies {
		sl := lf.Sources[dep.ID]
		sl.ArtifactURL = artifact.ResolveArtifactURL(dep.Source, tag, modelSlug)
		sl.ArtifactHash = checksum
		sl.IngestionType = "artifact"
		lf.SetSource(dep.ID, sl)
	}
	if err := lf.WriteFile("mycelium.lock"); err != nil {
		return fmt.Errorf("write lockfile: %w", err)
	}

	cmd.Printf("Published %s (%d chunks)\n", artifactName, len(allChunks))
	return nil
}

func uploadToGitHubRelease(cmd *cobra.Command, tag, artifactPath, checksumPath string) error {
	ctx := cmd.Context()

	// Check if the release exists; create it if not.
	checkCmd := exec.CommandContext(ctx, "gh", "release", "view", tag)
	if err := checkCmd.Run(); err != nil {
		cmd.Printf("Creating release %s...\n", tag)
		createCmd := exec.CommandContext(ctx, "gh", "release", "create", tag, "--title", tag, "--notes", "Mycelium embedding artifact release")
		createCmd.Stdout = cmd.OutOrStdout()
		createCmd.Stderr = cmd.ErrOrStderr()
		if err := createCmd.Run(); err != nil {
			return fmt.Errorf("create release %s: %w", tag, err)
		}
	}

	// Upload both files.
	cmd.Printf("Uploading artifacts to release %s...\n", tag)
	uploadCmd := exec.CommandContext(ctx, "gh", "release", "upload", tag, artifactPath, checksumPath, "--clobber")
	uploadCmd.Stdout = cmd.OutOrStdout()
	uploadCmd.Stderr = cmd.ErrOrStderr()
	if err := uploadCmd.Run(); err != nil {
		return fmt.Errorf("upload artifacts: %w", err)
	}

	return nil
}
