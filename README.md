# QCOW2 Golden Image Builder

This repository automates the creation, customization, and distribution of
generic QEMU virtual machine templates (Golden Images) across multiple
architectures (`arm64` and `amd64`), on macOS, Linux, and in GitHub Actions.

By default, it builds a lightweight **Ubuntu 24.04 LTS (Noble Numbat)**
Desktop template, but the underlying mechanics (cloud-init compilation,
sysprep wiping, and ORAS distribution) are universally applicable to modern
Linux distributions.

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

`astroimg` runs identically at the command line and inside CI: it builds the
NoCloud seed ISO with a pure-Go ISO9660 writer (no `hdiutil`/`genisoimage`
dependency), can run fully headless (`--headless`, auto-enabled when `$CI`
is set), and never touches your real `~/.ssh/known_hosts` -- VM host keys
are pinned to a project-local `build/known_hosts` instead.

## Directory Structure

* `cmd/astroimg/`: the CLI's cobra commands.
* `internal/`: the CLI's packages -- `config` (distro/layer name resolution),
  `platform` (arch/accel/EFI/display detection), `cloudinit` (template
  rendering), `iso` (pure-Go ISO9660 writer), `imagefetch` (download +
  checksum verification), `qemurun` (QEMU process, SSH wait, cloud-init
  wait), `orasclient` (push/pull).
* `distros/<name>/`: per-distro base config -- `user-data.template`,
  `meta-data`, and `distro.yaml` (version, release codename, image URL
  template, checksum URL template). Select with `--distro <name>` (default
  `ubuntu`).
* `layers/<name>/`: optional add-on config applied on top of an
  already-built base image instead of a fresh download -- same
  `user-data.template`/`meta-data` shape, but the template only needs the
  delta (extra packages, `runcmd`, re-injected SSH key). Select with
  `--layer <name>`.
* `build/`: a git-ignored directory generated dynamically. Holds all
  downloaded disks, ISOs, generated SSH keys, the project-local
  `known_hosts`, and compiled configuration files.
* `Justfile`: `just build` / `just install` build or install the `astroimg`
  binary; `just test` / `just lint` run the Go test suite and linters. All
  actual pipeline commands are `astroimg` subcommands (see below), not
  Justfile recipes.
* `.github/workflows/build-image.yml`: manual-dispatch CI pipeline.

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

`astroimg pipeline` runs prepare, download, iso, run, waits for cloud-init to
finish, syspreps, and finalizes the artifact -- fully automated, no manual
"open a second terminal for sysprep" step required (that's still available
via the standalone `run`/`sysprep` commands for interactive local use).

A layer boots the base's finished image (`--layer-base-image`, defaults to
the non-layer final image) with a fresh cloud-init instance-id, so
cloud-init re-runs even though the disk was already sysprepped. Chain
layers by pointing `--layer-base-image build/<previous-layer>.qcow2` at
another layer's output.

To add a new distro, create `distros/<name>/{user-data.template,meta-data,distro.yaml}`
following `distros/ubuntu/` as a template. `astroimg list distros` /
`astroimg list layers` show what's available.

---

## Prerequisites

- **Go** 1.26+ (only to build the CLI itself).
- **just** (optional): `brew install just` -- convenience wrapper for
  building/installing the CLI and running tests/lint. Not required; plain
  `go build`/`go install` work too.
- **QEMU**: `brew install qemu` (macOS) / `apt install qemu-system-x86 ovmf`
  or `qemu-system-arm qemu-efi-aarch64` (Linux, matching your target arch).
- **ORAS CLI** (only needed for `push`/`test-oci`): `brew install oras`.
- **Git**: used to default the registry namespace (`git config user.name`)
  for `push`/`test-oci` if `--gh-user` isn't passed.

No `hdiutil`/`genisoimage`/`xorriso` needed -- the ISO is built in pure Go.

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

### The Fast Track

```bash
astroimg pipeline
```

Runs the entire pipeline (prepare, download, iso, run, wait for cloud-init,
sysprep, build) for the default distro/arch. Add flags as needed, e.g.
`astroimg pipeline --distro ubuntu --layer docker --arch amd64`.

### Cross-Architecture Support (ARM64 & AMD64)

`astroimg` auto-detects your host architecture and applies native hardware
acceleration (HVF on macOS, KVM on Linux) when the target matches. Building
for the other architecture works via `--arch`:

```bash
# Build the amd64 template on an Apple Silicon Mac
astroimg pipeline --arch amd64
```

> **Note:** Cross-architecture builds fall back to software emulation (TCG).
> Because a graphical desktop environment is being installed, this is
> *significantly* slower during the package-installation phase.

### Individual steps

Every pipeline phase is also its own command, useful for interactive local
work (watch the GUI, run `sysprep` from another terminal yourself):

```bash
astroimg prepare       # generate SSH key + render cloud-init user-data/meta-data
astroimg download      # fetch + checksum-verify the upstream image, build the base disk
astroimg iso           # package the NoCloud seed ISO
astroimg run           # boot the VM and leave it running
# ... in another terminal, once cloud-init finishes ...
astroimg sysprep       # wipe cloud-init/SSH state and halt the guest
astroimg build         # rename the sysprepped disk to its final release name
astroimg push          # push the artifact to the registry with ORAS
astroimg test-oci      # pull it back down and verify it with qemu-img
```

In `--headless` mode there's no GUI window to watch, so `run`/`pipeline`
print a heartbeat every ~15-30s while waiting on the SSH host key and on
cloud-init so it doesn't look stuck, and stream `cloud-init status --wait`'s
own progress output live once SSH is up. Add `--verbose` to also tail the
guest's serial console (boot messages, package-install output) straight to
your terminal:

```bash
astroimg run --headless --verbose
```

`astroimg run` prints the SSH command it's using (scoped to
`build/known_hosts`, never your real one):

```bash
ssh -i build/id_ed25519 -p 2222 -o UserKnownHostsFile=build/known_hosts ubuntu@localhost
```

Credentials inside the guest: username `ubuntu`, password `ubuntu`.

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
Docker to run it -- just the `oras` CLI to pull the raw file straight to
disk.

**1. Pull the raw `.qcow2` disk:**
```bash
oras pull ghcr.io/YOUR_GITHUB_USERNAME/ubuntu-24.04-desktop-arm64:arm64
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
    -drive if=virtio,file=ubuntu-24.04-desktop-arm64.qcow2,format=qcow2 \
    -drive if=virtio,file=lab-cloud-init.iso,format=raw \
    -smbios type=1,serial=ds=nocloud \
    -device virtio-gpu-pci -display cocoa,show-cursor=on \
    -device virtio-mouse-pci -device virtio-keyboard-pci \
    -device virtio-net-pci,netdev=net0 -netdev user,id=net0,hostfwd=tcp::2222-:22
```
You can now log into the graphical desktop instantly as `labadmin` with the password `testpassword`!

---

## 💖 Support & Sponsoring

If you find this project helpful for your infrastructure, test labs, or daily workflow, please consider supporting the development! Your sponsorship helps maintain and expand these open-source tools.

You can support the project via Liberapay:

[![Liberapay](https://img.shields.io/badge/Liberapay-Support_Astrona.io-F6C915?logo=liberapay&logoColor=black&style=for-the-badge)](https://liberapay.com/Astrona.io)

### Hardware & Graphics Acceleration

`astroimg` selects QEMU flags per host/target architecture (see
`internal/platform`):
- `-accel hvf -cpu host` on macOS building for its own architecture.
- `-accel kvm -cpu host` on Linux building for its own architecture
  (including GitHub-hosted `arm64`/`amd64` runners, which expose `/dev/kvm`).
- Software emulation (`-cpu cortex-a57` / `-cpu qemu64`) otherwise.
- `-smp 4 -m 4096`: 4 CPU cores and 4GB of RAM.
- Interactive (non-headless) runs add
  `-device virtio-gpu-pci -display cocoa,show-cursor=on -device
  virtio-mouse-pci -device virtio-keyboard-pci`.
- Headless runs (`--headless`, auto-enabled under `$CI` or non-macOS hosts)
  use `-display none` with the serial console redirected to a log file
  under `build/`.

### Network and SSH Ports

User-mode networking (`-netdev user`) forwards host port `2222` (override
with `--ssh-port`) to the guest's SSH port `22`. No bridge configuration or
root access is needed.
