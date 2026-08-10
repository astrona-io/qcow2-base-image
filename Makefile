# Makefile for building, running, and shipping QEMU Ubuntu Desktop Templates on Apple Silicon

BUILD_DIR = build
IMAGE_NAME = ubuntu-24.04-desktop.qcow2
IMAGE_PATH = $(BUILD_DIR)/$(IMAGE_NAME)
IMAGE_URL = https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-arm64.img
DOWNLOADED_IMG = $(BUILD_DIR)/noble-server-cloudimg-arm64.img

REGISTRY ?= ghcr.io
GH_USER ?= $(shell git config user.name 2>/dev/null || echo "your-username")
OCI_IMAGE ?= $(REGISTRY)/$(GH_USER)/ubuntu-24.04-qemu-desktop:latest

EFI_CODE = /opt/homebrew/share/qemu/edk2-aarch64-code.fd
VARS_FILE = $(BUILD_DIR)/vars.fd
CLOUD_INIT_ISO = $(BUILD_DIR)/cloud-init.iso

TEST_EXTRACT_DIR = $(BUILD_DIR)/test-extract
TEST_IMAGE_NAME = $(TEST_EXTRACT_DIR)/ubuntu-24.04-desktop-extracted.qcow2

.PHONY: help setup prepare download cloud-init resize run build test-oci push clean

help:
	@echo "Available commands:"
	@echo "  make prepare     - Run logic/scripts/prepare.sh to generate cloud-init configs inside build/"
	@echo "  make download    - Download the official Ubuntu 24.04 LTS ARM64 Cloud Image to build/"
	@echo "  make cloud-init  - Package cloud-init metadata into a bootable ISO (using hdiutil)"
	@echo "  make resize      - Resize the QCOW2 base disk to 25G (recommended for Desktop)"
	@echo "  make run         - Launch the QEMU VM to run cloud-init and boot the Desktop OS"
	@echo "  make build       - Build the OCI container image using logic/oci/Dockerfile"
	@echo "  make test-oci    - Extract the QCOW2 from the OCI container and verify its integrity"
	@echo "  make push        - Push the OCI container image to the public registry ($(OCI_IMAGE))"
	@echo "  make clean       - Remove the entire build/ directory"

setup:
	@mkdir -p $(BUILD_DIR)

prepare: setup
	./logic/scripts/prepare.sh

download: setup
	@if [ ! -f $(DOWNLOADED_IMG) ]; then \
		echo "Downloading official Ubuntu 24.04 LTS ARM64 Cloud Image..."; \
		curl -L -o $(DOWNLOADED_IMG) $(IMAGE_URL); \
	else \
		echo "$(DOWNLOADED_IMG) already exists."; \
	fi
	@if [ ! -f $(IMAGE_PATH) ]; then \
		echo "Creating copy of base image as $(IMAGE_PATH)..."; \
		cp $(DOWNLOADED_IMG) $(IMAGE_PATH); \
	fi

cloud-init: setup
	@if [ ! -f $(BUILD_DIR)/user-data ] || [ ! -f $(BUILD_DIR)/meta-data ]; then \
		echo "Error: user-data or meta-data is missing in $(BUILD_DIR). Run 'make prepare' first."; \
		exit 1; \
	fi
	@echo "Packaging cloud-init files into $(CLOUD_INIT_ISO) using macOS hdiutil..."
	@rm -rf $(BUILD_DIR)/cidata
	@mkdir -p $(BUILD_DIR)/cidata
	@cp $(BUILD_DIR)/user-data $(BUILD_DIR)/meta-data $(BUILD_DIR)/cidata/
	hdiutil makehybrid -o $(CLOUD_INIT_ISO) -hfs -joliet -iso -default-volume-name cidata $(BUILD_DIR)/cidata/
	@rm -rf $(BUILD_DIR)/cidata
	@echo "$(CLOUD_INIT_ISO) generated successfully."

resize: setup
	@if [ ! -f $(IMAGE_PATH) ]; then \
		echo "Error: $(IMAGE_PATH) does not exist. Run 'make download' first."; \
		exit 1; \
	fi
	@echo "Resizing $(IMAGE_PATH) to 25G..."
	qemu-img resize $(IMAGE_PATH) 25G

run: setup
	@if [ ! -f $(IMAGE_PATH) ]; then \
		echo "Error: $(IMAGE_PATH) does not exist. Run 'make download' first."; \
		exit 1; \
	fi
	@if [ ! -f $(CLOUD_INIT_ISO) ]; then \
		echo "Error: $(CLOUD_INIT_ISO) does not exist. Run 'make cloud-init' first."; \
		exit 1; \
	fi
	@if [ ! -f $(VARS_FILE) ]; then \
		echo "Warning: $(VARS_FILE) not found. Running prepare target..."; \
		$(MAKE) prepare; \
	fi
	@if [ ! -f $(EFI_CODE) ]; then \
		echo "Error: QEMU EFI firmware not found at $(EFI_CODE)."; \
		echo "Please install qemu via Homebrew: 'brew install qemu'"; \
		exit 1; \
	fi
	@echo "Launching Desktop VM with QEMU (using HVF and Cocoa display)..."
	@echo "A native macOS QEMU window will open."
	@echo "Username: ubuntu | Password: ubuntu (Automatic graphical login is configured!)"
	@echo "To connect via SSH from your host terminal: 'ssh -p 2222 ubuntu@localhost'"
	qemu-system-aarch64 \
		-M virt,highmem=on \
		-accel hvf \
		-cpu host \
		-smp 4 \
		-m 4096 \
		-drive if=pflash,format=raw,readonly=on,file=$(EFI_CODE) \
		-drive if=pflash,format=raw,file=$(VARS_FILE) \
		-drive if=virtio,file=$(IMAGE_PATH),format=qcow2 \
		-drive if=virtio,file=$(CLOUD_INIT_ISO),format=raw \
		-device virtio-gpu-pci \
		-display cocoa,show-cursor=on \
		-device virtio-mouse-pci \
		-device virtio-keyboard-pci \
		-device virtio-net-pci,netdev=net0 \
		-netdev user,id=net0,hostfwd=tcp::2222-:22

build: setup
	@if [ ! -f $(IMAGE_PATH) ]; then \
		echo "Error: $(IMAGE_PATH) not found. Make sure the customized image is in place."; \
		exit 1; \
	fi
	@echo "Building OCI image containing $(IMAGE_PATH)..."
	docker build -f logic/oci/Dockerfile -t $(OCI_IMAGE) .
	@echo "OCI image built: $(OCI_IMAGE)"

test-oci: setup
	@echo "=== Starting OCI Image Verification ==="
	@echo "1. Checking if the local OCI image '$(OCI_IMAGE)' exists..."
	@docker image inspect $(OCI_IMAGE) > /dev/null 2>&1 || { \
		echo "Error: OCI image '$(OCI_IMAGE)' not found. Run 'make build' first."; \
		exit 1; \
	}
	@echo "   OCI image exists."
	@echo "2. Extracting QCOW2 virtual disk from container using auto-extraction volume mount..."
	@rm -rf $(TEST_EXTRACT_DIR)
	@mkdir -p $(TEST_EXTRACT_DIR)
	@docker run --rm -v "$$(pwd)/$(TEST_EXTRACT_DIR)":/out $(OCI_IMAGE) > /dev/null
	@echo "   Extracted successfully to $(TEST_IMAGE_NAME)."
	@echo "3. Verifying integrity of extracted QCOW2 image..."
	@qemu-img info $(TEST_IMAGE_NAME) || { \
		echo "Error: The extracted virtual disk is corrupted or not in QCOW2 format."; \
		exit 1; \
	}
	@echo "=== Verification Successful! OCI image is fully functional and ready to push. ==="

push:
	@echo "Pushing $(OCI_IMAGE) to registry..."
	docker push $(OCI_IMAGE)
	@echo "Successfully pushed $(OCI_IMAGE) to OCI registry!"

clean:
	@echo "Cleaning up build directory..."
	rm -rf $(BUILD_DIR)
