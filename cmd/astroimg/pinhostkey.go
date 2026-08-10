package main

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/astrona-io/qcow2-base-image/internal/qemurun"
)

var pinHostkeyCmd = &cobra.Command{
	Use:   "pin-hostkey",
	Short: "Manually pin the VM's SSH host key if 'astroimg run' gave up waiting",
	RunE: func(cmd *cobra.Command, _ []string) error {
		knownHostsFile := filepath.Join(flagBuildDir, "known_hosts")
		hostPort := fmt.Sprintf("[localhost]:%d", flagSSHPort)
		_ = qemurun.RemoveHostKey(cmd.Context(), knownHostsFile, hostPort)

		if err := qemurun.WaitAndPinHostKey(cmd.Context(), knownHostsFile, flagSSHPort, time.Minute); err != nil {
			return fmt.Errorf("VM SSH still not reachable on port %d after 1 minute, is it still booting? %w", flagSSHPort, err)
		}

		fmt.Println("host key pinned")

		return nil
	},
}
