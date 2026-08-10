package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// copyFile copies src to dst, creating dst's parent directory if needed.
// Used for both small template-vars copies and multi-GB disk-image clones,
// so it streams via io.Copy rather than reading whole files into memory.
func copyFile(src, dst string) error {
	// src/dst are always resolved from an already-validated distro/layer
	// name and build-dir (see config.Resolve), never raw user input.
	in, err := os.Open(src) //nolint:gosec // path is internally resolved, not raw user input
	if err != nil {
		return fmt.Errorf("opening %s: %w", src, err)
	}
	defer func() { _ = in.Close() }()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600) //nolint:gosec // path is internally resolved, not raw user input
	if err != nil {
		return fmt.Errorf("creating %s: %w", dst, err)
	}

	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()

		return fmt.Errorf("copying %s to %s: %w", src, dst, err)
	}

	return out.Close()
}

// runExternal runs an external tool with an explicit argv (never a shell
// string), streaming its output live.
func runExternal(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // argv-only, name/args are fixed or internally validated, never shell-interpolated
	cmd.Stdout = os.Stdout

	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}

	return nil
}

// gitConfigUserNameOrDefault mirrors the old Makefile's
// `git config user.name 2>/dev/null || echo "your-username"` default for
// the OCI image namespace.
func gitConfigUserNameOrDefault() string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, "git", "config", "user.name").Output()

	name := strings.TrimSpace(string(out))
	if err != nil || name == "" {
		return "your-username"
	}

	return name
}
