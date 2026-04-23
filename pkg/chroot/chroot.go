//go:build linux

// Package chroot mounts a block device at a scratch path, bind-mounts
// the kernel pseudo-filesystems inside it, and chroots there. No
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
)

// DefaultMountPoint is where Enter mounts the target block device
// before chrooting. Exported for tests / inspection.
const DefaultMountPoint = "/mountAction"

// Enter mounts blockDev (fsType) at DefaultMountPoint, bind-mounts
// /dev, /proc, /sys inside it, chroots there, and chdirs to /.
// After Enter returns nil, subsequent exec.Command sees the new
// root. Caller should exit or continue doing work inside the new
// root — there is no matching Leave.
//
// blockDev must be an absolute path to a block device
// (e.g. "/dev/vg0/root", "/dev/md0"); fsType is a kernel-known
// filesystem name (e.g. "ext4", "xfs").
func Enter(blockDev, fsType string) error {
	if err := os.MkdirAll(DefaultMountPoint, 0o755); err != nil {
		return fmt.Errorf("chroot: mkdir %s: %w", DefaultMountPoint, err)
	}
	if err := syscall.Mount(blockDev, DefaultMountPoint, fsType, 0, ""); err != nil {
		return fmt.Errorf("chroot: mount %s (%s) at %s: %w", blockDev, fsType, DefaultMountPoint, err)
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
