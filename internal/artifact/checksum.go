package artifact

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"strings"
)

// ComputeChecksum reads all of r and returns "sha256:<hex>".
func ComputeChecksum(r io.Reader) (string, error) {
	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return "", fmt.Errorf("compute checksum: %w", err)
	}
	return fmt.Sprintf("sha256:%x", h.Sum(nil)), nil
}

// VerifyChecksum computes the SHA-256 of r and returns an error if it does
// not match expected (format "sha256:<hex>").
func VerifyChecksum(r io.ReadSeeker, expected string) error {
	checksum, err := ComputeChecksum(r)
	if err != nil {
		return err
	}
	if checksum != expected {
		return fmt.Errorf("checksum mismatch: got %s, want %s", checksum, expected)
	}
	// Seek back to start so the reader can be used again.
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("seek after verify: %w", err)
	}
	return nil
}

// WriteChecksumFile writes a BSD-style checksum file: "<hex>  <filename>\n".
func WriteChecksumFile(path, checksum, filename string) error {
	hex := strings.TrimPrefix(checksum, "sha256:")
	content := fmt.Sprintf("%s  %s\n", hex, filename)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return fmt.Errorf("write checksum file: %w", err)
	}
	return nil
}

// ReadChecksumFile reads a BSD-style checksum file and returns "sha256:<hex>".
func ReadChecksumFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read checksum file: %w", err)
	}
	line := strings.TrimSpace(string(data))
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return "", fmt.Errorf("empty checksum file")
	}
	return "sha256:" + fields[0], nil
}
