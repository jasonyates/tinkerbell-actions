//go:build linux

package main

import (
	"os"
	"os/exec"

	log "github.com/sirupsen/logrus"
	"github.com/tinkerbell/actions/pkg/chroot"
)

// grub2disk (BIOS mode only; Task 8 adds MODE=efi) mounts the target
// filesystem, chroots in, and runs grub-install against the disk
// identified by GRUB_DISK. Back-compat env vars preserved.
func main() {
	log.Infof("grub2disk - BIOS grub-install wrapper")

	grubDisk := os.Getenv("GRUB_DISK")
	fsType := os.Getenv("FS_TYPE")
	blockDev := os.Getenv("GRUB_INSTALL_PATH")
	if grubDisk == "" {
		log.Fatal("GRUB_DISK is required")
	}
	if blockDev == "" {
		log.Fatal("GRUB_INSTALL_PATH is required")
	}

	if err := chroot.Enter(blockDev, fsType); err != nil {
		log.Fatal(err)
	}

	cmd := exec.Command("grub-install", grubDisk)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		log.Fatalf("grub-install %s: %v", grubDisk, err)
	}
	log.Infof("grub successfully written on [%s]", grubDisk)
}
