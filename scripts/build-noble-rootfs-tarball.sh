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
# MODE=bios installs grub-pc, MODE=efi installs grub-efi-amd64 + efibootmgr.
MODE="${MODE:-bios}"
OUT_BASE="${OUT_BASE:-noble-rootfs-${MODE}}"

mkdir -p "$WORK" "$OUT_DIR"
cd "$WORK"

# 1. Fetch + convert to raw
if [ ! -f noble.raw ]; then
  wget -O noble.qcow2 "$CLOUD_IMG_URL"
  qemu-img convert -f qcow2 -O raw noble.qcow2 noble.raw
fi

# 1a. Grow the rootfs partition. Cloud image ships a ~2.4 GiB rootfs that can't
# fit linux-image-generic + firmware + microcode (~1 GiB). Grow by EXTRA_GIB
# (default 4) and resize p1 to consume the new space. p1 is last on disk in
# Noble cloud images (p14=BIOS boot, p15=ESP, p1=rootfs at offset 227328),
# so a straight growpart works. Idempotent via a marker file.
EXTRA_GIB="${EXTRA_GIB:-4}"
if [ ! -f noble.raw.grown ]; then
  sudo qemu-img resize -f raw noble.raw "+${EXTRA_GIB}G"
  touch noble.raw.grown
fi

# 2. Loop mount partitions
LOOP=$(losetup --show -f -P noble.raw)

# Relocate the GPT backup header to the end of the now-larger disk, then grow
# p1 to fill. sgdisk ships with the `gdisk` package.
sudo sgdisk -e "$LOOP" || true
sudo parted -s "$LOOP" resizepart 1 100%
sudo partprobe "$LOOP" || true
sudo e2fsck -fy "${LOOP}p1" || true
sudo resize2fs "${LOOP}p1"
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
sudo chroot rootfs /bin/bash -eux <<CHROOT
export DEBIAN_FRONTEND=noninteractive
apt-get update
# Defer bootloader install target — we do it per-disk at provision time
echo 'grub-pc grub-pc/install_devices_empty boolean true' | debconf-set-selections
echo 'grub-pc grub-pc/install_devices multiselect' | debconf-set-selections

# linux-image-generic is essential — without a kernel, update-initramfs and
# update-grub silently no-op and the resulting tarball isn't bootable.
# Do NOT drop --no-install-recommends without replacing it: the cloud image
# rootfs can ship without a kernel and we must pull one in explicitly.
if [ "${MODE}" = "efi" ]; then
  apt-get install -y --no-install-recommends \\
    linux-image-generic mdadm grub-common grub-efi-amd64 grub-efi-amd64-bin \\
    efibootmgr dosfstools initramfs-tools
  apt-get purge -y grub-pc grub-pc-bin 2>/dev/null || true
else
  apt-get install -y --no-install-recommends \\
    linux-image-generic mdadm grub-common grub-pc initramfs-tools
  apt-get purge -y grub-efi-amd64 grub-efi-amd64-bin grub-efi-amd64-signed 2>/dev/null || true
fi

# Sanity gate: fail the build if no kernel ended up in /boot.
if ! ls /boot/vmlinuz-* >/dev/null 2>&1; then
  echo "FATAL: /boot has no kernel after apt install; refusing to produce a non-bootable tarball" >&2
  exit 1
fi

apt-get clean
rm -rf /var/lib/apt/lists/*
CHROOT

# 5. Strip dynamic/runtime state before tarring
sudo rm -f rootfs/etc/resolv.conf
sudo rm -f rootfs/etc/machine-id
sudo touch rootfs/etc/machine-id

sudo umount rootfs/dev rootfs/proc rootfs/sys

# 6. Produce tarball + checksum
#
# archive2disk verifies checksum of the DECOMPRESSED tar stream, not the
# gzipped file. We emit two checksum files:
#   * .tar.gz.sha256     — hash of the downloadable .tar.gz (normal sha256sum)
#   * .tar.sha256        — hash of the uncompressed tar content; this is the
#                          value to pin in TARFILE_CHECKSUM.
# See archive2disk/archive/utils.go (io.TeeReader after gzip.NewReader).
TAR="$OUT_DIR/${OUT_BASE}.tar.gz"
sudo tar --numeric-owner --one-file-system \
  --exclude='./proc/*' --exclude='./sys/*' --exclude='./dev/*' \
  --exclude='./tmp/*'  --exclude='./run/*' \
  --exclude='./var/cache/apt/archives/*.deb' \
  -C rootfs -czf "$TAR" .

( cd "$OUT_DIR" && sha256sum "$(basename "$TAR")" > "$(basename "$TAR").sha256" )
gunzip -c "$TAR" | sha256sum \
  | sed "s| -$|  $(basename "${TAR%.gz}")|" \
  > "${TAR%.gz}.sha256"

sudo umount rootfs
sudo losetup -d "$LOOP"
trap - EXIT

echo "---"
echo "Tarball: $TAR"
echo "sha256 of .tar.gz (for sanity/HTTP verification):"
cat "${TAR}.sha256"
echo "sha256 of uncompressed tar (USE THIS for archive2disk TARFILE_CHECKSUM):"
cat "${TAR%.gz}.sha256"
