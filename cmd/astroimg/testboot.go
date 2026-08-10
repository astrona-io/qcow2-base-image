package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/astrona-io/qcow2-base-image/internal/cloudinit"
	"github.com/astrona-io/qcow2-base-image/internal/iso"
	"github.com/astrona-io/qcow2-base-image/internal/platform"
	"github.com/astrona-io/qcow2-base-image/internal/qemurun"
)

// testBootUserData is a minimal cloud-config for booting an already-built,
// already-sysprepped artifact: no packages, no runcmd, just re-inject an
// SSH key (sysprep wiped the old one) so the fork is reachable. Reusing
// the "ubuntu" user cloud-init already created is enough -- the software
// is already installed, this is a boot verification, not a provisioning run.
const testBootUserData = `#cloud-config
users:
  - name: ubuntu
    ssh_authorized_keys:
      - ${SSH_KEY}
`

const testBootMetaData = `instance-id: placeholder
local-hostname: test-boot
`

var testBootCmd = &cobra.Command{
	Use:   "test-boot",
	Short: "Fork a finished qcow2 artifact into a throwaway overlay and boot it, without ever mutating the artifact",
	Long: `Creates a disposable copy-on-write overlay backed by the built artifact
(build/<tag>-<arch>.qcow2), boots that overlay instead of the artifact
itself, and deletes the overlay when you're done -- so you can boot the
same shipped image as many times as you like and it never accumulates
writes. For a layer, this forks a 3-file chain: throwaway fork -> layer
artifact -> base artifact.`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		r, _, err := resolveBuild(cmd)
		if err != nil {
			return err
		}

		artifactPath := filepath.Join(r.BuildDir, r.FinalImageName)
		if _, err := os.Stat(artifactPath); err != nil {
			return fmt.Errorf("%s not found, run 'astroimg build' first: %w", artifactPath, err)
		}

		headless := platform.Headless(headlessOverride(cmd))

		fork, err := newTestFork(cmd.Context(), r.BuildDir, artifactPath, r.Arch, flagSSHPort, headless, flagVerbose)
		if err != nil {
			return err
		}
		defer fork.cleanup()

		fmt.Println("test-boot running. Ctrl-C to stop and discard the fork.")

		return fork.qemuCmd.Wait()
	},
}

// testFork is a disposable copy-on-write overlay booted from an
// already-built artifact, plus everything needed to reach it over SSH.
// cleanup() always removes every file it created, so the artifact it forked
// from is never mutated.
type testFork struct {
	forkPath, isoPath, varsPath, serialLogPath string
	keyPath, knownHostsFile                    string
	sshPort                                    int
	qemuCmd                                    *exec.Cmd
}

func (f *testFork) cleanup() {
	if f.qemuCmd != nil && f.qemuCmd.Process != nil {
		if err := f.qemuCmd.Process.Kill(); err != nil {
			fmt.Println("cleanup: killing qemu:", err)
		}

		if err := f.qemuCmd.Wait(); err != nil {
			fmt.Println("cleanup: qemu exited with:", err)
		}
	}

	for _, p := range []string{f.forkPath, f.isoPath, f.varsPath, f.serialLogPath} {
		if p == "" {
			continue
		}

		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			fmt.Printf("cleanup: removing %s: %v\n", p, err)
		}
	}
}

// newTestFork forks artifactPath into a throwaway overlay under buildDir,
// attaches a minimal SSH-only cloud-init seed, boots it, and waits for its
// SSH host key. The caller must call fork.cleanup() when done.
func newTestFork(ctx context.Context, buildDir, artifactPath, arch string, sshPort int, headless, verbose bool) (*testFork, error) {
	qcfg, err := platform.DetectForHost(arch)
	if err != nil {
		return nil, err
	}

	tag := fmt.Sprintf("fork-%s-%d", strings.TrimSuffix(filepath.Base(artifactPath), ".qcow2"), time.Now().Unix())

	fork := &testFork{
		forkPath:       filepath.Join(buildDir, tag+".qcow2"),
		isoPath:        filepath.Join(buildDir, tag+"-cloud-init.iso"),
		varsPath:       filepath.Join(buildDir, tag+"-vars.fd"),
		serialLogPath:  filepath.Join(buildDir, tag+"-console.log"),
		keyPath:        filepath.Join(buildDir, "id_ed25519"),
		knownHostsFile: filepath.Join(buildDir, "known_hosts"),
		sshPort:        sshPort,
	}

	if err := createOverlayDisk(ctx, artifactPath, fork.forkPath); err != nil {
		fork.cleanup()
		return nil, err
	}

	if err := qemurun.GenerateSSHKey(ctx, fork.keyPath); err != nil {
		fork.cleanup()
		return nil, err
	}

	pub, err := os.ReadFile(fork.keyPath + ".pub")
	if err != nil {
		fork.cleanup()
		return nil, err
	}

	userData := cloudinit.Substitute(testBootUserData, map[string]string{"SSH_KEY": strings.TrimSpace(string(pub))})

	instanceID := cloudinit.GenerateInstanceID("test-boot", time.Now)

	metaData, err := cloudinit.RenderMetaData(testBootMetaData, instanceID)
	if err != nil {
		fork.cleanup()
		return nil, err
	}

	if err := iso.WriteCIData(fork.isoPath, []byte(userData), []byte(metaData)); err != nil {
		fork.cleanup()
		return nil, err
	}

	if err := copyFile(qcfg.EFIVars, fork.varsPath); err != nil {
		fork.cleanup()
		return nil, err
	}

	qrCfg := qemurun.Config{
		QEMU:          qcfg,
		InstanceDisk:  fork.forkPath,
		CloudInitISO:  fork.isoPath,
		VarsFile:      fork.varsPath,
		SSHPort:       sshPort,
		Headless:      headless,
		SerialLogPath: fork.serialLogPath,
	}
	args := qemurun.BuildArgs(qrCfg)

	hostPort := fmt.Sprintf("[localhost]:%d", sshPort)
	_ = qemurun.RemoveHostKey(ctx, fork.knownHostsFile, hostPort)

	fmt.Printf("forked %s -> %s (throwaway, deleted on exit)\n", artifactPath, fork.forkPath)

	cmd, err := qemurun.Start(ctx, qcfg.Binary, args)
	if err != nil {
		fork.cleanup()
		return nil, err
	}

	fork.qemuCmd = cmd

	if headless && verbose {
		go func() { _ = qemurun.TailFile(ctx, fork.serialLogPath, os.Stdout) }()
	}

	fmt.Println("waiting for VM SSH host key...")

	// Same 10-minute budget as `run`'s own boot wait (see startVM):
	// systemd-networkd-wait-online alone can burn ~90-120s on images built
	// before the wait-online mask fix, and a layer's multi-level backing
	// chain (fork -> layer -> compressed base) adds real decompression
	// overhead on top of that.
	if err := qemurun.WaitAndPinHostKey(ctx, fork.knownHostsFile, sshPort, 10*time.Minute); err != nil {
		fork.cleanup()
		return nil, fmt.Errorf("fork never came up: %w", err)
	}

	fmt.Printf("SSH: ssh -i %s -p %d -o UserKnownHostsFile=%s ubuntu@localhost\n", fork.keyPath, sshPort, fork.knownHostsFile)

	return fork, nil
}

// verifySSH confirms the fork is actually reachable and accepting logins
// over SSH, not just that its port is open.
func (f *testFork) verifySSH(ctx context.Context) error {
	if _, err := qemurun.RunSSH(ctx, f.keyPath, f.knownHostsFile, f.sshPort, "ubuntu", "localhost", "true"); err != nil {
		return fmt.Errorf("SSH login to fork failed: %w", err)
	}

	return nil
}
