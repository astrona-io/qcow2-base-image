package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/astrona-io/qcow2-base-image/internal/config"
	"github.com/astrona-io/qcow2-base-image/internal/qemurun"
)

var sysprepCmd = &cobra.Command{
	Use:   "sysprep",
	Short: "Connect to the running VM, wipe cloud-init/SSH state, and shut it down to seal the golden image",
	RunE: func(cmd *cobra.Command, _ []string) error {
		r, _, err := resolveBuild(cmd)
		if err != nil {
			return err
		}

		return doSysprep(cmd.Context(), r, flagSSHPort)
	},
}

func doSysprep(ctx context.Context, r config.Resolved, sshPort int) error {
	keyPath := filepath.Join(r.RuntimeDir, "id_ed25519")
	knownHostsFile := filepath.Join(r.RuntimeDir, "known_hosts")

	fmt.Println("connecting to VM to wipe cloud-init state, SSH host keys, and authorized_keys...")

	sysprepCommand := strings.Join(r.SysprepCommands, " && ")

	if _, err := qemurun.RunSSH(ctx, keyPath, knownHostsFile, sshPort, r.SSHUser, "localhost", sysprepCommand); err != nil {
		// The remote command ends in `sudo poweroff`, which terminates the SSH
		// session out from under us -- that looks like an SSH error but is
		// the expected, successful outcome.
		fmt.Println("note: SSH session ended (expected once the guest halts):", err)
	}

	fmt.Println("sysprep commands sent, VM is shutting down")

	return nil
}
