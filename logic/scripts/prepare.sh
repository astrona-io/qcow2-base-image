#!/usr/bin/env bash

set -euo pipefail

# -----------------------------------------------------------------------------
# prepare.sh - Injects environment variables into templates and stages files
# -----------------------------------------------------------------------------

BUILD_DIR="build"
TEMPLATE_DIR="logic/cloud-init"

echo "=== Step 1: Locating Host SSH Public Key ==="
SSH_KEY=""
for key_file in "$HOME/.ssh/id_ed25519.pub" "$HOME/.ssh/id_rsa.pub"; do
    if [[ -f "$key_file" ]]; then
        SSH_KEY=$(cat "$key_file")
        echo "Found SSH key: $key_file"
        break
    fi
done

if [[ -z "$SSH_KEY" ]]; then
    echo "Warning: No standard SSH public key found (~/.ssh/id_ed25519.pub or ~/.ssh/id_rsa.pub)."
    echo "Using a fallback placeholder SSH key."
    SSH_KEY="ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAAAgQDh7F5x... placeholder-key"
fi

echo "=== Step 2: Compiling $BUILD_DIR/user-data ==="
# Export so envsubst can read it. We only substitute SSH_KEY to avoid breaking bash scripts inside the yaml.
export SSH_KEY
envsubst '${SSH_KEY}' < "$TEMPLATE_DIR/user-data.template" > "$BUILD_DIR/user-data"
echo "Compiled user-data successfully."

echo "=== Step 3: Staging $BUILD_DIR/meta-data ==="
cp "$TEMPLATE_DIR/meta-data" "$BUILD_DIR/meta-data"
echo "Copied meta-data successfully."

echo "=== Step 4: Provisioning $BUILD_DIR/vars.fd ==="
BREW_QEMU_DIR="/opt/homebrew/share/qemu"
if [[ -f "$BREW_QEMU_DIR/edk2-arm-vars.fd" ]]; then
    cp "$BREW_QEMU_DIR/edk2-arm-vars.fd" "$BUILD_DIR/vars.fd"
    echo "Copied edk2-arm-vars.fd to $BUILD_DIR/vars.fd."
else
    echo "Warning: QEMU EFI variables file not found at $BREW_QEMU_DIR/edk2-arm-vars.fd"
    echo "Creating an empty vars.fd (this might trigger a QEMU warning but should work)."
    touch "$BUILD_DIR/vars.fd"
fi

echo "=== Preparation Complete ==="
