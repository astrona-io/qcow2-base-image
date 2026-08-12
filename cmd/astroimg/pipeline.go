package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/astrona-io/qcow2-base-image/internal/platform"
)

var flagPipelineAll bool

var pipelineCmd = &cobra.Command{
	Use:   "pipeline",
	Short: "Run the full pipeline end-to-end: prepare, download, iso, run, wait for cloud-init, sysprep, build",
	Long: `Runs prepare, download, iso, run in sequence, then -- unlike the
interactive 'run' command -- automatically waits for cloud-init to finish
provisioning, runs sysprep over SSH, waits for the guest to power off, and
finalizes the artifact. This is the single command CI calls.

With --all, loops every distro under distros/* instead of just --distro:
for each distro, builds its base image first, then every one of its
layers in turn -- a layer is never started before its own distro's base
is done.`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		if flagPipelineAll {
			if cmd.Flags().Changed("layer") {
				return fmt.Errorf("--all and --layer are mutually exclusive: --all already builds every layer for every distro")
			}

			return runPipelineAll(cmd)
		}

		if !cmd.Flags().Changed("distro") {
			return fmt.Errorf("pipeline needs --distro <name> or --all: refusing to guess and build a default")
		}

		return runPipelineOnce(cmd)
	},
}

func init() {
	pipelineCmd.Flags().BoolVar(&flagPipelineAll, "all", false, "build every distro under distros/*, base first then that distro's layers, in sequence")
}

// runPipelineAll loops every distros/* entry, building each distro's base
// image before any of that distro's layers -- a layer never starts before
// its own distro's base has finished. flagDistro/flagLayer are mutated in
// place per iteration since resolveBuild (via runPipelineOnce) reads them.
func runPipelineAll(cmd *cobra.Command) error {
	entries, err := os.ReadDir(distrosRoot)
	if err != nil {
		return fmt.Errorf("reading %s: %w", distrosRoot, err)
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}

		distro := e.Name()

		flagDistro = distro
		flagLayer = ""

		if err := runPipelineOnce(cmd); err != nil {
			return fmt.Errorf("pipeline distro=%s: %w", distro, err)
		}

		layerEntries, err := os.ReadDir(filepath.Join(distrosRoot, distro, "layers"))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}

			return fmt.Errorf("reading %s layers: %w", distro, err)
		}

		for _, le := range layerEntries {
			if !le.IsDir() {
				continue
			}

			layer := le.Name()

			flagDistro = distro
			flagLayer = layer

			if err := runPipelineOnce(cmd); err != nil {
				return fmt.Errorf("pipeline distro=%s layer=%s: %w", distro, layer, err)
			}
		}
	}

	fmt.Println("=== pipeline --all complete ===")

	return nil
}

// runPipelineOnce runs prepare/download/iso/run/seal/build for the single
// distro+layer currently held in flagDistro/flagLayer.
func runPipelineOnce(cmd *cobra.Command) error {
	ctx := cmd.Context()

	r, _, err := resolveBuild(cmd)
	if err != nil {
		return err
	}

	fmt.Printf("=== pipeline: distro=%s layer=%q arch=%s ===\n", r.Distro, r.Layer, r.Arch)

	if !flagDryRun {
		if err := doPrepare(ctx, r); err != nil {
			return fmt.Errorf("prepare: %w", err)
		}

		if err := doDownload(ctx, r); err != nil {
			return fmt.Errorf("download: %w", err)
		}

		if err := doISO(r); err != nil {
			return fmt.Errorf("iso: %w", err)
		}
	}

	headless := platform.Headless(headlessOverride(cmd))

	qemuCmd, knownHostsFile, err := startVM(ctx, r, headless, flagSSHPort, flagVerbose, flagDryRun)
	if err != nil {
		return fmt.Errorf("run: %w", err)
	}

	if flagDryRun {
		return nil
	}

	if err := sealVM(ctx, r, qemuCmd, knownHostsFile); err != nil {
		return err
	}

	if err := doBuild(ctx, r, flagFlatten); err != nil {
		return fmt.Errorf("build: %w", err)
	}

	fmt.Println("=== pipeline complete ===")

	return nil
}
