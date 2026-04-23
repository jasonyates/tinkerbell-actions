package grub

import (
	"reflect"
	"testing"
)

func TestBuildEFIInstallArgs(t *testing.T) {
	got := BuildEFIInstallArgs("ubuntu")
	want := []string{
		"--target=x86_64-efi",
		"--efi-directory=/boot/efi",
		"--bootloader-id=ubuntu",
		"--recheck",
		"--no-nvram",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}
}

func TestBuildEFIInstallArgs_custom(t *testing.T) {
	got := BuildEFIInstallArgs("ubuntu-backup")
	if got[2] != "--bootloader-id=ubuntu-backup" {
		t.Errorf("bootloader-id arg: got %q, want --bootloader-id=ubuntu-backup", got[2])
	}
}

func TestFallbackSource_shimPreferred(t *testing.T) {
	if got := FallbackSource("ubuntu"); got != "/boot/efi/EFI/ubuntu/shimx64.efi" {
		t.Errorf("got %q, want shim path", got)
	}
}

func TestFallbackSourceGrub_fallsBackToGrub(t *testing.T) {
	if got := FallbackSourceGrub("ubuntu"); got != "/boot/efi/EFI/ubuntu/grubx64.efi" {
		t.Errorf("got %q, want grub path", got)
	}
}

func TestBuildEFIBootMgrArgs(t *testing.T) {
	got := BuildEFIBootMgrArgs("/dev/sda", 1, "ubuntu")
	want := []string{
		"--create",
		"--disk", "/dev/sda",
		"--part", "1",
		"--label", "ubuntu",
		"--loader", `\EFI\ubuntu\shimx64.efi`,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}
}
