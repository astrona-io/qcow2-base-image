package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/astrona-io/qcow2-base-image/internal/config"
)

var buildCmd = &cobra.Command{
	Use:   "build",
	Short: "Finalize the sysprepped instance disk into the release-named artifact",
	RunE: func(cmd *cobra.Command, _ []string) error {
		r, _, err := resolveBuild(cmd)
		if err != nil {
			return err
		}

		return doBuild(r)
	},
}

func doBuild(r config.Resolved) error {
	if _, err := os.Stat(r.InstanceDisk); errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%s not found, you must run the VM first", r.InstanceDisk)
	}

	finalPath := filepath.Join(r.BuildDir, r.FinalImageName)
	if err := os.Rename(r.InstanceDisk, finalPath); err != nil {
		return fmt.Errorf("finalizing artifact: %w", err)
	}

	fmt.Printf("final artifact ready: %s\n", finalPath)

	return nil
}
