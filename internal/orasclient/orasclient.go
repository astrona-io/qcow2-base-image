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

// File is one blob to attach to a pushed OCI artifact manifest.
type File struct {
	Path      string
	MediaType string
}

// Push uploads files to image as a raw OCI artifact with the given manifest
// annotations, streaming oras's own progress output to stdout/stderr.
// Passing more than one File attaches all of them as blobs on the same
// manifest (e.g. a layer qcow2 plus the base qcow2 it's backed by, so a
// single pull yields everything needed to boot it) -- oras titles each blob
// with its own file's basename automatically. Setting the standard
// org.opencontainers.image.source annotation to this repo's GitHub URL is
// what makes GHCR auto-link the resulting package to the repo -- and, for a
// package that doesn't already exist, makes it inherit that repo's
// visibility (public repo -> public package) instead of defaulting to
// private.
func Push(ctx context.Context, image string, files []File, annotations map[string]string) error {
	args := []string{"push", image}
	for k, v := range annotations {
		args = append(args, "--annotation", k+"="+v)
	}

	for _, f := range files {
		args = append(args, f.Path+":"+f.MediaType)
	}

	cmd := exec.CommandContext(ctx, "oras", args...) //nolint:gosec // argv-only, all values are internally constructed, not raw user input
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
