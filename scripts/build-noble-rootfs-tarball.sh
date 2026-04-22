#!/bin/bash
# Build a Noble rootfs tarball customised for mdadm RAID1 (+ optional LVM)
# with BIOS or EFI boot.
# Starts from Canonical's prebuilt root tarball (the same artefact MAAS uses),
# chroots in to add mdadm + lvm2 + grub-{pc,efi}, and re-tars as gzip for archive2disk.
#
# One-time admin task. Re-run whenever you want to refresh (e.g. CVE patches).
# Requires: wget, tar, sudo, chroot. Run on a trusted build host.
set -euxo pipefail

ROOT_TAR_URL="${ROOT_TAR_URL:-https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-amd64-root.tar.xz}"
WORK="${WORK:-/tmp/noble-rootfs-build}"
OUT_DIR="${OUT_DIR:-/srv/tinkerbell-artifacts}"
# MODE=bios installs grub-pc; MODE=efi installs grub-efi-amd64 + efibootmgr.
MODE="${MODE:-bios}"
OUT_BASE="${OUT_BASE:-noble-rootfs-${MODE}}"

mkdir -p "$WORK" "$OUT_DIR"
cd "$WORK"

# Unmount anything mounted under $1 (an absolute path), deepest-first.
# Uses /proc/mounts directly because (a) findmnt can miss entries across
# mount namespace boundaries, and (b) umount -R only works when its
# argument is itself a mountpoint — here the rootfs dir is a plain
# directory with bind-mounts nested inside it.
unmount_under() {
  local path="$1"
  grep -E " ${path}(/|\$)" /proc/mounts | awk '{print $2}' \
    | awk '{print length, $0}' | sort -rn | cut -d' ' -f2- \
    | xargs -r -n1 sudo umount -l 2>/dev/null || true
}

# If a previous run crashed before unmounting, leftover bind mounts under
# rootfs/ will make `rm -rf` fail with "Operation not permitted" on sysfs
# entries. Tear them down before continuing.
if [ -d rootfs ]; then
  unmount_under "$PWD/rootfs"
fi

# 1. Fetch Canonical's prebuilt root tarball (already contains the kernel,
# cloud-init, and a base server seed — no qemu/losetup gymnastics required).
if [ ! -f root.tar.xz ]; then
  wget -O root.tar.xz "$ROOT_TAR_URL"
fi

# 2. Extract into a fresh rootfs dir
sudo rm -rf rootfs
mkdir -p rootfs
sudo tar --numeric-owner -xf root.tar.xz -C rootfs

# 3. Bind-mount pseudo-fs and inject working DNS for apt
cleanup() {
  # Tear down deepest-first so nested mounts (efivarfs under /sys, etc.) go
  # before their parent; lazy so we don't block on EBUSY.
  if [ -d rootfs ]; then
    unmount_under "$PWD/rootfs"
  fi
}
trap cleanup EXIT

sudo mount --bind /dev  rootfs/dev
sudo mount -t devpts devpts rootfs/dev/pts 2>/dev/null || true
sudo mount --bind /proc rootfs/proc
# `rbind` (recursive bind) so that nested mounts under /sys (efivarfs,
# cgroup, etc.) are propagated as separate mount entries inside the
# chroot — the chroot needs them, and teardown via findmnt -R below
# can then umount them in deepest-first order.
sudo mount --rbind /sys rootfs/sys
sudo mount --make-rslave rootfs/sys 2>/dev/null || true
sudo rm -f rootfs/etc/resolv.conf
sudo cp -L /etc/resolv.conf rootfs/etc/resolv.conf

# 4. Install mdadm + lvm2 + bootloader inside the chroot
sudo chroot rootfs /bin/bash -eux <<CHROOT
export DEBIAN_FRONTEND=noninteractive
apt-get update

# Defer bootloader install target — we do it per-disk at provision time
echo 'grub-pc grub-pc/install_devices_empty boolean true' | debconf-set-selections
echo 'grub-pc grub-pc/install_devices multiselect' | debconf-set-selections

if [ "${MODE}" = "efi" ]; then
  apt-get install -y --no-install-recommends \\
    mdadm lvm2 thin-provisioning-tools \\
    grub-common grub-efi-amd64 grub-efi-amd64-bin \\
    efibootmgr dosfstools initramfs-tools
  apt-get purge -y grub-pc grub-pc-bin 2>/dev/null || true
else
  apt-get install -y --no-install-recommends \\
    mdadm lvm2 thin-provisioning-tools \\
    grub-common grub-pc initramfs-tools
  apt-get purge -y grub-efi-amd64 grub-efi-amd64-bin grub-efi-amd64-signed 2>/dev/null || true
fi

# Canonical's tarball ships linux-image-virtual; upgrade to linux-image-generic
# and bundle linux-modules-extra-generic so the initramfs has drivers for the
# full range of bare-metal hardware (additional NICs, RAID controllers, etc.).
apt-get install -y --no-install-recommends \
  linux-image-generic linux-modules-extra-generic

# Sanity gate: fail the build if no kernel ended up in /boot.
if ! ls /boot/vmlinuz-* >/dev/null 2>&1; then
  echo "FATAL: /boot has no kernel after apt install" >&2
  exit 1
fi

apt-get clean
rm -rf /var/lib/apt/lists/*
CHROOT

# 5. Strip runtime state before tarring
sudo rm -f rootfs/etc/resolv.conf
sudo rm -f rootfs/etc/machine-id
sudo touch rootfs/etc/machine-id

cleanup
trap - EXIT

# 6. Produce tarball + checksum
TAR="$OUT_DIR/${OUT_BASE}.tar.gz"
sudo tar --numeric-owner --one-file-system \
  --exclude='./proc/*' --exclude='./sys/*' --exclude='./dev/*' \
  --exclude='./tmp/*'  --exclude='./run/*' \
  --exclude='./var/cache/apt/archives/*.deb' \
  -C rootfs -czf "$TAR" .

( cd "$OUT_DIR" && sha256sum "$(basename "$TAR")" > "$(basename "$TAR").sha256" )

echo "---"
echo "Tarball: $TAR"
cat "${TAR}.sha256"
