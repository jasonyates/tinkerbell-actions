//go:build linux

// Package chroot mounts a block device at a scratch path, optionally
// mounts sibling filesystems (e.g. a separate /var LV) inside it,
// bind-mounts the kernel pseudo-filesystems, and chroots there. No
// teardown — Tinkerbell action containers are short-lived and the
// kernel reclaims mounts on exit.
//
// All mount operations use syscall.Mount directly (no shelling out to
// the mount(8) binary) so this package works inside FROM scratch
// images where no mount binary is available.
package chroot

import (
	"fmt"
	"os"
	"syscall"

	"github.com/tinkerbell/actions/pkg/metadata"
)

// DefaultMountPoint is where MountTree/Enter mount the primary block
// device before chrooting. Exported for tests / inspection.
const DefaultMountPoint = "/mountAction"

// MountTree mounts blockDev at DefaultMountPoint, then mounts each
// sibling filesystem from extras at DefaultMountPoint/<mount.point>.
// See MountExtras for the filter and ordering rules.
//
// Callers that need the mount tree but not a chroot (archive2disk
// writing a tarball onto the correct LVs) use this directly.
func MountTree(blockDev, fsType string, extras []metadata.Filesystem) error {
	if err := os.MkdirAll(DefaultMountPoint, 0o755); err != nil {
		return fmt.Errorf("chroot: mkdir %s: %w", DefaultMountPoint, err)
	}
	if err := syscall.Mount(blockDev, DefaultMountPoint, fsType, 0, ""); err != nil {
		return fmt.Errorf("chroot: mount %s (%s) at %s: %w", blockDev, fsType, DefaultMountPoint, err)
	}
	return MountExtras(extras)
}

// MountExtras mounts each sibling filesystem in extras at
// DefaultMountPoint/<f.Mount.Point>. Filter:
//   - skip entries with empty or "/" mount point (primary already mounted)
//   - skip swap (wrong verb — it's swapon, not mount)
//   - skip vfat (ESPs have disk-specific lifecycle; actions mount them explicitly)
//
// Ordering: ascending by path depth, so /var mounts before /var/lib/docker.
// Used by cexec (after its own primary mount) to avoid pulling in the
// rest of pkg/chroot's machinery.
func MountExtras(extras []metadata.Filesystem) error {
	for _, f := range filterExtras(extras) {
		target := DefaultMountPoint + f.Mount.Point
		if err := os.MkdirAll(target, 0o755); err != nil {
			return fmt.Errorf("chroot: mkdir %s: %w", target, err)
		}
		if err := syscall.Mount(f.Mount.Device, target, f.Mount.Format, 0, ""); err != nil {
			return fmt.Errorf("chroot: mount %s (%s) at %s: %w", f.Mount.Device, f.Mount.Format, target, err)
		}
	}
	return nil
}

// Enter mounts the full tree (primary + extras), bind-mounts /dev
// /proc /sys inside it, chroots there, and chdirs to /. After Enter
// returns nil, subsequent exec.Command sees the new root. There is
// no matching Leave — action containers exit after their work.
//
// blockDev must be an absolute path to a block device
// (e.g. "/dev/vg0/root"); fsType is a kernel-known filesystem name
// (e.g. "ext4"). extras may be nil for the legacy single-root case.
func Enter(blockDev, fsType string, extras []metadata.Filesystem) error {
	if err := MountTree(blockDev, fsType, extras); err != nil {
		return err
	}
	for _, sub := range []string{"dev", "proc", "sys"} {
		target := DefaultMountPoint + "/" + sub
		if err := os.MkdirAll(target, 0o755); err != nil {
			return fmt.Errorf("chroot: mkdir %s: %w", target, err)
		}
		if err := syscall.Mount("/"+sub, target, "", syscall.MS_BIND, ""); err != nil {
			return fmt.Errorf("chroot: bind /%s → %s: %w", sub, target, err)
		}
	}
	if err := syscall.Chroot(DefaultMountPoint); err != nil {
		return fmt.Errorf("chroot: chroot(%s): %w", DefaultMountPoint, err)
	}
	if err := os.Chdir("/"); err != nil {
		return fmt.Errorf("chroot: chdir /: %w", err)
	}
	return nil
}

