package main

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/astrona-io/qcow2-base-image/internal/platform"
	"github.com/astrona-io/qcow2-base-image/internal/qemurun"
)

var pipelineCmd = &cobra.Command{
	Use:   "pipeline",
	Short: "Run the full pipeline end-to-end: prepare, download, iso, run, wait for cloud-init, sysprep, build",
	Long: `Runs prepare, download, iso, run in sequence, then -- unlike the
interactive 'run' command -- automatically waits for cloud-init to finish
provisioning, runs sysprep over SSH, waits for the guest to power off, and
finalizes the artifact. This is the single command CI calls.`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		ctx := cmd.Context()

		r, _, err := resolveBuild(cmd)
		if err != nil {
			return err
		}

		fmt.Printf("=== pipeline: distro=%s layer=%q arch=%s ===\n", r.Distro, r.Layer, r.Arch)

		if err := doPrepare(ctx, r); err != nil {
			return fmt.Errorf("prepare: %w", err)
		}

		if err := doDownload(ctx, r); err != nil {
			return fmt.Errorf("download: %w", err)
		}

		if err := doISO(r); err != nil {
			return fmt.Errorf("iso: %w", err)
		}

		headless := platform.Headless(headlessOverride(cmd))

		qemuCmd, knownHostsFile, err := startVM(ctx, r, headless, flagSSHPort)
		if err != nil {
			return fmt.Errorf("run: %w", err)
		}

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

		if err := qemuCmd.Wait(); err != nil {
			// `sudo halt` inside the guest is what ends the qemu process;
			// depending on platform that can surface as a non-zero exit,
			// which is expected here, not a pipeline failure.
			fmt.Println("note: qemu exited with:", err)
		}

		if err := doBuild(r); err != nil {
			return fmt.Errorf("build: %w", err)
		}

		fmt.Println("=== pipeline complete ===")

		return nil
	},
}
