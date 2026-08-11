# QCOW2 Golden Image Builder

[![Liberapay](https://img.shields.io/badge/Liberapay-Support_Astrona.io-F6C915?logo=liberapay&logoColor=black&style=for-the-badge)](https://liberapay.com/Astrona.io)

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

### Image size: cleanup, compression, and small layers

A freshly-installed Ubuntu desktop base easily lands around 7G. `astroimg`
keeps that down in two independent ways:

**1. Sysprep cleanup + compression (shrinks the base itself).** `sysprep`
now also runs `apt-get clean`, drops `/var/lib/apt/lists/*`, vacuums the
journal, clears `/tmp`, and runs `fstrim -av` before halting -- paired with
`discard=unmap` on the VM's disk drive, this actually punches the freed
blocks out of the qcow2 file instead of leaving them allocated. `build` then
runs `qemu-img convert -O qcow2 -c` (zlib compression) into the final
artifact instead of a plain rename. Freeing space before compressing
matters a lot: discarded/zeroed blocks compress far better than
freed-but-still-allocated ones.

**2. Backing-file overlays for layers (keeps layers small).** A layer's
instance disk is no longer a full copy of the base -- it's created as a
qcow2 **backing-file overlay** (`qemu-img create -b <base> -F qcow2`):
unmodified blocks are read straight from the base at boot, and only new or
changed blocks (e.g. installing `docker.io`) get written into the overlay
itself. `build` compresses that overlay while *preserving* the backing-file
reference, so the final layer artifact is genuinely just the delta -- a
base at 7G plus a docker layer might only add ~100M, not another 7G file.

**The tradeoff you need to know**: a backing-file overlay is **not a
standalone image**. It stores a path reference to its base and won't boot
without that exact base file present alongside it. `build` records that
reference as a *relative* filename (not an absolute build-machine path), so
it resolves correctly as long as both files end up in the same directory --
which is exactly what happens next:

- `astroimg push` for a layer built without `--flatten` bundles the base
  qcow2 into the layer's own OCI manifest as a second blob (registries
  dedupe identical blobs by digest, so this doesn't cost extra storage per
  layer). One `astroimg push --distro ubuntu --layer docker` is enough --
  no separate base push required for that layer to be pullable and
  bootable on its own.
- Push the base under its own tag too (`astroimg push --distro ubuntu`) if
  you also want base-only consumers to be able to pull it standalone.
- If you need a single, fully self-contained file instead (simpler
  distribution, larger file, no bundling), pass `--flatten` to
  `build`/`pipeline` for that layer: it folds the base's data into the
  output instead of keeping the backing-file reference, trading the size
  win for portability.

```bash
astroimg pipeline --distro ubuntu --layer docker --flatten
# -> a standalone ubuntu-24.04-desktop-docker-arm64.qcow2, no base needed to boot it
```

### Testing a built image without ever mutating it

A finished artifact (`build/<tag>-<arch>.qcow2`) is meant to be the shipped
product -- never booted directly, since any write would mutate the file
you're about to distribute. Both test commands below solve this the same
way: fork a disposable copy-on-write overlay from the artifact, boot *that*,
delete it when done. The artifact itself never changes -- boot it a hundred
times and it stays byte-identical (verify with `md5`/`sha256sum` before and
after if you don't believe it).

**`astroimg test-boot`** -- test a local artifact interactively:

```bash
astroimg test-boot --distro ubuntu --layer docker
# forks build/ubuntu-24.04-desktop-docker-arm64.qcow2 into a throwaway
# overlay (a 3-file chain: fork -> layer -> base), boots it, prints an SSH
# command, and deletes the fork on Ctrl-C
```

No packages get installed and no `runcmd` runs -- the artifact is already
fully provisioned and sysprepped. The fork only gets a fresh SSH key
injected (sysprep wiped the old one) so you can log in and poke around.
Add `--headless --verbose` to boot it without a GUI window and stream the
console instead.

**`astroimg test-oci`** -- confirm what's actually sitting in your registry
works, not just what's on disk locally:

```bash
astroimg test-oci --distro ubuntu --layer docker
# 1. pulls the layer artifact from GHCR -- for a non-flattened layer this
#    manifest already bundles the base qcow2 too (see "Backing-file
#    overlays for layers" above), so one pull lands both files together
#    and the layer's relative backing_file reference resolves as-is
# 2. verifies it with `qemu-img info`
# 3. forks a throwaway overlay from it, boots it headless, confirms SSH
#    login actually works, then deletes the fork
```

For a flattened layer or a base image (`--layer` omitted), it's the same
one-pull, verify, boot-test sequence -- there's just no second blob to
bundle since the artifact is already self-contained.

Both commands need `astroimg build` (or `push`, for `test-oci`) to have
already produced the artifact you're pointing them at.

---

## Prerequisites

- **Go** 1.26+ (only to build the CLI itself).
- **just** (optional): `brew install just` -- convenience wrapper for
  building/installing the CLI and running tests/lint. Not required; plain
  `go build`/`go install` work too.
- **QEMU**: `brew install qemu` (macOS) / `apt install qemu-system-x86 ovmf`
  or `qemu-system-arm qemu-efi-aarch64` (Linux, matching your target arch).
- **ORAS CLI** (only needed for `push`/`test-oci`): `brew install oras`.
- **Git**: used to default the registry namespace for `push`/`test-oci` if
  `--gh-user` isn't passed -- this repo's GitHub org/user from `git remote
  get-url origin` (e.g. `astrona-io`), falling back to the `gh` CLI's
  logged-in username if there's no GitHub remote.

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
astroimg build         # compress the sysprepped disk into its final release name
astroimg test-boot     # fork the finished artifact and boot it, without mutating it
astroimg push          # push the artifact to the registry with ORAS
astroimg test-oci      # pull it back down and boot-test it
```

`push` never builds anything itself -- it only ships whatever's already at
`build/<image-tag>-<arch>.qcow2`. Run `build` (directly, or via `pipeline`,
which ends with a `build` step) first; `push` fails fast with "run
'astroimg build' first" if that file isn't there yet.

```bash
astroimg pipeline --distro ubuntu --layer docker   # runs prepare/download/iso/run/sysprep/build
astroimg push      --distro ubuntu --layer docker
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
Default ref is `ghcr.io/<git-config-user.name>/<image-tag>:<arch>` --
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
