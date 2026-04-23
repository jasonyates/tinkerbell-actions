package storage

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"syscall"

	log "github.com/sirupsen/logrus"
)

// RAID is aliased from pkg/metadata via types.go so the wire format
// stays a single source of truth; see storage/types.go.

// Accept both the numeric mdadm form (md0, /dev/md0) and the udev-style
// named form (/dev/md/<name>). The named form is what mdadm produces when
// --name is used and is common in Equinix/Packet CPR metadata.
var raidNameRegexp = regexp.MustCompile(`^(/dev/)?md([0-9]+|/[A-Za-z][A-Za-z0-9_.-]*)$`)

// validLevels maps supported RAID levels to the minimum number of data devices.
var validLevels = map[string]int{
	"0":      2,
	"1":      2,
	"4":      3,
	"5":      3,
	"6":      4,
	"10":     4,
	"linear": 2,
}

// ValidateRAID checks a RAID entry against basic requirements.
func ValidateRAID(r RAID) error {
	if r.Name == "" {
		return fmt.Errorf("raid: name is required")
	}
	if !raidNameRegexp.MatchString(r.Name) {
		return fmt.Errorf("raid: invalid name %q (expected mdX or /dev/mdX)", r.Name)
	}
	if r.Level == "" {
		return fmt.Errorf("raid: level is required")
	}
	minDevs, ok := validLevels[r.Level]
	if !ok {
		return fmt.Errorf("raid: unsupported level %q", r.Level)
	}
	if len(r.Devices) < minDevs {
		return fmt.Errorf("raid: level %s requires at least %d devices, got %d", r.Level, minDevs, len(r.Devices))
	}
	return nil
}

// normalizeRAIDDevice returns the full /dev/ path for a RAID array name.
func normalizeRAIDDevice(name string) string {
	if strings.HasPrefix(name, "/dev/") {
		return name
	}
	return "/dev/" + name
}

// BuildMdadmCreateArgs constructs the argument list for `mdadm --create`.
func BuildMdadmCreateArgs(r RAID) []string {
	args := []string{
		"--create", normalizeRAIDDevice(r.Name),
		"--metadata=1.2",
		"--level=" + r.Level,
		"--raid-devices=" + strconv.Itoa(len(r.Devices)),
	}
	if len(r.Spare) > 0 {
		args = append(args, "--spare-devices="+strconv.Itoa(len(r.Spare)))
	}
	args = append(args, "--run", "--force")
	args = append(args, r.Devices...)
	args = append(args, r.Spare...)
	return args
}

// CreateRAID validates and assembles the RAID array via mdadm.
func CreateRAID(r RAID) error {
	if err := ValidateRAID(r); err != nil {
		return err
	}
	dev := normalizeRAIDDevice(r.Name)
	log.Infof("Creating RAID%s array %s across %v", r.Level, dev, r.Devices)

	// Named form (/dev/md/<name>) needs /dev/md/ to exist so mdadm can
	// mknod the symlink. udev normally creates this; HookOS has no udev.
	if strings.HasPrefix(dev, "/dev/md/") {
		if err := os.MkdirAll("/dev/md", 0o755); err != nil {
			return fmt.Errorf("raid: could not create /dev/md: %w", err)
		}
	}

	args := BuildMdadmCreateArgs(r)
	return runMdadm(args...)
}

// StopRAID stops an active mdadm array if present. Non-fatal if not assembled.
func StopRAID(name string) error {
	dev := normalizeRAIDDevice(name)
	if _, err := os.Stat(dev); os.IsNotExist(err) {
		return nil
	}
	// Force-unmount any mountpoint backed by this device. HookOS (or
	// leftover state from a previous install) sometimes auto-assembles
	// and auto-mounts old arrays at boot, which makes mdadm --stop and
	// the subsequent partition wipe fail with EBUSY. Best-effort; a
	// no-op when the mount namespace doesn't include the host's mounts.
	forceUnmount(dev)

	log.Infof("Stopping RAID array %s", dev)
	return runMdadm("--stop", dev)
}

// forceUnmount reads /proc/mounts and lazy-force-unmounts every
// mountpoint backed by dev. Best-effort — errors are logged but
// never returned.
func forceUnmount(dev string) {
	data, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != dev {
			continue
		}
		mp := fields[1]
		log.Infof("Force-unmounting %s from %s before stopping array", dev, mp)
		if err := syscall.Unmount(mp, syscall.MNT_FORCE|syscall.MNT_DETACH); err != nil {
			log.Warnf("umount %s: %v (continuing)", mp, err)
		}
	}
}

// ZeroSuperblock clears any mdadm superblock from a member device. Ignores
// devices without a superblock.
func ZeroSuperblock(device string) error {
	if _, err := os.Stat(device); os.IsNotExist(err) {
		return nil
	}
	log.Infof("Zeroing RAID superblock on %s", device)
	cmd := exec.Command("/sbin/mdadm", "--zero-superblock", device)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	// Best-effort: mdadm returns non-zero if there is no superblock to zero.
	_ = cmd.Run()
	return nil
}

func runMdadm(args ...string) error {
	cmd := exec.Command("/sbin/mdadm", args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("mdadm %s: %w", strings.Join(args, " "), err)
	}
	return nil
}
