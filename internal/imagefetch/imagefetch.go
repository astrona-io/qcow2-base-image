// Package imagefetch downloads upstream cloud images and verifies them
// against a published SHA256SUMS file before the pipeline trusts them. The
// old shell pipeline just `curl`'d the image and used it as-is; this closes
// that gap.
package imagefetch

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Download streams url to destPath, writing to a temporary "<destPath>.part"
// file first and renaming into place only on success, so a failed or
// interrupted download never leaves a corrupt file at destPath.
func Download(ctx context.Context, client *http.Client, url, destPath string) error {
	if client == nil {
		client = http.DefaultClient
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("building request for %s: %w", url, err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("downloading %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("downloading %s: unexpected status %s", url, resp.Status)
	}

	if err := os.MkdirAll(filepath.Dir(destPath), 0o750); err != nil {
		return fmt.Errorf("creating destination dir: %w", err)
	}

	tmpPath := destPath + ".part"

	f, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600) //nolint:gosec // destPath is internally resolved, not raw user input
	if err != nil {
		return fmt.Errorf("creating %s: %w", tmpPath, err)
	}

	if _, err := io.Copy(f, resp.Body); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)

		return fmt.Errorf("writing %s: %w", tmpPath, err)
	}

	if err := f.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("closing %s: %w", tmpPath, err)
	}

	if err := os.Rename(tmpPath, destPath); err != nil {
		return fmt.Errorf("finalizing %s: %w", destPath, err)
	}

	return nil
}

// FetchText retrieves a small text document (e.g. a SHA256SUMS file) into
// memory.
func FetchText(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	if client == nil {
		client = http.DefaultClient
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("building request for %s: %w", url, err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching %s: unexpected status %s", url, resp.Status)
	}

	return io.ReadAll(resp.Body)
}

// ParseSHA256Sums parses a `sha256sum`-style or BSD-style (Fedora) checksum
// document and returns the lowercase hex digest recorded for filename.
// Handles both "<hash>  <name>", "<hash> *<name>", and "SHA256 (filename) = hash"
// forms, and tolerates a "./" prefix on the name.
func ParseSHA256Sums(data []byte, filename string) (string, error) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		// Handle BSD/Fedora format: SHA256 (filename) = hash
		if strings.HasPrefix(line, "SHA256 (") && strings.Contains(line, ") = ") {
			idxOpen := strings.Index(line, "(")
			idxClose := strings.LastIndex(line, ")")
			idxEq := strings.LastIndex(line, "=")

			if idxOpen != -1 && idxClose > idxOpen && idxEq > idxClose {
				name := strings.TrimSpace(line[idxOpen+1 : idxClose])
				hash := strings.TrimSpace(line[idxEq+1:])

				name = strings.TrimPrefix(name, "./")

				if name == filename {
					return strings.ToLower(hash), nil
				}
			}

			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		hash := fields[0]
		name := strings.TrimPrefix(fields[len(fields)-1], "*")

		name = strings.TrimPrefix(name, "./")
		if name == filename {
			return strings.ToLower(hash), nil
		}
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("parsing checksum document: %w", err)
	}

	return "", fmt.Errorf("no checksum entry found for %q", filename)
}

// SHA256File computes the lowercase hex SHA-256 digest of a file on disk.
func SHA256File(path string) (string, error) {
	f, err := os.Open(path) //nolint:gosec // path is internally resolved (the just-downloaded image), not raw user input
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hashing %s: %w", path, err)
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// VerifyFile checks that filePath's SHA-256 digest matches the entry for
// filename inside a SHA256SUMS document.
func VerifyFile(sumsData []byte, filename, filePath string) error {
	want, err := ParseSHA256Sums(sumsData, filename)
	if err != nil {
		return err
	}

	got, err := SHA256File(filePath)
	if err != nil {
		return err
	}

	if !strings.EqualFold(want, got) {
		return fmt.Errorf("checksum mismatch for %s: expected %s, got %s", filename, want, got)
	}

	return nil
}
