package config

import (
	"os"
	"path/filepath"
	"testing"
)

func ubuntuCfg() DistroConfig {
	return DistroConfig{
		OSVersion:        "24.04",
		OSRelease:        "noble",
		ImageVariant:     "desktop",
		ImageURLTemplate: "https://cloud-images.ubuntu.com/{{.Release}}/current/{{.Release}}-server-cloudimg-{{.Arch}}.img",
		SSHUser:          "ubuntu",
		SSHPassword:      "ubuntu",
	}
}

// These expected values were captured from `make -n test-run` / `make -n
// test-run LAYER=docker` dry-run output against the Makefile pipeline this
// package replaces — they must not drift silently.
func TestResolveBase(t *testing.T) {
	cfg := ubuntuCfg()

	r, err := Resolve(cfg, "distros/ubuntu", "", Options{
		Distro: "ubuntu",
		Arch:   "arm64",
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	want := map[string]string{
		"TemplateDir":      "distros/ubuntu",
		"ImageTag":         "ubuntu-24.04-desktop",
		"BaseDisk":         "build/base-ubuntu-arm64.qcow2",
		"SourceDisk":       "build/base-ubuntu-arm64.qcow2",
		"FinalImageName":   "ubuntu-24.04-desktop-arm64.qcow2",
		"InstanceDisk":     "build/instance-ubuntu-24.04-desktop-arm64.qcow2",
		"CloudInitISO":     "build/ubuntu-24.04-desktop-cloud-init.iso",
		"UserData":         "build/ubuntu-24.04-desktop-user-data",
		"MetaData":         "build/ubuntu-24.04-desktop-meta-data",
		"DownloadedImg":    "build/noble-server-cloudimg-arm64.img",
		"ImageURL":         "https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-arm64.img",
		"InstanceIDPrefix": "ubuntu-24.04-base",
	}

	got := map[string]string{
		"TemplateDir":      r.TemplateDir,
		"ImageTag":         r.ImageTag,
		"BaseDisk":         r.BaseDisk,
		"SourceDisk":       r.SourceDisk,
		"FinalImageName":   r.FinalImageName,
		"InstanceDisk":     r.InstanceDisk,
		"CloudInitISO":     r.CloudInitISO,
		"UserData":         r.UserData,
		"MetaData":         r.MetaData,
		"DownloadedImg":    r.DownloadedImg,
		"ImageURL":         r.ImageURL,
		"InstanceIDPrefix": r.InstanceIDPrefix,
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s = %q, want %q", k, got[k], w)
		}
	}
}

func checkBaseResolve(t *testing.T, distro string, cfg DistroConfig, wantAMD map[string]string, wantARMImageURL string) {
	t.Helper()

	r, err := Resolve(cfg, filepath.Join("distros", distro), "", Options{
		Distro: distro,
		Arch:   "amd64",
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	for k, w := range wantAMD {
		var got string

		switch k {
		case "TemplateDir":
			got = r.TemplateDir
		case "ImageTag":
			got = r.ImageTag
		case "BaseDisk":
			got = r.BaseDisk
		case "FinalImageName":
			got = r.FinalImageName
		case "ImageURL":
			got = r.ImageURL
		}

		if got != w {
			t.Errorf("%s %s = %q, want %q", distro, k, got, w)
		}
	}

	if r.SSHUser != cfg.SSHUser {
		t.Errorf("%s SSHUser = %q, want %q", distro, r.SSHUser, cfg.SSHUser)
	}

	if r.SSHPassword != cfg.SSHPassword {
		t.Errorf("%s SSHPassword = %q, want %q", distro, r.SSHPassword, cfg.SSHPassword)
	}

	r2, err := Resolve(cfg, filepath.Join("distros", distro), "", Options{
		Distro: distro,
		Arch:   "arm64",
	})
	if err != nil {
		t.Fatalf("Resolve ARM64: %v", err)
	}

	if r2.ImageURL != wantARMImageURL {
		t.Errorf("%s ARM64 ImageURL = %q, want %q", distro, r2.ImageURL, wantARMImageURL)
	}
}

func TestResolveFedoraBase(t *testing.T) {
	cfg := DistroConfig{
		OSVersion:        "44",
		OSRelease:        "44",
		ImageVariant:     "cloud",
		ImageURLTemplate: "https://download.fedoraproject.org/pub/fedora/linux/releases/{{.Release}}/Cloud/{{if eq .Arch \"amd64\"}}x86_64{{else}}aarch64{{end}}/images/Fedora-Cloud-Base-Generic-{{.Release}}-1.7.{{if eq .Arch \"amd64\"}}x86_64{{else}}aarch64{{end}}.qcow2",
		SSHUser:          "fedora",
		SSHPassword:      "fedora",
	}

	wantAMD := map[string]string{
		"TemplateDir":    "distros/fedora",
		"ImageTag":       "fedora-44-cloud",
		"BaseDisk":       "build/base-fedora-amd64.qcow2",
		"FinalImageName": "fedora-44-cloud-amd64.qcow2",
		"ImageURL":       "https://download.fedoraproject.org/pub/fedora/linux/releases/44/Cloud/x86_64/images/Fedora-Cloud-Base-Generic-44-1.7.x86_64.qcow2",
	}

	checkBaseResolve(t, "fedora", cfg, wantAMD, "https://download.fedoraproject.org/pub/fedora/linux/releases/44/Cloud/aarch64/images/Fedora-Cloud-Base-Generic-44-1.7.aarch64.qcow2")
}

func TestResolveOpenSUSEBase(t *testing.T) {
	cfg := DistroConfig{
		OSVersion:        "15.6",
		OSRelease:        "15.6",
		ImageVariant:     "nocloud",
		ImageURLTemplate: "https://download.opensuse.org/repositories/Cloud:/Images:/Leap_{{.Release}}/images/openSUSE-Leap-{{.Release}}.{{if eq .Arch \"amd64\"}}x86_64{{else}}aarch64{{end}}-NoCloud.qcow2",
		SSHUser:          "opensuse",
		SSHPassword:      "opensuse",
	}

	wantAMD := map[string]string{
		"TemplateDir":    "distros/opensuse",
		"ImageTag":       "opensuse-15.6-nocloud",
		"BaseDisk":       "build/base-opensuse-amd64.qcow2",
		"FinalImageName": "opensuse-15.6-nocloud-amd64.qcow2",
		"ImageURL":       "https://download.opensuse.org/repositories/Cloud:/Images:/Leap_15.6/images/openSUSE-Leap-15.6.x86_64-NoCloud.qcow2",
	}

	checkBaseResolve(t, "opensuse", cfg, wantAMD, "https://download.opensuse.org/repositories/Cloud:/Images:/Leap_15.6/images/openSUSE-Leap-15.6.aarch64-NoCloud.qcow2")
}

func TestResolveLayer(t *testing.T) {
	cfg := ubuntuCfg()

	r, err := Resolve(cfg, "distros/ubuntu", "layers/docker", Options{
		Distro: "ubuntu",
		Layer:  "docker",
		Arch:   "arm64",
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	want := map[string]string{
		"TemplateDir":      "layers/docker",
		"ImageTag":         "ubuntu-24.04-desktop-docker",
		"LayerBaseImage":   "build/ubuntu-24.04-desktop-arm64.qcow2",
		"SourceDisk":       "build/ubuntu-24.04-desktop-arm64.qcow2",
		"FinalImageName":   "ubuntu-24.04-desktop-docker-arm64.qcow2",
		"InstanceDisk":     "build/overlay-ubuntu-24.04-desktop-docker-arm64.qcow2",
		"CloudInitISO":     "build/ubuntu-24.04-desktop-docker-cloud-init.iso",
		"UserData":         "build/ubuntu-24.04-desktop-docker-user-data",
		"MetaData":         "build/ubuntu-24.04-desktop-docker-meta-data",
		"InstanceIDPrefix": "ubuntu-24.04-docker",
	}

	got := map[string]string{
		"TemplateDir":      r.TemplateDir,
		"ImageTag":         r.ImageTag,
		"LayerBaseImage":   r.LayerBaseImage,
		"SourceDisk":       r.SourceDisk,
		"FinalImageName":   r.FinalImageName,
		"InstanceDisk":     r.InstanceDisk,
		"CloudInitISO":     r.CloudInitISO,
		"UserData":         r.UserData,
		"MetaData":         r.MetaData,
		"InstanceIDPrefix": r.InstanceIDPrefix,
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s = %q, want %q", k, got[k], w)
		}
	}
}

func TestResolveLayerBaseImageOverrideChainsLayers(t *testing.T) {
	cfg := ubuntuCfg()

	r, err := Resolve(cfg, "distros/ubuntu", "layers/monitoring", Options{
		Distro:         "ubuntu",
		Layer:          "monitoring",
		Arch:           "arm64",
		LayerBaseImage: "build/ubuntu-24.04-desktop-docker-arm64.qcow2",
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if r.SourceDisk != "build/ubuntu-24.04-desktop-docker-arm64.qcow2" {
		t.Errorf("SourceDisk = %q, want chained override", r.SourceDisk)
	}
}

func TestValidateNameRejectsTraversal(t *testing.T) {
	cases := []string{"../etc", "ubuntu/../../etc", "UPPER", "with space", "-leadinghyphen", ""}
	for _, c := range cases {
		if err := ValidateName("distro", c); err == nil {
			t.Errorf("ValidateName(%q) = nil, want error", c)
		}
	}
}

func TestValidateNameAcceptsSaneNames(t *testing.T) {
	cases := []string{"ubuntu", "ubuntu-24-04", "debian12"}
	for _, c := range cases {
		if err := ValidateName("distro", c); err != nil {
			t.Errorf("ValidateName(%q) = %v, want nil", c, err)
		}
	}
}

func TestLoadDistroRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	if _, _, err := LoadDistro(dir, "../escape"); err == nil {
		t.Fatal("expected error for traversal distro name")
	}
}

func TestLoadDistroRequiresFields(t *testing.T) {
	root := t.TempDir()

	distroDir := filepath.Join(root, "incomplete")
	if err := os.MkdirAll(distroDir, 0o750); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(distroDir, "distro.yaml"), []byte("os_version: \"1\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, _, err := LoadDistro(root, "incomplete"); err == nil {
		t.Fatal("expected error for missing required fields")
	}
}

func TestValidateLayerDirRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	if _, err := ValidateLayerDir(dir, "../escape"); err == nil {
		t.Fatal("expected error for traversal layer name")
	}
}

func TestValidateLayerDirEmptyIsNoop(t *testing.T) {
	dir := t.TempDir()

	got, err := ValidateLayerDir(dir, "")
	if err != nil || got != "" {
		t.Fatalf("ValidateLayerDir(empty) = (%q, %v), want (\"\", nil)", got, err)
	}
}
