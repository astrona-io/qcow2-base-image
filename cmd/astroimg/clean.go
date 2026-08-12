package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var cleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Remove the runtime directory (downloads, instance disks, ISOs, SSH keys/known_hosts)",
	RunE: func(_ *cobra.Command, _ []string) error {
		fmt.Printf("removing %s...\n", flagRuntimeDir)
		// build/ holds finished artifacts (the whole point of running the
		// pipeline); .runtime/ holds everything disposable/intermediate
		// (downloads, instance disks, ISOs, generated SSH keys, the
		// project-local known_hosts) -- that's what clean should clear.
		return os.RemoveAll(flagRuntimeDir)
	},
}
