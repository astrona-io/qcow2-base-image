package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/astrona-io/qcow2-base-image/internal/config"
)

var buildCmd = &cobra.Command{
	Use:   "build",
	Short: "Compress the sysprepped instance disk into the release-named artifact",
	RunE: func(cmd *cobra.Command, _ []string) error {
		r, _, err := resolveBuild(cmd)
		if err != nil {
			return err
		}

		return doBuild(cmd.Context(), r, flagFlatten)
	},
}

// doBuild compresses the sysprepped instance disk (qemu-img convert -c) into
// the release-named final artifact. For a layer build, the compressed
// output keeps a backing-file reference to the base by default -- the final
// artifact stays a small delta, not a full copy -- unless flatten is set,
// which folds the base's data in too for a single self-contained image.
func doBuild(ctx context.Context, r config.Resolved, flatten bool) error {
	if _, err := os.Stat(r.InstanceDisk); errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%s not found, you must run the VM first", r.InstanceDisk)
	}

	if err := os.MkdirAll(r.BuildDir, 0o750); err != nil {
		return fmt.Errorf("creating %s: %w", r.BuildDir, err)
	}

	finalPath := filepath.Join(r.BuildDir, r.FinalImageName)

	args := []string{"convert", "-O", "qcow2", "-c"}

	if r.Layer != "" && !flatten {
		// A relative (basename-only) backing_file, not an absolute path: it
		// resolves against finalPath's own directory both here at build time
		// (base and layer share BuildDir) and after an OCI pull lands both
		// files together in one output dir (see push.go/orasclient.Push) --
		// no rebase step needed on the consuming end either way.
		args = append(args, "-o", "backing_file="+filepath.Base(r.SourceDisk)+",backing_fmt=qcow2")
	}

	args = append(args, r.InstanceDisk, finalPath)

	fmt.Println("compressing final artifact...")

	if err := runExternal(ctx, "qemu-img", args...); err != nil {
		return fmt.Errorf("compressing final artifact: %w", err)
	}

	if err := os.Remove(r.InstanceDisk); err != nil {
		return fmt.Errorf("removing intermediate instance disk: %w", err)
	}

	fmt.Printf("final artifact ready: %s\n", finalPath)

	if r.Layer != "" && !flatten {
		fmt.Printf("note: %s is a backing-file overlay on %s -- both files are needed to boot it elsewhere (see README)\n", finalPath, r.SourceDisk)
	}

	return nil
}
