// Package orasclient wraps the `oras` CLI for pushing/pulling the qcow2
// artifact as a raw OCI artifact. Every invocation is argv-only
// (exec.CommandContext) -- no shell string building. Swapping this for the
// oras-go SDK (removing the external `oras` binary dependency entirely) is
// tracked as a deliberate follow-up, not done here.
package orasclient

import (
	"context"
	"fmt"
	"os"
	"os/exec"
)

// CheckInstalled returns an actionable error if the oras CLI isn't on PATH.
func CheckInstalled() error {
	if _, err := exec.LookPath("oras"); err != nil {
		return fmt.Errorf("'oras' CLI not found on PATH: install it (e.g. 'brew install oras') first: %w", err)
	}

	return nil
}

// Push uploads filePath to image as a raw OCI artifact with the given media
// type, streaming oras's own progress output to stdout/stderr.
func Push(ctx context.Context, image, filePath, mediaType string) error {
	cmd := exec.CommandContext(ctx, "oras", "push", image, filePath+":"+mediaType) //nolint:gosec // argv-only, image/filePath/mediaType are internally constructed, not raw user input
	cmd.Stdout = os.Stdout

	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("oras push %s: %w", image, err)
	}

	return nil
}

// Pull downloads image's artifact contents into outDir.
func Pull(ctx context.Context, image, outDir string) error {
	if err := os.MkdirAll(outDir, 0o750); err != nil {
		return fmt.Errorf("creating %s: %w", outDir, err)
	}

	cmd := exec.CommandContext(ctx, "oras", "pull", image, "-o", outDir) //nolint:gosec // argv-only, image/outDir are internally constructed, not raw user input
	cmd.Stdout = os.Stdout

	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("oras pull %s: %w", image, err)
	}

	return nil
}
