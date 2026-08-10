package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/astrona-io/qcow2-base-image/internal/orasclient"
)

var testOCICmd = &cobra.Command{
	Use:   "test-oci",
	Short: "Pull the pushed QCOW2 artifact from the OCI registry with ORAS and verify it",
	RunE: func(cmd *cobra.Command, _ []string) error {
		r, _, err := resolveBuild(cmd)
		if err != nil {
			return err
		}

		if err := orasclient.CheckInstalled(); err != nil {
			return err
		}

		image := ociImage(r)
		if err := os.RemoveAll(r.TestExtractDir); err != nil {
			return err
		}

		fmt.Printf("pulling %s...\n", image)

		if err := orasclient.Pull(cmd.Context(), image, r.TestExtractDir); err != nil {
			return fmt.Errorf("did you run 'astroimg push' first? %w", err)
		}

		fmt.Println("pulled successfully to", r.TestExtractDir)

		fmt.Println("verifying extracted qcow2 image...")

		if err := runExternal(cmd.Context(), "qemu-img", "info", r.TestImageName); err != nil {
			return fmt.Errorf("extracted disk is corrupted or not in qcow2 format: %w", err)
		}

		fmt.Println("verification successful, OCI artifact is fully functional")

		return nil
	},
}
