// Package fstab renders /etc/fstab content from metadata filesystems.
package fstab

import (
	"fmt"
	"strings"

	log "github.com/sirupsen/logrus"
	"github.com/tinkerbell/actions/pkg/metadata"
)

// Render returns the complete /etc/fstab body (trailing newline) for
// the given filesystems. Filesystems without a label (no -L for
// ext*/xfs, no -n for vfat in their mkfs options) are skipped with a
// warning — we don't want a device-path-based fstab line silently
// taking over and breaking boot if device paths change.
func Render(fs []metadata.Filesystem) string {
	var b strings.Builder
	for _, f := range fs {
		label := labelFromOptions(f.Mount.Format, f.Mount.Create.Options)
		if label == "" {
			log.Warnf("fstab: no label for %s at %s; skipping", f.Mount.Device, f.Mount.Point)
			continue
		}
		opts := defaultOpts(f.Mount.Format)
		pass := 2
		if f.Mount.Point == "/" {
			pass = 1
		}
		fmt.Fprintf(&b, "LABEL=%s  %s  %s  %s  %d  %d\n",
			label, f.Mount.Point, f.Mount.Format, opts, 0, pass)
	}
	return b.String()
}

// labelFromOptions scans the mkfs options list for -L (ext/xfs) or
// -n (vfat) followed by the label value.
func labelFromOptions(format string, opts []string) string {
	flag := "-L"
	if format == "vfat" {
		flag = "-n"
	}
	for i := 0; i < len(opts)-1; i++ {
		if opts[i] == flag {
			return opts[i+1]
		}
	}
	return ""
}

// defaultOpts picks sensible mount options by filesystem type.
func defaultOpts(format string) string {
	switch format {
	case "vfat":
		return "defaults,nofail"
	default:
		return "defaults"
	}
}
