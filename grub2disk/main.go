//go:build linux

package main

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"

	log "github.com/sirupsen/logrus"
	"github.com/tinkerbell/actions/grub2disk/grub"
	"github.com/tinkerbell/actions/pkg/chroot"
	"github.com/tinkerbell/actions/pkg/metadata"
)

// grub2disk installs GRUB into the target root of a Tinkerbell-
// provisioned host. Two modes:
//   - MODE=bios (default): mounts the root filesystem, chroots,
//     runs grub-install <GRUB_DISK> and grub-mkconfig.
//   - MODE=efi: as above, plus mounts <EFI_PARTITION> at /boot/efi,
//     runs grub-install --target=x86_64-efi with <BOOTLOADER_ID>,
//     copies shim/grub into /EFI/BOOT/BOOTX64.EFI (firmware
//     fallback), and best-effort registers an NVRAM entry via
//     efibootmgr.
func main() {
	mode := envOr("MODE", "bios")
	blockDev := os.Getenv("GRUB_INSTALL_PATH")
	fsType := os.Getenv("FS_TYPE")
	if blockDev == "" {
		log.Fatal("GRUB_INSTALL_PATH is required")
	}

	log.Infof("grub2disk - MODE=%s", mode)

	if err := chroot.Enter(blockDev, fsType, fetchSiblings()); err != nil {
		log.Fatal(err)
	}

	switch mode {
	case "bios":
		doBIOS()
	case "efi":
		doEFI()
	default:
		log.Fatalf("unknown MODE=%q (want bios|efi)", mode)
	}
}

func doBIOS() {
	disk := os.Getenv("GRUB_DISK")
	if disk == "" {
		log.Fatal("GRUB_DISK is required for MODE=bios")
	}
	must("grub-install", disk)
	must("grub-mkconfig", "-o", "/boot/grub/grub.cfg")
	log.Infof("grub BIOS install complete on %s", disk)
}

func doEFI() {
	efiPart := os.Getenv("EFI_PARTITION")
	if efiPart == "" {
		log.Fatal("EFI_PARTITION is required for MODE=efi")
	}
	bootloaderID := envOr("BOOTLOADER_ID", "ubuntu")

	if err := os.MkdirAll("/boot/efi", 0o755); err != nil {
		log.Fatal(err)
	}
	if err := run("mount", efiPart, "/boot/efi"); err != nil {
		log.Fatalf("mount %s /boot/efi: %v", efiPart, err)
	}
	defer func() {
		if err := run("umount", "/boot/efi"); err != nil {
			log.Warnf("umount /boot/efi: %v", err)
		}
	}()

	args := append([]string{"grub-install"}, grub.BuildEFIInstallArgs(bootloaderID)...)
	must(args[0], args[1:]...)
	must("update-grub")

	// Firmware fallback: copy shim (preferred) or grub as BOOTX64.EFI.
	if err := os.MkdirAll("/boot/efi/EFI/BOOT", 0o755); err != nil {
		log.Fatal(err)
	}
	src := grub.FallbackSource(bootloaderID)
	if _, err := os.Stat(src); err != nil {
		src = grub.FallbackSourceGrub(bootloaderID)
	}
	if err := run("cp", src, "/boot/efi/EFI/BOOT/BOOTX64.EFI"); err != nil {
		log.Fatalf("cp fallback: %v", err)
	}
	// Also copy grubx64.efi into /EFI/BOOT so the shim can find it.
	_ = run("cp", grub.FallbackSourceGrub(bootloaderID), "/boot/efi/EFI/BOOT/grubx64.efi")

	// Best-effort NVRAM registration. efivarfs may not be writable from
	// the provisioning container; the firmware fallback above keeps the
	// disk bootable either way.
	if envOr("REGISTER_NVRAM", "true") == "true" {
		disk, part := parseEFIPartition(efiPart)
		if disk == "" {
			log.Warnf("can't parse EFI_PARTITION=%q into disk + part; skipping NVRAM", efiPart)
		} else {
			args := append([]string{"efibootmgr"}, grub.BuildEFIBootMgrArgs(disk, part, bootloaderID)...)
			if err := run(args[0], args[1:]...); err != nil {
				log.Warnf("efibootmgr (best-effort): %v", err)
			}
		}
	}

	log.Infof("grub EFI install complete on %s (bootloader-id=%s)", efiPart, bootloaderID)
}

// parseEFIPartition turns "/dev/sda1" into ("/dev/sda", 1).
// Returns "", 0 if the input doesn't end in digits (e.g. "/dev/nvme0n1p1"
// needs different parsing — we accept the simple sd[a-z][0-9]+ case).
func parseEFIPartition(p string) (disk string, part int) {
	i := len(p)
	for i > 0 && p[i-1] >= '0' && p[i-1] <= '9' {
		i--
	}
	if i == len(p) {
		return "", 0
	}
	n, err := strconv.Atoi(p[i:])
	if err != nil {
		return "", 0
	}
	disk = p[:i]
	// NVMe: /dev/nvme0n1p1 — trim the trailing 'p' if present.
	disk = strings.TrimSuffix(disk, "p")
	return disk, n
}

// fetchSiblings returns metadata.instance.storage.filesystems when
// MIRROR_HOST is set, so chroot.Enter can mount sibling LVs (/var,
// /home, …) before chrooting. Returns nil (no siblings) for legacy
// env-only flows that don't set MIRROR_HOST. Failures are logged and
// degraded — grub-install still works on a root-only mount tree.
func fetchSiblings() []metadata.Filesystem {
	if os.Getenv("MIRROR_HOST") == "" {
		return nil
	}
	c, err := metadata.New()
	if err != nil {
		log.Warnf("grub2disk: metadata client: %v (continuing without siblings)", err)
		return nil
	}
	md, err := c.Fetch(context.Background())
	if err != nil {
		log.Warnf("grub2disk: metadata fetch: %v (continuing without siblings)", err)
		return nil
	}
	return md.Instance.Storage.Filesystems
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func must(name string, args ...string) {
	if err := run(name, args...); err != nil {
		log.Fatalf("%s %v: %v", name, args, err)
	}
}

func run(name string, args ...string) error {
	c := exec.Command(name, args...)
	c.Stdout, c.Stderr = os.Stdout, os.Stderr
	return c.Run()
}
