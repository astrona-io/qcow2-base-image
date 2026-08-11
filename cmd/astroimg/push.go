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

		const qcowMediaType = "application/vnd.qemu.disk.qcow2"

		files := []orasclient.File{{Path: finalPath, MediaType: qcowMediaType}}

		if r.Layer != "" {
			backing, err := qemuImgBackingFile(cmd.Context(), finalPath)
			if err != nil {
				return fmt.Errorf("inspecting %s: %w", finalPath, err)
			}

			if backing != "" {
				if _, err := os.Stat(r.SourceDisk); errors.Is(err, os.ErrNotExist) {
					return fmt.Errorf("%s (this layer's backing base) not found, run 'astroimg build' for the base first", r.SourceDisk)
				}

				fmt.Printf("bundling base %s into the same manifest (this layer is a backing-file overlay, not --flatten)\n", r.SourceDisk)

				files = append(files, orasclient.File{Path: r.SourceDisk, MediaType: qcowMediaType})
			}
		}

		image := ociImage(r)
		fmt.Printf("pushing %s to %s...\n", finalPath, image)

		if err := orasclient.Push(cmd.Context(), image, files, ociSourceAnnotations(r.FinalImageName)); err != nil {
			return err
		}

		fmt.Println("pushed", image)

		return nil
	},
}
