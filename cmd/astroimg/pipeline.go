package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/astrona-io/qcow2-base-image/internal/platform"
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

		qemuCmd, knownHostsFile, err := startVM(ctx, r, headless, flagSSHPort, flagVerbose)
		if err != nil {
			return fmt.Errorf("run: %w", err)
		}

		if err := sealVM(ctx, r, qemuCmd, knownHostsFile); err != nil {
			return err
		}

		if err := doBuild(ctx, r, flagFlatten); err != nil {
			return fmt.Errorf("build: %w", err)
		}

		fmt.Println("=== pipeline complete ===")

		return nil
	},
}
