#!/usr/bin/env bash

set -euo pipefail

# -----------------------------------------------------------------------------
# prepare.sh - Injects environment variables into templates and stages files
#
# Env vars (set by Makefile):
#   BUILD_DIR           default: build
#   TEMPLATE_DIR        required. distros/<distro> or layers/<layer>
#   OUT_USER_DATA       default: $BUILD_DIR/user-data
#   OUT_META_DATA       default: $BUILD_DIR/meta-data
#   INSTANCE_ID_PREFIX  default: base
# -----------------------------------------------------------------------------

BUILD_DIR="${BUILD_DIR:-build}"
TEMPLATE_DIR="${TEMPLATE_DIR:?TEMPLATE_DIR must be set (e.g. distros/ubuntu or layers/docker)}"
OUT_USER_DATA="${OUT_USER_DATA:-$BUILD_DIR/user-data}"
OUT_META_DATA="${OUT_META_DATA:-$BUILD_DIR/meta-data}"
INSTANCE_ID_PREFIX="${INSTANCE_ID_PREFIX:-base}"

echo "🔑 Step 1: Generating Local SSH Key..."
SSH_KEY_PATH="$BUILD_DIR/id_ed25519"

if [[ ! -f "$SSH_KEY_PATH" ]]; then
    echo "   ✨ Generating new Ed25519 SSH keypair for the VM at $SSH_KEY_PATH..."
    ssh-keygen -t ed25519 -f "$SSH_KEY_PATH" -N "" -C "vm-key" -q
else
    echo "   ✅ Using existing SSH keypair at $SSH_KEY_PATH."
fi

SSH_KEY=$(cat "${SSH_KEY_PATH}.pub")

echo "📄 Step 2: Compiling $OUT_USER_DATA from $TEMPLATE_DIR/user-data.template..."
# Export so envsubst can read it. We only substitute SSH_KEY to avoid breaking bash scripts inside the yaml.
export SSH_KEY
envsubst '${SSH_KEY}' < "$TEMPLATE_DIR/user-data.template" > "$OUT_USER_DATA"
echo "   ✅ Compiled $OUT_USER_DATA successfully."

echo "📝 Step 3: Staging $OUT_META_DATA..."
# Give every build a unique instance-id. cloud-init only runs per-instance
# modules (users, ssh_authorized_keys, packages, power_state, ...) once per
# instance-id, so re-using a static id causes cloud-init to silently skip
# re-provisioning (e.g. injecting a freshly regenerated SSH key) on VMs
# whose instance disk already booted once before. This matters even more for
# layers, which boot an already-sysprepped base disk and rely on a fresh
# instance-id to force cloud-init to run again.
INSTANCE_ID="${INSTANCE_ID_PREFIX}-$(date +%s)"
sed "s/^instance-id:.*/instance-id: ${INSTANCE_ID}/" "$TEMPLATE_DIR/meta-data" > "$OUT_META_DATA"
echo "   ✅ Compiled $OUT_META_DATA with instance-id=${INSTANCE_ID}."

echo "🚀 Preparation Complete!"
