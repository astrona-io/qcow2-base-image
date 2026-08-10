package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/astrona-io/qcow2-base-image/internal/config"
	"github.com/astrona-io/qcow2-base-image/internal/iso"
)

var isoCmd = &cobra.Command{
	Use:   "iso",
	Short: "Package the rendered cloud-init files into a bootable NoCloud ISO",
	RunE: func(cmd *cobra.Command, _ []string) error {
		r, _, err := resolveBuild(cmd)
		if err != nil {
			return err
		}

		return doISO(r)
	},
}

func doISO(r config.Resolved) error {
	userData, err := os.ReadFile(r.UserData)
	if err != nil {
		return fmt.Errorf("%s missing, run 'astroimg prepare' first: %w", r.UserData, err)
	}

	metaData, err := os.ReadFile(r.MetaData)
	if err != nil {
		return fmt.Errorf("%s missing, run 'astroimg prepare' first: %w", r.MetaData, err)
	}

	if err := iso.WriteCIData(r.CloudInitISO, userData, metaData); err != nil {
		return err
	}

	fmt.Printf("wrote %s\n", r.CloudInitISO)

	return nil
}
