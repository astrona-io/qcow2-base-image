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
- **Docker/Podman**: Container runtime to package and push the template to GHCR.
- **Git**: Configured to resolve your username for the registry image name.

---

## Quick Start

You can build, customize, boot, and package the template using the automated `Makefile` targets.

### 1. Prepare Configuration
Scan your host for SSH keys and generate the local VM configuration:
```bash
make prepare
```
This script:
* Reads your `~/.ssh/id_ed25519.pub` (or `~/.ssh/id_rsa.pub`) and injects it into the `user-data` cloud-init file.
* Configures automatic graphical login into the desktop interface as user `ubuntu`.
* Sets the default user password to `ubuntu` (required for unlocking screens or performing administrative tasks).

### 2. Download Base Image
Fetch the official upstream Ubuntu 24.04 cloud image:
```bash
make download
```

### 3. Generate Cloud-Init ISO
Package the metadata into an ISO 9660 volume labeled `cidata`:
```bash
make cloud-init
```

### 4. Resize the Disk
Before installing a full desktop environment, you must expand the base virtual disk. Expand the image to 25GB:
```bash
make resize
```

### 5. Boot the VM and Install Desktop
Run the virtual machine. A native Cocoa window will open, and cloud-init will begin installing the minimal desktop automatically on first boot:
```bash
make run
```
* **First Boot Duration:** Because it is downloading and installing the GNOME graphical desktop packages, the first boot and configuration step will take several minutes. You can monitor the progress on-screen or watch cloud-init logs.
* **Credentials:**
  * **Username:** `ubuntu`
  * **Password:** `ubuntu`
  * *(Automatic graphical login is configured, so it will log in to the desktop directly).*
* **Terminal/SSH Access:** You can also connect via SSH from your standard host terminal while the VM is running:
  ```bash
  ssh -p 2222 ubuntu@localhost
  ```

### 6. Package as OCI Container
Once you have shut down the VM and are satisfied with the customized base image (`ubuntu-24.04-desktop.qcow2`), build the container image:
```bash
make build
```

### 7. Test and Verify the OCI Container
Before pushing your image to the public registry, you should verify its validity and ensure it was packaged without any corruption:
```bash
make test-oci
```
This automated test will:
1. Confirm the local OCI container image exists.
2. Extract the QCOW2 virtual disk from the OCI container into a separate `test-extract` directory using a safe container copying mechanism (no mounting required).
3. Validate the integrity and format of the extracted virtual disk using `qemu-img info`.

If the script outputs `Verification Successful!`, the container is fully functional and ready to ship.

### 8. Push to OCI Registry
Push your new template directly to your public registry (defaults to GHCR):
```bash
# Log in to your registry first (e.g., GHCR)
echo $CR_PAT | docker login ghcr.io -u YOUR_GITHUB_USERNAME --password-stdin

# Push the container
make push GH_USER=YOUR_GITHUB_USERNAME
```

---

## Reference & Deep Dive

### How the OCI Shipping Method Works

Instead of needing complex third-party CLI tools like `oras` to upload non-container artifacts, this approach utilizes a **scratch Dockerfile** (also known as a container-disk pattern popular in tools like KubeVirt).

The `Dockerfile` is simple:
```dockerfile
FROM scratch
COPY ubuntu-24.04-desktop.qcow2 /disk/ubuntu-24.04-desktop.qcow2
```

This packages the QCOW2 image inside a standard, ultra-lightweight OCI layer. It is fully supported by standard container registries (GHCR, Docker Hub, ECR, etc.) and can be managed using standard container security scan tools, version tags, and CI pipelines (such as GitHub Actions).

### How to Retrieve/Extract the QCOW2 Template

We've designed the OCI image to be completely self-documenting and self-extracting for your consumers. They do not need to memorize complex CLI tools or lookup `qemu` flags online.

To use the template on another machine, the consumer simply runs:

```bash
docker run --rm -v "$(pwd)":/out ghcr.io/YOUR_GITHUB_USERNAME/ubuntu-24.04-qemu-desktop:latest
```

This single command will:
1. Download the template from your registry.
2. Detect the mounted `/out` volume and **automatically copy the `ubuntu-24.04-desktop.qcow2` virtual disk** to the consumer's current working directory.
3. Print a beautiful terminal guide with the exact, copy-pasteable KVM (Linux) and HVF (macOS) `qemu-system-aarch64` commands required to boot the template natively!

### Hardware & Graphics Acceleration (macOS HVF)

The QEMU commands are optimized to run with hypervisor acceleration and graphical devices native to macOS:
- `-accel hvf`: Activates macOS Hypervisor.framework.
- `-cpu host`: Exposes Apple Silicon CPU features to the guest.
- `-smp 4 -m 4096`: Allocates 4 CPU cores and 4GB of RAM (essential for a smooth desktop experience).
- `-device virtio-gpu-pci -display cocoa,show-cursor=on`: Initiates hardware-accelerated GPU emulation mapped to a native macOS Cocoa window display.
- `-device virtio-mouse-pci -device virtio-keyboard-pci`: Redirects input devices using standard high-performance VirtIO drivers.

### Network and SSH Ports

The network configuration uses User-mode networking (`-netdev user`) to direct host port `2222` to the guest VM SSH port `22`. No bridge configuration or root access is needed on your Mac.
