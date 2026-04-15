#!/bin/bash
# Build a Noble rootfs tarball with mdadm + grub-pc preinstalled.
# Produces noble-rootfs.tar.gz + .sha256 ready to serve over HTTP for archive2disk.
#
# One-time admin task. Re-run whenever you want to refresh the base image
# (e.g. for CVE patches). Requires: qemu-utils, losetup, debootstrap-like
# privileges (sudo). Run on a trusted build host, not in production.
set -euxo pipefail

CLOUD_IMG_URL="${CLOUD_IMG_URL:-https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-amd64.img}"
WORK="${WORK:-/tmp/noble-rootfs-build}"
OUT_DIR="${OUT_DIR:-/srv/tinkerbell-artifacts}"
OUT_BASE="${OUT_BASE:-noble-rootfs}"

mkdir -p "$WORK" "$OUT_DIR"
cd "$WORK"

# 1. Fetch + convert to raw
if [ ! -f noble.raw ]; then
  wget -O noble.qcow2 "$CLOUD_IMG_URL"
  qemu-img convert -f qcow2 -O raw noble.qcow2 noble.raw
fi

# 2. Loop mount partitions
LOOP=$(losetup --show -f -P noble.raw)
cleanup() {
  sudo umount -R rootfs 2>/dev/null || true
  sudo losetup -d "$LOOP" 2>/dev/null || true
}
trap cleanup EXIT

# Cloud image's rootfs is partition 1 (BIOS-boot images may use partition 16 for /boot/efi)
mkdir -p rootfs
sudo mount "${LOOP}p1" rootfs

# 3. Bind mounts for chroot + DNS
sudo mount --bind /dev  rootfs/dev
sudo mount --bind /proc rootfs/proc
sudo mount --bind /sys  rootfs/sys
# Cloud image ships /etc/resolv.conf as a dangling symlink to
# /run/systemd/resolve/stub-resolv.conf. Replace it with a real file
# (we strip this back out before tarring).
sudo rm -f rootfs/etc/resolv.conf
sudo cp -L /etc/resolv.conf rootfs/etc/resolv.conf

# 4. Install mdadm + grub-pc (BIOS bootloader) inside the chroot
sudo chroot rootfs /bin/bash -eux <<'CHROOT'
export DEBIAN_FRONTEND=noninteractive
apt-get update
# grub-pc postinst asks where to install — defer, we do it per-disk at provision time
echo 'grub-pc grub-pc/install_devices_empty boolean true' | debconf-set-selections
echo 'grub-pc grub-pc/install_devices multiselect' | debconf-set-selections
apt-get install -y --no-install-recommends \
  mdadm \
  grub-pc \
  grub-common \
  initramfs-tools
# Pre-remove grub-efi-amd64 if present (we're doing BIOS boot); harmless if absent.
apt-get purge -y grub-efi-amd64 grub-efi-amd64-bin grub-efi-amd64-signed 2>/dev/null || true
apt-get clean
rm -rf /var/lib/apt/lists/*
CHROOT

# 5. Strip dynamic/runtime state before tarring
sudo rm -f rootfs/etc/resolv.conf
sudo rm -f rootfs/etc/machine-id
sudo touch rootfs/etc/machine-id

sudo umount rootfs/dev rootfs/proc rootfs/sys

# 6. Produce tarball + checksum
TAR="$OUT_DIR/${OUT_BASE}.tar.gz"
sudo tar --numeric-owner --one-file-system \
  --exclude='./proc/*' --exclude='./sys/*' --exclude='./dev/*' \
  --exclude='./tmp/*'  --exclude='./run/*' \
  --exclude='./var/cache/apt/archives/*.deb' \
  -C rootfs -czf "$TAR" .

( cd "$OUT_DIR" && sha256sum "$(basename "$TAR")" > "$(basename "$TAR").sha256" )

sudo umount rootfs
sudo losetup -d "$LOOP"
trap - EXIT

echo "---"
echo "Tarball: $TAR"
echo "Checksum:"
cat "${TAR}.sha256"
