// Package grub builds argv lists and paths for grub-install and
// efibootmgr invocations in both BIOS and UEFI modes. The
// orchestration (mount ESP, run the commands, copy fallback) lives
// in grub2disk/main.go; this package is purely argv construction
// plus path constants so it's unit-testable without a chroot.
package grub

import "strconv"

// BuildEFIInstallArgs returns the grub-install flags for a UEFI
// install with the given bootloader ID. Callers prepend
// "grub-install" and exec.
func BuildEFIInstallArgs(bootloaderID string) []string {
	return []string{
		"--target=x86_64-efi",
		"--efi-directory=/boot/efi",
		"--bootloader-id=" + bootloaderID,
		"--recheck",
		"--no-nvram",
	}
}

// FallbackSource returns the preferred source path for copying into
// /boot/efi/EFI/BOOT/BOOTX64.EFI (the firmware fallback). Callers
// should cp this and fall back to FallbackSourceGrub if the shim
// isn't present.
func FallbackSource(bootloaderID string) string {
	return "/boot/efi/EFI/" + bootloaderID + "/shimx64.efi"
}

// FallbackSourceGrub is the non-shim fallback source path.
func FallbackSourceGrub(bootloaderID string) string {
	return "/boot/efi/EFI/" + bootloaderID + "/grubx64.efi"
}

// BuildEFIBootMgrArgs returns the efibootmgr flags for registering
// an NVRAM entry pointing at the shim/grub loader. disk is a block
// device path (e.g. /dev/sda); part is the ESP partition number.
func BuildEFIBootMgrArgs(disk string, part int, bootloaderID string) []string {
	return []string{
		"--create",
		"--disk", disk,
		"--part", strconv.Itoa(part),
		"--label", bootloaderID,
		"--loader", `\EFI\` + bootloaderID + `\shimx64.efi`,
	}
}
