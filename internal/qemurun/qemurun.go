// Package qemurun builds and drives the QEMU VM: process argv construction,
// waiting for the guest's SSH port, host-key pinning scoped to a
// project-local known_hosts file (never the user's real ~/.ssh/known_hosts),
// and detecting "provisioning finished" via `cloud-init status --wait` over
// SSH instead of a fixed time heuristic. Every external command is invoked
// with an explicit argv slice (exec.CommandContext) -- nothing is ever
// built as a shell string.
package qemurun

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/astrona-io/qcow2-base-image/internal/platform"
)

// Config describes one QEMU launch.
type Config struct {
	QEMU          platform.QEMUConfig
	InstanceDisk  string
	CloudInitISO  string
	VarsFile      string
	SMP           int // default 4
	MemoryMB      int // default 4096
	SSHPort       int // default 2222
	Headless      bool
	SerialLogPath string // only used when Headless
}

// BuildArgs constructs the qemu-system-* argv (excluding the binary name
// itself). Pure function, no I/O, so it's fully unit-testable.
func BuildArgs(cfg Config) []string {
	smp := cfg.SMP
	if smp == 0 {
		smp = 4
	}

	mem := cfg.MemoryMB
	if mem == 0 {
		mem = 4096
	}

	sshPort := cfg.SSHPort
	if sshPort == 0 {
		sshPort = 2222
	}

	args := []string{"-M", cfg.QEMU.Machine}
	args = append(args, cfg.QEMU.AccelArgs...)
	args = append(args,
		"-smp", strconv.Itoa(smp),
		"-m", strconv.Itoa(mem),
		"-drive", "if=pflash,format=raw,readonly=on,file="+cfg.QEMU.EFICode,
		"-drive", "if=pflash,format=raw,file="+cfg.VarsFile,
		"-drive", "if=virtio,file="+cfg.InstanceDisk+",format=qcow2",
		"-drive", "if=virtio,file="+cfg.CloudInitISO+",format=raw",
		"-smbios", "type=1,serial=ds=nocloud",
		"-device", "virtio-net-pci,netdev=net0",
		"-netdev", fmt.Sprintf("user,id=net0,hostfwd=tcp::%d-:22", sshPort),
	)

	if cfg.Headless {
		args = append(args, platform.DisplayArgs(true, cfg.SerialLogPath)...)
	} else {
		args = append(args,
			"-device", "virtio-gpu-pci",
			"-device", "virtio-mouse-pci",
			"-device", "virtio-keyboard-pci",
		)
		args = append(args, platform.DisplayArgs(false, "")...)
	}

	return args
}

// Start launches qemu-system-* and returns immediately with a handle whose
// Wait() blocks until the VM process exits. The process is killed if ctx is
// canceled.
func Start(ctx context.Context, binary string, args []string) (*exec.Cmd, error) {
	cmd := exec.CommandContext(ctx, binary, args...) //nolint:gosec // argv-only, binary/args come from platform detection + resolved config, never shell-interpolated
	cmd.Stdout = os.Stdout

	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting %s: %w", binary, err)
	}

	return cmd, nil
}

// TailFile streams newly-appended bytes from path to w, polling until ctx
// is done. It tolerates the file not existing yet (QEMU creates the serial
// log lazily on first write), retrying until it appears. Used to surface
// the guest's serial console in --headless --verbose mode, where there's
// no GUI window to watch instead.
func TailFile(ctx context.Context, path string, w io.Writer) error {
	var (
		f   *os.File
		err error
	)

	for f == nil {
		f, err = os.Open(path) //nolint:gosec // path is internally constructed (build/<tag>-console.log)
		if err == nil {
			break
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	defer func() { _ = f.Close() }()

	buf := make([]byte, 4096)

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		n, readErr := f.Read(buf)
		if n > 0 {
			_, _ = w.Write(buf[:n])
		}

		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				return fmt.Errorf("tailing %s: %w", path, readErr)
			}

			select {
			case <-ctx.Done():
				return nil
			case <-time.After(300 * time.Millisecond):
			}
		}
	}
}

// WaitForPort blocks until host:port accepts a TCP connection or ctx is done.
func WaitForPort(ctx context.Context, host string, port int, interval time.Duration) error {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	for {
		conn, err := (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, "tcp4", addr)
		if err == nil {
			_ = conn.Close()
			return nil
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("waiting for %s: %w", addr, ctx.Err())
		case <-time.After(interval):
		}
	}
}

func ensureFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND, 0o600) //nolint:gosec // path is internally constructed (build/known_hosts or SSH key path), not raw user input
	if err != nil {
		return fmt.Errorf("creating %s: %w", path, err)
	}

	return f.Close()
}

// RemoveHostKey drops any stale entry for hostPort (e.g. "[localhost]:2222")
// from a project-local known_hosts file. Best-effort: an absent entry is not
// an error, matching the old Makefile's `|| true`.
func RemoveHostKey(ctx context.Context, knownHostsFile, hostPort string) error {
	if err := ensureFile(knownHostsFile); err != nil {
		return err
	}

	cmd := exec.CommandContext(ctx, "ssh-keygen", "-R", hostPort, "-f", knownHostsFile) //nolint:gosec // argv-only; hostPort/knownHostsFile are internally constructed, not raw user input
	_ = cmd.Run()

	return nil
}

// pinHostKeyOnce runs a single ssh-keyscan attempt and appends any result to
// knownHostsFile.
func pinHostKeyOnce(ctx context.Context, knownHostsFile string, port int, perAttemptTimeout time.Duration) error {
	attemptCtx, cancel := context.WithTimeout(ctx, perAttemptTimeout)
	defer cancel()

	cmd := exec.CommandContext(attemptCtx, "ssh-keyscan", "-4", "-T", "2", "-p", strconv.Itoa(port), "-H", "localhost") //nolint:gosec // argv-only, fixed flags plus an internally-resolved port number

	out, err := cmd.Output()
	if err != nil || len(bytes.TrimSpace(out)) == 0 {
		return fmt.Errorf("ssh-keyscan on port %d returned no host key", port)
	}

	f, err := os.OpenFile(knownHostsFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600) //nolint:gosec // knownHostsFile is internally resolved, not raw user input
	if err != nil {
		return fmt.Errorf("opening %s: %w", knownHostsFile, err)
	}
	defer func() { _ = f.Close() }()

	if _, err := f.Write(out); err != nil {
		return fmt.Errorf("writing %s: %w", knownHostsFile, err)
	}

	return nil
}

// WaitAndPinHostKey retries ssh-keyscan against localhost:port every 2s
// until it succeeds or maxWait elapses, appending the captured host key to
// a project-local known_hosts file (never the user's real ~/.ssh/known_hosts).
// It prints a heartbeat every ~30s so a long wait (headless mode has no GUI
// to watch) doesn't look stuck.
func WaitAndPinHostKey(ctx context.Context, knownHostsFile string, port int, maxWait time.Duration) error {
	if err := ensureFile(knownHostsFile); err != nil {
		return err
	}

	deadlineCtx, cancel := context.WithTimeout(ctx, maxWait)
	defer cancel()

	start := time.Now()

	for attempt := 0; ; attempt++ {
		if err := pinHostKeyOnce(deadlineCtx, knownHostsFile, port, 2*time.Second); err == nil {
			return nil
		}

		if attempt > 0 && attempt%15 == 0 {
			fmt.Printf("   ...still waiting for SSH host key (%ds elapsed)\n", int(time.Since(start).Seconds()))
		}

		select {
		case <-deadlineCtx.Done():
			return fmt.Errorf("gave up waiting for SSH host key on port %d after %s", port, maxWait)
		case <-time.After(2 * time.Second):
		}
	}
}

// SSHArgs builds the argv for an `ssh` invocation scoped to a project-local
// known_hosts file with strict host-key checking enabled -- the pinned key
// is actually verified, unlike the old sysprep step which disabled checking
// entirely.
func SSHArgs(keyPath, knownHostsFile string, port int, user, host string, remoteCmd ...string) []string {
	args := []string{
		"-i", keyPath,
		"-p", strconv.Itoa(port),
		"-o", "UserKnownHostsFile=" + knownHostsFile,
		"-o", "StrictHostKeyChecking=yes",
		"-o", "LogLevel=ERROR",
		user + "@" + host,
	}

	return append(args, remoteCmd...)
}

// RunSSH executes a remote command over SSH and returns its combined output.
func RunSSH(ctx context.Context, keyPath, knownHostsFile string, port int, user, host string, remoteCmd ...string) ([]byte, error) {
	args := SSHArgs(keyPath, knownHostsFile, port, user, host, remoteCmd...)
	cmd := exec.CommandContext(ctx, "ssh", args...) //nolint:gosec // argv-only, args are built by SSHArgs from internally-resolved values

	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("ssh %s: %w (output: %s)", strings.Join(remoteCmd, " "), err, bytes.TrimSpace(out))
	}

	return out, nil
}

// RunSSHStreaming executes a remote command over SSH, streaming its output
// live to stdout/stderr instead of buffering it. Used for long-running
// remote commands (like `cloud-init status --wait`, which itself prints
// periodic progress) so a multi-minute wait shows activity instead of
// looking stuck -- this matters most in --headless mode, where there's no
// GUI console to watch instead.
func RunSSHStreaming(ctx context.Context, keyPath, knownHostsFile string, port int, user, host string, remoteCmd ...string) error {
	args := SSHArgs(keyPath, knownHostsFile, port, user, host, remoteCmd...)
	cmd := exec.CommandContext(ctx, "ssh", args...) //nolint:gosec // argv-only, args are built by SSHArgs from internally-resolved values
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ssh %s: %w", strings.Join(remoteCmd, " "), err)
	}

	return nil
}

// WaitForCloudInit first retries a trivial SSH command until authentication
// succeeds (sshd comes up well before cloud-init has written
// ssh_authorized_keys, so early auth failures are expected transient
// state), printing a heartbeat every ~15s, then streams
// `cloud-init status --wait` live -- cloud-init itself only returns from
// that once provisioning has actually finished (replacing the old
// fixed-heuristic "SSH is reachable" proxy), and it prints its own periodic
// progress while waiting, which streaming surfaces instead of hiding.
func WaitForCloudInit(ctx context.Context, keyPath, knownHostsFile string, port int, user, host string, timeout time.Duration) error {
	deadlineCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var lastErr error

	start := time.Now()

	for attempt := 0; ; attempt++ {
		attemptCtx, cancelAttempt := context.WithTimeout(deadlineCtx, 15*time.Second)
		_, err := RunSSH(attemptCtx, keyPath, knownHostsFile, port, user, host, "true")

		cancelAttempt()

		if err == nil {
			break
		}

		lastErr = err

		if attempt > 0 && attempt%5 == 0 {
			fmt.Printf("   ...still waiting for SSH auth (%ds elapsed)\n", int(time.Since(start).Seconds()))
		}

		select {
		case <-deadlineCtx.Done():
			return fmt.Errorf("SSH auth never became ready: %w", lastErr)
		case <-time.After(3 * time.Second):
		}
	}

	fmt.Println("SSH ready, waiting for cloud-init to finish provisioning...")

	if err := RunSSHStreaming(deadlineCtx, keyPath, knownHostsFile, port, user, host, "cloud-init", "status", "--wait"); err != nil {
		return fmt.Errorf("waiting for cloud-init to finish: %w", err)
	}

	return nil
}

// GenerateSSHKey creates a new ed25519 keypair at path if one doesn't
// already exist, reusing it across builds like the old prepare.sh did.
func GenerateSSHKey(ctx context.Context, path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
	}

	cmd := exec.CommandContext(ctx, "ssh-keygen", "-t", "ed25519", "-f", path, "-N", "", "-C", "astroimg-vm-key", "-q") //nolint:gosec // argv-only, path is internally resolved, not raw user input
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ssh-keygen: %w (%s)", err, bytes.TrimSpace(out))
	}

	return nil
}

// SysprepRemoteCommand is the fixed (no interpolated input) shell command
// run on the guest to wipe cloud-init state, host SSH keys, and the
// injected authorized_keys before the disk is sealed as a golden image.
const SysprepRemoteCommand = "sudo cloud-init clean --logs --machine-id && " +
	"sudo rm -f /home/ubuntu/.ssh/authorized_keys && " +
	"sudo rm -f /etc/ssh/ssh_host_* && " +
	"sudo sync && sudo halt"
