#!/usr/bin/env bash

set -euo pipefail

# -----------------------------------------------------------------------------
# prepare.sh - Injects environment variables into templates and stages files
# -----------------------------------------------------------------------------

BUILD_DIR="build"
TEMPLATE_DIR="logic/cloud-init"

echo "🔑 Step 1: Generating Local SSH Key..."
SSH_KEY_PATH="$BUILD_DIR/id_ed25519"

if [[ ! -f "$SSH_KEY_PATH" ]]; then
    echo "   ✨ Generating new Ed25519 SSH keypair for the VM at $SSH_KEY_PATH..."
    ssh-keygen -t ed25519 -f "$SSH_KEY_PATH" -N "" -C "ubuntu-vm-key" -q
else
    echo "   ✅ Using existing SSH keypair at $SSH_KEY_PATH."
fi

SSH_KEY=$(cat "${SSH_KEY_PATH}.pub")

echo "📄 Step 2: Compiling $BUILD_DIR/user-data..."
# Export so envsubst can read it. We only substitute SSH_KEY to avoid breaking bash scripts inside the yaml.
export SSH_KEY
envsubst '${SSH_KEY}' < "$TEMPLATE_DIR/user-data.template" > "$BUILD_DIR/user-data"
echo "   ✅ Compiled user-data successfully."

echo "📝 Step 3: Staging $BUILD_DIR/meta-data..."
cp "$TEMPLATE_DIR/meta-data" "$BUILD_DIR/meta-data"
echo "   ✅ Copied meta-data successfully."

echo "💿 Step 4: Provisioning $BUILD_DIR/vars.fd..."
BREW_QEMU_DIR="/opt/homebrew/share/qemu"
if [[ -f "$BREW_QEMU_DIR/edk2-arm-vars.fd" ]]; then
    cp "$BREW_QEMU_DIR/edk2-arm-vars.fd" "$BUILD_DIR/vars.fd"
    echo "   ✅ Copied edk2-arm-vars.fd to $BUILD_DIR/vars.fd."
else
    echo "   ⚠️ Warning: QEMU EFI variables file not found at $BREW_QEMU_DIR/edk2-arm-vars.fd"
    echo "   🔨 Creating an empty vars.fd (this might trigger a QEMU warning but should work)."
    touch "$BUILD_DIR/vars.fd"
fi

echo "🚀 Preparation Complete!"
