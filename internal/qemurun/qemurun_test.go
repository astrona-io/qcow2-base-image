package qemurun

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/astrona-io/qcow2-base-image/internal/platform"
)

func TestBuildArgsHeadless(t *testing.T) {
	cfg := Config{
		QEMU: platform.QEMUConfig{
			Machine:   "virt,highmem=on",
			AccelArgs: []string{"-accel", "hvf", "-cpu", "host"},
			EFICode:   "/fw/code.fd",
		},
		InstanceDisk:  "build/instance.qcow2",
		CloudInitISO:  "build/ci.iso",
		VarsFile:      "build/vars.fd",
		Headless:      true,
		SerialLogPath: "build/console.log",
	}
	args := BuildArgs(cfg)
	joined := strings.Join(args, " ")

	for _, want := range []string{
		"-M virt,highmem=on",
		"-accel hvf -cpu host",
		"-smp 4",
		"-m 4096",
		"if=pflash,format=raw,readonly=on,file=/fw/code.fd",
		"if=virtio,file=build/instance.qcow2,format=qcow2",
		"if=virtio,file=build/ci.iso,format=raw",
		"hostfwd=tcp::2222-:22",
		"-display none",
		"file:build/console.log",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("args missing %q; got: %s", want, joined)
		}
	}

	if strings.Contains(joined, "virtio-gpu-pci") {
		t.Error("headless args should not include GPU/mouse/keyboard devices")
	}
}

func TestBuildArgsGUI(t *testing.T) {
	cfg := Config{
		QEMU:         platform.QEMUConfig{Machine: "q35", AccelArgs: []string{"-cpu", "qemu64"}, EFICode: "/fw/code.fd"},
		InstanceDisk: "build/instance.qcow2",
		CloudInitISO: "build/ci.iso",
		VarsFile:     "build/vars.fd",
		SSHPort:      3333,
	}
	args := BuildArgs(cfg)
	joined := strings.Join(args, " ")

	for _, want := range []string{"-display cocoa,show-cursor=on", "virtio-gpu-pci", "hostfwd=tcp::3333-:22"} {
		if !strings.Contains(joined, want) {
			t.Errorf("args missing %q; got: %s", want, joined)
		}
	}
}

func TestSSHArgs(t *testing.T) {
	args := SSHArgs("build/id_ed25519", "build/known_hosts", 2222, "ubuntu", "localhost", "cloud-init", "status", "--wait")

	joined := strings.Join(args, " ")
	for _, want := range []string{
		"-i build/id_ed25519",
		"-p 2222",
		"-o UserKnownHostsFile=build/known_hosts",
		"-o StrictHostKeyChecking=yes",
		"ubuntu@localhost",
		"cloud-init status --wait",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("ssh args missing %q; got: %s", want, joined)
		}
	}
}

func testListenerPort(ctx context.Context, t *testing.T) (*net.TCPListener, int) {
	t.Helper()

	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	tcpLn, ok := ln.(*net.TCPListener)
	if !ok {
		t.Fatalf("expected *net.TCPListener, got %T", ln)
	}

	addr, ok := tcpLn.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("expected *net.TCPAddr, got %T", tcpLn.Addr())
	}

	return tcpLn, addr.Port
}

func TestWaitForPortSucceedsOnceListening(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	ln, port := testListenerPort(ctx, t)
	defer func() { _ = ln.Close() }()

	if err := WaitForPort(ctx, "127.0.0.1", port, 50*time.Millisecond); err != nil {
		t.Errorf("expected success, got %v", err)
	}
}

func TestWaitForPortTimesOutWhenNothingListening(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	// Grab a port and immediately close it so nothing is listening.
	ln, port := testListenerPort(ctx, t)
	if err := ln.Close(); err != nil {
		t.Fatalf("closing listener: %v", err)
	}

	if err := WaitForPort(ctx, "127.0.0.1", port, 50*time.Millisecond); err == nil {
		t.Error("expected timeout error, got nil")
	}
}
