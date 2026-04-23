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

	if err := runCmd("grub-install", grubDisk); err != nil {
		log.Fatalf("grub-install %s: %v", grubDisk, err)
	}
	if err := runCmd("grub-mkconfig", "-o", "/boot/grub/grub.cfg"); err != nil {
		log.Fatalf("grub-mkconfig: %v", err)
	}
	log.Infof("grub successfully written on [%s]", grubDisk)
}

func runCmd(name string, args ...string) error {
	c := exec.Command(name, args...)
	c.Stdout, c.Stderr = os.Stdout, os.Stderr
	return c.Run()
}
