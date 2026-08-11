package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/astrona-io/qcow2-base-image/internal/orasclient"
)

var testOCICmd = &cobra.Command{
	Use:   "test-oci",
	Short: "Pull the pushed QCOW2 artifact(s) from the OCI registry and boot-test them",
	Long: `Pulls the artifact for --distro/--layer/--arch from the registry,
verifies it with qemu-img, then forks a throwaway overlay and boots it to
confirm it's actually bootable -- not just structurally valid. For a layer
built without --flatten (the default), 'astroimg push' bundles the layer's
base qcow2 into the same manifest, so this single pull already lands both
files together and the layer's relative backing_file resolves against them
with no rebase needed.`,
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

		fmt.Println("verifying pulled qcow2 image...")

		if err := runExternal(ctx, "qemu-img", "info", r.TestImageName); err != nil {
			return fmt.Errorf("pulled disk is corrupted or not in qcow2 format: %w", err)
		}

		fmt.Println("boot-testing pulled artifact...")

		fork, err := newTestFork(ctx, r.RuntimeDir, r.TestImageName, r.Arch, flagSSHPort, true, flagVerbose, r.SSHUser)
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
