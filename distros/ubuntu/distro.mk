# Ubuntu distro config. Included by Makefile when DISTRO=ubuntu.

OS_VERSION ?= 24.04
OS_RELEASE ?= noble
IMAGE_VARIANT ?= desktop

IMAGE_URL = https://cloud-images.ubuntu.com/$(OS_RELEASE)/current/$(OS_RELEASE)-server-cloudimg-$(ARCH).img
