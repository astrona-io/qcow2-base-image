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
# Give every build a unique instance-id. cloud-init only runs per-instance
# modules (users, ssh_authorized_keys, packages, power_state, ...) once per
# instance-id, so re-using a static id causes cloud-init to silently skip
# re-provisioning (e.g. injecting a freshly regenerated SSH key) on VMs
# whose instance disk already booted once before.
INSTANCE_ID="ubuntu-24-04-desktop-base-$(date +%s)"
sed "s/^instance-id:.*/instance-id: ${INSTANCE_ID}/" "$TEMPLATE_DIR/meta-data" > "$BUILD_DIR/meta-data"
echo "   ✅ Compiled meta-data with instance-id=${INSTANCE_ID}."

echo "🚀 Preparation Complete!"
