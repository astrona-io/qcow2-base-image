# Use a tiny alpine base image so we can execute our custom entrypoint script
FROM alpine:3.20

# Document the image metadata
LABEL org.opencontainers.image.title="Ubuntu 24.04 LTS Noble QEMU Desktop Template" \
      org.opencontainers.image.description="A pre-configured Ubuntu 24.04 LTS Desktop QCOW2 image for QEMU. Run this container to view boot instructions and auto-extract the disk." \
      org.opencontainers.image.source="https://github.com/your-username/your-repo" \
      org.opencontainers.image.licenses="Apache-2.0"

# Copy the pre-configured QCOW2 image into the container's standard KubeVirt path
COPY build/ubuntu-24.04-desktop.qcow2 /disk/ubuntu-24.04-desktop.qcow2

# Add our helpful guide script
COPY logic/oci/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

# When a user runs the image, fire the guide/extraction script
ENTRYPOINT ["/entrypoint.sh"]
