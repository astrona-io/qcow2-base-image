# QEMU Ubuntu 24.04 LTS Desktop Template (ARM64)

This project contains automation files and a guide to build a customized, minimal Ubuntu 24.04 LTS (Noble Numbat) Desktop virtual machine base image (QCOW2 format) on an Apple Silicon Mac, run it locally with QEMU (using native hardware acceleration and graphical display), and package/ship it as a container image to GitHub Container Registry (GHCR) or any other OCI-compliant registry.

## Overview

This repository automates the creation of a **Desktop + Terminal Only** Ubuntu template by utilizing Ubuntu's official minimal desktop meta-package (`ubuntu-desktop-minimal`). This avoids pre-installing bloatware (like office suites, media players, or games), leaving you with a lightweight graphical desktop environment, a file manager, and a terminal.

## Directory Structure

This project uses a clean, modular architecture separating templates, build logic, and output artifacts:

* `logic/cloud-init/`: Contains the base YAML configurations (`user-data.template`, `meta-data`).
* `logic/scripts/`: Bash scripts for environment injection and automation (e.g., `prepare.sh`).
* `logic/oci/`: Contains the `Dockerfile` and self-extracting `entrypoint.sh` for container packaging.
* `build/`: A git-ignored directory generated dynamically. It holds all transient artifacts, downloaded disks, ISOs, and compiled configuration files.
* `Makefile`: The single entry point to orchestrate the pipeline.

---

## Prerequisites

Ensure you have the following installed on your macOS machine:

- **QEMU**: Complete emulator and virtualizer.
  ```bash
  brew install qemu
  ```
- **ORAS CLI**: The OCI Registry As Storage tool to natively push/pull raw artifacts to GHCR.
  ```bash
  brew install oras
  ```
- **Git**: Configured to resolve your username for the registry image name.

---

## Quick Start

You can build, customize, boot, and package the template using the automated `Makefile` targets.

### The Fast Track
If you just want to run the entire pipeline end-to-step and boot the VM immediately, use the unified test command:
```bash
make test-run
```
This will automatically execute **Prepare**, **Download**, **Cloud-Init**, and **Run** in sequence.

### Cross-Architecture Support (ARM64 & AMD64)

This project natively supports building Golden Images for both Apple Silicon (`arm64`) and Intel/AMD (`amd64`). The `Makefile` will automatically detect your host architecture and download the correct Ubuntu image, map the correct QEMU binary, and apply native hardware acceleration (HVF on macOS, KVM on Linux).

If you want to cross-compile an image for a different architecture (e.g., building an `amd64` image while sitting on an Apple Silicon M1 Mac), simply append the `ARCH` variable to your make commands:

```bash
# Build the AMD64 template on an ARM64 Mac
make test-run ARCH=amd64

# Push the AMD64 template to the registry
make push ARCH=amd64
```

> **Warning:** Running cross-architecture builds utilizes software emulation (`TCG` vs `HVF/KVM`). Because we are installing a graphical desktop environment, software emulation will be *significantly* slower during the 10-minute `cloud-init` installation phase.

---

### Step-by-Step Execution

#### 1. Prepare Configuration
Generate a dedicated local SSH key and compile the VM configuration:
```bash
make prepare
```
This script:
* Generates a new, dedicated SSH keypair inside the `build/` directory (`build/id_ed25519`) and injects it into the `user-data` cloud-init file.
* Configures automatic graphical login into the desktop interface as user `ubuntu`.
* Sets the default user password to `ubuntu` (required for unlocking screens or performing administrative tasks).

### 2. Download and Convert Base Image
Fetch the official upstream Ubuntu 24.04 image, convert it, and automatically pre-resize it into a pristine `base-ubuntu.qcow2` template (25GB):
```bash
make download
```

### 3. Generate Cloud-Init ISO
Package the metadata into an ISO 9660 volume labeled `cidata`:
```bash
make cloud-init
```

### 4. Boot and Customize the VM Instance
Run the virtual machine. This command automatically spawns a fresh `instance.qcow2` copied from your pristine `base-ubuntu.qcow2` so your base remains completely untouched:
```bash
make run
```
* **First Boot Duration:** Because it is downloading and installing the GNOME graphical desktop packages, the first boot and configuration step will take several minutes. You can monitor the progress on-screen or watch cloud-init logs.
* **Credentials:**
  * **Username:** `ubuntu`
  * **Password:** `ubuntu`
  * *(Automatic graphical login is configured, so it will log in to the desktop directly).*
* **Terminal/SSH Access:** You can also connect via SSH from your standard host terminal while the VM is running by using the custom key generated in the `build/` folder:
  ```bash
  ssh -i build/id_ed25519 -p 2222 ubuntu@localhost
  ```

### 5. Seal the Golden Image (Sysprep)
To make your VM disk reusable by other developers or test labs, you must erase its memory of your specific SSH keys and initial setup. While the VM is still running, open a new terminal and run:
```bash
make sysprep
```
This command automatically connects via SSH, wipes the `cloud-init` logs, deletes your temporary SSH keys, resets the machine ID, and securely powers down the VM.

### 6. Prepare Artifact for Distribution
Once the VM has shut down from the sysprep command, finalize the artifact. This target will automatically rename `instance.qcow2` into the final release-ready `ubuntu-24.04-desktop.qcow2`:
```bash
make build
```

### 7. Push as an OCI Artifact
Instead of wrapping the disk in a Docker container layer, we use **ORAS** to push the raw `.qcow2` file directly to the GitHub Container Registry as an OCI Artifact.

```bash
# Log in to your registry first (e.g., GHCR) using ORAS
echo $CR_PAT | oras login ghcr.io -u YOUR_GITHUB_USERNAME --password-stdin

# Push the raw QCOW2 file directly to GHCR
make push GH_USER=YOUR_GITHUB_USERNAME
```

### 8. Test and Verify the Push
After pushing, you can verify that the artifact can be successfully pulled down and isn't corrupted:
```bash
make test-oci
```
This will use `oras pull` to download the file into a temporary directory and verify its structure with `qemu-img`.

---

## Reference & Deep Dive

### How to Retrieve the Golden Image (Downstream Consumption)

Because we pushed the template as a raw OCI Artifact, consumers don't need Docker to run it. They just need the `oras` CLI to pull the raw file straight to their hard drive.

Here is an example of how a downstream test lab would consume your Golden Image:

**1. Pull the raw `.qcow2` disk:**
```bash
oras pull ghcr.io/YOUR_GITHUB_USERNAME/ubuntu-24.04-qemu-desktop:latest
```

**2. Create a new Test Lab `user-data`:**
Create a file named `user-data` that creates a new admin user called `labadmin`:
```yaml
#cloud-config
users:
  - name: labadmin
    sudo: ALL=(ALL) NOPASSWD:ALL
    shell: /bin/bash
    lock_passwd: false
chpasswd:
  list: |
    labadmin:testpassword
  expire: false
ssh_pwauth: true
```

**3. Create a new `meta-data`:**
Create a file named `meta-data`:
```yaml
instance-id: test-lab-vm
local-hostname: test-lab
```

**4. Generate the Cloud-Init ISO & EFI Variables:**
```bash
# Package the metadata into a local ISO
mkdir -p cidata
cp user-data meta-data cidata/
hdiutil makehybrid -iso -joliet -default-volume-name cidata -o lab-cloud-init.iso cidata/

# Copy the QEMU EFI variables template so the VM can boot
cp /opt/homebrew/share/qemu/edk2-arm-vars.fd vars.fd
```

**5. Boot the VM:**
Now boot the extracted Golden Image with the new lab configurations:
```bash
qemu-system-aarch64 \
    -M virt,highmem=on -accel hvf -cpu host -smp 4 -m 4096 \
    -drive if=pflash,format=raw,readonly=on,file=/opt/homebrew/share/qemu/edk2-aarch64-code.fd \
    -drive if=pflash,format=raw,file=vars.fd \
    -drive if=virtio,file=ubuntu-24.04-desktop.qcow2,format=qcow2 \
    -drive if=virtio,file=lab-cloud-init.iso,format=raw \
    -smbios type=1,serial=ds=nocloud \
    -device virtio-gpu-pci -display cocoa,show-cursor=on \
    -device virtio-mouse-pci -device virtio-keyboard-pci \
    -device virtio-net-pci,netdev=net0 -netdev user,id=net0,hostfwd=tcp::2222-:22
```
You can now log into the graphical desktop instantly as `labadmin` with the password `testpassword`!

### Hardware & Graphics Acceleration (macOS HVF)

The QEMU commands are optimized to run with hypervisor acceleration and graphical devices native to macOS:
- `-accel hvf`: Activates macOS Hypervisor.framework.
- `-cpu host`: Exposes Apple Silicon CPU features to the guest.
- `-smp 4 -m 4096`: Allocates 4 CPU cores and 4GB of RAM (essential for a smooth desktop experience).
- `-device virtio-gpu-pci -display cocoa,show-cursor=on`: Initiates hardware-accelerated GPU emulation mapped to a native macOS Cocoa window display.
- `-device virtio-mouse-pci -device virtio-keyboard-pci`: Redirects input devices using standard high-performance VirtIO drivers.

### Network and SSH Ports

The network configuration uses User-mode networking (`-netdev user`) to direct host port `2222` to the guest VM SSH port `22`. No bridge configuration or root access is needed on your Mac.
