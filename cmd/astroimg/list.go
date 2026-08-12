package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/astrona-io/qcow2-base-image/internal/config"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List available distros or layers",
}

func init() {
	listCmd.AddCommand(
		&cobra.Command{
			Use:   "distros",
			Short: "List available DISTRO values (distros/*)",
			RunE: func(_ *cobra.Command, _ []string) error {
				return listDir(distrosRoot)
			},
		},
		&cobra.Command{
			Use:   "layers",
			Short: "List available LAYER values (distros/<distro>/layers/*)",
			RunE: func(_ *cobra.Command, _ []string) error {
				distroLayersRoot := filepath.Join(distrosRoot, flagDistro, "layers")
				return listDir(distroLayersRoot)
			},
		},
		&cobra.Command{
			Use:   "os-versions",
			Short: "List available OS-VERSION values for --distro (distros/<distro>/distro.yaml releases:)",
			RunE: func(_ *cobra.Command, _ []string) error {
				versions, err := config.ListReleases(distrosRoot, flagDistro)
				if err != nil {
					return err
				}

				for _, v := range versions {
					fmt.Println(v)
				}

				return nil
			},
		},
	)
}

func listDir(root string) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}

		return err
	}

	for _, e := range entries {
		if e.IsDir() {
			fmt.Println(e.Name())
		}
	}

	return nil
}
