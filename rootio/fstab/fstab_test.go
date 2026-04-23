package fstab

import (
	"os"
	"strings"
	"testing"

	"github.com/tinkerbell/actions/pkg/metadata"
)

func TestRender_matchesGolden(t *testing.T) {
	fs := []metadata.Filesystem{
		mk("/dev/vg0/root", "ext4", "/", "-L", "ROOT"),
		mk("/dev/vg0/var", "ext4", "/var", "-L", "VAR"),
		mk("/dev/vg0/home", "ext4", "/home", "-L", "HOME"),
		mk("/dev/vg0/docker", "ext4", "/var/lib/docker", "-L", "DOCKER"),
		mk("/dev/sda1", "vfat", "/boot/efi", "-F", "32", "-n", "EFI_SDA"),
		mk("/dev/sdb1", "vfat", "/boot/efi2", "-F", "32", "-n", "EFI_SDB"),
	}
	want, err := os.ReadFile("testdata/full.golden")
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	got := Render(fs)
	if string(want) != got {
		t.Errorf("fstab mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestRender_skipsUnlabeled(t *testing.T) {
	fs := []metadata.Filesystem{mk("/dev/sdc1", "ext4", "/data")}
	got := Render(fs)
	if got != "" {
		t.Errorf("want empty, got %q", got)
	}
}

func TestRender_labelFromDashNForVfat(t *testing.T) {
	fs := []metadata.Filesystem{mk("/dev/sda1", "vfat", "/boot/efi", "-F", "32", "-n", "EFI_SDA")}
	got := Render(fs)
	if !strings.Contains(got, "LABEL=EFI_SDA") {
		t.Errorf("vfat -n flag should yield LABEL=EFI_SDA; got %q", got)
	}
}

func TestRender_rootGetsPassOne(t *testing.T) {
	fs := []metadata.Filesystem{mk("/dev/vg0/root", "ext4", "/", "-L", "ROOT")}
	got := Render(fs)
	// last column is fs_passno; expect 1 for /
	if !strings.HasSuffix(strings.TrimRight(got, "\n"), " 1") {
		t.Errorf("root should have fs_passno=1; got %q", got)
	}
}

func TestRender_otherPointsGetPassTwo(t *testing.T) {
	fs := []metadata.Filesystem{mk("/dev/vg0/var", "ext4", "/var", "-L", "VAR")}
	got := Render(fs)
	if !strings.HasSuffix(strings.TrimRight(got, "\n"), " 2") {
		t.Errorf("non-root should have fs_passno=2; got %q", got)
	}
}

func mk(device, format, point string, opts ...string) metadata.Filesystem {
	var f metadata.Filesystem
	f.Mount.Device = device
	f.Mount.Format = format
	f.Mount.Point = point
	f.Mount.Create.Options = opts
	return f
}
