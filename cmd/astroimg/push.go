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

		var backing string
		if r.Layer != "" {
			backing, err = qemuImgBackingFile(cmd.Context(), finalPath)
			if err != nil {
				return fmt.Errorf("inspecting %s: %w", finalPath, err)
			}
		}

		// Every pushed manifest's own artifact is staged and pushed under
		// the fixed name "image.qcow2" (and, when bundling a base,
		// "base.qcow2") -- never the distro/layer/arch-specific local
		// filename -- so a consumer can always `oras pull` any tag and find
		// the same two filenames, without inspecting the manifest first.
		// Staging happens in a temp dir under BuildDir (same filesystem, so
		// hard-linking works) and is never done in place: the build/
		// artifact must stay byte-identical and unmutated.
		stageDir, err := os.MkdirTemp(r.BuildDir, ".oci-stage-")
		if err != nil {
			return fmt.Errorf("creating staging dir: %w", err)
		}
		defer func() { _ = os.RemoveAll(stageDir) }()

		stagedImage := filepath.Join(stageDir, "image.qcow2")
		files := []orasclient.File{{Name: "image.qcow2", MediaType: qcowMediaType}}

		if backing == "" {
			if err := linkOrCopy(finalPath, stagedImage); err != nil {
				return fmt.Errorf("staging %s: %w", finalPath, err)
			}
		} else {
			if _, err := os.Stat(r.SourceDisk); errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("%s (this layer's backing base) not found, run 'astroimg build' for the base first", r.SourceDisk)
			}

			stagedBase := filepath.Join(stageDir, "base.qcow2")
			if err := linkOrCopy(r.SourceDisk, stagedBase); err != nil {
				return fmt.Errorf("staging %s: %w", r.SourceDisk, err)
			}

			if err := copyFile(finalPath, stagedImage); err != nil {
				return fmt.Errorf("staging %s: %w", finalPath, err)
			}

			// Metadata-only: re-point the staged copy's backing_file at
			// "base.qcow2" (cwd = stageDir, so both names resolve there
			// too) to match the fixed name it'll actually be pulled as --
			// the original backing_file (this layer's real local base
			// filename) only makes sense inside BuildDir.
			if err := runExternalIn(cmd.Context(), stageDir, "qemu-img", "rebase", "-u", "-F", "qcow2", "-f", "qcow2", "-b", "base.qcow2", "image.qcow2"); err != nil {
				return fmt.Errorf("repointing staged image at base.qcow2: %w", err)
			}

			fmt.Printf("bundling base %s into the same manifest (this layer is a backing-file overlay, not --flatten)\n", r.SourceDisk)

			files = append(files, orasclient.File{Name: "base.qcow2", MediaType: qcowMediaType})
		}

		image := ociImage(r)
		fmt.Printf("pushing %s to %s...\n", finalPath, image)

		if err := orasclient.Push(cmd.Context(), image, stageDir, files, ociSourceAnnotations(r.FinalImageName)); err != nil {
			return err
		}

		fmt.Println("pushed", image)

		return nil
	},
}
