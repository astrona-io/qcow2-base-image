package main

import (
	"fmt"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/astrona-io/qcow2-base-image/internal/config"
	"github.com/astrona-io/qcow2-base-image/internal/platform"
)

const (
	distrosRoot = "distros"
)

var (
	flagDistro         string
	flagLayer          string
	flagArch           string
	flagBuildDir       string
	flagRuntimeDir     string
	flagLayerBaseImage string
	flagSSHPort        int
	flagRegistry       string
	flagGHUser         string
	flagVerbose        bool
	flagFlatten        bool
	flagDryRun         bool
)

var rootCmd = &cobra.Command{
	Use:           "astroimg",
	Short:         "Build, run, and ship QEMU golden images across distros, layers, and architectures",
	SilenceUsage:  true,
	SilenceErrors: false,
}

func init() {
	rootCmd.PersistentFlags().StringVar(&flagDistro, "distro", "ubuntu", "distro to build (see distros/)")
	rootCmd.PersistentFlags().StringVar(&flagLayer, "layer", "", "optional layer to build on top of a finished base image (see layers/)")
	rootCmd.PersistentFlags().StringVar(&flagArch, "arch", defaultArch(), "target architecture: arm64 or amd64")
	rootCmd.PersistentFlags().StringVar(&flagBuildDir, "build-dir", "build", "directory for final generated build artifacts")
	rootCmd.PersistentFlags().StringVar(&flagRuntimeDir, "runtime-dir", ".runtime", "directory for intermediate downloads and temporary runtime files")
	rootCmd.PersistentFlags().StringVar(&flagLayerBaseImage, "layer-base-image", "", "override the base image a layer boots from (default: this distro's finished non-layer image)")
	rootCmd.PersistentFlags().IntVar(&flagSSHPort, "ssh-port", 2222, "host port forwarded to the guest's SSH port")
	rootCmd.PersistentFlags().StringVar(&flagRegistry, "registry", "ghcr.io", "OCI registry for push/test-oci")
	rootCmd.PersistentFlags().StringVar(&flagGHUser, "gh-user", "", "registry namespace/org for push/test-oci (default: this repo's GitHub org from 'git remote origin', then 'gh' CLI login)")
	rootCmd.PersistentFlags().BoolVar(&flagVerbose, "verbose", false, "verbose output")
	rootCmd.PersistentFlags().BoolVar(&flagFlatten, "flatten", false, "for layer builds: fold the base's data into a standalone image instead of a small base-backed overlay (bigger, but self-contained)")
	rootCmd.PersistentFlags().BoolVar(&flagDryRun, "dry-run", false, "print the QEMU command and exit without starting the VM")
	rootCmd.PersistentFlags().Bool("headless", false, "run QEMU without a GUI window (default: auto-detect via $CI / host OS)")

	rootCmd.AddCommand(
		prepareCmd,
		downloadCmd,
		isoCmd,
		runCmd,
		pinHostkeyCmd,
		sysprepCmd,
		buildCmd,
		pushCmd,
		testOCICmd,
		cleanCmd,
		pipelineCmd,
		testBootCmd,
		listCmd,
	)
}

// defaultArch maps the host's Go runtime architecture to astroimg's target
// architecture names. runtime.GOARCH is already normalized to "amd64"/
// "arm64" by Go itself, unlike the Makefile which had to translate uname's
// "x86_64"/"aarch64" by hand.
func defaultArch() string {
	switch runtime.GOARCH {
	case platform.ArchAMD64, platform.ArchARM64:
		return runtime.GOARCH
	default:
		return platform.ArchAMD64
	}
}

// headlessOverride returns a pointer to the --headless value only when the
// user explicitly passed the flag, so platform.Headless can distinguish
// "explicitly requested" from "use the auto-detected default".
func headlessOverride(cmd *cobra.Command) *bool {
	if !cmd.Flags().Changed("headless") {
		return nil
	}

	v, _ := cmd.Flags().GetBool("headless")

	return &v
}

// resolveBuild validates --distro/--layer/--arch, loads distros/<distro>/distro.yaml,
// and computes every derived path/name for this invocation.
func resolveBuild(_ *cobra.Command) (config.Resolved, config.DistroConfig, error) {
	cfg, distroDir, err := config.LoadDistro(distrosRoot, flagDistro)
	if err != nil {
		return config.Resolved{}, cfg, err
	}

	distroLayersRoot := filepath.Join(distrosRoot, flagDistro, "layers")

	layerDir, err := config.ValidateLayerDir(distroLayersRoot, flagLayer)
	if err != nil {
		return config.Resolved{}, cfg, err
	}

	if err := platform.ValidateArch(flagArch); err != nil {
		return config.Resolved{}, cfg, err
	}

	opts := config.Options{
		Distro:         flagDistro,
		Layer:          flagLayer,
		Arch:           flagArch,
		BuildDir:       flagBuildDir,
		RuntimeDir:     flagRuntimeDir,
		LayerBaseImage: flagLayerBaseImage,
	}

	r, err := config.Resolve(cfg, distroDir, layerDir, opts)
	if err != nil {
		return r, cfg, err
	}

	if flagVerbose {
		fmt.Printf("resolved: image_tag=%s template_dir=%s source_disk=%s\n", r.ImageTag, r.TemplateDir, r.SourceDisk)
	}

	return r, cfg, nil
}

// ociImage builds the OCI artifact reference used by push/test-oci.
func ociImage(r config.Resolved) string {
	ghUser := flagGHUser
	if ghUser == "" {
		ghUser = gitConfigUserNameOrDefault()
	}

	return config.OCIImage(flagRegistry, ghUser, r.OCIRepo, r.OCITag)
}

// ociSourceAnnotations builds the OCI manifest annotations set on push.
// org.opencontainers.image.source is what makes GHCR auto-link the
// resulting package to this repo -- and, for a package that doesn't
// already exist, makes it inherit the repo's visibility (public repo ->
// public package) instead of defaulting to private. No-op (empty map) if
// origin isn't a github.com remote.
func ociSourceAnnotations(title string) map[string]string {
	annotations := map[string]string{"org.opencontainers.image.title": title}

	if src := gitRemoteHTTPSURL(); src != "" {
		annotations["org.opencontainers.image.source"] = src
	}

	return annotations
}
