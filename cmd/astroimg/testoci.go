package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/astrona-io/qcow2-base-image/internal/config"
	"github.com/astrona-io/qcow2-base-image/internal/orasclient"
)

var testOCICmd = &cobra.Command{
	Use:   "test-oci",
	Short: "Pull the pushed QCOW2 artifact(s) from the OCI registry and boot-test them",
	Long: `Pulls the artifact for --distro/--layer/--arch from the registry,
verifies it with qemu-img, then forks a throwaway overlay and boots it to
confirm it's actually bootable -- not just structurally valid. For a layer
built without --flatten (the default), this also pulls the layer's base
artifact and rebases the pulled layer onto it (the backing-file path
embedded at push time pointed at the build machine's filesystem, not this
one) -- exactly what a real downstream consumer has to do.`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		ctx := cmd.Context()

		r, _, err := resolveBuild(cmd)
		if err != nil {
			return err
		}

		if err := orasclient.CheckInstalled(); err != nil {
			return err
		}

		if err := os.RemoveAll(r.TestExtractDir); err != nil {
			return err
		}

		image := ociImage(r)
		fmt.Printf("pulling %s...\n", image)

		if err := orasclient.Pull(ctx, image, r.TestExtractDir); err != nil {
			return fmt.Errorf("did you run 'astroimg push' first? %w", err)
		}

		fmt.Println("pulled successfully to", r.TestExtractDir)

		if r.Layer != "" {
			if err := pullAndRebaseLayerBase(ctx, r); err != nil {
				return err
			}
		}

		fmt.Println("verifying pulled qcow2 image...")

		if err := runExternal(ctx, "qemu-img", "info", r.TestImageName); err != nil {
			return fmt.Errorf("pulled disk is corrupted or not in qcow2 format: %w", err)
		}

		fmt.Println("boot-testing pulled artifact...")

		fork, err := newTestFork(ctx, r.BuildDir, r.TestImageName, r.Arch, flagSSHPort, true, flagVerbose)
		if err != nil {
			return fmt.Errorf("pulled artifact failed to boot: %w", err)
		}
		defer fork.cleanup()

		if err := fork.verifySSH(ctx); err != nil {
			return fmt.Errorf("pulled artifact booted but SSH login failed: %w", err)
		}

		fmt.Println("verification successful: OCI artifact pulls, verifies, and boots correctly")

		return nil
	},
}

// pullAndRebaseLayerBase pulls a layer's base artifact alongside the layer
// (downstream consumers need both anyway) and, if the pulled layer is a
// backing-file overlay (not built with --flatten), rebases it onto the
// just-pulled base's local path so it can actually boot on this machine.
func pullAndRebaseLayerBase(ctx context.Context, r config.Resolved) error {
	baseR, err := resolveBaseOf(r)
	if err != nil {
		return fmt.Errorf("resolving base for layer %q: %w", r.Layer, err)
	}

	baseImage := ociImage(baseR)
	fmt.Printf("pulling base %s (needed to boot the layer)...\n", baseImage)

	if err := orasclient.Pull(ctx, baseImage, r.TestExtractDir); err != nil {
		return fmt.Errorf("pulling base artifact: %w", err)
	}

	pulledBase := filepath.Join(r.TestExtractDir, baseR.FinalImageName)

	backing, err := qemuImgBackingFile(ctx, r.TestImageName)
	if err != nil {
		return fmt.Errorf("inspecting pulled layer: %w", err)
	}

	if backing == "" {
		fmt.Println("pulled layer has no backing file (built with --flatten), skipping rebase")
		return nil
	}

	absBase, err := filepath.Abs(pulledBase)
	if err != nil {
		return fmt.Errorf("resolving absolute path for %s: %w", pulledBase, err)
	}

	fmt.Printf("rebasing pulled layer onto %s...\n", absBase)

	return runExternal(ctx, "qemu-img", "rebase", "-u", "-F", "qcow2", "-f", "qcow2", "-b", absBase, r.TestImageName)
}

type qemuImgInfo struct {
	BackingFilename string `json:"backing-filename"`
}

// qemuImgBackingFile reports the backing-file path recorded in path's qcow2
// header, or "" if it has none.
func qemuImgBackingFile(ctx context.Context, path string) (string, error) {
	cmd := exec.CommandContext(ctx, "qemu-img", "info", "--output=json", path) //nolint:gosec // path is internally resolved (a just-pulled OCI artifact under our own TestExtractDir), not raw user input

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
