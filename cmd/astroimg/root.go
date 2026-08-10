package main

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/astrona-io/qcow2-base-image/internal/config"
	"github.com/astrona-io/qcow2-base-image/internal/platform"
)

const (
	distrosRoot = "distros"
	layersRoot  = "layers"
)

var (
	flagDistro         string
	flagLayer          string
	flagArch           string
	flagBuildDir       string
	flagLayerBaseImage string
	flagSSHPort        int
	flagRegistry       string
	flagGHUser         string
	flagVerbose        bool
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
	rootCmd.PersistentFlags().StringVar(&flagBuildDir, "build-dir", "build", "directory for downloaded/generated build artifacts")
	rootCmd.PersistentFlags().StringVar(&flagLayerBaseImage, "layer-base-image", "", "override the base image a layer boots from (default: this distro's finished non-layer image)")
	rootCmd.PersistentFlags().IntVar(&flagSSHPort, "ssh-port", 2222, "host port forwarded to the guest's SSH port")
	rootCmd.PersistentFlags().StringVar(&flagRegistry, "registry", "ghcr.io", "OCI registry for push/test-oci")
	rootCmd.PersistentFlags().StringVar(&flagGHUser, "gh-user", "", "registry namespace/user for push/test-oci (default: git config user.name)")
	rootCmd.PersistentFlags().BoolVar(&flagVerbose, "verbose", false, "verbose output")
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

	layerDir, err := config.ValidateLayerDir(layersRoot, flagLayer)
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

	return config.OCIImage(flagRegistry, ghUser, r.ImageTag, r.Arch)
}
