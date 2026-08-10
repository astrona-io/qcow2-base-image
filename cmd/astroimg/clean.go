package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var cleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Remove the entire build directory",
	RunE: func(_ *cobra.Command, _ []string) error {
		fmt.Printf("removing %s...\n", flagBuildDir)
		// Nothing to clean in ~/.ssh: unlike the old Makefile, astroimg never
		// writes VM host keys anywhere but build/known_hosts, so deleting
		// the build dir is the whole cleanup.
		return os.RemoveAll(flagBuildDir)
	},
}
