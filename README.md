# QCOW2 Golden Image Builder

[![Liberapay](https://img.shields.io/badge/Liberapay-Support_Astrona.io-F6C915?logo=liberapay&logoColor=black&style=for-the-badge)](https://liberapay.com/Astrona.io)

This repository automates the creation, customization, and distribution of
generic QEMU virtual machine templates (Golden Images) across multiple
architectures (`arm64` and `amd64`).

By default, it builds a lightweight base template, but the underlying mechanics
(cloud-init compilation, sysprep wiping, and ORAS distribution) are
universally applicable to modern Linux distributions.

## Overview

The pipeline is a single Go CLI, `astroimg` (`cmd/astroimg`), orchestrated
through distinct, reproducible phases:

1. **Configuration:** Auto-generates a local, ephemeral SSH keypair and
   renders `cloud-init` templates.

2. **Download:** Fetches the upstream cloud image, verifies it against the
   distro's published `SHA256SUMS`, and converts it into a pristine QCOW2
   base template.

3. **Execution:** Clones the base template into an instance disk and boots
   QEMU (native macOS HVF or Linux KVM acceleration) to install packages,
   waiting on `cloud-init status --wait` over SSH to know when provisioning
   has actually finished.

4. **Sysprep:** Wipes the instance disk's history (machine ID, SSH host
   keys, cloud-init logs, injected `authorized_keys`), turning it into a
   pristine "Golden Image".

5. **Distribution:** Pushes the finalized raw `.qcow2` artifact to an
   OCI-compliant registry with ORAS.

`astroimg` works the same locally and in CI. It builds the NoCloud seed ISO
without external tools, supports headless execution, and stores VM SSH host
keys in `build/known_hosts` instead of modifying your `~/.ssh/known_hosts`.

### Distros and Layers

Build a base image once, then layer additional customization on top of it
into a *new* qcow2 without re-running the OS install:

```bash
# 1. Build the ubuntu base (downloads the cloud image, boots, sysprep, package)
astroimg pipeline --distro ubuntu
# -> build/ubuntu-24.04-desktop-arm64.qcow2

# 2. Layer "docker" on top of that base into a separate image
astroimg pipeline --distro ubuntu --layer docker
# -> build/ubuntu-24.04-desktop-docker-arm64.qcow2
```

`astroimg pipeline` automates the complete image build process: prepare, download, create the ISO, run the VM, wait for cloud-init, sysprep, and finalize the image.

Layers are built on top of an existing image. Use `--layer-base-image` to select the base image, allowing multiple layers to be chained together.

To add a new distribution, create:

`distros/<name>/{user-data.template,meta-data,distro.yaml}`

Use `distros/ubuntu/` as a reference.

To see available distributions and layers:

- `astroimg get distro`
- `astroimg get layer`
- `astroimg get os-version`

### Image size: cleanup, compression, and layers

`astroimg` reduces image size using two approaches:

#### 1. Cleanup and compression

During `sysprep`, `astroimg` removes unnecessary files such as package caches, journal logs, and temporary files. It also trims unused disk space before creating the final image.

The final QCOW2 image is then compressed using `qemu-img`, reducing the size of the base image.

#### 2. Small layers using backing files

Layers use QCOW2 backing-file overlays instead of copying the entire base image. Only changes made by the layer are stored in the new QCOW2 file.

For example:

    Ubuntu base      ~7 GB
    └── Docker layer ~100 MB

The Docker layer references the Ubuntu base instead of containing another full copy of it.

### Layer portability

A layer using a backing file is **not a standalone image**. The base image must also be available for the layer to boot.

When pushing a layer with `astroimg push`, both files are included:

    image.qcow2    # Layer
    base.qcow2     # Base image

OCI registries deduplicate identical blobs by digest, so the same base image does not need to consume additional storage for every layer.

Push a layer with:

    astroimg push --distro ubuntu --layer docker

You can also publish the base separately for users who only need the base image:

    astroimg push --distro ubuntu

### Standalone layers

If you need a single QCOW2 file without a dependency on the base image, use `--flatten`:

    astroimg pipeline --distro ubuntu --layer docker --flatten

This produces a standalone image:

    ubuntu-24.04-desktop-docker-arm64.qcow2

Flattened images are easier to distribute but are larger because they include the base image data.

### Testing images without modifying them

Built images in `build/` are final artifacts and should not be booted directly. Booting a QCOW2 image can modify it, which would change the artifact you intend to distribute.

`astroimg` avoids this by creating a temporary copy-on-write overlay for testing. The temporary overlay is booted instead of the original image and removed afterward.

The original artifact remains unchanged.

### Test a local image

Use `astroimg test-boot` to test a locally built image:

    astroimg test-boot --distro ubuntu --layer docker

The command:

1. Creates a temporary overlay from the built image.
2. Boots the temporary image.
3. Provides SSH access for testing.
4. Removes the temporary image when stopped.

For headless testing with console output:

    astroimg test-boot --distro ubuntu --layer docker --headless --verbose

The image is already provisioned and sysprepped, so cloud-init provisioning is not run again. A temporary SSH key is injected only to provide access during testing.

### Test an image from an OCI registry

Use `astroimg test-oci` to verify that the image stored in the registry can actually be pulled and booted:

    astroimg test-oci --distro ubuntu --layer docker

The command:

1. Pulls the image from the OCI registry.
2. Pulls the required base image when using a non-flattened layer.
3. Verifies the image with `qemu-img info`.
4. Creates a temporary overlay.
5. Boots the image headless.
6. Verifies SSH access.
7. Removes the temporary overlay.

For flattened layers and base images, only the standalone QCOW2 image is required.

> Both commands require the image to already exist. Use `astroimg build` before `test-boot`, or `astroimg push` before `test-oci`.

### Build/install the CLI

```bash
just install          # builds, then installs to ~/.local/bin/astroimg (no sudo)
# or, without just:
go build -o bin/astroimg ./cmd/astroimg && mkdir -p ~/.local/bin && install -m 0755 bin/astroimg ~/.local/bin/astroimg

# or just build it locally without installing:
just build             # -> bin/astroimg
# or: go build -o bin/astroimg ./cmd/astroimg
```

Make sure `~/.local/bin` is on your `PATH` (default on most Linux distros;
on macOS you may need to add `export PATH="$HOME/.local/bin:$PATH"` to your
shell profile once).

The rest of this README assumes `astroimg` is on your `PATH`; substitute
`./bin/astroimg` if you built it locally instead of installing it.

---
## Quick Start

### Build an image

The easiest way to build images is with `pipeline`:

    astroimg pipeline --all

This loops every distro under `distros/*` and builds each one's base image,
then that distro's layers -- a layer never starts before its own distro's
base has finished. `pipeline` always needs either `--all` or an explicit
`--distro`; run bare with neither and it refuses instead of guessing one.

To build a single distro/layer instead of everything:

    astroimg pipeline --distro ubuntu --layer docker --arch amd64

### ARM64 and AMD64

`astroimg` automatically detects your host architecture and uses hardware acceleration when possible:

- HVF on macOS
- KVM on Linux

To build for a different architecture, use `--arch`:

    astroimg pipeline --distro ubuntu --arch amd64

> **Note:** Building for a different architecture uses software emulation and can be significantly slower.

### Build and push

`astroimg push` only uploads an existing artifact. It does not build the image.

Build the image first:

    astroimg pipeline --distro ubuntu --layer docker

Then push it:

    astroimg push --distro ubuntu --layer docker

If the image has not been built, `astroimg push` will fail and ask you to run `astroimg build` first.

### Headless mode

Use `--headless` to run without opening a VM window:

    astroimg run --headless

While running headless, `astroimg` prints progress information while waiting for SSH and cloud-init.

For additional VM console output, add `--verbose`:

    astroimg run --headless --verbose

### SSH access

When the VM starts, `astroimg run` prints the SSH command you can use to connect:

    ssh -i build/id_ed25519 -p 2222 -o UserKnownHostsFile=build/known_hosts ubuntu@localhost

SSH host keys are stored in `build/known_hosts`, so your personal `~/.ssh/known_hosts` file is not modified.

Default guest credentials match the distro name (`ssh_user`/`ssh_password` in
`distros/<distro>/distro.yaml`), e.g. `ubuntu`/`ubuntu`, `fedora`/`fedora`,
`opensuse`/`opensuse`.

### Publishing to GHCR manually (do this once before automating it in CI)

Walk through this by hand first so you know it actually works end to end
before `build-image.yml` does it unattended:

**1. Build the base:**
```bash
astroimg pipeline --distro ubuntu --headless --verbose
# -> build/ubuntu-24.04-desktop-arm64.qcow2
```

**2. Build the layer on top of it:**
```bash
astroimg pipeline --distro ubuntu --layer docker --headless --verbose
# -> build/ubuntu-24.04-desktop-docker-arm64.qcow2
```

**3. Sanity-check locally before touching the registry:**
```bash
astroimg test-boot --distro ubuntu --layer docker
# forks a disposable 3rd file, boots it, gives you SSH -- Ctrl-C when
# satisfied, the fork deletes itself, the artifact is untouched
```

**4. Log in to GHCR** (needs a GitHub token with `write:packages` scope):
```bash
echo $GITHUB_TOKEN | oras login ghcr.io -u YOUR_GITHUB_USERNAME --password-stdin
```

**5. Push both artifacts.** The layer push bundles the base qcow2 into its
own manifest automatically (so it's pullable/bootable standalone), but push
the base under its own tag too, for consumers who just want the base:
```bash
astroimg push --distro ubuntu
astroimg push --distro ubuntu --layer docker
```
Default ref is
`ghcr.io/<git-config-user.name>/<distro>-qcow2-image:<os-version>-<layer-or-"base">-<arch>`
(e.g. `ghcr.io/astrona-io/ubuntu-qcow2-image:24.04-docker-arm64`) --
override the namespace with `--gh-user` if that's wrong, `--registry` if
you're not using GHCR.

**6. Verify what's actually sitting in the registry**, not just your local
files -- this pulls the layer artifact fresh (base included) and boot-tests
the result:
```bash
astroimg test-oci --distro ubuntu --layer docker
```

**7. Package visibility:** a freshly-pushed GHCR package defaults to
**private**. `test-oci` still works (it pulls using your own logged-in
credentials), but anyone else pulling it needs the package made public (or
granted access) in the package's settings on github.com.

Once steps 1-6 work manually, `build-image.yml` (below) automates exactly
this same sequence unattended.

### Running in CI

`.github/workflows/build-image.yml` runs the same `astroimg pipeline`
headlessly on GitHub-hosted runners (native `arm64` and `amd64` runners, so
both legs get real KVM acceleration -- no slow TCG emulation). It's
`workflow_dispatch`-only by default since a full desktop install is slow;
wire up a schedule/push trigger yourself if you want that tradeoff. Trigger
it from the Actions tab, or:

```bash
gh workflow run build-image.yml -f distro=ubuntu -f layer= -f push=true
```

---

## Reference & Deep Dive

### How to Retrieve the Golden Image (Downstream Consumption)

Because the template is pushed as a raw OCI Artifact, consumers don't need
Docker to run it -- just the `oras` CLI to pull the raw file(s) straight to
disk. Every tag pulls to the same fixed filenames, regardless of
distro/version/layer/arch -- `image.qcow2` always, plus `base.qcow2` too
when the tag is a non-flattened layer (its `backing_file` already points at
`base.qcow2`, so it boots as pulled, no rebase step needed):

**1. Pull the disk (base image, in this example):**
```bash
oras pull ghcr.io/YOUR_GITHUB_USERNAME/ubuntu-qcow2-image:24.04-base-arm64
# -> image.qcow2
```
Pulling a layer tag instead (e.g. `:24.04-docker-arm64`) lands both
`image.qcow2` (the layer) and `base.qcow2` (what it's backed by) in one
pull.

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

Any tool that writes a plain ISO9660 volume labeled `cidata` containing
exactly those two files works -- e.g. on macOS:

```bash
mkdir -p cidata
cp user-data meta-data cidata/
hdiutil makehybrid -iso -joliet -default-volume-name cidata -o lab-cloud-init.iso cidata/

# Copy the QEMU EFI variables template so the VM can boot
cp /opt/homebrew/share/qemu/edk2-arm-vars.fd vars.fd
```

**5. Boot the VM:**
```bash
qemu-system-aarch64 \
    -M virt,highmem=on -accel hvf -cpu host -smp 4 -m 4096 \
    -drive if=pflash,format=raw,readonly=on,file=/opt/homebrew/share/qemu/edk2-aarch64-code.fd \
    -drive if=pflash,format=raw,file=vars.fd \
    -drive if=virtio,file=image.qcow2,format=qcow2 \
    -drive if=virtio,file=lab-cloud-init.iso,format=raw \
    -smbios type=1,serial=ds=nocloud \
    -device virtio-gpu-pci -display cocoa,show-cursor=on \
    -device virtio-mouse-pci -device virtio-keyboard-pci \
    -device virtio-net-pci,netdev=net0 -netdev user,id=net0,hostfwd=tcp::2222-:22
```
You can now log into the graphical desktop instantly as `labadmin` with the password `testpassword`!

---

### Hardware & Graphics Acceleration

`astroimg` selects QEMU flags per host/target architecture (see
`internal/platform`):
- `-accel hvf -cpu host` on macOS building for its own architecture.
- `-accel kvm -cpu host` on Linux building for its own architecture
  (including GitHub-hosted `arm64`/`amd64` runners, which expose `/dev/kvm`).
- Software emulation (`-cpu cortex-a57` / `-cpu qemu64`) otherwise.
- `-smp 4 -m 4096`: 4 CPU cores and 4GB of RAM.
- Interactive (non-headless) runs add
  `-device virtio-gpu-pci,xres=1920,yres=1080 -display
  cocoa,show-cursor=on,zoom-to-fit=on -device virtio-mouse-pci -device
  virtio-keyboard-pci`. `zoom-to-fit` makes the cocoa window resizable and
  scales the guest framebuffer to fit whatever size you drag it to --
  without it the window is stuck at a tiny, fixed, non-resizable size
  (especially bad on a Retina display). `xres`/`yres` just set a bigger
  starting size, not a hard limit.
- Headless runs (`--headless`, auto-enabled under `$CI` or non-macOS hosts)
  use `-display none` with the serial console redirected to a log file
  under `build/`.

### Network and SSH Ports

User-mode networking (`-netdev user`) forwards host port `2222` (override
with `--ssh-port`) to the guest's SSH port `22`. No bridge configuration or
root access is needed.
