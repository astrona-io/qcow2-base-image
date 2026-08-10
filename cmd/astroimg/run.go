package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/astrona-io/qcow2-base-image/internal/config"
	"github.com/astrona-io/qcow2-base-image/internal/platform"
	"github.com/astrona-io/qcow2-base-image/internal/qemurun"
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Boot the VM instance and leave it running (interactive use: run 'astroimg sysprep' from elsewhere once it's provisioned)",
	RunE: func(cmd *cobra.Command, _ []string) error {
		r, _, err := resolveBuild(cmd)
		if err != nil {
			return err
		}

		headless := platform.Headless(headlessOverride(cmd))

		qemuCmd, _, err := startVM(cmd.Context(), r, headless, flagSSHPort, flagVerbose)
		if err != nil {
			return err
		}

		fmt.Println("VM running. When finished, run 'astroimg sysprep' to seal the golden image.")

		return qemuCmd.Wait()
	},
}

// startVM validates prerequisites, clones the source disk into a fresh
// instance disk if needed, launches qemu-system-*, and waits for its SSH
// host key to be reachable and pinned. It returns the running process
// handle (whose Wait() blocks until the guest powers off) and the
// project-local known_hosts path used to reach it. When headless and
// verbose are both set, the guest's serial console is tailed live to
// stdout -- otherwise headless mode has no visible output at all until the
// SSH-wait heartbeats kick in, which can look stuck.
func startVM(ctx context.Context, r config.Resolved, headless bool, sshPort int, verbose bool) (*exec.Cmd, string, error) {
	qcfg, err := prepareVMDisks(ctx, r)
	if err != nil {
		return nil, "", err
	}

	knownHostsFile := filepath.Join(r.BuildDir, "known_hosts")
	hostPort := fmt.Sprintf("[localhost]:%d", sshPort)
	_ = qemurun.RemoveHostKey(ctx, knownHostsFile, hostPort)

	qrCfg := qemurun.Config{
		QEMU:          qcfg,
		InstanceDisk:  r.InstanceDisk,
		CloudInitISO:  r.CloudInitISO,
		VarsFile:      r.VarsFile,
		SSHPort:       sshPort,
		Headless:      headless,
		SerialLogPath: filepath.Join(r.BuildDir, r.ImageTag+"-console.log"),
	}
	args := qemurun.BuildArgs(qrCfg)

	keyPath := filepath.Join(r.BuildDir, "id_ed25519")
	fmt.Printf("launching %s %s VM with %s (headless=%v)\n", r.Arch, r.ImageTag, qcfg.Binary, headless)

	if !headless {
		fmt.Println("Username: ubuntu | Password: ubuntu")
	}

	fmt.Printf("SSH: ssh -i %s -p %d -o UserKnownHostsFile=%s ubuntu@localhost\n", keyPath, sshPort, knownHostsFile)

	cmd, err := qemurun.Start(ctx, qcfg.Binary, args)
	if err != nil {
		return nil, "", err
	}

	if headless && verbose {
		fmt.Printf("tailing guest console live (%s) -- omit --verbose for quieter output\n", qrCfg.SerialLogPath)

		go func() { _ = qemurun.TailFile(ctx, qrCfg.SerialLogPath, os.Stdout) }()
	}

	fmt.Println("waiting for VM SSH host key (first boot can take 5-10+ min)...")

	if err := qemurun.WaitAndPinHostKey(ctx, knownHostsFile, sshPort, 10*time.Minute); err != nil {
		fmt.Println("warning:", err, "-- run 'astroimg pin-hostkey' manually once it's up")
	} else {
		fmt.Println("host key pinned")
	}

	return cmd, knownHostsFile, nil
}

// prepareVMDisks validates that the source disk and cloud-init ISO exist,
// renders a missing vars file via prepare if needed, detects this host's
// QEMU config, and clones the source disk into a fresh instance disk if one
// doesn't already exist.
func prepareVMDisks(ctx context.Context, r config.Resolved) (platform.QEMUConfig, error) {
	if _, err := os.Stat(r.SourceDisk); errors.Is(err, os.ErrNotExist) {
		hint := "run 'astroimg download' first"
		if r.Layer != "" {
			hint = fmt.Sprintf("build the base image first ('astroimg pipeline --distro %s'), or pass --layer-base-image", r.Distro)
		}

		return platform.QEMUConfig{}, fmt.Errorf("%s does not exist: %s", r.SourceDisk, hint)
	}

	if _, err := os.Stat(r.CloudInitISO); errors.Is(err, os.ErrNotExist) {
		return platform.QEMUConfig{}, fmt.Errorf("%s does not exist, run 'astroimg iso' first", r.CloudInitISO)
	}

	if _, err := os.Stat(r.VarsFile); errors.Is(err, os.ErrNotExist) {
		fmt.Println("vars file missing, running prepare...")

		if err := doPrepare(ctx, r); err != nil {
			return platform.QEMUConfig{}, err
		}
	}

	qcfg, err := platform.DetectForHost(r.Arch)
	if err != nil {
		return platform.QEMUConfig{}, err
	}

	if _, err := os.Stat(r.InstanceDisk); errors.Is(err, os.ErrNotExist) {
		fmt.Printf("creating fresh ephemeral instance from %s...\n", r.SourceDisk)

		if err := copyFile(r.SourceDisk, r.InstanceDisk); err != nil {
			return platform.QEMUConfig{}, err
		}
	} else {
		fmt.Printf("using existing %s\n", r.InstanceDisk)
	}

	return qcfg, nil
}
