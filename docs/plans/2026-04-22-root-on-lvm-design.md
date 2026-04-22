# Root-on-LVM on top of RAID1 — design

Date: 2026-04-22
Status: Agreed, ready to implement

## Goal

Provision bare-metal hosts with the root filesystem (and /var, /home,
/var/lib/docker) living on LVM logical volumes stacked on top of a single
mdadm RAID1 mirror. Stream the Ubuntu Noble rootfs onto the root LV via
`archive2disk`, boot via GRUB-EFI from per-disk ESPs.

Feature parity on the rootio + Tinkerbell + template trio, following the
same pattern used to add RAID support recently.

## On-disk layout

```
Per disk (sda, sdb):
  p1: EFI (256 MiB, FAT32, per-disk, not in RAID)
  p2: LINUX (rest of disk) — paired into md0

RAID:
  md0 = sda2 + sdb2, level 1        ← the sole PV

VG vg0 on /dev/md0, LVs:
  root    40 GiB   ext4  →  /
  var     40 GiB   ext4  →  /var
  home    10 GiB   ext4  →  /home
  docker  fill     ext4  →  /var/lib/docker
```

Per-disk ESPs stay outside LVM — GRUB can't boot from an LV-only layout
without a non-LVM ESP. Each disk gets its own ESP so either disk can boot
if the other fails.

`/var/lib/docker` gets its own LV for churn isolation and snapshot
granularity.

One LV per VG is allowed `size=0`, which maps to `-l 100%FREE` in
`lvcreate` and must be last.

## Scope — three repos, landing order

1. **tinkerbell** — CR types + Hegel passthrough (can ship standalone)
2. **actions/rootio** — fix the LVM driver bug, add validation + preflight
3. **actions/scripts** — add lvm2 to the rootfs tarball
4. **actions/docs/templates** — new template + metadata shape

1 and 2 can land in parallel. 3 needs 2. 4 needs all three.

## 1. Tinkerbell changes

Mirrors the RAID passthrough commit (82e7fcab) but larger, because LVM
isn't modeled in the CR today.

### 1.1 `pkg/api/v1alpha1/tinkerbell/hardware.go`

Add two types and wire one field into `MetadataInstanceStorage`:

```go
type MetadataInstanceStorageVolumeGroup struct {
    Name            string                                  `json:"name,omitempty"`
    PhysicalVolumes []string                                `json:"physical_volumes,omitempty"`
    LogicalVolumes  []*MetadataInstanceStorageLogicalVolume `json:"logical_volumes,omitempty"`
    Tags            []string                                `json:"tags,omitempty"`
}

type MetadataInstanceStorageLogicalVolume struct {
    Name string   `json:"name,omitempty"`
    Size uint64   `json:"size,omitempty"`
    Tags []string `json:"tags,omitempty"`
    Opts []string `json:"opts,omitempty"`
}
```

Add `VolumeGroups []*MetadataInstanceStorageVolumeGroup \`json:"volume_groups,omitempty"\``
on `MetadataInstanceStorage`.

Field names and JSON tags match rootio's existing Go types exactly — no
translation layer, no schema drift risk.

### 1.2 Regenerate CRDs + deepcopy

`make generate` (confirm target from Makefile) regenerates
`zz_generated.deepcopy.go` and the CRD YAML under `crd/`.

### 1.3 `pkg/data/instance.go`

Extend the inline `HackInstance` struct under
`.Metadata.Instance.Storage` with a `VolumeGroups` block matching the CR
shape. Same inlining style the RAID commit used.

### 1.4 `pkg/backend/kube/tootles_test.go`

Add `TestToHackInstance_PassesThroughVolumeGroups`: build a `Hardware`
with `storage.volume_groups` populated, pipe through `toHackInstance`,
reparse the JSON, assert the VG + LV list round-tripped. Mirrors the
RAID passthrough test structure.

### 1.5 Release workflow

Add `feat/metadata-lvm-passthrough` (or whatever the branch ends up
being) to the trigger list in
`.github/workflows/release-ghcr-fork.yaml` so the fork image publishes
automatically.

### Out of scope

No EC2 subtree / tootles HTTP-handler changes — rootio hits `/metadata`
directly (served by `HackInstance`), not the EC2 tree.

## 2. rootio changes

Most of the plumbing is already in place:

- `rootio/storage/metadata.go` already defines `VolumeGroup` and
  `LogicalVolume` types and includes them in `Instance.Storage.VolumeGroups`.
- `rootio/lvm/lvm.go` is a full pvcreate/vgcreate/lvcreate driver.
- `rootio/storage/lvm.go` stitches the driver into a
  `CreateVolumeGroup(vg)` entry point.
- `rootio/cmd/rootio.go:138–146` already loops `VolumeGroups` in the
  `partition` command, **after** RAID creation.
- `lvm.static` is already shipped in the scratch image
  (`rootio/Dockerfile` lines 35–37, 57).

### 2.1 Bug fix: `/sbin/lvm` path

`rootio/lvm/lvm.go:25,34,45` (and similar) call `run("lvm", ...)`. The
scratch image has no PATH, so `exec.Command("lvm")` can't find
`/sbin/lvm`. Change all `run("lvm", ...)` to `run("/sbin/lvm", ...)`,
matching the `/sbin/mdadm` pattern in `raid.go`.

This is why the LVM path has never actually worked end-to-end — it was
wired in the command layer but would fail at first `pvcreate`.

### 2.2 `rootio/storage/lvm.go` — add `ValidateVolumeGroup`

Mirror `ValidateRAID`:

- `vgNameRegexp` / `lvNameRegexp` (reuse from `lvm/lvm.go`)
- `PhysicalVolumes` non-empty; each PV must be an absolute device path
- At most one LV with `Size == 0` per VG, and it must be last
- Tag validation via `lvm.ValidateTag`

Call `ValidateVolumeGroup` from `CreateVolumeGroup` before any LVM
commands run.

### 2.3 `rootio/cmd/rootio.go` — idempotent LVM preflight

Add to both `wipe` and `partition` commands, **before** the existing
`StopRAID` loop:

```
for each VG in metadata:
    /sbin/lvm vgchange -an <name>       (non-fatal)
    /sbin/lvm vgremove -ff <name>       (non-fatal)
for each PV across VGs:
    /sbin/lvm pvremove -ff <pv>         (non-fatal)
    wipefs -a <pv>
```

Must run before `StopRAID` — pvremove on a stopped array is a no-op and
leaves stale signatures on the member disks.

Implement as `storage.TeardownVolumeGroups(metadata)` to keep the cmd
layer thin.

### 2.4 Test fixture + tests

- New `rootio/test/lvm.json` — one disk → one md0 → one VG → LVs
- New `rootio/storage/lvm_test.go` covering `ValidateVolumeGroup` +
  JSON unmarshalling, mirroring `raid_test.go`

### 2.5 README

Add an "LVM on top of RAID" section with a worked example.

### Notes / caveats

- **Size units inconsistency:** `Partitions.Size` is sectors (512 B),
  `LogicalVolume.Size` is bytes. Already present in rootio; keep as-is
  and document in the README rather than change semantics.
- **`size=0` only valid on last LV:** enforced by `ValidateVolumeGroup`.

## 3. Rootfs tarball changes

### 3.1 `scripts/build-noble-rootfs-tarball.sh`

Add `lvm2` and `thin-provisioning-tools` to both package lists (EFI and
BIOS), alongside `mdadm`:

```
mdadm lvm2 thin-provisioning-tools grub-common grub-efi-amd64 ...
```

Installing `lvm2` in the chroot triggers initramfs-tools' lvm2 hook, so
a subsequent `update-initramfs -u -k all` bakes `lvm`, `dm_mod`, and the
activation scripts into the initrd. No separate apt step in the
template, no network in chroot.

`thin-provisioning-tools` silences an update-initramfs warning even
though we're not using thin provisioning — lvm2 recommends it.

### 3.2 Republish artifact

Rebuild and push `noble-rootfs-efi.tar.gz` (and the BIOS variant if in
use) to the mirror at `http://31.24.228.5:7173/`.

## 4. Template + metadata

### 4.1 New file: `docs/templates/ubuntu-noble-lvm-raid1-efi.yaml`

Based on `ubuntu-noble-raid1-efi.yaml`, with these changes:

| Action | Before | After |
|---|---|---|
| `extract-rootfs` | `DEST_DISK: /dev/md0` | `DEST_DISK: /dev/vg0/root` |
| `configure-network-dhcp` | `DEST_DISK: /dev/md0` | `DEST_DISK: /dev/vg0/root` |
| `configure-cloud-init` | `DEST_DISK: /dev/md0` | `DEST_DISK: /dev/vg0/root` |
| All `cexec` chroot steps | `BLOCK_DEVICE: /dev/md0` | `BLOCK_DEVICE: /dev/vg0/root` |

`/dev/md0` still exists — it's the PV — but nothing mounts it
directly. Device-mapper nodes persist across action containers since
they share the host kernel and `/dev` is volume-mounted, so
`/dev/vg0/root` is visible in every subsequent action after
`rootio-partition`.

### 4.2 New step: `configure-grub-lvm`

Inserted before `update-initramfs`:

```yaml
- name: "configure-grub-lvm"
  image: ghcr.io/jasonyates/tinkerbell-actions/cexec:latest
  timeout: 60
  environment:
    BLOCK_DEVICE: "/dev/vg0/root"
    FS_TYPE: "ext4"
    CHROOT: "y"
    DEFAULT_INTERPRETER: "/bin/bash -eux -c"
    CMD_LINE: "echo 'GRUB_PRELOAD_MODULES=\"lvm mdraid1x\"' >> /etc/default/grub"
```

Defensive. `grub-install` normally auto-detects LVM when root lives on
an LV, but explicit preloads guarantee the modules are embedded even if
auto-detection misses.

### 4.3 Leave the existing template in place

`ubuntu-noble-raid1-efi.yaml` stays untouched so the non-LVM path
remains available.

### 4.4 Expected metadata shape

```yaml
storage:
  disks:
    - device: /dev/sda
      wipe_table: true
      partitions:
        - {label: EFI,   number: 1, size: 524288}  # sectors
        - {label: LINUX, number: 2, size: 0}       # fill
    - device: /dev/sdb
      wipe_table: true
      partitions:
        - {label: EFI,   number: 1, size: 524288}
        - {label: LINUX, number: 2, size: 0}
  raid:
    - name: /dev/md0
      level: "1"
      devices: [/dev/sda2, /dev/sdb2]
  volume_groups:
    - name: vg0
      physical_volumes: [/dev/md0]
      logical_volumes:
        - {name: root,   size: 42949672960}  # 40 GiB, bytes
        - {name: var,    size: 42949672960}
        - {name: home,   size: 10737418240}
        - {name: docker, size: 0}             # fill
  filesystems:
    - mount: {device: /dev/vg0/root,   format: ext4, point: /,
              create: {options: [-L, ROOT]}}
    - mount: {device: /dev/vg0/var,    format: ext4, point: /var,
              create: {options: [-L, VAR]}}
    - mount: {device: /dev/vg0/home,   format: ext4, point: /home,
              create: {options: [-L, HOME]}}
    - mount: {device: /dev/vg0/docker, format: ext4, point: /var/lib/docker,
              create: {options: [-L, DOCKER]}}
    - mount: {device: /dev/sda1,       format: vfat, point: /boot/efi,
              create: {options: [-F, "32", -n, EFI_SDA]}}
    - mount: {device: /dev/sdb1,       format: vfat, point: /boot/efi2,
              create: {options: [-F, "32", -n, EFI_SDB]}}
```

Two ESP filesystem entries — sdb1 must be formatted too or
`grub-install-sdb` fails at `mount /dev/sdb1 /boot/efi` (lesson from the
RAID-only layout debug).

### 4.5 fstab

Generated by the `configure-fstab` step. LABEL-based so the device path
shuffles don't matter:

```
LABEL=ROOT    /                ext4  defaults           0 1
LABEL=VAR     /var             ext4  defaults           0 2
LABEL=HOME    /home            ext4  defaults           0 2
LABEL=DOCKER  /var/lib/docker  ext4  defaults           0 2
LABEL=EFI_SDA /boot/efi        vfat  defaults,nofail    0 2
LABEL=EFI_SDB /boot/efi2       vfat  defaults,nofail    0 2
```

LV labels come from rootio's `format` step via the `-L` mkfs option in
metadata.

## Boot path verification

At first boot the kernel+initramfs needs to:

1. Assemble `md0` from the two superblock-tagged partitions (mdadm
   initramfs hook, reading `/etc/mdadm/mdadm.conf` baked in).
2. Scan PVs, discover `vg0` on `md0`, activate it (lvm2 initramfs hook,
   present because `lvm2` is installed in the chroot).
3. Mount `LABEL=ROOT` as `/` (vg0/root, ext4).
4. Switch root, systemd takes over, mounts the rest from `/etc/fstab`.

GRUB needs the `lvm` and `mdraid1x` modules embedded — handled by
`configure-grub-lvm` + `grub-install`'s auto-detection.

## Testing strategy

- **rootio:** unit tests in `lvm_test.go` + fixture; run `partition`
  against a loop-device setup locally with `TEST=1
  JSON_FILE=rootio/test/lvm.json`.
- **tinkerbell:** `go test ./pkg/backend/kube/...` against the new
  passthrough test.
- **End-to-end:** provision one test host
  (`eu-lon1-control-001` or a spare) with the new template + metadata.
  Verify `cat /proc/mdstat`, `vgs`, `lvs`, `mount`, then reboot and
  verify it comes back up.

## Open risks

- **LVM activation ordering across action containers.** Device-mapper
  nodes should persist under `/dev`, but if the kernel drops the dm
  module between actions (unlikely, but worth verifying) we'd need an
  explicit `vgchange -ay` at the start of every cexec action that
  mounts an LV. Verify on first end-to-end run.
- **Alpine lvm2-static version pin.** The Dockerfile pins
  `lvm2-static=2.03.21-r3`; may need bumping as Alpine 3.18 ages.
- **`wipefs -a` availability in scratch.** `wipefs` is part of
  util-linux. If not already in the image, ship a busybox variant or
  accept that pvremove alone is enough.
