#!/bin/sh
set -e

# OCI Container Entrypoint: Self-extracting and Self-documenting guide
# This script runs when a user pulls and runs the image via Docker.

echo "=========================================================================="
echo "          Ubuntu 24.04 LTS Desktop (ARM64 QEMU Base Template)             "
echo "=========================================================================="
echo ""

# Feature: Automatic Extraction
# If the user mounted a volume to /out, automatically copy the image there!
if [ -d "/out" ] && [ -w "/out" ]; then
    echo ">>> Extracting Ubuntu 24.04 LTS Desktop QCOW2..."
    cp /disk/ubuntu-24.04-desktop.qcow2 /out/ubuntu-24.04-desktop.qcow2
    echo ">>> Extraction complete! Saved to your host volume as: ubuntu-24.04-desktop.qcow2"
    echo ""
else
    echo ">>> Note: If you want to extract this virtual disk to your computer,"
    echo ">>> run this container with a volume mount:"
    echo ">>>   docker run --rm -v \"\$(pwd)\":/out <this-image>"
    echo ""
fi

echo "--- How to Boot this Image ---"
echo ""
echo "Credentials: username 'ubuntu' | password 'ubuntu'"
echo "             (Automatic graphical login is configured)"
echo ""
echo "1. On Apple Silicon Mac (ARM64 host with Hypervisor.framework):"
echo "   Ensure QEMU is installed (brew install qemu), then run:"
echo ""
echo "   qemu-system-aarch64 \\"
echo "     -M virt,highmem=on -accel hvf -cpu host -smp 4 -m 4096 \\"
echo "     -drive if=pflash,format=raw,readonly=on,file=/opt/homebrew/share/qemu/edk2-aarch64-code.fd \\"
echo "     -drive if=virtio,file=ubuntu-24.04-desktop.qcow2,format=qcow2 \\"
echo "     -device virtio-gpu-pci -display cocoa,show-cursor=on \\"
echo "     -device virtio-mouse-pci -device virtio-keyboard-pci \\"
echo "     -device virtio-net-pci,netdev=net0 -netdev user,id=net0,hostfwd=tcp::2222-:22"
echo ""
echo "2. On Linux (ARM64 host with KVM):"
echo "   Ensure QEMU and KVM are installed, then run:"
echo ""
echo "   qemu-system-aarch64 \\"
echo "     -M virt -accel kvm -cpu host -smp 4 -m 4096 \\"
echo "     -bios /usr/share/qemu/edk2-aarch64-code.fd \\"
echo "     -drive if=virtio,file=ubuntu-24.04-desktop.qcow2,format=qcow2 \\"
echo "     -device virtio-gpu-pci -display gtk,show-cursor=on \\"
echo "     -device virtio-mouse-pci -device virtio-keyboard-pci \\"
echo "     -device virtio-net-pci,netdev=net0 -netdev user,id=net0,hostfwd=tcp::2222-:22"
echo ""
echo "=========================================================================="
