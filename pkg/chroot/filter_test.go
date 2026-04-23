package chroot

import (
	"reflect"
	"testing"

	"github.com/tinkerbell/actions/pkg/metadata"
)

func fs(device, format, point string) metadata.Filesystem {
	f := metadata.Filesystem{}
	f.Mount.Device = device
	f.Mount.Format = format
	f.Mount.Point = point
	return f
}

func TestFilterExtras(t *testing.T) {
	root := fs("/dev/vg0/root", "ext4", "/")
	varLv := fs("/dev/vg0/var", "ext4", "/var")
	home := fs("/dev/vg0/home", "ext4", "/home")
	docker := fs("/dev/vg0/docker", "ext4", "/var/lib/docker")
	esp := fs("/dev/sda1", "vfat", "/boot/efi")
	esp2 := fs("/dev/sdb1", "vfat", "/boot/efi2")
	swap := fs("/dev/vg0/swap", "swap", "")
	unlabeled := fs("/dev/vg0/x", "ext4", "")

	got := filterExtras([]metadata.Filesystem{
		root, docker, esp, varLv, esp2, home, swap, unlabeled,
	})

	// Root/ESP/swap/empty filtered out; siblings ordered by depth.
	want := []metadata.Filesystem{varLv, home, docker}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("filter+sort mismatch\n got=%+v\nwant=%+v", got, want)
	}
}

func TestFilterExtrasEmpty(t *testing.T) {
	if got := filterExtras(nil); len(got) != 0 {
		t.Fatalf("nil input: got %v, want empty", got)
	}
	if got := filterExtras([]metadata.Filesystem{fs("/dev/vg0/root", "ext4", "/")}); len(got) != 0 {
		t.Fatalf("only root: got %v, want empty", got)
	}
}
