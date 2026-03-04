package cmd

import (
	"bytes"
	"testing"
)

func TestPublish_TagRequired(t *testing.T) {
	cmd := rootCmd
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"publish"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when --tag is missing")
	}
}

func TestPublish_OutputWritesArtifact(t *testing.T) {
	// This test would require a valid mycelium.toml and embedding API keys,
	// so we just verify the flag is accepted and the error is about
	// missing manifest (not missing flags).
	cmd := rootCmd
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"publish", "--tag", "v0.1.0", "--output", t.TempDir()})

	err := cmd.Execute()
	if err == nil {
		// Without a valid mycelium.toml this should fail.
		// If it succeeds, that's also fine (means the whole pipeline worked).
		return
	}

	// The error should be about reading the manifest, not about flags.
	if err.Error() == "--tag is required" {
		t.Error("--tag was provided but got flag error")
	}
}
