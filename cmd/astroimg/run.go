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
	Short: "Boot the VM instance (add --seal to also wait for cloud-init, sysprep, and wait for shutdown)",
	RunE: func(cmd *cobra.Command, _ []string) error {
		ctx := cmd.Context()

		r, _, err := resolveBuild(cmd)
		if err != nil {
			return err
		}

		headless := platform.Headless(headlessOverride(cmd))

		qemuCmd, knownHostsFile, err := startVM(ctx, r, headless, flagSSHPort, flagVerbose)
		if err != nil {
			return err
		}

		seal, _ := cmd.Flags().GetBool("seal")
		if !seal {
			fmt.Println("VM running. When finished, run 'astroimg sysprep' to seal the golden image.")
			return qemuCmd.Wait()
		}

		if err := sealVM(ctx, r, qemuCmd, knownHostsFile); err != nil {
			return err
		}

		fmt.Println("sealed. Run 'astroimg build' to finalize the artifact.")

		return nil
	},
}

func init() {
	runCmd.Flags().Bool("seal", false, "wait for cloud-init to finish, sysprep, and wait for shutdown automatically (skips the final 'astroimg build' rename -- run that yourself after)")
}

// sealVM waits for cloud-init to finish, runs sysprep over SSH, and waits
// for the guest to power off -- the automated "finish building" tail shared
// by `pipeline` and `run --seal`.
func sealVM(ctx context.Context, r config.Resolved, qemuCmd *exec.Cmd, knownHostsFile string) error {
	keyPath := filepath.Join(r.BuildDir, "id_ed25519")

	fmt.Println("waiting for cloud-init to finish provisioning (this can take 5-10+ min)...")

	if err := qemurun.WaitForCloudInit(ctx, keyPath, knownHostsFile, flagSSHPort, "ubuntu", "localhost", 30*time.Minute); err != nil {
		return fmt.Errorf("waiting for cloud-init: %w", err)
	}

	fmt.Println("cloud-init finished provisioning, sealing the image...")

	if err := doSysprep(ctx, r.BuildDir, flagSSHPort); err != nil {
		return fmt.Errorf("sysprep: %w", err)
	}

	fmt.Println("waiting for the VM to power off...")

	if err := waitForShutdown(qemuCmd, 2*time.Minute); err != nil {
		return fmt.Errorf("%w -- check %s", err, filepath.Join(r.BuildDir, r.ImageTag+"-console.log"))
	}

	return nil
}

// waitForShutdown blocks until qemuCmd exits or maxWait elapses. If sysprep's
// `sudo poweroff` never actually reaches the guest (e.g. it rebooted instead, or
// the SSH command failed silently), qemuCmd.Wait() alone would block
// forever with no feedback -- this forces a clear, actionable error and
// kills the process instead of hanging indefinitely.
func waitForShutdown(qemuCmd *exec.Cmd, maxWait time.Duration) error {
	waitErr := make(chan error, 1)
	go func() { waitErr <- qemuCmd.Wait() }()

	select {
	case err := <-waitErr:
		if err != nil {
			// `sudo poweroff` inside the guest is what ends the qemu process;
			// depending on platform that can surface as a non-zero exit,
			// which is expected here, not a failure.
			fmt.Println("note: qemu exited with:", err)
		}

		return nil
	case <-time.After(maxWait):
		_ = qemuCmd.Process.Kill()
		return fmt.Errorf("guest did not power off within %s after sysprep", maxWait)
	}
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
		if err := createInstanceDisk(ctx, r); err != nil {
			return platform.QEMUConfig{}, err
		}
	} else {
		fmt.Printf("using existing %s\n", r.InstanceDisk)
	}

	return qcfg, nil
}

// createInstanceDisk creates r.InstanceDisk from r.SourceDisk: a
// copy-on-write overlay for layer builds (so the instance disk isn't a full
// copy of the base), or a full clone for base builds (which have no base of
// their own to reference).
func createInstanceDisk(ctx context.Context, r config.Resolved) error {
	if r.Layer != "" {
		fmt.Printf("creating copy-on-write overlay instance backed by %s...\n", r.SourceDisk)
		return createOverlayDisk(ctx, r.SourceDisk, r.InstanceDisk)
	}

	fmt.Printf("creating fresh ephemeral instance from %s...\n", r.SourceDisk)

	return copyFile(r.SourceDisk, r.InstanceDisk)
}
