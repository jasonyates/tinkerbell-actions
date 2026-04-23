package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	log "github.com/sirupsen/logrus"
	"github.com/tinkerbell/actions/archive2disk/archive"
	"github.com/tinkerbell/actions/pkg/chroot"
	"github.com/tinkerbell/actions/pkg/metadata"
)

const mountAction = "/mountAction"

func main() {
	fmt.Printf("Archive2Disk - Archive streamer\n------------------------\n")
	blockDevice := os.Getenv("DEST_DISK")
	filesystemType := os.Getenv("FS_TYPE")
	path := os.Getenv("DEST_PATH")
	archiveURL := os.Getenv("ARCHIVE_URL")
	archiveType := os.Getenv("ARCHIVE_TYPE")
	httpClientTimeoutMinutesKey := "HTTP_CLIENT_TIMEOUT_MINUTES"
	httpClientTimoutMinutes := 5
	var err error
	if _, exists := os.LookupEnv(httpClientTimeoutMinutesKey); exists {
		httpClientTimoutMinutes, err = strconv.Atoi(os.Getenv(httpClientTimeoutMinutesKey))
		if err != nil {
			log.Fatalf("Parsing failed for environment variable [%s].  %v", httpClientTimeoutMinutesKey, err)
		}
	}
	checksumOverrideKey := "INSECURE_NO_TARFILE_CHECKSUM_VERIFICATION"
	checksumOverride := false
	if _, exists := os.LookupEnv(checksumOverrideKey); exists {
		checksumOverride, err = strconv.ParseBool(os.Getenv(checksumOverrideKey))
		if err != nil {
			log.Fatalf("Parsing failed for environment variable [%s].  %v", checksumOverrideKey, err)
		}
	}
	// checksum to validate tarfile, must be of the format
	// checksum name:checsum
	// ex: sha256:shasum sha512:shasum
	tarfileChecksum := ""
	if !checksumOverride {
		tarfileChecksum = os.Getenv("TARFILE_CHECKSUM")
		if tarfileChecksum == "" {
			log.Fatalf("No checksum specified with Environment Variable [TARFILE_CHECKSUM]")
		}
	}
	if blockDevice == "" {
		log.Fatalf("No Block Device speified with Environment Variable [DEST_DISK]")
	}

	// Force-unmount any pre-existing mount of this device before we
	// try to mount it ourselves. HookOS (or leftover state from a
	// previous install) occasionally auto-assembles + auto-mounts
	// md arrays and filesystems on boot, which makes our mount fail
	// with EBUSY. Best-effort — a no-op when there's nothing to
	// unmount, or when the mount namespace doesn't include the
	// offending mount.
	forceUnmount(blockDevice)

	// Mount primary + siblings. When MIRROR_HOST is set we fetch
	// metadata and mount every non-root, non-ESP filesystem (e.g.
	// /var, /home, /var/lib/docker) under /mountAction so the tarball
	// extract lands directly on the right LVs instead of populating
	// root-LV paths that later get shadowed by the sibling mounts at
	// boot. Without MIRROR_HOST (legacy flows) we mount only primary.
	extras := fetchSiblings()
	if err := chroot.MountTree(blockDevice, filesystemType, extras); err != nil {
		log.Fatalf("Mount tree: %v", err)
	}
	log.Infof("Mounted [%s] -> [%s] (+%d sibling filesystems)", blockDevice, mountAction, len(extras))

	// Write the image to disk
	err = archive.Write(archiveURL, archiveType, filepath.Join(mountAction, path), tarfileChecksum, httpClientTimoutMinutes)
	if err != nil {
		log.Fatal(err)
	}
	log.Infof("Successfully unpacked [%s] to [%s] on device [%s]", archiveURL, path, blockDevice)
}

// fetchSiblings returns metadata.instance.storage.filesystems when
// MIRROR_HOST is set. Logged-and-continued on error so legacy
// single-LV flows still work when Hegel isn't configured.
func fetchSiblings() []metadata.Filesystem {
	if os.Getenv("MIRROR_HOST") == "" {
		return nil
	}
	c, err := metadata.New()
	if err != nil {
		log.Warnf("metadata client: %v (continuing without siblings)", err)
		return nil
	}
	md, err := c.Fetch(context.Background())
	if err != nil {
		log.Warnf("metadata fetch: %v (continuing without siblings)", err)
		return nil
	}
	return md.Instance.Storage.Filesystems
}

// forceUnmount reads /proc/mounts and lazy-force-unmounts every
// mountpoint backed by dev. Best-effort; errors are logged but never
// fatal so a first-provision run (where no stale mount exists) still
// proceeds.
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
		log.Infof("Force-unmounting %s from %s before mount", dev, mp)
		if err := syscall.Unmount(mp, syscall.MNT_FORCE|syscall.MNT_DETACH); err != nil {
			log.Warnf("umount %s: %v (continuing)", mp, err)
		}
	}
}
