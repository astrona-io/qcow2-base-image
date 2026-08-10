// Package platform detects the QEMU binary, machine type, acceleration
// flags, EFI firmware paths, and default display mode for the host running
// astroimg, given the architecture being targeted. It replaces the Makefile's
// uname-based ifeq blocks.
package platform

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// Supported target architectures.
const (
	ArchARM64 = "arm64"
	ArchAMD64 = "amd64"
)

const (
	flagAccel   = "-accel"
	flagDisplay = "-display"
	flagCPU     = "-cpu"
)

// ValidateArch checks that arch is one astroimg knows how to build for.
func ValidateArch(arch string) error {
	switch arch {
	case ArchARM64, ArchAMD64:
		return nil
	default:
		return fmt.Errorf("unsupported architecture %q (want %q or %q)", arch, ArchARM64, ArchAMD64)
	}
}

// QEMUConfig is everything needed to launch qemu-system-* for a given target
// architecture on the current host.
type QEMUConfig struct {
	Binary    string
	Machine   string
	AccelArgs []string // e.g. ["-accel", "hvf", "-cpu", "host"]
	EFICode   string   // read-only firmware
	EFIVars   string   // template vars file to copy per-build
}

// BinaryAndMachine returns the qemu-system-* binary name and -M machine
// string for the target architecture. Pure function, no I/O.
func BinaryAndMachine(targetArch string) (binary, machine string, err error) {
	switch targetArch {
	case ArchARM64:
		return "qemu-system-aarch64", "virt,highmem=on", nil
	case ArchAMD64:
		return "qemu-system-x86_64", "q35", nil
	default:
		return "", "", fmt.Errorf("unsupported architecture %q", targetArch)
	}
}

// SelectAccel returns the QEMU acceleration/cpu flags for running
// targetArch on a host with hostOS/hostArch. Pure function, no I/O.
func SelectAccel(hostOS, hostArch, targetArch string) []string {
	if hostArch == targetArch {
		if hostOS == "darwin" {
			return []string{flagAccel, "hvf", flagCPU, "host"}
		}

		return []string{flagAccel, "kvm", flagCPU, "host"}
	}
	// Cross-architecture emulation: no hardware acceleration available.
	if targetArch == ArchARM64 {
		return []string{flagCPU, "cortex-a57"}
	}

	return []string{flagCPU, "qemu64"}
}

// efiCandidates lists, in preference order, where each OS's package
// managers commonly install EFI firmware for a given target architecture.
// The first pair that both exist on disk is used.
func efiCandidates(hostOS, targetArch string) []struct{ code, vars string } {
	switch hostOS {
	case "darwin":
		if targetArch == ArchARM64 {
			return []struct{ code, vars string }{
				{"/opt/homebrew/share/qemu/edk2-aarch64-code.fd", "/opt/homebrew/share/qemu/edk2-arm-vars.fd"},
				{"/usr/local/share/qemu/edk2-aarch64-code.fd", "/usr/local/share/qemu/edk2-arm-vars.fd"},
			}
		}

		return []struct{ code, vars string }{
			{"/opt/homebrew/share/qemu/edk2-x86_64-code.fd", "/opt/homebrew/share/qemu/edk2-i386-vars.fd"},
			{"/usr/local/share/qemu/edk2-x86_64-code.fd", "/usr/local/share/qemu/edk2-i386-vars.fd"},
		}
	default: // linux and other unix-likes
		if targetArch == ArchARM64 {
			return []struct{ code, vars string }{
				{"/usr/share/AAVMF/AAVMF_CODE.fd", "/usr/share/AAVMF/AAVMF_VARS.fd"},
				{"/usr/share/qemu/edk2-aarch64-code.fd", "/usr/share/qemu/edk2-arm-vars.fd"},
			}
		}

		return []struct{ code, vars string }{
			{"/usr/share/OVMF/OVMF_CODE.fd", "/usr/share/OVMF/OVMF_VARS.fd"},
			{"/usr/share/OVMF/OVMF_CODE_4M.fd", "/usr/share/OVMF/OVMF_VARS_4M.fd"},
			{"/usr/share/qemu/edk2-x86_64-code.fd", "/usr/share/qemu/edk2-i386-vars.fd"},
		}
	}
}

// FindEFI locates EFI firmware for targetArch on hostOS. Returns an error
// listing every candidate tried if none are found, so a missing-firmware
// failure is immediately actionable (matches the old Makefile's explicit
// "install qemu" hint).
func FindEFI(hostOS, targetArch string) (code, vars string, err error) {
	candidates := efiCandidates(hostOS, targetArch)

	var tried []string
	for _, c := range candidates {
		tried = append(tried, c.code)
		if fileExists(c.code) && fileExists(c.vars) {
			return c.code, c.vars, nil
		}
	}

	return "", "", fmt.Errorf("no EFI firmware found for %s/%s, tried: %v (install qemu / OVMF for your platform)", hostOS, targetArch, tried)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// Detect builds the full QEMUConfig for running targetArch on a host
// identified by hostOS/hostArch (inject runtime.GOOS/runtime.GOARCH in
// production; fixed values in tests).
func Detect(hostOS, hostArch, targetArch string) (QEMUConfig, error) {
	if err := ValidateArch(targetArch); err != nil {
		return QEMUConfig{}, err
	}

	binary, machine, err := BinaryAndMachine(targetArch)
	if err != nil {
		return QEMUConfig{}, err
	}

	code, vars, err := FindEFI(hostOS, targetArch)
	if err != nil {
		return QEMUConfig{}, err
	}

	return QEMUConfig{
		Binary:    binary,
		Machine:   machine,
		AccelArgs: SelectAccel(hostOS, hostArch, targetArch),
		EFICode:   code,
		EFIVars:   vars,
	}, nil
}

// DetectForHost is Detect using the real running host.
func DetectForHost(targetArch string) (QEMUConfig, error) {
	return Detect(runtime.GOOS, runtime.GOARCH, targetArch)
}

// Headless decides whether QEMU should run without a GUI window: explicit
// always wins; otherwise CI environments and non-macOS hosts default to
// headless, macOS interactive use defaults to the GUI window.
func Headless(explicit *bool) bool {
	if explicit != nil {
		return *explicit
	}

	if os.Getenv("CI") != "" {
		return true
	}

	return runtime.GOOS != "darwin"
}

// DisplayArgs returns the QEMU display-related arguments. serialLogPath is
// only used in headless mode, where the serial console is redirected to a
// file so boot/cloud-init output stays inspectable without a GUI.
func DisplayArgs(headless bool, serialLogPath string) []string {
	if headless {
		return []string{flagDisplay, "none", "-serial", "file:" + filepath.Clean(serialLogPath)}
	}

	// zoom-to-fit makes the cocoa window resizable and scales the guest
	// framebuffer to fit whatever size you drag it to -- without it the
	// window is fixed at the guest's native (low) resolution, which looks
	// tiny and can't be resized at all, especially on a Retina display.
	return []string{flagDisplay, "cocoa,show-cursor=on,zoom-to-fit=on"}
}
