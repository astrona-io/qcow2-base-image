package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/astrona-io/qcow2-base-image/internal/orasclient"
)

var pushCmd = &cobra.Command{
	Use:   "push",
	Short: "Push the final QCOW2 artifact to an OCI registry with ORAS",
	RunE: func(cmd *cobra.Command, _ []string) error {
		r, _, err := resolveBuild(cmd)
		if err != nil {
			return err
		}

		if err := orasclient.CheckInstalled(); err != nil {
			return err
		}

		finalPath := filepath.Join(r.BuildDir, r.FinalImageName)
		if _, err := os.Stat(finalPath); errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%s not found, run 'astroimg build' first", finalPath)
		}

		image := ociImage(r)
		fmt.Printf("pushing %s to %s...\n", finalPath, image)

		if err := orasclient.Push(cmd.Context(), image, finalPath, "application/vnd.qemu.disk.qcow2", ociSourceAnnotations(r.FinalImageName)); err != nil {
			return err
		}

		fmt.Println("pushed", image)

		return nil
	},
}
