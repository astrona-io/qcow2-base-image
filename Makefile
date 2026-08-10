# Makefile for building, running, and shipping QEMU Ubuntu Desktop Templates

# --- Architecture Detection & Configuration ---
HOST_ARCH := $(shell uname -m)
ifeq ($(HOST_ARCH),x86_64)
	HOST_ARCH = amd64
endif
ifeq ($(HOST_ARCH),aarch64)
	HOST_ARCH = arm64
endif
ifeq ($(HOST_ARCH),arm64)
	HOST_ARCH = arm64
endif

ARCH ?= $(HOST_ARCH)
OS := $(shell uname -s)

# --- OS Configuration ---
OS_DISTRO ?= ubuntu
OS_VERSION ?= 24.04
OS_RELEASE ?= noble

# Architecture-specific QEMU configurations
ifeq ($(ARCH),arm64)
	QEMU_BIN = qemu-system-aarch64
	QEMU_MACHINE = virt,highmem=on
	EFI_CODE = /opt/homebrew/share/qemu/edk2-aarch64-code.fd
	EFI_VARS_SRC = /opt/homebrew/share/qemu/edk2-arm-vars.fd
else ifeq ($(ARCH),amd64)
	QEMU_BIN = qemu-system-x86_64
	QEMU_MACHINE = q35
	EFI_CODE = /opt/homebrew/share/qemu/edk2-x86_64-code.fd
	EFI_VARS_SRC = /opt/homebrew/share/qemu/edk2-i386-vars.fd
else
	$(error Unsupported architecture: $(ARCH))
endif

# Hardware Acceleration vs Emulation Logic
ifeq ($(HOST_ARCH),$(ARCH))
	# Native execution
	ifeq ($(OS),Darwin)
		QEMU_ACCEL = -accel hvf -cpu host
	else
		QEMU_ACCEL = -accel kvm -cpu host
	endif
else
	# Cross-architecture emulation
	ifeq ($(ARCH),arm64)
		QEMU_ACCEL = -cpu cortex-a57
	else
		QEMU_ACCEL = -cpu qemu64
	endif
endif

BUILD_DIR = build
FINAL_IMAGE_NAME = $(OS_DISTRO)-$(OS_VERSION)-desktop-$(ARCH).qcow2
INSTANCE_DISK = $(BUILD_DIR)/instance-$(ARCH).qcow2
BASE_DISK = $(BUILD_DIR)/base-$(OS_DISTRO)-$(ARCH).qcow2

IMAGE_URL = https://cloud-images.ubuntu.com/$(OS_RELEASE)/current/$(OS_RELEASE)-server-cloudimg-$(ARCH).img
DOWNLOADED_IMG = $(BUILD_DIR)/$(OS_RELEASE)-server-cloudimg-$(ARCH).img

REGISTRY ?= ghcr.io
GH_USER ?= $(shell git config user.name 2>/dev/null || echo "your-username")
OCI_IMAGE ?= $(REGISTRY)/$(GH_USER)/$(OS_DISTRO)-$(OS_VERSION)-qemu-desktop:$(ARCH)

VARS_FILE = $(BUILD_DIR)/vars-$(ARCH).fd
CLOUD_INIT_ISO = $(BUILD_DIR)/cloud-init.iso

TEST_EXTRACT_DIR = $(BUILD_DIR)/test-extract
TEST_IMAGE_NAME = $(TEST_EXTRACT_DIR)/$(FINAL_IMAGE_NAME)

.PHONY: help setup prepare download cloud-init run sysprep build test-oci push clean test-run

help:
	@echo "Available commands (Host: $(OS) $(HOST_ARCH)):"
	@echo "  make test-run    - QUICKSTART: Runs the entire pipeline sequentially for $(ARCH)"
	@echo "  make prepare     - Run logic/scripts/prepare.sh to generate cloud-init configs"
	@echo "  make download    - Download the $(ARCH) Ubuntu image and create pristine base"
	@echo "  make cloud-init  - Package cloud-init metadata into a bootable ISO"
	@echo "  make run         - Boot VM. Override arch with 'make run ARCH=amd64' (Emulation)"
	@echo "  make sysprep     - Connect to the running VM, clean cloud-init data, and shut it down"
	@echo "  make build       - Finalize the instance artifact for OCI pushing"
	@echo "  make test-oci    - Pull the $(ARCH) QCOW2 from OCI using ORAS and verify it"
	@echo "  make push        - Push the $(ARCH) QCOW2 artifact to OCI using ORAS"
	@echo "  make clean       - Remove the entire build/ directory"

test-run:
	@echo "🔥 === Starting Full Pipeline Test Run for $(ARCH) === 🔥"
	$(MAKE) prepare ARCH=$(ARCH)
	$(MAKE) download ARCH=$(ARCH)
	$(MAKE) cloud-init ARCH=$(ARCH)
	$(MAKE) run ARCH=$(ARCH)

setup:
	@mkdir -p $(BUILD_DIR)

prepare: setup
	./logic/scripts/prepare.sh
	@if [ -f $(EFI_VARS_SRC) ]; then \
		cp $(EFI_VARS_SRC) $(VARS_FILE); \
		echo "✅ Copied $(ARCH) EFI vars to $(VARS_FILE)"; \
	else \
		echo "⚠️ Warning: QEMU EFI vars not found at $(EFI_VARS_SRC)"; \
		touch $(VARS_FILE); \
	fi

download: setup
	@if [ ! -f $(DOWNLOADED_IMG) ]; then \
		echo "🌍 Downloading official Ubuntu 24.04 LTS $(ARCH) Cloud Image..."; \
		curl -L -o $(DOWNLOADED_IMG) $(IMAGE_URL); \
	else \
		echo "✅ $(DOWNLOADED_IMG) already exists."; \
	fi
	@if [ ! -f $(BASE_DISK) ]; then \
		echo "⚙️  Converting downloaded image to pristine $(ARCH) QCOW2 base disk..."; \
		qemu-img convert -f qcow2 -O qcow2 $(DOWNLOADED_IMG) $(BASE_DISK); \
		echo "📏 Resizing pristine base disk to 25G..."; \
		qemu-img resize $(BASE_DISK) 25G; \
	fi

cloud-init: setup
	@if [ ! -f $(BUILD_DIR)/user-data ] || [ ! -f $(BUILD_DIR)/meta-data ]; then \
		echo "❌ Error: user-data or meta-data is missing in $(BUILD_DIR). Run 'make prepare' first."; \
		exit 1; \
	fi
	@echo "📦 Packaging cloud-init files into $(CLOUD_INIT_ISO) using macOS hdiutil..."
	@rm -rf $(BUILD_DIR)/cidata
	@mkdir -p $(BUILD_DIR)/cidata
	@cp $(BUILD_DIR)/user-data $(BUILD_DIR)/meta-data $(BUILD_DIR)/cidata/
	@rm -f $(CLOUD_INIT_ISO)
	hdiutil makehybrid -iso -joliet -default-volume-name cidata -o $(CLOUD_INIT_ISO) $(BUILD_DIR)/cidata/
	@rm -rf $(BUILD_DIR)/cidata
	@echo "✅ $(CLOUD_INIT_ISO) generated successfully."

run: setup
	@if [ ! -f $(BASE_DISK) ]; then \
		echo "❌ Error: $(BASE_DISK) does not exist. Run 'make download' first."; \
		exit 1; \
	fi
	@if [ ! -f $(CLOUD_INIT_ISO) ]; then \
		echo "❌ Error: $(CLOUD_INIT_ISO) does not exist. Run 'make cloud-init' first."; \
		exit 1; \
	fi
	@if [ ! -f $(VARS_FILE) ]; then \
		echo "⚠️  Warning: $(VARS_FILE) not found. Running prepare target..."; \
		$(MAKE) prepare ARCH=$(ARCH); \
	fi
	@if [ ! -f $(EFI_CODE) ]; then \
		echo "❌ Error: $(ARCH) EFI firmware not found at $(EFI_CODE)."; \
		echo "💡 Please install the appropriate qemu packages."; \
		exit 1; \
	fi
	@if [ ! -f $(INSTANCE_DISK) ]; then \
		echo "👯 Creating a fresh ephemeral instance for $(ARCH)..."; \
		cp $(BASE_DISK) $(INSTANCE_DISK); \
	else \
		echo "✅ Using existing $(INSTANCE_DISK)."; \
	fi
	@echo "🖥️  Launching $(ARCH) Desktop VM with $(QEMU_BIN)..."
	@echo "🚀 Acceleration Mode: $(QEMU_ACCEL)"
	@echo "👀 A native macOS QEMU window will open."
	@echo "🔐 Username: ubuntu | Password: ubuntu"
	@echo "🔌 To connect via SSH from your host terminal: 'ssh -i $(BUILD_DIR)/id_ed25519 -p 2222 ubuntu@localhost'"
	@echo "✨ When finished, run 'make sysprep' in another terminal to seal the Golden Image."
	$(QEMU_BIN) \
		-M $(QEMU_MACHINE) \
		$(QEMU_ACCEL) \
		-smp 4 \
		-m 4096 \
		-drive if=pflash,format=raw,readonly=on,file=$(EFI_CODE) \
		-drive if=pflash,format=raw,file=$(VARS_FILE) \
		-drive if=virtio,file=$(INSTANCE_DISK),format=qcow2 \
		-drive if=virtio,file=$(CLOUD_INIT_ISO),format=raw \
		-smbios type=1,serial=ds=nocloud \
		-device virtio-gpu-pci \
		-display cocoa,show-cursor=on \
		-device virtio-mouse-pci \
		-device virtio-keyboard-pci \
		-device virtio-net-pci,netdev=net0 \
		-netdev user,id=net0,hostfwd=tcp::2222-:22

sysprep:
	@echo "🧼 === Starting Golden Image Sysprep === 🧼"
	@echo "Connecting to VM to clean cloud-init data, wipe SSH keys, and reset machine-id..."
	@ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -i $(BUILD_DIR)/id_ed25519 -p 2222 ubuntu@localhost \
		"sudo cloud-init clean --logs --machine-id && sudo rm -f /home/ubuntu/.ssh/authorized_keys && sudo rm -f /etc/ssh/ssh_host_* && sudo sync && sudo halt" || true
	@echo "✅ Cleanup commands sent! The VM is shutting down."
	@echo "   You can now safely close the QEMU window and run 'make build'."

build: setup
	@if [ ! -f $(INSTANCE_DISK) ]; then \
		echo "❌ Error: $(INSTANCE_DISK) not found. You must run the VM first to install the desktop."; \
		exit 1; \
	fi
	@echo "🏗️  Preparing final artifact for OCI packaging..."
	@mv $(INSTANCE_DISK) $(BUILD_DIR)/$(FINAL_IMAGE_NAME)
	@echo "✅ Final artifact ready: $(BUILD_DIR)/$(FINAL_IMAGE_NAME)"

test-oci: setup
	@if ! command -v oras >/dev/null 2>&1; then echo "❌ Error: 'oras' CLI is required. Run 'brew install oras'."; exit 1; fi
	@echo "🧪 === Starting OCI Artifact Verification === 🧪"
	@echo "1️⃣  Pulling OCI artifact '$(OCI_IMAGE)' using ORAS..."
	@rm -rf $(TEST_EXTRACT_DIR)
	@mkdir -p $(TEST_EXTRACT_DIR)
	@oras pull $(OCI_IMAGE) -o $(TEST_EXTRACT_DIR) || { \
		echo "❌ Error: Failed to pull OCI image '$(OCI_IMAGE)'. Did you run 'make push' first?"; \
		exit 1; \
	}
	@echo "   ✅ Pulled successfully to $(TEST_EXTRACT_DIR)."
	@echo "2️⃣  Verifying integrity of extracted QCOW2 image..."
	@qemu-img info $(TEST_IMAGE_NAME) || { \
		echo "❌ Error: The extracted virtual disk is corrupted or not in QCOW2 format."; \
		exit 1; \
	}
	@echo "🎉 === Verification Successful! OCI artifact is fully functional. === 🎉"

push:
	@if ! command -v oras >/dev/null 2>&1; then echo "❌ Error: 'oras' CLI is required. Run 'brew install oras'."; exit 1; fi
	@if [ ! -f $(BUILD_DIR)/$(FINAL_IMAGE_NAME) ]; then echo "❌ Error: Final artifact not found. Run 'make build' first."; exit 1; fi
	@echo "🚀 Pushing $(BUILD_DIR)/$(FINAL_IMAGE_NAME) to registry as a raw OCI artifact..."
	oras push $(OCI_IMAGE) $(BUILD_DIR)/$(FINAL_IMAGE_NAME):application/vnd.qemu.disk.qcow2
	@echo "✅ Successfully pushed $(OCI_IMAGE) to OCI registry!"

clean:
	@echo "🧹 Cleaning up build directory..."
	rm -rf $(BUILD_DIR)
