package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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

// createOverlayDisk creates a new qcow2 file at instancePath backed by
// basePath: it starts as a tiny (few-hundred-KB) file, and QEMU reads
// unmodified blocks straight from the base at boot, only writing new or
// changed blocks into the overlay itself. Used for layer builds so the
// instance disk isn't a full copy of the (often multi-GB) base.
func createOverlayDisk(ctx context.Context, basePath, instancePath string) error {
	absBase, err := filepath.Abs(basePath)
	if err != nil {
		return fmt.Errorf("resolving absolute path for %s: %w", basePath, err)
	}

	return runExternal(ctx, "qemu-img", "create", "-f", "qcow2", "-F", "qcow2", "-b", absBase, instancePath)
}

// runExternal runs an external tool with an explicit argv (never a shell
// string), streaming its output live.
func runExternal(ctx context.Context, name string, args ...string) error {
	return runExternalIn(ctx, "", name, args...)
}

// runExternalIn is runExternal but run with cwd set to dir (or the current
// process's cwd if dir is ""). Needed when an argument is a relative path
// (e.g. a qcow2 backing_file) that must resolve against something other
// than wherever astroimg itself happens to be running from.
func runExternalIn(ctx context.Context, dir, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // argv-only, name/args are fixed or internally validated, never shell-interpolated
	cmd.Dir = dir
	cmd.Stdout = os.Stdout

	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}

	return nil
}

// linkOrCopy hard-links src to dst -- fast, no extra disk space for
// multi-GB qcow2 files -- falling back to a full copy if hard-linking
// isn't possible (e.g. src/dst on different filesystems).
func linkOrCopy(src, dst string) error {
	if err := os.Link(src, dst); err == nil {
		return nil
	}

	return copyFile(src, dst)
}

// gitConfigUserNameOrDefault resolves a default OCI registry namespace when
// --gh-user isn't passed, in order of preference:
//  1. This repo's own GitHub org/user, parsed from `git remote get-url
//     origin` -- the artifact naturally belongs with whoever owns the repo
//     that builds it, not whoever happens to be running the build.
//  2. The `gh` CLI's authenticated login, for repos without a GitHub
//     remote (or when running outside this repo's checkout).
//  3. A sanitized `git config user.name` as a last resort: it's a
//     free-text display name -- e.g. "Paris Nakita Kejser" -- which isn't
//     a valid OCI repository path component (spaces aren't allowed) and
//     breaks `oras push` with an "invalid reference" error if used as-is.
func gitConfigUserNameOrDefault() string {
	if owner := gitRemoteOwner(); owner != "" {
		return owner
	}

	if login := ghCLIUsername(); login != "" {
		return login
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, "git", "config", "user.name").Output()

	name := strings.TrimSpace(string(out))
	if err != nil || name == "" {
		return "your-username"
	}

	return sanitizeOCIPathComponent(name)
}

// gitRemoteOwner extracts the "owner" segment (org or user) from origin's
// github.com URL. Returns "" if origin isn't a github.com remote or
// doesn't exist.
func gitRemoteOwner() string {
	owner, _ := gitRemoteOwnerRepo()
	return owner
}

// gitRemoteHTTPSURL returns origin's github.com URL normalized to
// "https://github.com/owner/repo" form, suitable for the
// org.opencontainers.image.source annotation. Returns "" if origin isn't a
// github.com remote or doesn't exist.
func gitRemoteHTTPSURL() string {
	owner, repo := gitRemoteOwnerRepo()
	if owner == "" || repo == "" {
		return ""
	}

	return fmt.Sprintf("https://github.com/%s/%s", owner, repo)
}

// gitRemoteOwnerRepo parses origin's github.com URL, in either SSH
// (git@github.com:owner/repo.git) or HTTPS
// (https://github.com/owner/repo.git) form, into its owner and repo
// segments. Returns ("", "") if origin isn't a github.com remote or
// doesn't exist.
func gitRemoteOwnerRepo() (owner, repo string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, "git", "remote", "get-url", "origin").Output()
	if err != nil {
		return "", ""
	}

	url := strings.TrimSuffix(strings.TrimSpace(string(out)), ".git")

	var path string

	switch {
	case strings.Contains(url, "github.com:"):
		path = url[strings.Index(url, "github.com:")+len("github.com:"):]
	case strings.Contains(url, "github.com/"):
		path = url[strings.Index(url, "github.com/")+len("github.com/"):]
	default:
		return "", ""
	}

	owner, repo, _ = strings.Cut(path, "/")

	return owner, repo
}

// ghCLIUsername returns the authenticated GitHub CLI user's login, or ""
// if `gh` isn't installed or isn't authenticated.
func ghCLIUsername() string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, "gh", "api", "user", "--jq", ".login").Output()
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(out))
}

type qemuImgInfo struct {
	BackingFilename string `json:"backing-filename"`
}

// qemuImgBackingFile reports the backing-file path recorded in path's qcow2
// header, or "" if it has none.
func qemuImgBackingFile(ctx context.Context, path string) (string, error) {
	cmd := exec.CommandContext(ctx, "qemu-img", "info", "--output=json", path) //nolint:gosec // path is internally resolved, not raw user input

	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return "", fmt.Errorf("qemu-img info %s: %w (%s)", path, err, exitErr.Stderr)
		}

		return "", fmt.Errorf("qemu-img info %s: %w", path, err)
	}

	var info qemuImgInfo
	if err := json.Unmarshal(out, &info); err != nil {
		return "", fmt.Errorf("parsing qemu-img info output: %w", err)
	}

	return info.BackingFilename, nil
}

// sanitizeOCIPathComponent lowercases s and collapses every run of
// characters outside [a-z0-9] into a single "-", so the result is always a
// valid OCI repository path component (distribution spec:
// [a-z0-9]+((\.|_|__|-+)[a-z0-9]+)*).
func sanitizeOCIPathComponent(s string) string {
	var b strings.Builder

	prevDash := true // avoid a leading '-'

	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)

			prevDash = false
		case !prevDash:
			b.WriteByte('-')

			prevDash = true
		}
	}

	return strings.TrimSuffix(b.String(), "-")
}
