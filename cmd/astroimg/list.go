package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
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
			Short: "List available LAYER values (layers/*)",
			RunE: func(_ *cobra.Command, _ []string) error {
				return listDir(layersRoot)
			},
		},
	)
}

func listDir(root string) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}

	for _, e := range entries {
		if e.IsDir() {
			fmt.Println(e.Name())
		}
	}

	return nil
}
