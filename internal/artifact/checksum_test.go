package artifact

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestComputeChecksum_Deterministic(t *testing.T) {
	data := "hello, world!"

	c1, err := ComputeChecksum(strings.NewReader(data))
	if err != nil {
		t.Fatalf("ComputeChecksum: %v", err)
	}

	c2, err := ComputeChecksum(strings.NewReader(data))
	if err != nil {
		t.Fatalf("ComputeChecksum: %v", err)
	}

	if c1 != c2 {
		t.Errorf("checksums differ: %s vs %s", c1, c2)
	}

	if !strings.HasPrefix(c1, "sha256:") {
		t.Errorf("checksum should start with sha256:, got %q", c1)
	}
}

func TestVerifyChecksum_Pass(t *testing.T) {
	data := []byte("test data")
	r := bytes.NewReader(data)

	expected, err := ComputeChecksum(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("ComputeChecksum: %v", err)
	}

	if err := VerifyChecksum(r, expected); err != nil {
		t.Errorf("VerifyChecksum should pass: %v", err)
	}
}

func TestVerifyChecksum_Fail(t *testing.T) {
	data := []byte("test data")
	r := bytes.NewReader(data)

	if err := VerifyChecksum(r, "sha256:0000000000000000000000000000000000000000000000000000000000000000"); err == nil {
		t.Error("VerifyChecksum should fail on mismatch")
	}
}

func TestWriteReadChecksumFile_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.sha256")
	checksum := "sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	filename := "mycelium-voyage-code-2.jsonl.gz"

	if err := WriteChecksumFile(path, checksum, filename); err != nil {
		t.Fatalf("WriteChecksumFile: %v", err)
	}

	// Verify file content format: "<hex>  <filename>\n"
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	wantContent := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890  mycelium-voyage-code-2.jsonl.gz\n"
	if string(data) != wantContent {
		t.Errorf("file content = %q, want %q", string(data), wantContent)
	}

	got, err := ReadChecksumFile(path)
	if err != nil {
		t.Fatalf("ReadChecksumFile: %v", err)
	}

	if got != checksum {
		t.Errorf("ReadChecksumFile = %q, want %q", got, checksum)
	}
}

func TestReadChecksumFile_Empty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.sha256")
	if err := os.WriteFile(path, []byte("  \n"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := ReadChecksumFile(path)
	if err == nil {
		t.Error("expected error for empty checksum file")
	}
}
