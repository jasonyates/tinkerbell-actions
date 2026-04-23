package chroot

import (
	"sort"
	"strings"

	"github.com/tinkerbell/actions/pkg/metadata"
)

// filterExtras applies MountExtras's filter + sort and returns the
// result. Skips the primary root ("/"), empty mount points, swap, and
// vfat. Sorts ascending by path depth so /var mounts before
// /var/lib/docker.
func filterExtras(extras []metadata.Filesystem) []metadata.Filesystem {
	out := make([]metadata.Filesystem, 0, len(extras))
	for _, f := range extras {
		p := f.Mount.Point
		if p == "" || p == "/" {
			continue
		}
		switch f.Mount.Format {
		case "swap", "vfat":
			continue
		}
		out = append(out, f)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return pathDepth(out[i].Mount.Point) < pathDepth(out[j].Mount.Point)
	})
	return out
}

func pathDepth(p string) int {
	return strings.Count(strings.Trim(p, "/"), "/")
}
