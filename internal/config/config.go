// Package config resolves distro/layer/arch inputs into every derived path
// and name the pipeline needs (equivalent of the Makefile's IMAGE_TAG,
// BASE_DISK, FINAL_IMAGE_NAME, etc.). Resolve itself performs no I/O so it is
// fully unit-testable; LoadDistro and ValidateLayerDir do the filesystem
// access and are where path-traversal guarding happens.
package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"

	"gopkg.in/yaml.v3"
)

const defaultSSHValue = "ubuntu"

var namePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// ValidateName checks that a distro or layer name is safe to use as a path
// component: lowercase alphanumeric plus hyphens, starting with alphanumeric.
// This blocks path traversal (e.g. "../../etc") and shell-metacharacter
// smuggling before the name ever reaches a filesystem or exec call.
func ValidateName(kind, name string) error {
	if !namePattern.MatchString(name) {
		return fmt.Errorf("invalid %s name %q: must match %s", kind, name, namePattern.String())
	}

	return nil
}

// DistroConfig is the schema of distros/<name>/distro.yaml.
type DistroConfig struct {
	OSVersion           string   `yaml:"os_version"`
	OSRelease           string   `yaml:"os_release"`
	ImageVariant        string   `yaml:"image_variant"`
	ImageURLTemplate    string   `yaml:"image_url_template"`
	ChecksumURLTemplate string   `yaml:"checksum_url_template"`
	SSHUser             string   `yaml:"ssh_user"`
	SSHPassword         string   `yaml:"ssh_password"`
	SysprepCommands     []string `yaml:"sysprep_commands"`
}

// LoadDistro reads and validates distros/<distro>/distro.yaml under
// distrosRoot, returning the parsed config and the distro's directory.
func LoadDistro(distrosRoot, distro string) (DistroConfig, string, error) {
	var cfg DistroConfig
	if err := ValidateName("distro", distro); err != nil {
		return cfg, "", err
	}

	dir := filepath.Join(distrosRoot, distro)
	if err := ensureUnder(distrosRoot, dir); err != nil {
		return cfg, "", err
	}

	path := filepath.Join(dir, "distro.yaml")

	data, err := os.ReadFile(path) //nolint:gosec // path is built from a name already validated + confirmed to stay under distrosRoot (ensureUnder)
	if err != nil {
		return cfg, "", fmt.Errorf("reading %s: %w", path, err)
	}

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, "", fmt.Errorf("parsing %s: %w", path, err)
	}

	if cfg.OSVersion == "" || cfg.OSRelease == "" || cfg.ImageVariant == "" || cfg.ImageURLTemplate == "" {
		return cfg, "", fmt.Errorf("%s: os_version, os_release, image_variant, image_url_template are required", path)
	}

	if cfg.SSHUser == "" {
		cfg.SSHUser = defaultSSHValue
	}

	if cfg.SSHPassword == "" {
		cfg.SSHPassword = defaultSSHValue
	}

	if len(cfg.SysprepCommands) == 0 {
		cfg.SysprepCommands = []string{
			"sudo cloud-init clean --logs --machine-id",
			"sudo rm -f /home/" + cfg.SSHUser + "/.ssh/authorized_keys",
			"sudo rm -f /etc/ssh/ssh_host_*",
			"if command -v apt-get >/dev/null; then sudo apt-get clean && sudo rm -rf /var/lib/apt/lists/*; elif command -v dnf >/dev/null; then sudo dnf clean all && sudo rm -rf /var/cache/dnf/*; elif command -v zypper >/dev/null; then sudo zypper clean -a; fi",
			"sudo journalctl --vacuum-time=1s",
			"sudo rm -rf /tmp/* /var/tmp/*",
			"sudo fstrim -av",
			"sudo sync",
			"sudo poweroff",
		}
	}

	return cfg, dir, nil
}

// ValidateLayerDir validates a layer name (if non-empty) and returns its
// directory under layersRoot, guarding against path traversal.
func ValidateLayerDir(layersRoot, layer string) (string, error) {
	if layer == "" {
		return "", nil
	}

	if err := ValidateName("layer", layer); err != nil {
		return "", err
	}

	dir := filepath.Join(layersRoot, layer)
	if err := ensureUnder(layersRoot, dir); err != nil {
		return "", err
	}

	return dir, nil
}

func ensureUnder(root, path string) error {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return err
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}

	rel, err := filepath.Rel(absRoot, absPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path %q escapes root %q", path, root)
	}

	return nil
}

// Options are the per-invocation inputs that, together with a DistroConfig,
// fully determine every derived path and name in the pipeline.
type Options struct {
	Distro     string
	Layer      string
	Arch       string
	BuildDir   string // defaults to "build" when empty
	RuntimeDir string // defaults to ".runtime" when empty

	// LayerBaseImage overrides the source disk a layer boots from. When
	// empty, it defaults to this distro's finished non-layer final image,
	// allowing layers to be chained by pointing this at another layer's
	// output.
	LayerBaseImage string
}

// Resolved holds every derived path/name the pipeline needs for one
// distro+layer+arch build.
type Resolved struct {
	Distro, Layer, Arch, BuildDir, RuntimeDir string

	TemplateDir      string
	ImageTag         string
	InstanceIDPrefix string

	OSRelease   string
	ImageURL    string
	ChecksumURL string

	DownloadedImg  string
	BaseDisk       string
	LayerBaseImage string
	SourceDisk     string

	FinalImageName string
	InstanceDisk   string
	VarsFile       string
	CloudInitISO   string
	UserData       string
	MetaData       string

	TestExtractDir string
	TestImageName  string

	SSHUser         string
	SSHPassword     string
	SysprepCommands []string
}

// Resolve computes every derived path/name for one distro+layer+arch build.
// distroDir/layerDir must already be validated (LoadDistro / ValidateLayerDir).
func Resolve(cfg DistroConfig, distroDir, layerDir string, opts Options) (Resolved, error) {
	buildDir := opts.BuildDir
	if buildDir == "" {
		buildDir = "build"
	}

	runtimeDir := opts.RuntimeDir
	if runtimeDir == "" {
		runtimeDir = ".runtime"
	}

	r := Resolved{
		Distro:          opts.Distro,
		Layer:           opts.Layer,
		Arch:            opts.Arch,
		BuildDir:        buildDir,
		RuntimeDir:      runtimeDir,
		OSRelease:       cfg.OSRelease,
		SSHUser:         cfg.SSHUser,
		SSHPassword:     cfg.SSHPassword,
		SysprepCommands: cfg.SysprepCommands,
	}

	imageURL, err := renderTemplate("image_url_template", cfg.ImageURLTemplate, cfg, opts.Arch)
	if err != nil {
		return r, err
	}

	r.ImageURL = imageURL

	if cfg.ChecksumURLTemplate != "" {
		checksumURL, err := renderTemplate("checksum_url_template", cfg.ChecksumURLTemplate, cfg, opts.Arch)
		if err != nil {
			return r, err
		}

		r.ChecksumURL = checksumURL
	}

	baseImageTag := fmt.Sprintf("%s-%s-%s", opts.Distro, cfg.OSVersion, cfg.ImageVariant)
	r.BaseDisk = filepath.Join(runtimeDir, fmt.Sprintf("base-%s-%s.qcow2", opts.Distro, opts.Arch))
	r.DownloadedImg = filepath.Join(runtimeDir, fmt.Sprintf("%s-server-cloudimg-%s.img", cfg.OSRelease, opts.Arch))

	defaultLayerBaseImage := filepath.Join(buildDir, fmt.Sprintf("%s-%s.qcow2", baseImageTag, opts.Arch))

	r.LayerBaseImage = opts.LayerBaseImage
	if r.LayerBaseImage == "" {
		r.LayerBaseImage = defaultLayerBaseImage
	}

	if opts.Layer == "" {
		r.TemplateDir = distroDir
		r.ImageTag = baseImageTag
		r.InstanceIDPrefix = fmt.Sprintf("%s-%s-base", opts.Distro, cfg.OSVersion)
		r.SourceDisk = r.BaseDisk
	} else {
		r.TemplateDir = layerDir
		r.ImageTag = fmt.Sprintf("%s-%s", baseImageTag, opts.Layer)
		r.InstanceIDPrefix = fmt.Sprintf("%s-%s-%s", opts.Distro, cfg.OSVersion, opts.Layer)
		r.SourceDisk = r.LayerBaseImage
	}

	// A layer's in-progress disk is a copy-on-write overlay backed by the
	// base (see cmd/astroimg createInstanceDisk), not a full clone, so it
	// gets its own "overlay-" prefix instead of "instance-" to say so.
	instancePrefix := "instance"
	if opts.Layer != "" {
		instancePrefix = "overlay"
	}

	r.FinalImageName = fmt.Sprintf("%s-%s.qcow2", r.ImageTag, opts.Arch)
	r.InstanceDisk = filepath.Join(runtimeDir, fmt.Sprintf("%s-%s-%s.qcow2", instancePrefix, r.ImageTag, opts.Arch))
	r.VarsFile = filepath.Join(runtimeDir, fmt.Sprintf("vars-%s.fd", opts.Arch))
	r.CloudInitISO = filepath.Join(runtimeDir, r.ImageTag+"-cloud-init.iso")
	r.UserData = filepath.Join(runtimeDir, r.ImageTag+"-user-data")
	r.MetaData = filepath.Join(runtimeDir, r.ImageTag+"-meta-data")
	r.TestExtractDir = filepath.Join(runtimeDir, "test-extract")
	r.TestImageName = filepath.Join(r.TestExtractDir, r.FinalImageName)

	return r, nil
}

// OCIImage builds the OCI artifact reference used by push/test-oci.
func OCIImage(registry, ghUser, imageTag, arch string) string {
	return fmt.Sprintf("%s/%s/%s:%s", registry, ghUser, imageTag, arch)
}

type templateVars struct {
	Arch    string
	Release string
}

func renderTemplate(name, tmplStr string, cfg DistroConfig, arch string) (string, error) {
	tmpl, err := template.New(name).Parse(tmplStr)
	if err != nil {
		return "", fmt.Errorf("parsing %s: %w", name, err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, templateVars{Arch: arch, Release: cfg.OSRelease}); err != nil {
		return "", fmt.Errorf("executing %s: %w", name, err)
	}

	return buf.String(), nil
}
